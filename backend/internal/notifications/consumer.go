package notifications

import (
	"context"
	"strings"
	"time"

	"backend/internal/notifications/policy"
)

type ConsumerService struct {
	store     *PostgresStore
	endpoints *EndpointService
	now       func() time.Time
}

func NewConsumerService(store *PostgresStore, now func() time.Time) (*ConsumerService, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ConsumerService{store: store, now: now}, nil
}

func (s *ConsumerService) WithEndpoints(e *EndpointService) *ConsumerService {
	if s != nil {
		s.endpoints = e
	}
	return s
}

func (s *ConsumerService) GetPreferences(ctx context.Context, userID ID) ([]policy.EffectivePreference, error) {
	if userID.IsZero() {
		return nil, policy.ErrInvalidActor
	}
	settings, err := s.store.ListPreferenceSettings(ctx, userID)
	if err != nil {
		return nil, err
	}
	return policy.EffectivePreferenceRows(policy.PreferenceDocument{Settings: settings}), nil
}

func (s *ConsumerService) PatchPreferences(ctx context.Context, userID ID, patch policy.PreferencePatch) ([]policy.EffectivePreference, error) {
	if userID.IsZero() {
		return nil, policy.ErrInvalidActor
	}
	if err := policy.ValidatePreferencePatch(patch); err != nil {
		return nil, err
	}
	now := s.now()
	deduped := map[string]policy.PreferenceOverride{}
	order := make([]string, 0, len(patch.Overrides))
	for _, o := range patch.Overrides {
		key := string(o.Channel) + "|" + string(o.ScopeType) + "|" + strings.TrimSpace(o.ScopeKey)
		if _, ok := deduped[key]; !ok {
			order = append(order, key)
		}
		deduped[key] = o
	}
	for _, key := range order {
		o := deduped[key]
		if err := s.store.UpsertPreferenceSetting(ctx, userID, policy.PreferenceSetting{
			Channel: o.Channel, ScopeType: o.ScopeType, ScopeKey: strings.TrimSpace(o.ScopeKey), Enabled: o.Enabled,
		}, now); err != nil {
			return nil, err
		}
	}
	return s.GetPreferences(ctx, userID)
}

type ConsentView struct {
	Type          policy.ConsentType
	Decision      *policy.ConsentStatus
	PolicyVersion string
	RecordedAt    *time.Time
	Source        policy.ConsentSource
}

func (s *ConsumerService) GetConsents(ctx context.Context, userID ID) ([]ConsentView, error) {
	if userID.IsZero() {
		return nil, policy.ErrInvalidActor
	}
	current, err := s.store.ListCurrentConsents(ctx, userID)
	if err != nil {
		return nil, err
	}
	byType := map[policy.ConsentType]policy.ConsentSnapshot{}
	for _, c := range current {
		byType[c.Type] = c
	}
	types := []policy.ConsentType{policy.ConsentCommercialEmail, policy.ConsentCommercialSMS, policy.ConsentCommercialPush}
	out := make([]ConsentView, 0, len(types))
	for _, typ := range types {
		v := ConsentView{Type: typ, PolicyVersion: policy.PolicyDocumentVersion}
		if c, ok := byType[typ]; ok {
			st := c.Status
			v.Decision = &st
			t := c.CapturedAt
			v.RecordedAt = &t
			v.Source = c.Source
			v.PolicyVersion = c.PolicyVersion
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *ConsumerService) RecordConsent(ctx context.Context, userID ID, typ policy.ConsentType, grant bool) (policy.ConsentSnapshot, error) {
	if userID.IsZero() {
		return policy.ConsentSnapshot{}, policy.ErrInvalidActor
	}
	snap, err := policy.ValidateConsentDecision(policy.ConsentDecision{
		Type:   typ,
		Grant:  grant,
		Source: policy.ConsentSourceSettingsWeb,
	}, s.now())
	if err != nil {
		return policy.ConsentSnapshot{}, err
	}
	return s.store.InsertConsentDecision(ctx, userID, snap)
}

type InboxPage struct {
	Items      []InboxRow
	NextCursor string
}

func (s *ConsumerService) ListInbox(ctx context.Context, userID ID, cursorRaw string, limit int) (InboxPage, error) {
	if userID.IsZero() {
		return InboxPage{}, policy.ErrInvalidActor
	}
	limit, err := normalizeInboxLimit(limit)
	if err != nil {
		return InboxPage{}, err
	}
	var cursor *inboxCursor
	if strings.TrimSpace(cursorRaw) != "" {
		cur, err := decodeInboxCursor(cursorRaw)
		if err != nil {
			return InboxPage{}, err
		}
		cursor = &cur
	}
	rows, err := s.store.ListInbox(ctx, userID, cursor, limit+1)
	if err != nil {
		return InboxPage{}, err
	}
	page := InboxPage{Items: rows}
	if len(rows) > limit {
		page.Items = rows[:limit]
		next, err := encodeInboxCursor(page.Items[len(page.Items)-1])
		if err != nil {
			return InboxPage{}, err
		}
		page.NextCursor = next
	}
	return page, nil
}

func (s *ConsumerService) MarkRead(ctx context.Context, userID, inboxID ID) (InboxRow, error) {
	if userID.IsZero() || inboxID.IsZero() {
		return InboxRow{}, errNotFound
	}
	return s.store.MarkInboxRead(ctx, userID, inboxID, s.now())
}

func (s *ConsumerService) MarkAllRead(ctx context.Context, userID ID) error {
	if userID.IsZero() {
		return policy.ErrInvalidActor
	}
	_, err := s.store.MarkAllInboxRead(ctx, userID, s.now())
	return err
}

func (s *ConsumerService) RegisterPushEndpoint(ctx context.Context, userID ID, in PushRegistration) (PushEndpointView, error) {
	if s.endpoints == nil {
		return PushEndpointView{}, errUnavailable
	}
	return s.endpoints.Register(ctx, userID, in)
}

func (s *ConsumerService) ListPushEndpoints(ctx context.Context, userID ID) ([]PushEndpointView, error) {
	if s.endpoints == nil {
		return nil, errUnavailable
	}
	return s.endpoints.List(ctx, userID)
}

func (s *ConsumerService) RevokePushEndpoint(ctx context.Context, userID, endpointID ID) (PushEndpointView, error) {
	if s.endpoints == nil {
		return PushEndpointView{}, errUnavailable
	}
	return s.endpoints.Revoke(ctx, userID, endpointID)
}
