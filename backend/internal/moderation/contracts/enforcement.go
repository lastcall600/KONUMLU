package contracts

import (
	"context"
	"errors"
)

var (
	ErrNotImplemented = errors.New("moderation enforcement not implemented")
	ErrZeroID         = errors.New("moderation id must not be zero")
	ErrInvalidIntent  = errors.New("invalid moderation execution intent")
)

// ID is a moderation action, case, or target UUID. Other domains must not treat
// this as a handle into Moderation tables.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// ExecutionIntent describes a staff decision. Listing restrict/remove is applied
// through listings/contracts.ModerationEnforcement, and public_profile
// restrict/remove through identity/contracts.PublicProfileModeration, before
// the action is marked executed. This port does not hide targets by itself.
type ExecutionIntent struct {
	ActionID   ID
	CaseID     ID
	TargetType string
	TargetID   ID
	ActionType string
}

func (e ExecutionIntent) Validate() error {
	if e.ActionID.IsZero() || e.CaseID.IsZero() || e.TargetID.IsZero() {
		return ErrZeroID
	}
	if e.TargetType == "" || e.ActionType == "" {
		return ErrInvalidIntent
	}
	return nil
}

// Enforcer remains a future-facing port for account-level targets. Listing and
// public-profile enforcement are owning-domain contracts wired on the
// Moderation service — not this interface.
type Enforcer interface {
	Apply(ctx context.Context, intent ExecutionIntent) error
}
