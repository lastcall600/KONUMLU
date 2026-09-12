package identity

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/platform/cache"
)

const passkeyBootstrapKeyPrefix = "identity:passkey-bootstrap:"

const (
	bootstrapKindSignup   = "signup"
	bootstrapKindPassword = "password_reauth"
)

// PasskeyBootstrap is first-passkey enrollment authority only.
// It is not recent-strong Step-Up and never authorizes passkey_remove.
type PasskeyBootstrap struct {
	hot       sessionHotCache
	passkeys  *Passkeys
	passwords *Passwords
	policy    StepUpPolicy
	now       func() time.Time
}

type passkeyBootstrapRecord struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Scope     string `json:"scope"`
	Kind      string `json:"kind"`
	IssuedAt  string `json:"issued_at"`
}

func NewPasskeyBootstrap(hot sessionHotCache, passkeys *Passkeys, passwords *Passwords, policy StepUpPolicy, now func() time.Time) (*PasskeyBootstrap, error) {
	if hot == nil || passkeys == nil || passwords == nil {
		return nil, errStoreRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &PasskeyBootstrap{hot: hot, passkeys: passkeys, passwords: passwords, policy: policy, now: now}, nil
}

func passkeyBootstrapKey(sessionID ID) string {
	if sessionID.IsZero() {
		return ""
	}
	return passkeyBootstrapKeyPrefix + sessionID.String()
}

func (b *PasskeyBootstrap) GrantSignup(ctx context.Context, session Session) error {
	return b.grant(ctx, session, bootstrapKindSignup, nil)
}

func (b *PasskeyBootstrap) GrantPassword(ctx context.Context, session Session, password []byte) error {
	return b.grant(ctx, session, bootstrapKindPassword, password)
}

func (b *PasskeyBootstrap) grant(ctx context.Context, session Session, kind string, password []byte) error {
	if b == nil {
		return errUnavailable
	}
	if err := session.DurableValid(b.now()); err != nil {
		return err
	}
	if b.hot == nil || b.passkeys == nil || b.passwords == nil {
		return errUnavailable
	}
	active, err := b.passkeys.ListActiveForUser(ctx, session.UserID)
	if err != nil {
		return err
	}
	if len(active) > 0 {
		if kind == bootstrapKindPassword {
			b.passwords.DummyVerify(password)
		}
		return errStepUpRequired
	}
	switch kind {
	case bootstrapKindSignup:
		hasPassword, err := b.passwords.HasActive(ctx, session.UserID)
		if err != nil {
			return err
		}
		if hasPassword {
			return errStepUpRequired
		}
	case bootstrapKindPassword:
		hasPassword, err := b.passwords.HasActive(ctx, session.UserID)
		if err != nil {
			return err
		}
		if !hasPassword {
			b.passwords.DummyVerify(password)
			return errUnauthenticated
		}
		if _, err := b.passwords.Verify(ctx, session.UserID, password); err != nil {
			return err
		}
	default:
		return errUnavailable
	}
	rec := passkeyBootstrapRecord{
		UserID:    session.UserID.String(),
		SessionID: session.ID.String(),
		Scope:     string(SensitivePasskeyAdd),
		Kind:      kind,
		IssuedAt:  b.now().UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return errUnavailable
	}
	key := passkeyBootstrapKey(session.ID)
	if key == "" {
		return errUnavailable
	}
	if err := b.hot.Set(ctx, key, string(raw), b.policy.TTL); err != nil {
		if isCacheUnavailable(err) || errors.Is(err, cache.ErrUnavailable) {
			return errUnavailable
		}
		return errUnavailable
	}
	return nil
}

func (b *PasskeyBootstrap) Allow(ctx context.Context, session Session, op SensitiveOperation) error {
	if b == nil {
		return errUnavailable
	}
	if op != SensitivePasskeyAdd {
		return errStepUpRequired
	}
	if err := session.DurableValid(b.now()); err != nil {
		return err
	}
	if b.hot == nil || b.passkeys == nil {
		return errUnavailable
	}
	key := passkeyBootstrapKey(session.ID)
	if key == "" {
		return errUnavailable
	}
	raw, err := b.hot.Get(ctx, key)
	if err != nil {
		if errors.Is(err, cache.ErrMiss) {
			return errStepUpRequired
		}
		return errUnavailable
	}
	var rec passkeyBootstrapRecord
	if err := json.Unmarshal([]byte(raw), &rec); err != nil {
		_ = b.hot.Delete(ctx, key)
		return errStepUpRequired
	}
	if rec.UserID != session.UserID.String() || rec.SessionID != session.ID.String() || rec.Scope != string(SensitivePasskeyAdd) {
		_ = b.hot.Delete(ctx, key)
		return errStepUpRequired
	}
	active, err := b.passkeys.ListActiveForUser(ctx, session.UserID)
	if err != nil {
		return err
	}
	if len(active) > 0 {
		_ = b.hot.Delete(ctx, key)
		return errStepUpRequired
	}
	return nil
}

func (b *PasskeyBootstrap) Consume(ctx context.Context, sessionID ID) {
	b.Clear(ctx, sessionID)
}

func (b *PasskeyBootstrap) Clear(ctx context.Context, sessionID ID) {
	if b == nil || b.hot == nil {
		return
	}
	key := passkeyBootstrapKey(sessionID)
	if key == "" {
		return
	}
	_ = b.hot.Delete(ctx, key)
}
