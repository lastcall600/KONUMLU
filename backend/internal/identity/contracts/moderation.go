package contracts

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrConflict               = errors.New("identity public profile conflict")
	ErrInvalidModerationState = errors.New("invalid public profile moderation state")
	ErrModerationNotEnforced  = errors.New("public profile moderation state is not an enforceable restriction")
)

const (
	ModerationStateNone       = "none"
	ModerationStateRestricted = "restricted"
	ModerationStateRemoved    = "removed"
)

// NormalizeModerationState maps omitted/legacy empty values to none.
func NormalizeModerationState(raw string) string {
	state := strings.TrimSpace(raw)
	if state == "" {
		return ModerationStateNone
	}
	return state
}

// PubliclyVisible is true only when the profile is not staff-hidden.
// Account disabled/deleted is a separate Identity check in the owning service.
func PubliclyVisible(moderationState string) bool {
	return NormalizeModerationState(moderationState) == ModerationStateNone
}

// ApplyPublicProfileModerationInput is an Identity-owned hide command.
// Moderation maps restrict/remove here; it must not write identity tables.
// Restoration uses ClearModerationState, not State=none on this input.
type ApplyPublicProfileModerationInput struct {
	PublicProfileID ID
	State           string
}

func (in ApplyPublicProfileModerationInput) Validate() error {
	if in.PublicProfileID.IsZero() {
		return ErrZeroID
	}
	switch NormalizeModerationState(in.State) {
	case ModerationStateRestricted, ModerationStateRemoved:
		return nil
	case ModerationStateNone:
		return ErrModerationNotEnforced
	default:
		return ErrInvalidModerationState
	}
}

// PublicProfileModeration applies staff hide/remove and appeal restoration on
// public-profile visibility. It never changes account, auth, session, credentials,
// email/phone, legal identity, or internal user state.
//
// Failure semantics (no distributed transaction):
//   - Identity commits the public_profiles row in one Identity transaction.
//   - Callers must not record a Moderation "executed" action unless
//     ApplyModerationState returns nil. Callers must not mark an appeal
//     accepted after a public_profile restrict/remove unless ClearModerationState
//     returns nil.
//   - If Identity succeeds and the caller then fails, retry the same
//     operation: same-state apply and already-none clear are idempotent.
//   - Clearing moderation_state does not disable/enable login, revoke sessions,
//     or alter Trust projection data. Public GET uses this state; self GET/PATCH
//     remain available.
type PublicProfileModeration interface {
	ApplyModerationState(ctx context.Context, in ApplyPublicProfileModerationInput) error
	ClearModerationState(ctx context.Context, publicProfileID ID) error
}
