package identity

import "time"

// SessionPolicy is injected idle/absolute lifetime configuration (ADR-002 OI-002-01/02).
// Durations are not product defaults; callers must supply positive values.
type SessionPolicy struct {
	Idle     time.Duration
	Absolute time.Duration
}

func (p SessionPolicy) Validate() error {
	if p.Idle <= 0 || p.Absolute <= 0 {
		return errInvalidPolicy
	}
	if p.Absolute < p.Idle {
		return errInvalidPolicy
	}
	return nil
}

// PasswordPolicy is injected Argon2id parameters (ADR-004 OI-004-01).
// Values are not product defaults; callers must supply a valid policy.
type PasswordPolicy struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLen     uint32
	KeyLen      uint32
}

func (p PasswordPolicy) Validate() error {
	if p.MemoryKiB == 0 || p.Iterations == 0 || p.Parallelism == 0 {
		return errInvalidPasswordPolicy
	}
	if p.SaltLen < 16 || p.KeyLen < 16 {
		return errInvalidPasswordPolicy
	}
	return nil
}

func (p SessionPolicy) idleExpiresAt(now, absoluteExpiresAt time.Time) time.Time {
	idle := now.Add(p.Idle)
	if idle.After(absoluteExpiresAt) {
		return absoluteExpiresAt
	}
	return idle
}

// VerificationChallengePolicy is injected challenge TTL, attempt cap, and OTP width.
// Values are not product defaults; callers must supply a valid policy.
type VerificationChallengePolicy struct {
	TTL            time.Duration
	MaxAttempts    int
	PhoneOTPDigits int
}

func (p VerificationChallengePolicy) Validate() error {
	if p.TTL <= 0 || p.MaxAttempts <= 0 || p.PhoneOTPDigits <= 0 {
		return errInvalidChallengePolicy
	}
	return nil
}

// IssuanceLimitPolicy is injected SMS-pumping / resend windows (Valkey, not PostgreSQL).
// Limits and windows are not product defaults; callers must supply valid values.
type IssuanceLimitPolicy struct {
	DestinationMax    int
	DestinationWindow time.Duration
	IPMax             int
	IPWindow          time.Duration
}

func (p IssuanceLimitPolicy) Validate() error {
	if p.DestinationMax <= 0 || p.DestinationWindow <= 0 || p.IPMax <= 0 || p.IPWindow <= 0 {
		return errInvalidIssuancePolicy
	}
	return nil
}

// SignupProofPolicy is the injected short-lived signup-proof TTL.
type SignupProofPolicy struct {
	TTL time.Duration
}

func (p SignupProofPolicy) Validate() error {
	if p.TTL <= 0 {
		return errInvalidSignupProof
	}
	return nil
}

// ResetProofPolicy is the injected short-lived password-reset-proof TTL.
type ResetProofPolicy struct {
	TTL time.Duration
}

func (p ResetProofPolicy) Validate() error {
	if p.TTL <= 0 {
		return errInvalidResetProof
	}
	return nil
}
