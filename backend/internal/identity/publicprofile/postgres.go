package publicprofile

import (
	"context"
	"errors"
	"time"

	"backend/internal/identity"
	"backend/internal/platform/db"
)

var _ store = (*PostgresStore)(nil)

const profileSelectCols = `user_id, public_profile_id, display_name, moderation_state, created_at, updated_at`

// PostgresStore persists Identity public profiles in the identity schema.
type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

func (p *PostgresStore) GetUser(ctx context.Context, id identity.ID) (identity.User, error) {
	if p.db == nil {
		return identity.User{}, ErrUnavailable
	}
	if id.IsZero() {
		return identity.User{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, created_at, updated_at, disabled_at, deleted_at, session_epoch
		FROM identity.users
		WHERE id = $1`, id)
	var u identity.User
	if err := row.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.DisabledAt, &u.DeletedAt, &u.SessionEpoch); err != nil {
		return identity.User{}, mapStoreErr(err)
	}
	return u, nil
}

func (p *PostgresStore) GetByUserID(ctx context.Context, userID identity.ID) (Profile, error) {
	if p.db == nil {
		return Profile{}, ErrUnavailable
	}
	if userID.IsZero() {
		return Profile{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+profileSelectCols+`
		FROM identity.public_profiles
		WHERE user_id = $1`, userID)
	return scanProfile(row)
}

func (p *PostgresStore) GetByPublicID(ctx context.Context, publicID identity.ID) (storedPublic, error) {
	if p.db == nil {
		return storedPublic{}, ErrUnavailable
	}
	if publicID.IsZero() {
		return storedPublic{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT p.user_id, p.public_profile_id, p.display_name, p.moderation_state, p.created_at, p.updated_at,
			u.created_at, u.disabled_at, u.deleted_at
		FROM identity.public_profiles p
		INNER JOIN identity.users u ON u.id = p.user_id
		WHERE p.public_profile_id = $1`, publicID)
	var out storedPublic
	var display *string
	var moderation string
	if err := row.Scan(
		&out.Profile.UserID, &out.Profile.PublicProfileID, &display, &moderation,
		&out.Profile.CreatedAt, &out.Profile.UpdatedAt,
		&out.MemberSince, &out.DisabledAt, &out.DeletedAt,
	); err != nil {
		return storedPublic{}, mapStoreErr(err)
	}
	state, err := ParseModerationState(moderation)
	if err != nil {
		return storedPublic{}, ErrUnavailable
	}
	out.Profile.DisplayName = display
	out.Profile.ModerationState = state
	return out, nil
}

func (p *PostgresStore) Insert(ctx context.Context, profile Profile) error {
	if p.db == nil {
		return ErrUnavailable
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO identity.public_profiles (
			user_id, public_profile_id, display_name, moderation_state, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id) DO NOTHING`,
		profile.UserID, profile.PublicProfileID, profile.DisplayName, string(profile.ModerationState.Normalized()),
		profile.CreatedAt, profile.UpdatedAt,
	)
	return mapStoreErr(err)
}

func (p *PostgresStore) UpdateDisplayName(ctx context.Context, userID identity.ID, displayName *string, updatedAt time.Time) error {
	if p.db == nil {
		return ErrUnavailable
	}
	if userID.IsZero() || updatedAt.IsZero() {
		return ErrUnavailable
	}
	n, err := p.db.Exec(ctx, `
		UPDATE identity.public_profiles
		SET display_name = $2, updated_at = $3
		WHERE user_id = $1`, userID, displayName, updatedAt)
	if err != nil {
		return mapStoreErr(err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresStore) UpdateModeration(ctx context.Context, next Profile, expectedUpdatedAt time.Time) error {
	if p.db == nil {
		return ErrUnavailable
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if expectedUpdatedAt.IsZero() {
		return ErrUnavailable
	}
	n, err := p.db.Exec(ctx, `
		UPDATE identity.public_profiles
		SET moderation_state = $2, updated_at = $3
		WHERE public_profile_id = $1 AND updated_at = $4`,
		next.PublicProfileID, string(next.ModerationState.Normalized()), next.UpdatedAt, expectedUpdatedAt,
	)
	if err != nil {
		return mapStoreErr(err)
	}
	if n == 0 {
		_, getErr := p.GetByPublicID(ctx, next.PublicProfileID)
		if errors.Is(getErr, ErrNotFound) {
			return ErrNotFound
		}
		if getErr != nil {
			return getErr
		}
		return errConflict
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProfile(row scanner) (Profile, error) {
	var p Profile
	var display *string
	var moderation string
	if err := row.Scan(&p.UserID, &p.PublicProfileID, &display, &moderation, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Profile{}, mapStoreErr(err)
	}
	state, err := ParseModerationState(moderation)
	if err != nil {
		return Profile{}, ErrUnavailable
	}
	p.DisplayName = display
	p.ModerationState = state
	return p, nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return ErrNotFound
	}
	if errors.Is(err, db.ErrConflict) {
		return ErrUnavailable
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrNotFound) || errors.Is(err, errZeroID) ||
		errors.Is(err, ErrInvalidDisplayName) || errors.Is(err, ErrInvalidPublicID) ||
		errors.Is(err, ErrInvalidModerationState) || errors.Is(err, ErrModerationNotEnforced) ||
		errors.Is(err, errConflict) {
		return err
	}
	return ErrUnavailable
}
