package identity

import (
	"context"
	"errors"
)

// Exported sentinels for HTTP adapters and tests. Values equal the internal errors.
var (
	ErrUnavailable         = errUnavailable
	ErrUnauthenticated     = errUnauthenticated
	ErrAccountIneligible   = errAccountIneligible
	ErrCounterConflict     = errCounterConflict
	ErrCredentialConflict  = errCredentialConflict
	ErrZeroID              = errZeroID
	ErrInvalidChallenge    = errInvalidChallenge
	ErrChallengeExpired    = errChallengeExpired
	ErrChallengeConsumed   = errChallengeConsumed
	ErrChallengeExhausted  = errChallengeExhausted
	ErrChallengeThrottled  = errChallengeThrottled
	ErrInvalidIdentifier   = errInvalidIdentifier
	ErrInvalidSignupProof  = errInvalidSignupProof
	ErrSignupProofExpired  = errSignupProofExpired
	ErrSignupProofConsumed = errSignupProofConsumed
	ErrInvalidResetProof   = errInvalidResetProof
	ErrResetProofExpired   = errResetProofExpired
	ErrResetProofConsumed  = errResetProofConsumed
	ErrIdentifierConflict  = errIdentifierConflict
	ErrInvalidPassword     = errInvalidPassword
	ErrPasswordTooLong     = errPasswordTooLong
)

// FailureClass is a coarse, non-sensitive mapping for the HTTP edge.
type FailureClass int

const (
	FailureNone FailureClass = iota
	FailureUnauthenticated
	FailureUnavailable
	FailureInternal
)

// Classify maps Identity errors to a public failure class.
// It does not return error strings, tokens, or verification details.
func Classify(err error) FailureClass {
	if err == nil {
		return FailureNone
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return FailureInternal
	}
	if errors.Is(err, errUnavailable) ||
		errors.Is(err, errStoreRequired) ||
		errors.Is(err, errInvalidWebAuthnConfig) ||
		errors.Is(err, errInvalidPolicy) ||
		errors.Is(err, errInvalidCeremonyPolicy) ||
		errors.Is(err, errInvalidChallengePolicy) ||
		errors.Is(err, errInvalidIssuancePolicy) {
		return FailureUnavailable
	}
	if errors.Is(err, errUnauthenticated) ||
		errors.Is(err, errWebAuthnVerification) ||
		errors.Is(err, errUnknownCredential) ||
		errors.Is(err, errCredentialRevoked) ||
		errors.Is(err, errAccountIneligible) ||
		errors.Is(err, errCounterConflict) ||
		errors.Is(err, errCredentialConflict) ||
		errors.Is(err, errCeremonyExpired) ||
		errors.Is(err, errCeremonyConsumed) ||
		errors.Is(err, errCeremonyKind) ||
		errors.Is(err, errInvalidCeremony) ||
		errors.Is(err, errSessionRevoked) ||
		errors.Is(err, errSessionIdleExpired) ||
		errors.Is(err, errSessionAbsExpired) ||
		errors.Is(err, errDeviceRevoked) ||
		errors.Is(err, errZeroID) ||
		errors.Is(err, errWeakSessionSecret) ||
		errors.Is(err, errInvalidTokenHash) ||
		errors.Is(err, errSignCountNotMonotonic) ||
		errors.Is(err, errInvalidChallenge) ||
		errors.Is(err, errChallengeExpired) ||
		errors.Is(err, errChallengeConsumed) ||
		errors.Is(err, errChallengeExhausted) ||
		errors.Is(err, errChallengeThrottled) ||
		errors.Is(err, errInvalidIdentifier) ||
		errors.Is(err, errInvalidSignupProof) ||
		errors.Is(err, errSignupProofExpired) ||
		errors.Is(err, errSignupProofConsumed) ||
		errors.Is(err, errInvalidResetProof) ||
		errors.Is(err, errResetProofExpired) ||
		errors.Is(err, errResetProofConsumed) {
		return FailureUnauthenticated
	}
	return FailureInternal
}
