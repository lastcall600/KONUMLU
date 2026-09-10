package publicprofile

import (
	"context"
	"errors"

	"backend/internal/identity/contracts"
)

// Resolver is the Identity public-profile contract implementation.
// Other domains must import contracts, not this type's package, for production calls.
type Resolver struct {
	svc *Service
}

func NewResolver(svc *Service) (*Resolver, error) {
	if svc == nil {
		return nil, ErrStoreRequired
	}
	return &Resolver{svc: svc}, nil
}

func (r *Resolver) ResolveByPublicID(ctx context.Context, publicProfileID contracts.ID) (contracts.PublicProfile, error) {
	if r == nil || r.svc == nil {
		return contracts.PublicProfile{}, contracts.ErrUnavailable
	}
	if publicProfileID.IsZero() {
		return contracts.PublicProfile{}, contracts.ErrZeroID
	}
	view, err := r.svc.GetPublic(ctx, toIdentityID(publicProfileID))
	return mapContract(view, err)
}

func (r *Resolver) ResolveByUserID(ctx context.Context, userID contracts.ID) (contracts.PublicProfile, error) {
	if r == nil || r.svc == nil {
		return contracts.PublicProfile{}, contracts.ErrUnavailable
	}
	if userID.IsZero() {
		return contracts.PublicProfile{}, contracts.ErrZeroID
	}
	view, err := r.svc.publicViewIfVisible(ctx, toIdentityID(userID))
	return mapContract(view, err)
}

func (r *Resolver) ApplyModerationState(ctx context.Context, in contracts.ApplyPublicProfileModerationInput) error {
	if r == nil || r.svc == nil {
		return contracts.ErrUnavailable
	}
	return r.svc.ApplyModerationState(ctx, in)
}

func (r *Resolver) ClearModerationState(ctx context.Context, publicProfileID contracts.ID) error {
	if r == nil || r.svc == nil {
		return contracts.ErrUnavailable
	}
	return r.svc.ClearModerationState(ctx, publicProfileID)
}

func (r *Resolver) ResolveUserIDByPublicID(ctx context.Context, publicProfileID contracts.ID) (contracts.ID, error) {
	if r == nil || r.svc == nil {
		return contracts.ID{}, contracts.ErrUnavailable
	}
	if publicProfileID.IsZero() {
		return contracts.ID{}, contracts.ErrZeroID
	}
	userID, err := r.svc.ResolveUserIDByPublicID(ctx, toIdentityID(publicProfileID))
	if err != nil {
		return contracts.ID{}, mapContractErr(err)
	}
	return toContractID(userID), nil
}

func mapContract(view PublicView, err error) (contracts.PublicProfile, error) {
	if err != nil {
		return contracts.PublicProfile{}, mapContractErr(err)
	}
	out := contracts.PublicProfile{
		PublicProfileID: toContractID(view.PublicProfileID),
		DisplayName:     cloneDisplay(view.DisplayName),
		MemberSince:     view.MemberSince,
	}
	return out, nil
}

func mapContractErr(err error) error {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, errZeroID):
		return contracts.ErrNotFound
	case errors.Is(err, ErrInvalidPublicID):
		return contracts.ErrZeroID
	case errors.Is(err, errConflict):
		return contracts.ErrConflict
	case errors.Is(err, ErrInvalidModerationState):
		return contracts.ErrInvalidModerationState
	case errors.Is(err, ErrModerationNotEnforced):
		return contracts.ErrModerationNotEnforced
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return contracts.ErrUnavailable
	}
}

func toContractID(id [16]byte) contracts.ID {
	var out contracts.ID
	copy(out[:], id[:])
	return out
}

func toIdentityID(id contracts.ID) [16]byte {
	var out [16]byte
	copy(out[:], id[:])
	return out
}

var _ contracts.PublicProfileResolver = (*Resolver)(nil)
var _ contracts.PublicProfileModeration = (*Resolver)(nil)
