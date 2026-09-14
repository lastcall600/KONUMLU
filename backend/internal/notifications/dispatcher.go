package notifications

import (
	"context"
	"errors"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/notifications/policy"
	"backend/internal/platform/observability"
)

type Dispatcher struct {
	store    *PostgresStore
	material *Materializer
	contacts identitycontracts.NotificationContactResolver
	email    ChannelSender
	sms      ChannelSender
	push     *PushDispatch
	cfg      DispatcherConfig
	now      func() time.Time
	wait     func(ctx context.Context, d time.Duration) error
}

func NewDispatcher(store *PostgresStore, material *Materializer, contacts identitycontracts.NotificationContactResolver, email, sms ChannelSender, push *PushDispatch, cfg DispatcherConfig, now func() time.Time) (*Dispatcher, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Dispatcher{
		store:    store,
		material: material,
		contacts: contacts,
		email:    email,
		sms:      sms,
		push:     push,
		cfg:      cfg.normalized(),
		now:      now,
		wait: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		},
	}, nil
}

func (d *Dispatcher) configuredChannels() []policy.Channel {
	out := make([]policy.Channel, 0, 2)
	if d.email != nil {
		out = append(out, policy.ChannelEmail)
	}
	if d.sms != nil {
		out = append(out, policy.ChannelSMS)
	}
	if d.push.webConfigured() {
		out = append(out, policy.ChannelWebPush)
	}
	if d.push.mobileConfigured() {
		out = append(out, policy.ChannelMobilePush)
	}
	return out
}

func (d *Dispatcher) Run(ctx context.Context) error {
	if d == nil || d.store == nil {
		return errStoreRequired
	}
	loggedSkip := false
	for {
		if ctx.Err() != nil {
			return nil
		}
		channels := d.configuredChannels()
		if len(channels) == 0 {
			if !loggedSkip {
				observability.FromContext(ctx).Info("notification_dispatch_unconfigured",
					"error_class", ErrorClassUnconfigured,
					"delivery_outcome", string(policy.DeliveryPending),
				)
				loggedSkip = true
			}
			if err := d.wait(ctx, d.cfg.PollInterval); err != nil {
				return nil
			}
			continue
		}
		n, err := d.ProcessBatch(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			observability.FromContext(ctx).Info("notification_dispatch_batch_failed",
				"error_class", ErrorClassRetryable,
			)
		}
		if n == 0 {
			if err := d.wait(ctx, d.cfg.PollInterval); err != nil {
				return nil
			}
		}
	}
}

func (d *Dispatcher) ProcessBatch(ctx context.Context) (int, error) {
	channels := d.configuredChannels()
	if len(channels) == 0 {
		return 0, nil
	}
	now := d.now().UTC()
	rows, err := d.store.ClaimChannelDeliveries(ctx, now, d.cfg.ProcessingHold, d.cfg.BatchSize, channels)
	if err != nil {
		return 0, err
	}
	for i := range rows {
		observability.FromContext(ctx).Info("notification_delivery_claimed",
			"channel_code", string(rows[i].Channel),
			"delivery_attempt", rows[i].Attempts,
			"intent_id", rows[i].IntentID.String(),
		)
		if err := d.processOne(ctx, rows[i]); err != nil && !errors.Is(err, context.Canceled) {
			observability.FromContext(ctx).Info("notification_delivery_process_failed",
				"error_class", ErrorClassRetryable,
				"intent_id", rows[i].IntentID.String(),
			)
		}
	}
	return len(rows), nil
}

func (d *Dispatcher) processOne(ctx context.Context, row ChannelDeliveryRow) error {
	now := d.now().UTC()
	intent, err := d.store.GetIntent(ctx, row.IntentID)
	if err != nil {
		return d.failRetryable(ctx, row, ErrorClassRetryable, now)
	}
	if reason, suppress := d.recheck(ctx, intent, row.Channel); suppress {
		row.State = policy.DeliverySuppressed
		row.SuppressionReason = &reason
		row.UpdatedAt = now
		row.CompletedAt = &now
		row.NextAttemptAt = nil
		observability.FromContext(ctx).Info("notification_delivery_suppressed",
			"channel_code", string(row.Channel),
			"suppression_reason", string(reason),
			"intent_id", intent.ID.String(),
		)
		return d.store.FinishChannelDelivery(ctx, row)
	}
	sender := d.sender(row.Channel)
	if sender == nil && !isPushChannel(row.Channel) {
		return d.parkUnconfigured(ctx, row, now)
	}
	if isPushChannel(row.Channel) {
		return d.processPush(ctx, row, intent, now)
	}
	dest, destErr := d.resolveDestination(ctx, intent.RecipientUserID, row.Channel)
	if destErr != nil {
		if errors.Is(destErr, identitycontracts.ErrNotFound) {
			reason := policy.SuppressNoDestination
			row.State = policy.DeliverySuppressed
			row.SuppressionReason = &reason
			row.UpdatedAt = now
			row.CompletedAt = &now
			return d.store.FinishChannelDelivery(ctx, row)
		}
		return d.failRetryable(ctx, row, ErrorClassRetryable, now)
	}
	req := ChannelSendRequest{
		Channel:        row.Channel,
		Destination:    dest,
		TemplateKey:    intent.TemplateKey,
		Locale:         "tr",
		Variables:      intent.Variables,
		IdempotencyKey: row.ID.String(),
	}
	if intent.LocaleHint != nil && *intent.LocaleHint != "" {
		req.Locale = *intent.LocaleHint
	}
	_ = RenderPlain(req.TemplateKey, req.Locale, req.Variables)
	res, sendErr := sender.Send(ctx, req)
	if sendErr != nil {
		class, retryable, _ := ClassifyProviderError(sendErr)
		if !retryable {
			row.State = policy.DeliveryPermanentlyFailed
			row.LastErrorClass = strptr(class)
			row.UpdatedAt = now
			row.CompletedAt = &now
			row.NextAttemptAt = nil
			observability.FromContext(ctx).Info("notification_delivery_permanent_failure",
				"channel_code", string(row.Channel),
				"error_class", class,
				"intent_id", intent.ID.String(),
			)
			return d.store.FinishChannelDelivery(ctx, row)
		}
		delay, giveUp := policy.BoundedBackoff(row.Attempts)
		if giveUp {
			row.State = policy.DeliveryPermanentlyFailed
			row.LastErrorClass = strptr(class)
			row.UpdatedAt = now
			row.CompletedAt = &now
			row.NextAttemptAt = nil
			return d.store.FinishChannelDelivery(ctx, row)
		}
		next := now.Add(time.Duration(delay) * time.Second)
		row.State = policy.DeliveryRetryableFailed
		row.LastErrorClass = strptr(class)
		row.NextAttemptAt = &next
		row.UpdatedAt = now
		observability.FromContext(ctx).Info("notification_delivery_retry_scheduled",
			"channel_code", string(row.Channel),
			"error_class", class,
			"delivery_attempt", row.Attempts,
			"intent_id", intent.ID.String(),
		)
		return d.store.FinishChannelDelivery(ctx, row)
	}
	row.State = policy.DeliveryAccepted
	if res.ProviderRef != "" {
		ref := res.ProviderRef
		row.ProviderRef = &ref
	}
	row.UpdatedAt = now
	row.CompletedAt = &now
	row.NextAttemptAt = nil
	observability.FromContext(ctx).Info("notification_delivery_accepted",
		"channel_code", string(row.Channel),
		"intent_id", intent.ID.String(),
		"delivery_outcome", string(policy.DeliveryAccepted),
	)
	return d.store.FinishChannelDelivery(ctx, row)
}

func (d *Dispatcher) recheck(ctx context.Context, intent SemanticIntent, ch policy.Channel) (policy.SuppressionReason, bool) {
	if d.material == nil {
		return "", false
	}
	prefs, prefErr := d.store.ListPreferenceSettings(ctx, intent.RecipientUserID)
	consents, consErr := d.store.ListCurrentConsents(ctx, intent.RecipientUserID)
	account := policy.AccountActive
	dest := policy.DestinationState{}
	if d.material.destinations != nil {
		var uid identitycontracts.ID
		copy(uid[:], intent.RecipientUserID[:])
		elig, err := d.material.destinations.ReadNotificationEligibility(ctx, uid)
		if err == nil {
			dest.EmailVerified = elig.EmailVerified
			dest.PhoneVerified = elig.PhoneVerified
			if elig.Deleted {
				account = policy.AccountDeleted
			} else if elig.Disabled {
				account = policy.AccountDisabled
			}
		}
	}
	if ok, err := d.store.HasActivePushEndpoint(ctx, intent.RecipientUserID, policy.ChannelWebPush); err == nil {
		dest.WebPushReady = ok
	}
	if ok, err := d.store.HasActivePushEndpoint(ctx, intent.RecipientUserID, policy.ChannelMobilePush); err == nil {
		dest.MobilePushReady = ok
	}
	resolution := policy.Resolve(policy.IntentInput{
		RecipientUserID:        intent.RecipientUserID.String(),
		EventType:              intent.EventType,
		DomainRef:              intent.DomainRef,
		Account:                account,
		Destinations:           dest,
		Preferences:            policy.PreferenceDocument{Settings: prefs},
		Consents:               consents,
		PreferenceLookupFailed: prefErr != nil,
		ConsentLookupFailed:    consErr != nil,
	})
	for _, decision := range resolution.Decisions {
		if decision.Channel != ch {
			continue
		}
		if !decision.Eligible {
			return decision.Suppressed, true
		}
		return "", false
	}
	return policy.SuppressPolicy, true
}

func (d *Dispatcher) resolveDestination(ctx context.Context, userID ID, ch policy.Channel) (string, error) {
	if d.contacts == nil {
		return "", identitycontracts.ErrNotFound
	}
	kind, ok := contactKind(ch)
	if !ok {
		return "", identitycontracts.ErrNotFound
	}
	var uid identitycontracts.ID
	copy(uid[:], userID[:])
	got, err := d.contacts.ResolveVerifiedContact(ctx, uid, kind)
	if err != nil {
		return "", err
	}
	return got.Value, nil
}

func (d *Dispatcher) sender(ch policy.Channel) ChannelSender {
	switch ch {
	case policy.ChannelEmail:
		return d.email
	case policy.ChannelSMS:
		return d.sms
	default:
		return nil
	}
}

func isPushChannel(ch policy.Channel) bool {
	return ch == policy.ChannelWebPush || ch == policy.ChannelMobilePush
}

func (d *Dispatcher) processPush(ctx context.Context, row ChannelDeliveryRow, intent SemanticIntent, now time.Time) error {
	if d.push == nil || d.push.Endpoints == nil {
		return d.parkUnconfigured(ctx, row, now)
	}
	if row.Channel == policy.ChannelWebPush && !d.push.webConfigured() {
		return d.parkUnconfigured(ctx, row, now)
	}
	if row.Channel == policy.ChannelMobilePush && !d.push.mobileConfigured() {
		return d.parkUnconfigured(ctx, row, now)
	}
	endpoints, err := d.push.Endpoints.ListDispatchRows(ctx, intent.RecipientUserID, row.Channel)
	if err != nil {
		return d.failRetryable(ctx, row, ErrorClassRetryable, now)
	}
	if len(endpoints) == 0 {
		reason := policy.SuppressNoDestination
		row.State = policy.DeliverySuppressed
		row.SuppressionReason = &reason
		row.UpdatedAt = now
		row.CompletedAt = &now
		row.NextAttemptAt = nil
		return d.store.FinishChannelDelivery(ctx, row)
	}

	payload := SafePushPayload(intent.TemplateKey, intent.DomainRef, string(intent.Purpose))
	agg := pushFanout{}
	for i := range endpoints {
		ep := endpoints[i]
		sender := d.push.sender(ep.Provider)
		if sender == nil {
			agg.unconfigured++
			continue
		}
		agg.configured++
		mat, openErr := d.push.Endpoints.OpenDispatchMaterial(ep)
		if openErr != nil {
			agg.noteRetryable(ErrorClassRetryable)
			continue
		}
		req := PushSendRequest{
			EndpointID:     ep.ID,
			Channel:        ep.Channel,
			Platform:       ep.Platform,
			Provider:       ep.Provider,
			Material:       mat,
			Payload:        payload,
			IdempotencyKey: row.ID.String() + ":" + ep.ID.String(),
		}
		res, sendErr := sender.Send(ctx, req)
		req.Material = PushProviderMaterial{}
		if sendErr == nil {
			agg.accepted++
			if res.ProviderRef != "" {
				agg.lastRef = res.ProviderRef
			}
			continue
		}
		if errors.Is(sendErr, errProviderEndpointInvalid) {
			if _, revErr := d.push.Endpoints.Revoke(ctx, intent.RecipientUserID, ep.ID); revErr != nil {
				agg.noteRetryable(ErrorClassRetryable)
				continue
			}
			agg.invalid++
			continue
		}
		class, retryable, _ := ClassifyProviderError(sendErr)
		if retryable {
			agg.noteRetryable(class)
			continue
		}
		agg.notePermanent(class)
	}

	state, class, reason := agg.decide()
	row.UpdatedAt = now
	switch state {
	case policy.DeliveryAccepted:
		row.State = policy.DeliveryAccepted
		row.CompletedAt = &now
		row.NextAttemptAt = nil
		if agg.lastRef != "" {
			ref := agg.lastRef
			row.ProviderRef = &ref
		}
		observability.FromContext(ctx).Info("notification_delivery_accepted",
			"channel_code", string(row.Channel),
			"intent_id", intent.ID.String(),
			"delivery_outcome", string(policy.DeliveryAccepted),
		)
		return d.store.FinishChannelDelivery(ctx, row)
	case policy.DeliveryPending:
		return d.parkUnconfigured(ctx, row, now)
	case policy.DeliverySuppressed:
		row.State = policy.DeliverySuppressed
		row.SuppressionReason = &reason
		row.CompletedAt = &now
		row.NextAttemptAt = nil
		return d.store.FinishChannelDelivery(ctx, row)
	case policy.DeliveryPermanentlyFailed:
		row.State = policy.DeliveryPermanentlyFailed
		row.LastErrorClass = strptr(class)
		row.CompletedAt = &now
		row.NextAttemptAt = nil
		observability.FromContext(ctx).Info("notification_delivery_permanent_failure",
			"channel_code", string(row.Channel),
			"error_class", class,
			"intent_id", intent.ID.String(),
		)
		return d.store.FinishChannelDelivery(ctx, row)
	default:
		return d.failRetryable(ctx, row, class, now)
	}
}

func (d *Dispatcher) failRetryable(ctx context.Context, row ChannelDeliveryRow, class string, now time.Time) error {
	delay, giveUp := policy.BoundedBackoff(row.Attempts)
	if giveUp {
		row.State = policy.DeliveryPermanentlyFailed
		row.LastErrorClass = strptr(class)
		row.UpdatedAt = now
		row.CompletedAt = &now
		row.NextAttemptAt = nil
		return d.store.FinishChannelDelivery(ctx, row)
	}
	next := now.Add(time.Duration(delay) * time.Second)
	row.State = policy.DeliveryRetryableFailed
	row.LastErrorClass = strptr(class)
	row.NextAttemptAt = &next
	row.UpdatedAt = now
	return d.store.FinishChannelDelivery(ctx, row)
}

func (d *Dispatcher) parkUnconfigured(ctx context.Context, row ChannelDeliveryRow, now time.Time) error {
	next := now.Add(time.Hour)
	row.State = policy.DeliveryPending
	row.LastErrorClass = strptr(ErrorClassUnconfigured)
	row.NextAttemptAt = &next
	row.UpdatedAt = now
	observability.FromContext(ctx).Info("notification_dispatch_unconfigured",
		"channel_code", string(row.Channel),
		"error_class", ErrorClassUnconfigured,
		"intent_id", row.IntentID.String(),
	)
	return d.store.FinishChannelDelivery(ctx, row)
}

func strptr(s string) *string { return &s }
