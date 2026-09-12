package identity

import "time"

// RiskDecision is the Identity-owned server-side auth security decision.
// It is not a Trust level, not a user-visible score, and not a reputation system.
type RiskDecision string

const (
	RiskAllow     RiskDecision = "allow"
	RiskChallenge RiskDecision = "challenge"
	RiskStepUp    RiskDecision = "step_up"
	RiskRestrict  RiskDecision = "restrict"
	RiskReview    RiskDecision = "review"
)

func (d RiskDecision) valid() bool {
	switch d {
	case RiskAllow, RiskChallenge, RiskStepUp, RiskRestrict, RiskReview:
		return true
	default:
		return false
	}
}

// RiskReason is an internal policy/telemetry code. It is not a user-facing message.
type RiskReason string

const (
	ReasonNone                 RiskReason = ""
	ReasonVelocityIP           RiskReason = "velocity_ip"
	ReasonVelocityAccount      RiskReason = "velocity_account"
	ReasonVelocityTarget       RiskReason = "velocity_target"
	ReasonChallengeRequired    RiskReason = "challenge_required"
	ReasonChallengeFailed      RiskReason = "challenge_failed"
	ReasonProviderUnavailable  RiskReason = "provider_unavailable"
	ReasonStorageUnavailable   RiskReason = "storage_unavailable"
	ReasonSuspiciousAuthState  RiskReason = "suspicious_auth_state"
)

func (r RiskReason) valid() bool {
	switch r {
	case ReasonNone, ReasonVelocityIP, ReasonVelocityAccount, ReasonVelocityTarget,
		ReasonChallengeRequired, ReasonChallengeFailed, ReasonProviderUnavailable,
		ReasonStorageUnavailable, ReasonSuspiciousAuthState:
		return true
	default:
		return false
	}
}

// StepUpRequirement is a reserved AUTH-B hook. AUTH-A does not implement step-up sessions.
type StepUpRequirement struct {
	Required bool
}

// RiskOutcome is the internal result of abuse/challenge orchestration.
// There is no numeric score field by design.
type RiskOutcome struct {
	Decision    RiskDecision
	Reason      RiskReason
	Dimension   RateLimitDimension
	Operation   AuthOperation
	RetryAfter  time.Duration
	Challenged  bool
	Provider    string
	ChallengeOK bool
}

func (o RiskOutcome) Allow() bool {
	return o.Decision == RiskAllow
}

func (o RiskOutcome) with(op AuthOperation, dim RateLimitDimension) RiskOutcome {
	o.Operation = op
	o.Dimension = dim
	return o
}

func (o RiskOutcome) withOp(op AuthOperation) RiskOutcome {
	o.Operation = op
	return o
}

func (o RiskOutcome) challenge(provider string, ok bool) RiskOutcome {
	o.Challenged = true
	o.Provider = provider
	o.ChallengeOK = ok
	return o
}

func allowOutcome() RiskOutcome {
	return RiskOutcome{Decision: RiskAllow}
}

func unavailableOutcome(reason RiskReason) RiskOutcome {
	if reason == "" {
		reason = ReasonStorageUnavailable
	}
	return RiskOutcome{Decision: RiskRestrict, Reason: reason}
}
