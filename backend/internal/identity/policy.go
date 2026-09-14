package identity

import (
	"strings"
	"time"
)

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

// TouchQuantum is the minimum time between durable idle-watermark writes.
// It is derived from idle lifetime so AUTH-B does not add a second knob.
func (p SessionPolicy) TouchQuantum() time.Duration {
	q := time.Minute
	if p.Idle > 0 {
		if quarter := p.Idle / 4; quarter > 0 && quarter < q {
			q = quarter
		}
	}
	if q < 5*time.Second {
		return 5 * time.Second
	}
	return q
}

// MaxStepUpTTL is the hard ceiling for recent-strong elevation.
const MaxStepUpTTL = 15 * time.Minute

// StepUpPolicy is injected recent-strong elevation lifetime.
type StepUpPolicy struct {
	TTL time.Duration
}

func (p StepUpPolicy) Validate() error {
	if p.TTL <= 0 || p.TTL > MaxStepUpTTL {
		return errInvalidStepUpPolicy
	}
	return nil
}

// SensitiveOperation is an Identity-owned step-up gate. It is not a Trust level.
type SensitiveOperation string

const (
	SensitivePasskeyAdd    SensitiveOperation = "passkey_add"
	SensitivePasskeyRemove SensitiveOperation = "passkey_remove"
)

func (op SensitiveOperation) valid() bool {
	switch op {
	case SensitivePasskeyAdd, SensitivePasskeyRemove:
		return true
	default:
		return false
	}
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

// RateLimitBucket is one independent Valkey counter window.
// Max==0 means the dimension is unused (not a fail-open wildcard).
type RateLimitBucket struct {
	Max    int
	Window time.Duration
}

func (b RateLimitBucket) enabled() bool {
	return b.Max > 0 && b.Window > 0
}

func (b RateLimitBucket) validateOptional() error {
	if b.Max == 0 && b.Window == 0 {
		return nil
	}
	if b.Max <= 0 || b.Window <= 0 {
		return errInvalidAbusePolicy
	}
	return nil
}

func (b RateLimitBucket) validateRequired() error {
	if b.Max <= 0 || b.Window <= 0 {
		return errInvalidAbusePolicy
	}
	return nil
}

// OperationLimits is the per-auth-operation dimension set.
// Device is defined for a future trustworthy device identity and must stay unused in AUTH-A.
type OperationLimits struct {
	IP      RateLimitBucket
	Account RateLimitBucket
	Target  RateLimitBucket
	Proof   RateLimitBucket
	Session RateLimitBucket
	Device  RateLimitBucket
}

func (l OperationLimits) validate() error {
	if err := l.IP.validateRequired(); err != nil {
		return err
	}
	if err := l.Account.validateOptional(); err != nil {
		return err
	}
	if err := l.Target.validateOptional(); err != nil {
		return err
	}
	if err := l.Proof.validateOptional(); err != nil {
		return err
	}
	if err := l.Session.validateOptional(); err != nil {
		return err
	}
	if l.Device.enabled() {
		return errInvalidAbusePolicy
	}
	return l.Device.validateOptional()
}

// AuthAbusePolicy is the Identity-owned multi-dimensional auth rate-limit model.
type AuthAbusePolicy struct {
	PasswordLogin          OperationLimits
	PasskeyLoginBegin      OperationLimits
	PasskeyLoginFinish     OperationLimits
	SignupStart            OperationLimits
	SignupFinish           OperationLimits
	SignupComplete         OperationLimits
	ResetStart             OperationLimits
	ResetVerify            OperationLimits
	ResetComplete          OperationLimits
	PasskeyRegisterBegin   OperationLimits
	PasskeyRegisterFinish  OperationLimits
	PasskeyRemove          OperationLimits
	StepUpBegin            OperationLimits
	StepUpFinish           OperationLimits
	SessionRevoke          OperationLimits
	PasswordReauth         OperationLimits
}

func (p AuthAbusePolicy) Validate() error {
	ops := []OperationLimits{
		p.PasswordLogin, p.PasskeyLoginBegin, p.PasskeyLoginFinish,
		p.SignupStart, p.SignupFinish, p.SignupComplete,
		p.ResetStart, p.ResetVerify, p.ResetComplete,
		p.PasskeyRegisterBegin, p.PasskeyRegisterFinish,
		p.PasskeyRemove, p.StepUpBegin, p.StepUpFinish, p.SessionRevoke,
		p.PasswordReauth,
	}
	for _, op := range ops {
		if err := op.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (p AuthAbusePolicy) For(op AuthOperation) (OperationLimits, bool) {
	switch op {
	case AuthOpPasswordLogin:
		return p.PasswordLogin, true
	case AuthOpPasskeyLoginBegin:
		return p.PasskeyLoginBegin, true
	case AuthOpPasskeyLoginFinish:
		return p.PasskeyLoginFinish, true
	case AuthOpSignupStart:
		return p.SignupStart, true
	case AuthOpSignupFinish:
		return p.SignupFinish, true
	case AuthOpSignupComplete:
		return p.SignupComplete, true
	case AuthOpResetStart:
		return p.ResetStart, true
	case AuthOpResetVerify:
		return p.ResetVerify, true
	case AuthOpResetComplete:
		return p.ResetComplete, true
	case AuthOpPasskeyRegisterBegin:
		return p.PasskeyRegisterBegin, true
	case AuthOpPasskeyRegisterFinish:
		return p.PasskeyRegisterFinish, true
	case AuthOpPasskeyRemove:
		return p.PasskeyRemove, true
	case AuthOpStepUpBegin:
		return p.StepUpBegin, true
	case AuthOpStepUpFinish:
		return p.StepUpFinish, true
	case AuthOpSessionRevoke:
		return p.SessionRevoke, true
	case AuthOpPasswordReauth:
		return p.PasswordReauth, true
	default:
		return OperationLimits{}, false
	}
}

// BuildAuthAbusePolicy fills every public auth operation from grouped buckets.
// Device stays unused. Missing optional dimensions stay disabled.
func BuildAuthAbusePolicy(ip, account, target, complete, sensitive RateLimitBucket) (AuthAbusePolicy, error) {
	public := OperationLimits{IP: ip}
	// Password login Target uses the account window, not IDENTITY_AUTH_TARGET_*.
	// The identifier is counted before account resolve so unknown vs known
	// identifiers share the same 429 threshold (no existence oracle).
	passwordLogin := OperationLimits{IP: ip, Account: account, Target: account}
	passkeyFinish := OperationLimits{IP: ip, Account: account}
	verify := OperationLimits{IP: ip, Target: target}
	consume := OperationLimits{IP: ip, Proof: complete}
	enroll := OperationLimits{IP: ip, Account: sensitive, Session: sensitive}
	p := AuthAbusePolicy{
		PasswordLogin:         passwordLogin,
		PasskeyLoginBegin:     public,
		PasskeyLoginFinish:    passkeyFinish,
		SignupStart:           public,
		SignupFinish:          verify,
		SignupComplete:        consume,
		ResetStart:            public,
		ResetVerify:           verify,
		ResetComplete:         consume,
		PasskeyRegisterBegin:  enroll,
		PasskeyRegisterFinish: enroll,
		PasskeyRemove:         enroll,
		StepUpBegin:           enroll,
		StepUpFinish:          enroll,
		SessionRevoke:         enroll,
		PasswordReauth:        OperationLimits{IP: ip, Account: account, Session: sensitive},
	}
	if err := p.Validate(); err != nil {
		return AuthAbusePolicy{}, err
	}
	return p, nil
}

// HumanChallengeProviderName is a server-side provider mode. It is not a vendor SDK.
const (
	HumanChallengeProviderNone         = "none"
	HumanChallengeProviderFake         = "fake"
	HumanChallengeProviderUnconfigured = "unconfigured"
	HumanChallengeProviderTurnstile    = "turnstile"
)

// HumanChallengePolicy decides when a human challenge is required.
// An empty Required set means no operation is challenged.
type HumanChallengePolicy struct {
	Provider         string
	Required         map[AuthOperation]struct{}
	Hostname         string
	AllowedHostnames []string
	ReplayTTL        time.Duration
}

func (p HumanChallengePolicy) Validate() error {
	switch strings.ToLower(strings.TrimSpace(p.Provider)) {
	case "", HumanChallengeProviderNone, HumanChallengeProviderFake, HumanChallengeProviderUnconfigured, HumanChallengeProviderTurnstile:
	default:
		return errInvalidAbusePolicy
	}
	for op := range p.Required {
		if !op.valid() {
			return errInvalidAbusePolicy
		}
	}
	if p.RequiresAny() && p.ReplayTTL <= 0 {
		return errInvalidAbusePolicy
	}
	if !p.RequiresAny() && p.ReplayTTL < 0 {
		return errInvalidAbusePolicy
	}
	if p.ProviderName() == HumanChallengeProviderTurnstile && p.RequiresAny() {
		if len(p.hostnameAllowlist()) == 0 {
			return errInvalidAbusePolicy
		}
		for _, h := range p.hostnameAllowlist() {
			if strings.Contains(h, "*") {
				return errInvalidAbusePolicy
			}
		}
	}
	return nil
}

func (p HumanChallengePolicy) hostnameAllowlist() []string {
	if len(p.AllowedHostnames) > 0 {
		out := make([]string, 0, len(p.AllowedHostnames))
		for _, h := range p.AllowedHostnames {
			h = strings.TrimSpace(h)
			if h != "" {
				out = append(out, h)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if h := strings.TrimSpace(p.Hostname); h != "" {
		return []string{h}
	}
	return nil
}

func (p HumanChallengePolicy) AllowsHostname(host string) bool {
	allowed := p.hostnameAllowlist()
	if len(allowed) == 0 {
		return true
	}
	host = strings.TrimSpace(host)
	for _, h := range allowed {
		if strings.EqualFold(host, h) {
			return true
		}
	}
	return false
}

func (p HumanChallengePolicy) RequiresAny() bool {
	return len(p.Required) > 0
}

func (p HumanChallengePolicy) Requires(op AuthOperation) bool {
	if len(p.Required) == 0 {
		return false
	}
	_, ok := p.Required[op]
	return ok
}

func (p HumanChallengePolicy) ProviderName() string {
	n := strings.ToLower(strings.TrimSpace(p.Provider))
	if n == "" {
		return HumanChallengeProviderNone
	}
	return n
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
