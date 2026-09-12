package identity

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/platform/cache"
)

const stepUpKeyPrefix = "identity:stepup:"

// StepUp is server-authoritative recent-strong elevation bound to one session.
// Valkey is the only store: loss requires stepping up again and never grants elevation.
type StepUp struct {
	hot    sessionHotCache
	policy StepUpPolicy
	now    func() time.Time
}

type stepUpRecord struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	IssuedAt  string `json:"issued_at"`
}

func NewStepUp(hot sessionHotCache, policy StepUpPolicy, now func() time.Time) (*StepUp, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &StepUp{hot: hot, policy: policy, now: now}, nil
}

func stepUpKey(sessionID ID) string {
	if sessionID.IsZero() {
		return ""
	}
	return stepUpKeyPrefix + sessionID.String()
}

func (s *StepUp) Grant(ctx context.Context, session Session) error {
	if s == nil {
		return errUnavailable
	}
	if session.ID.IsZero() || session.UserID.IsZero() {
		return errZeroID
	}
	if s.hot == nil {
		return errUnavailable
	}
	if err := session.DurableValid(s.now()); err != nil {
		return err
	}
	rec := stepUpRecord{
		UserID:    session.UserID.String(),
		SessionID: session.ID.String(),
		IssuedAt:  s.now().UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return errUnavailable
	}
	key := stepUpKey(session.ID)
	if key == "" {
		return errUnavailable
	}
	if err := s.hot.Set(ctx, key, string(raw), s.policy.TTL); err != nil {
		if isCacheUnavailable(err) || errors.Is(err, cache.ErrUnavailable) {
			return errUnavailable
		}
		return errUnavailable
	}
	return nil
}

func (s *StepUp) Require(ctx context.Context, session Session, op SensitiveOperation) error {
	if s == nil {
		return errUnavailable
	}
	if !op.valid() {
		return errStepUpRequired
	}
	if session.ID.IsZero() || session.UserID.IsZero() {
		return errZeroID
	}
	if err := session.DurableValid(s.now()); err != nil {
		return err
	}
	if s.hot == nil {
		return errUnavailable
	}
	key := stepUpKey(session.ID)
	if key == "" {
		return errUnavailable
	}
	raw, err := s.hot.Get(ctx, key)
	if err != nil {
		if errors.Is(err, cache.ErrMiss) {
			return errStepUpRequired
		}
		return errUnavailable
	}
	var rec stepUpRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		_ = s.hot.Delete(ctx, key)
		return errStepUpRequired
	}
	if rec.UserID != session.UserID.String() || rec.SessionID != session.ID.String() {
		_ = s.hot.Delete(ctx, key)
		return errStepUpRequired
	}
	return nil
}

func (s *StepUp) Clear(ctx context.Context, sessionID ID) {
	if s == nil || s.hot == nil {
		return
	}
	key := stepUpKey(sessionID)
	if key == "" {
		return
	}
	_ = s.hot.Delete(ctx, key)
}
