package contracts

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrUnavailable            = errors.New("listings unavailable")
	ErrConflict               = errors.New("listing conflict")
	ErrInvalidModerationState = errors.New("invalid listing moderation state")
	ErrModerationNotEnforced  = errors.New("listing moderation state is not an enforceable restriction")
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

// PubliclyVisible is true only for published listings with no moderation hide.
// Owner archive (status=archived) is independent of moderation_state.
func PubliclyVisible(status, moderationState string) bool {
	return status == StatusPublished && NormalizeModerationState(moderationState) == ModerationStateNone
}

func (r ListingRef) PubliclyVisible() bool {
	return PubliclyVisible(r.Status, r.ModerationState)
}

func (s ListingSnapshot) PubliclyVisible() bool {
	return PubliclyVisible(s.Status, s.ModerationState)
}

// ApplyModerationInput is a Listings-owned hide command. Moderation maps
// restrict/remove here; it must not write listings tables. Restoration uses
// ClearModerationState, not State=none on this input.
type ApplyModerationInput struct {
	ListingID ID
	State     string
}

func (in ApplyModerationInput) Validate() error {
	if in.ListingID.IsZero() {
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

// ModerationEnforcement applies staff hide/remove and appeal restoration on
// listing visibility. It never changes owner lifecycle status.
//
// Failure semantics (no distributed transaction):
//   - Listings commits its row + search outbox in one Listings transaction.
//   - Callers must not record a Moderation "executed" action unless
//     ApplyModerationState returns nil. Callers must not mark an appeal
//     accepted after a listing restrict/remove unless ClearModerationState
//     returns nil.
//   - If Listings succeeds and the caller then fails, retry the same
//     operation: same-state apply and already-none clear are idempotent and
//     re-emit a search visibility event so a lost/unprocessed outbox can heal.
//   - Search remaining briefly stale until the outbox worker rebuilds is the
//     same eventual model as owner archive; it is not 2PC.
//   - Clearing moderation_state does not publish, unarchive, or bypass
//     eligibility. Public visibility is derived from owner status +
//     moderation_state + existing rules.
type ModerationEnforcement interface {
	ApplyModerationState(ctx context.Context, in ApplyModerationInput) error
	ClearModerationState(ctx context.Context, listingID ID) error
}
