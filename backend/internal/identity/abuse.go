package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const (
	authRateLimitKeyPrefix     = "identity:auth:rl:"
	challengeReplayKeyPrefix   = "identity:auth:challenge:replay:"
	unknownRateLimitSubject    = "unknown"
)

// AuthOperation is a server-authoritative auth HTTP operation.
type AuthOperation string

const (
	AuthOpPasswordLogin         AuthOperation = "password_login"
	AuthOpPasskeyLoginBegin     AuthOperation = "passkey_login_begin"
	AuthOpPasskeyLoginFinish    AuthOperation = "passkey_login_finish"
	AuthOpSignupStart           AuthOperation = "signup_start"
	AuthOpSignupFinish          AuthOperation = "signup_finish"
	AuthOpSignupComplete        AuthOperation = "signup_complete"
	AuthOpResetStart            AuthOperation = "reset_start"
	AuthOpResetVerify           AuthOperation = "reset_verify"
	AuthOpResetComplete         AuthOperation = "reset_complete"
	AuthOpPasskeyRegisterBegin  AuthOperation = "passkey_register_begin"
	AuthOpPasskeyRegisterFinish AuthOperation = "passkey_register_finish"
)

func (op AuthOperation) valid() bool {
	switch op {
	case AuthOpPasswordLogin, AuthOpPasskeyLoginBegin, AuthOpPasskeyLoginFinish,
		AuthOpSignupStart, AuthOpSignupFinish, AuthOpSignupComplete,
		AuthOpResetStart, AuthOpResetVerify, AuthOpResetComplete,
		AuthOpPasskeyRegisterBegin, AuthOpPasskeyRegisterFinish:
		return true
	default:
		return false
	}
}

// ParseAuthOperation maps a config token to a known operation.
func ParseAuthOperation(raw string) (AuthOperation, bool) {
	op := AuthOperation(strings.ToLower(strings.TrimSpace(raw)))
	if !op.valid() {
		return "", false
	}
	return op, true
}

// RateLimitDimension is an independent counter family.
type RateLimitDimension string

const (
	DimensionIP      RateLimitDimension = "ip"
	DimensionAccount RateLimitDimension = "account"
	DimensionTarget  RateLimitDimension = "target"
	DimensionProof   RateLimitDimension = "proof"
	DimensionSession RateLimitDimension = "session"
	DimensionDevice  RateLimitDimension = "device"
	DimensionReplay  RateLimitDimension = "replay"
)

// RateLimitCounter is the Valkey atomic increment used for disposable windows.
type RateLimitCounter interface {
	Increment(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// AbuseSubject is the hashed-at-rest input for one auth request.
// Raw email/phone/tokens must not be written to Valkey keys or logs.
type AbuseSubject struct {
	Operation      AuthOperation
	IP             string
	AccountID      ID
	Target         string
	Proof          string
	SessionID      ID
	ChallengeToken string
	Hostname       string
}

// AbuseEngine is the Identity-owned rate-limit + challenge orchestrator.
type AbuseEngine struct {
	inc       RateLimitCounter
	policy    AuthAbusePolicy
	challenge HumanChallengePolicy
	verifier  HumanChallenge
}

// AbusePhase selects which independent checks run. Password login counts IP
// before identifier resolve and account after; the IP bucket must not double-count.
type AbusePhase uint

const (
	PhaseIP AbusePhase = 1 << iota
	PhaseTarget
	PhaseAccount
	PhaseProof
	PhaseSession
	PhaseChallenge
	PhaseAll = PhaseIP | PhaseTarget | PhaseAccount | PhaseProof | PhaseSession | PhaseChallenge
)

func (p AbusePhase) has(flag AbusePhase) bool {
	return p&flag != 0
}

func NewAbuseEngine(inc RateLimitCounter, policy AuthAbusePolicy, challenge HumanChallengePolicy, verifier HumanChallenge) (*AbuseEngine, error) {
	if inc == nil {
		return nil, errUnavailable
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if err := challenge.Validate(); err != nil {
		return nil, err
	}
	if challenge.RequiresAny() && verifier == nil {
		return nil, errUnavailable
	}
	if !challenge.RequiresAny() && verifier != nil && challenge.ProviderName() != HumanChallengeProviderNone {
		// A wired verifier with no required operations is allowed (tests) but must not be invoked.
	}
	return &AbuseEngine{inc: inc, policy: policy, challenge: challenge, verifier: verifier}, nil
}

func (e *AbuseEngine) Evaluate(ctx context.Context, sub AbuseSubject, phase AbusePhase) RiskOutcome {
	if e == nil || e.inc == nil {
		return unavailableOutcome(ReasonStorageUnavailable)
	}
	if phase == 0 {
		phase = PhaseAll
	}
	if !sub.Operation.valid() {
		return unavailableOutcome(ReasonSuspiciousAuthState)
	}
	limits, ok := e.policy.For(sub.Operation)
	if !ok {
		return unavailableOutcome(ReasonSuspiciousAuthState)
	}

	if phase.has(PhaseIP) {
		if out := e.checkDimension(ctx, sub.Operation, DimensionIP, subjectOrUnknown(sub.IP), limits.IP); !out.Allow() {
			return out
		}
	}
	if phase.has(PhaseTarget) && limits.Target.enabled() && strings.TrimSpace(sub.Target) != "" {
		if out := e.checkDimension(ctx, sub.Operation, DimensionTarget, sub.Target, limits.Target); !out.Allow() {
			return out
		}
	}
	if phase.has(PhaseAccount) && limits.Account.enabled() && !sub.AccountID.IsZero() {
		if out := e.checkDimension(ctx, sub.Operation, DimensionAccount, sub.AccountID.String(), limits.Account); !out.Allow() {
			return out
		}
	}
	if phase.has(PhaseProof) && limits.Proof.enabled() && strings.TrimSpace(sub.Proof) != "" {
		if out := e.checkDimension(ctx, sub.Operation, DimensionProof, sub.Proof, limits.Proof); !out.Allow() {
			return out
		}
	}
	if phase.has(PhaseSession) && limits.Session.enabled() && !sub.SessionID.IsZero() {
		if out := e.checkDimension(ctx, sub.Operation, DimensionSession, sub.SessionID.String(), limits.Session); !out.Allow() {
			return out
		}
	}
	if !phase.has(PhaseChallenge) {
		return allowOutcome().withOp(sub.Operation)
	}
	return e.evaluateChallenge(ctx, sub)
}

func (e *AbuseEngine) checkDimension(ctx context.Context, op AuthOperation, dim RateLimitDimension, raw string, bucket RateLimitBucket) RiskOutcome {
	if !bucket.enabled() {
		return allowOutcome()
	}
	key := FormatRateLimitKey(op, dim, raw)
	err := checkRateLimit(ctx, e.inc, key, bucket)
	if err == nil {
		return allowOutcome()
	}
	if errors.Is(err, errRateLimited) {
		return RiskOutcome{
			Decision:   RiskRestrict,
			Reason:     reasonForDimension(dim),
			Dimension:  dim,
			Operation:  op,
			RetryAfter: bucket.Window,
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return unavailableOutcome(ReasonStorageUnavailable).with(op, dim)
	}
	return unavailableOutcome(ReasonStorageUnavailable).with(op, dim)
}

func (e *AbuseEngine) evaluateChallenge(ctx context.Context, sub AbuseSubject) RiskOutcome {
	if !e.challenge.Requires(sub.Operation) {
		return allowOutcome().withOp(sub.Operation)
	}
	if e.verifier == nil {
		return unavailableOutcome(ReasonProviderUnavailable).withOp(sub.Operation)
	}
	token := strings.TrimSpace(sub.ChallengeToken)
	if token == "" {
		return RiskOutcome{
			Decision:  RiskChallenge,
			Reason:    ReasonChallengeRequired,
			Operation: sub.Operation,
			Challenged: true,
			Provider:  e.verifier.Name(),
		}
	}
	if expectedHost := strings.TrimSpace(e.challenge.Hostname); expectedHost != "" && !strings.EqualFold(strings.TrimSpace(sub.Hostname), expectedHost) {
		return RiskOutcome{
			Decision:    RiskChallenge,
			Reason:      ReasonChallengeFailed,
			Operation:   sub.Operation,
			Challenged:  true,
			Provider:    e.verifier.Name(),
			ChallengeOK: false,
		}
	}
	got, err := e.verifier.Verify(ctx, HumanChallengeInput{
		Token:    token,
		Action:   HumanChallengeAction(sub.Operation),
		Hostname: strings.TrimSpace(sub.Hostname),
	})
	if err != nil {
		return unavailableOutcome(ReasonProviderUnavailable).withOp(sub.Operation).challenge(e.verifier.Name(), false)
	}
	if !got.OK {
		return RiskOutcome{
			Decision:    RiskChallenge,
			Reason:      ReasonChallengeFailed,
			Operation:   sub.Operation,
			Challenged:  true,
			Provider:    e.verifier.Name(),
			ChallengeOK: false,
		}
	}
	if expected := HumanChallengeAction(sub.Operation); got.Action != "" && got.Action != expected {
		return RiskOutcome{
			Decision:    RiskChallenge,
			Reason:      ReasonChallengeFailed,
			Operation:   sub.Operation,
			Challenged:  true,
			Provider:    e.verifier.Name(),
			ChallengeOK: false,
		}
	}
	if err := e.consumeChallengeReplay(ctx, token); err != nil {
		if errors.Is(err, errRateLimited) {
			return RiskOutcome{
				Decision:    RiskChallenge,
				Reason:      ReasonChallengeFailed,
				Operation:   sub.Operation,
				Dimension:   DimensionReplay,
				Challenged:  true,
				Provider:    e.verifier.Name(),
				ChallengeOK: false,
			}
		}
		return unavailableOutcome(ReasonStorageUnavailable).withOp(sub.Operation).challenge(e.verifier.Name(), false)
	}
	return allowOutcome().withOp(sub.Operation).challenge(e.verifier.Name(), true)
}

func (e *AbuseEngine) consumeChallengeReplay(ctx context.Context, token string) error {
	ttl := e.challenge.ReplayTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	key := challengeReplayKeyPrefix + HashRateLimitSubject(token)
	return checkRateLimit(ctx, e.inc, key, RateLimitBucket{Max: 1, Window: ttl})
}

func checkRateLimit(ctx context.Context, inc RateLimitCounter, key string, bucket RateLimitBucket) error {
	if inc == nil {
		return errUnavailable
	}
	if !bucket.enabled() {
		return nil
	}
	if key == "" {
		return errUnavailable
	}
	n, err := inc.Increment(ctx, key, bucket.Window)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return errUnavailable
	}
	if n > int64(bucket.Max) {
		return errRateLimited
	}
	return nil
}

// HashRateLimitSubject returns a stable SHA-256 hex digest. Raw identifiers never go in keys.
func HashRateLimitSubject(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

// FormatRateLimitKey builds a namespaced Valkey key. The subject is hashed.
func FormatRateLimitKey(op AuthOperation, dim RateLimitDimension, rawSubject string) string {
	return authRateLimitKeyPrefix + string(op) + ":" + string(dim) + ":" + HashRateLimitSubject(subjectOrUnknown(rawSubject))
}

// PasswordLoginTarget is the existence-neutral rate-limit subject for a
// submitted identifier. It is applied before account resolution.
func PasswordLoginTarget(kind IdentifierKind, canonical string) string {
	return string(kind) + ":" + strings.TrimSpace(canonical)
}

func subjectOrUnknown(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return unknownRateLimitSubject
	}
	return raw
}

func reasonForDimension(dim RateLimitDimension) RiskReason {
	switch dim {
	case DimensionIP:
		return ReasonVelocityIP
	case DimensionAccount, DimensionSession:
		return ReasonVelocityAccount
	case DimensionTarget, DimensionProof:
		return ReasonVelocityTarget
	default:
		return ReasonSuspiciousAuthState
	}
}
