package notifications

import (
	"context"
	"errors"
	"time"

	"backend/internal/notifications/policy"
	"backend/internal/platform/crypto"
	"backend/internal/platform/observability"
)

var (
	errEndpointConflict = errors.New("push endpoint conflict")
	ErrEndpointConflict = errEndpointConflict
)

type EndpointService struct {
	store *PostgresStore
	aead  *crypto.AEAD
	hmac  crypto.HMACKey
	now   func() time.Time
}

func NewEndpointService(store *PostgresStore, aead *crypto.AEAD, hmacKey crypto.HMACKey, now func() time.Time) (*EndpointService, error) {
	if store == nil || aead == nil {
		return nil, errStoreRequired
	}
	if _, err := hmacKey.Sum([]byte("probe")); err != nil {
		return nil, errUnavailable
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &EndpointService{store: store, aead: aead, hmac: hmacKey, now: now}, nil
}

func (s *EndpointService) Register(ctx context.Context, userID ID, in PushRegistration) (PushEndpointView, error) {
	if s == nil || s.store == nil || s.aead == nil {
		return PushEndpointView{}, errUnavailable
	}
	if userID.IsZero() {
		return PushEndpointView{}, policy.ErrInvalidActor
	}
	if err := ValidatePushRegistration(in); err != nil {
		return PushEndpointView{}, err
	}
	sum, err := hashPushMaterial(s.hmac, in)
	if err != nil {
		return PushEndpointView{}, err
	}
	now := s.now().UTC()

	if active, err := s.store.GetPushEndpointByHash(ctx, sum); err == nil {
		if active.UserID != userID {
			observability.FromContext(ctx).Info("notification_push_endpoint_conflict",
				"channel_code", string(in.Channel),
				"result", "conflict",
			)
			return PushEndpointView{}, errEndpointConflict
		}
		return s.sealAndRefresh(ctx, active, userID, in, now)
	} else if !errors.Is(err, errNotFound) {
		return PushEndpointView{}, err
	}

	if owned, err := s.store.GetPushEndpointByHashAny(ctx, userID, sum); err == nil {
		return s.sealAndRefresh(ctx, owned, userID, in, now)
	} else if !errors.Is(err, errNotFound) {
		return PushEndpointView{}, err
	}

	id, err := NewID()
	if err != nil {
		return PushEndpointView{}, errUnavailable
	}
	env, err := s.sealPayload(id, userID, in)
	if err != nil {
		return PushEndpointView{}, err
	}
	row := PushEndpointRecord{
		ID: id, UserID: userID, Channel: in.Channel, Platform: in.Platform, Provider: in.Provider,
		Hash: sum, KeyID: env.KeyID, Nonce: env.Nonce, Ciphertext: env.Ciphertext,
		CreatedAt: now, UpdatedAt: now, LastSeenAt: now,
	}
	stored, err := s.store.InsertPushEndpoint(ctx, row)
	if err != nil {
		if errors.Is(err, errConflict) {
			active, getErr := s.store.GetPushEndpointByHash(ctx, sum)
			if getErr == nil && active.UserID != userID {
				return PushEndpointView{}, errEndpointConflict
			}
			if getErr == nil && active.UserID == userID {
				return s.sealAndRefresh(ctx, active, userID, in, now)
			}
			return PushEndpointView{}, errEndpointConflict
		}
		return PushEndpointView{}, err
	}
	observability.FromContext(ctx).Info("notification_push_endpoint_registered",
		"channel_code", string(in.Channel),
		"endpoint_id", stored.ID.String(),
	)
	return toPushView(stored), nil
}

func (s *EndpointService) List(ctx context.Context, userID ID) ([]PushEndpointView, error) {
	if userID.IsZero() {
		return nil, policy.ErrInvalidActor
	}
	rows, err := s.store.ListPushEndpoints(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]PushEndpointView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toPushView(row))
	}
	return out, nil
}

func (s *EndpointService) Revoke(ctx context.Context, userID, endpointID ID) (PushEndpointView, error) {
	if userID.IsZero() || endpointID.IsZero() {
		return PushEndpointView{}, policy.ErrInvalidActor
	}
	row, err := s.store.RevokePushEndpoint(ctx, endpointID, userID, s.now().UTC())
	if err != nil {
		return PushEndpointView{}, err
	}
	observability.FromContext(ctx).Info("notification_push_endpoint_revoked",
		"channel_code", string(row.Channel),
		"endpoint_id", row.ID.String(),
	)
	return toPushView(row), nil
}

func (s *EndpointService) HasActive(ctx context.Context, userID ID, ch policy.Channel) (bool, error) {
	return s.store.HasActivePushEndpoint(ctx, userID, ch)
}

func (s *EndpointService) DispatchTargets(ctx context.Context, userID ID, ch policy.Channel) ([]PushSendRequest, error) {
	rows, err := s.store.ListActivePushForDispatch(ctx, userID, ch)
	if err != nil {
		return nil, err
	}
	out := make([]PushSendRequest, 0, len(rows))
	for _, row := range rows {
		mat, err := s.openRow(row)
		if err != nil {
			return nil, err
		}
		out = append(out, PushSendRequest{
			EndpointID: row.ID,
			Channel:    row.Channel,
			Platform:   row.Platform,
			Provider:   row.Provider,
			Material:   mat,
		})
	}
	return out, nil
}

func (s *EndpointService) sealAndRefresh(ctx context.Context, existing PushEndpointRecord, userID ID, in PushRegistration, now time.Time) (PushEndpointView, error) {
	env, err := s.sealPayload(existing.ID, userID, in)
	if err != nil {
		return PushEndpointView{}, err
	}
	stored, err := s.store.RefreshPushEndpoint(ctx, existing.ID, userID, env.KeyID, env.Nonce, env.Ciphertext, now)
	if err != nil {
		return PushEndpointView{}, err
	}
	observability.FromContext(ctx).Info("notification_push_endpoint_refreshed",
		"channel_code", string(in.Channel),
		"endpoint_id", stored.ID.String(),
	)
	return toPushView(stored), nil
}

func (s *EndpointService) sealPayload(id, userID ID, in PushRegistration) (crypto.Envelope, error) {
	raw, err := marshalPushPayload(in)
	if err != nil {
		return crypto.Envelope{}, err
	}
	return s.aead.Seal(raw, pushAAD(id, userID))
}

func (s *EndpointService) openRow(row PushEndpointRecord) (PushProviderMaterial, error) {
	plain, err := s.aead.Open(crypto.Envelope{
		KeyID: row.KeyID, Nonce: row.Nonce, Ciphertext: row.Ciphertext,
	}, pushAAD(row.ID, row.UserID))
	if err != nil {
		return PushProviderMaterial{}, errUnavailable
	}
	return unmarshalPushPayload(plain)
}

func toPushView(row PushEndpointRecord) PushEndpointView {
	return PushEndpointView{
		ID:         row.ID,
		Channel:    row.Channel,
		Platform:   row.Platform,
		Provider:   row.Provider,
		CreatedAt:  row.CreatedAt,
		LastSeenAt: row.LastSeenAt,
		Revoked:    row.RevokedAt != nil,
	}
}
