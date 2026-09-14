package notifications

import (
	"context"
	"errors"
	"strings"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/notifications/policy"
	"backend/internal/platform/db"
	"backend/internal/platform/observability"
)

type MaterializeInput struct {
	RecipientUserID ID
	EventType       policy.EventType
	DomainRef       string
	ActorRef        string
	ResourceRef     string
	LocaleHint      string
	Variables       map[string]string
	CreatedAt       time.Time
}

type MaterializeResult struct {
	Intent     SemanticIntent
	Created    bool
	Unknown    bool
	Suppressed bool
}

type Materializer struct {
	store        *PostgresStore
	destinations identitycontracts.NotificationEligibilityReader
	now          func() time.Time
}

func NewMaterializer(store *PostgresStore, destinations identitycontracts.NotificationEligibilityReader, now func() time.Time) (*Materializer, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Materializer{store: store, destinations: destinations, now: now}, nil
}

func (m *Materializer) Materialize(ctx context.Context, in MaterializeInput) (MaterializeResult, error) {
	if m == nil || m.store == nil || m.store.db == nil {
		return MaterializeResult{}, errStoreRequired
	}
	if in.RecipientUserID.IsZero() {
		return MaterializeResult{}, policy.ErrInvalidActor
	}
	spec, ok := policy.LookupEvent(in.EventType)
	if !ok {
		observability.FromContext(ctx).Info("notification_unknown_event",
			"notification_event", string(in.EventType),
			"suppression_reason", string(policy.SuppressUnknownEvent),
		)
		return MaterializeResult{Unknown: true}, nil
	}
	vars, err := policy.FilterVariables(spec, in.Variables)
	if err != nil {
		return MaterializeResult{}, err
	}
	now := in.CreatedAt
	if now.IsZero() {
		now = m.now()
	} else {
		now = now.UTC()
	}

	prefs, prefErr := m.store.ListPreferenceSettings(ctx, in.RecipientUserID)
	consents, consErr := m.store.ListCurrentConsents(ctx, in.RecipientUserID)
	account := policy.AccountActive
	dest := policy.DestinationState{}
	if m.destinations != nil {
		var uid identitycontracts.ID
		copy(uid[:], in.RecipientUserID[:])
		elig, err := m.destinations.ReadNotificationEligibility(ctx, uid)
		if err != nil && !errors.Is(err, identitycontracts.ErrNotFound) {
			return MaterializeResult{}, errUnavailable
		}
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
	if webReady, err := m.store.HasActivePushEndpoint(ctx, in.RecipientUserID, policy.ChannelWebPush); err == nil {
		dest.WebPushReady = webReady
	}
	if mobileReady, err := m.store.HasActivePushEndpoint(ctx, in.RecipientUserID, policy.ChannelMobilePush); err == nil {
		dest.MobilePushReady = mobileReady
	}

	resolution := policy.Resolve(policy.IntentInput{
		RecipientUserID:        in.RecipientUserID.String(),
		EventType:              in.EventType,
		DomainRef:              strings.TrimSpace(in.DomainRef),
		ActorRef:               in.ActorRef,
		ResourceRef:            in.ResourceRef,
		LocaleHint:             in.LocaleHint,
		Variables:              vars,
		CreatedAt:              now,
		Account:                account,
		Destinations:           dest,
		Preferences:            policy.PreferenceDocument{Settings: prefs},
		Consents:               consents,
		PreferenceLookupFailed: prefErr != nil,
		ConsentLookupFailed:    consErr != nil,
	})
	if resolution.UnknownEvent {
		return MaterializeResult{Unknown: true}, nil
	}

	intentID, err := NewID()
	if err != nil {
		return MaterializeResult{}, errUnavailable
	}
	locale := nullableString(in.LocaleHint)
	intent := SemanticIntent{
		ID:              intentID,
		RecipientUserID: in.RecipientUserID,
		EventType:       spec.Type,
		Purpose:         spec.Purpose,
		CatalogVersion:  policy.CatalogVersion,
		TemplateKey:     spec.TemplateKey,
		Urgency:         spec.DefaultUrgency,
		LocaleHint:      locale,
		DomainRef:       strings.TrimSpace(in.DomainRef),
		ActorRef:        nullableString(in.ActorRef),
		ResourceRef:     nullableString(in.ResourceRef),
		DedupeKey:       resolution.DedupeKey,
		Variables:       vars,
		CreatedAt:       now,
	}
	if intent.DomainRef == "" {
		intent.DomainRef = resolution.DedupeKey
	}

	tx, err := m.store.db.Begin(ctx)
	if err != nil {
		return MaterializeResult{}, mapPolicyDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	txCtx := db.WithTx(ctx, tx)

	stored, created, err := m.store.insertIntentTx(txCtx, m.store.db, intent)
	if err != nil {
		return MaterializeResult{}, err
	}
	if !created {
		if err := tx.Commit(ctx); err != nil {
			return MaterializeResult{}, mapPolicyDBErr(err)
		}
		observability.FromContext(ctx).Info("notification_intent_deduped",
			"notification_event", string(spec.Type),
			"notify_purpose", string(spec.Purpose),
			"intent_id", stored.ID.String(),
			"user_id", in.RecipientUserID.String(),
		)
		return MaterializeResult{Intent: stored, Created: false}, nil
	}

	for _, d := range resolution.Decisions {
		rowID, err := NewID()
		if err != nil {
			return MaterializeResult{}, errUnavailable
		}
		chRow := ChannelDeliveryRow{
			ID:        rowID,
			IntentID:  stored.ID,
			Channel:   d.Channel,
			Attempts:  0,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if d.Eligible {
			switch d.Channel {
			case policy.ChannelInApp:
				inboxID, err := NewID()
				if err != nil {
					return MaterializeResult{}, errUnavailable
				}
				if _, err := m.store.insertInboxTx(txCtx, m.store.db, InboxRow{
					ID: inboxID, IntentID: stored.ID, UserID: in.RecipientUserID, CreatedAt: now,
				}); err != nil {
					return MaterializeResult{}, err
				}
				chRow.State = policy.DeliveryAccepted
			case policy.ChannelEmail, policy.ChannelSMS:
				chRow.State = policy.DeliveryPending
				next := now
				chRow.NextAttemptAt = &next
			case policy.ChannelWebPush, policy.ChannelMobilePush:
				chRow.State = policy.DeliveryPending
				next := now
				chRow.NextAttemptAt = &next
			default:
				chRow.State = policy.DeliverySuppressed
				reason := policy.SuppressPolicy
				chRow.SuppressionReason = &reason
			}
		} else {
			chRow.State = policy.DeliverySuppressed
			reason := d.Suppressed
			chRow.SuppressionReason = &reason
		}
		if _, err := m.store.insertChannelDeliveryTx(txCtx, m.store.db, chRow); err != nil {
			return MaterializeResult{}, err
		}
		observability.FromContext(ctx).Info("notification_channel_planned",
			"notification_event", string(spec.Type),
			"notify_purpose", string(spec.Purpose),
			"channel_code", string(d.Channel),
			"suppression_reason", string(d.Suppressed),
			"intent_id", stored.ID.String(),
			"delivery_outcome", string(chRow.State),
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return MaterializeResult{}, mapPolicyDBErr(err)
	}
	return MaterializeResult{Intent: stored, Created: true}, nil
}
