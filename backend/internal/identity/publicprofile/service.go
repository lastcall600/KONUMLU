package publicprofile

import (
	"context"
	"errors"
	"time"

	"backend/internal/identity"
	"backend/internal/identity/contracts"
)

const publicIDInsertAttempts = 5

// Service owns public profile identity fields.
//
// Profile rows are lazy-created on first self read/update or contract ResolveByUserID.
// Signup account creation is left unchanged. Public lookup never creates a row.
type Service struct {
	store store
	now   func() time.Time
}

func NewService(store store, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, ErrStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, now: now}, nil
}

func (s *Service) GetMe(ctx context.Context, userID identity.ID) (PublicView, error) {
	profile, user, err := s.ensureEligible(ctx, userID)
	if err != nil {
		return PublicView{}, err
	}
	return toPublicView(profile, user.CreatedAt), nil
}

func (s *Service) publicViewIfVisible(ctx context.Context, userID identity.ID) (PublicView, error) {
	view, err := s.GetMe(ctx, userID)
	if err != nil {
		return PublicView{}, err
	}
	profile, err := s.store.GetByUserID(ctx, userID)
	if err != nil {
		return PublicView{}, mapServiceErr(err)
	}
	if profile.ModerationState.hidesPublic() {
		return PublicView{}, ErrNotFound
	}
	return view, nil
}

func (s *Service) UpdateMe(ctx context.Context, userID identity.ID, displayName *string) (PublicView, error) {
	normalized, err := optionalDisplayName(displayName)
	if err != nil {
		return PublicView{}, err
	}
	if _, _, err := s.ensureEligible(ctx, userID); err != nil {
		return PublicView{}, err
	}
	if displayName != nil {
		if err := s.store.UpdateDisplayName(ctx, userID, normalized, s.now()); err != nil {
			return PublicView{}, mapServiceErr(err)
		}
	}
	return s.GetMe(ctx, userID)
}

func (s *Service) GetPublic(ctx context.Context, publicProfileID identity.ID) (PublicView, error) {
	got, err := s.visibleByPublicID(ctx, publicProfileID)
	if err != nil {
		return PublicView{}, err
	}
	return toPublicView(got.Profile, got.MemberSince), nil
}

func (s *Service) ResolveUserIDByPublicID(ctx context.Context, publicProfileID identity.ID) (identity.ID, error) {
	got, err := s.lookupByPublicID(ctx, publicProfileID)
	if err != nil {
		return identity.ID{}, err
	}
	if !got.accountPresent() {
		return identity.ID{}, ErrNotFound
	}
	return got.Profile.UserID, nil
}

// StaffByPublicID returns an operational view, including restricted/disabled profiles.
// It never lazy-creates a row and never includes the internal user id.
func (s *Service) StaffByPublicID(ctx context.Context, publicProfileID identity.ID) (StaffView, error) {
	got, err := s.lookupByPublicID(ctx, publicProfileID)
	if err != nil {
		return StaffView{}, err
	}
	return toStaffView(got), nil
}

// StaffByUserID returns an operational view for a listing owner mapping.
// Missing profiles are not created.
func (s *Service) StaffByUserID(ctx context.Context, userID identity.ID) (StaffView, error) {
	if s == nil || s.store == nil {
		return StaffView{}, ErrStoreRequired
	}
	if userID.IsZero() {
		return StaffView{}, errZeroID
	}
	profile, err := s.store.GetByUserID(ctx, userID)
	if err != nil {
		return StaffView{}, mapServiceErr(err)
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return StaffView{}, mapServiceErr(err)
	}
	return toStaffView(storedPublic{
		Profile:     profile,
		MemberSince: user.CreatedAt,
		DisabledAt:  user.DisabledAt,
		DeletedAt:   user.DeletedAt,
	}), nil
}

// StaffUserIDByPublicID maps a public profile id for server-side composition,
// including disabled or deleted accounts. Callers must not expose the user id.
func (s *Service) StaffUserIDByPublicID(ctx context.Context, publicProfileID identity.ID) (identity.ID, error) {
	got, err := s.lookupByPublicID(ctx, publicProfileID)
	if err != nil {
		return identity.ID{}, err
	}
	if got.Profile.UserID.IsZero() {
		return identity.ID{}, ErrNotFound
	}
	return got.Profile.UserID, nil
}

func (s *Service) ApplyModerationState(ctx context.Context, in contracts.ApplyPublicProfileModerationInput) error {
	if err := in.Validate(); err != nil {
		return mapContractApplyErr(err)
	}
	state, err := ParseModerationState(in.State)
	if err != nil {
		return mapContractApplyErr(err)
	}
	current, err := s.lookupByPublicID(ctx, toIdentityID(in.PublicProfileID))
	if err != nil {
		return mapContractApplyErr(err)
	}
	next, err := current.Profile.ApplyModeration(state, s.now().UTC())
	if err != nil {
		return mapContractApplyErr(err)
	}
	if err := s.store.UpdateModeration(ctx, next, current.Profile.UpdatedAt); err != nil {
		return mapContractApplyErr(err)
	}
	return nil
}

func (s *Service) ClearModerationState(ctx context.Context, publicProfileID contracts.ID) error {
	if publicProfileID.IsZero() {
		return contracts.ErrZeroID
	}
	current, err := s.lookupByPublicID(ctx, toIdentityID(publicProfileID))
	if err != nil {
		return mapContractApplyErr(err)
	}
	next, err := current.Profile.ClearModeration(s.now().UTC())
	if err != nil {
		return mapContractApplyErr(err)
	}
	if err := s.store.UpdateModeration(ctx, next, current.Profile.UpdatedAt); err != nil {
		return mapContractApplyErr(err)
	}
	return nil
}

func (s *Service) visibleByPublicID(ctx context.Context, publicProfileID identity.ID) (storedPublic, error) {
	got, err := s.lookupByPublicID(ctx, publicProfileID)
	if err != nil {
		return storedPublic{}, err
	}
	if !got.publiclyVisible() {
		return storedPublic{}, ErrNotFound
	}
	return got, nil
}

func (s *Service) lookupByPublicID(ctx context.Context, publicProfileID identity.ID) (storedPublic, error) {
	if s == nil || s.store == nil {
		return storedPublic{}, ErrStoreRequired
	}
	if publicProfileID.IsZero() {
		return storedPublic{}, ErrInvalidPublicID
	}
	got, err := s.store.GetByPublicID(ctx, publicProfileID)
	if err != nil {
		return storedPublic{}, mapServiceErr(err)
	}
	return got, nil
}

func (s *Service) ensureEligible(ctx context.Context, userID identity.ID) (Profile, identity.User, error) {
	if s == nil || s.store == nil {
		return Profile{}, identity.User{}, ErrStoreRequired
	}
	if userID.IsZero() {
		return Profile{}, identity.User{}, errZeroID
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return Profile{}, identity.User{}, mapServiceErr(err)
	}
	if !user.EligibleForSession() {
		return Profile{}, identity.User{}, ErrNotFound
	}
	profile, err := s.ensureRow(ctx, user)
	if err != nil {
		return Profile{}, identity.User{}, err
	}
	return profile, user, nil
}

func (s *Service) ensureRow(ctx context.Context, user identity.User) (Profile, error) {
	existing, err := s.store.GetByUserID(ctx, user.ID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Profile{}, mapServiceErr(err)
	}
	now := s.now()
	var last error
	for i := 0; i < publicIDInsertAttempts; i++ {
		publicID, err := identity.NewID()
		if err != nil {
			return Profile{}, ErrUnavailable
		}
		if publicID == user.ID {
			continue
		}
		row := Profile{
			UserID:          user.ID,
			PublicProfileID: publicID,
			ModerationState: ModerationNone,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := s.store.Insert(ctx, row); err != nil {
			last = mapServiceErr(err)
			continue
		}
		got, err := s.store.GetByUserID(ctx, user.ID)
		if err != nil {
			return Profile{}, mapServiceErr(err)
		}
		return got, nil
	}
	if last != nil {
		return Profile{}, last
	}
	return Profile{}, ErrUnavailable
}

func optionalDisplayName(in *string) (*string, error) {
	if in == nil {
		return nil, nil
	}
	return NormalizeDisplayName(*in)
}

func toPublicView(p Profile, memberSince time.Time) PublicView {
	return PublicView{
		PublicProfileID: p.PublicProfileID,
		DisplayName:     cloneDisplay(p.DisplayName),
		MemberSince:     memberSince.UTC(),
	}
}

func toStaffView(got storedPublic) StaffView {
	disabled := got.DisabledAt != nil
	deleted := got.DeletedAt != nil
	return StaffView{
		PublicProfileID: got.Profile.PublicProfileID,
		DisplayName:     cloneDisplay(got.Profile.DisplayName),
		ModerationState: got.Profile.ModerationState.Normalized(),
		MemberSince:     got.MemberSince.UTC(),
		CreatedAt:       got.Profile.CreatedAt.UTC(),
		UpdatedAt:       got.Profile.UpdatedAt.UTC(),
		AccountEligible: got.accountPresent(),
		Disabled:        disabled,
		Deleted:         deleted,
	}
}

func mapServiceErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnavailable) || errors.Is(err, ErrStoreRequired) ||
		errors.Is(err, ErrInvalidPublicID) || errors.Is(err, ErrInvalidDisplayName) || errors.Is(err, errZeroID) ||
		errors.Is(err, ErrInvalidModerationState) || errors.Is(err, ErrModerationNotEnforced) ||
		errors.Is(err, errConflict) {
		return err
	}
	return ErrUnavailable
}

func mapContractApplyErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) || errors.Is(err, ErrInvalidPublicID) {
		return contracts.ErrZeroID
	}
	if errors.Is(err, ErrNotFound) {
		return contracts.ErrNotFound
	}
	if errors.Is(err, errConflict) {
		return contracts.ErrConflict
	}
	if errors.Is(err, ErrInvalidModerationState) {
		return contracts.ErrInvalidModerationState
	}
	if errors.Is(err, ErrModerationNotEnforced) {
		return contracts.ErrModerationNotEnforced
	}
	if errors.Is(err, contracts.ErrZeroID) || errors.Is(err, contracts.ErrNotFound) ||
		errors.Is(err, contracts.ErrConflict) || errors.Is(err, contracts.ErrInvalidModerationState) ||
		errors.Is(err, contracts.ErrModerationNotEnforced) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return contracts.ErrUnavailable
}

var _ contracts.PublicProfileModeration = (*Service)(nil)
