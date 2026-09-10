package publicprofile

import (
	"context"
	"time"

	"backend/internal/identity"
)

type storedPublic struct {
	Profile     Profile
	MemberSince time.Time
	DisabledAt  *time.Time
	DeletedAt   *time.Time
}

func (s storedPublic) accountPresent() bool {
	return s.Profile.Validate() == nil && s.DisabledAt == nil && s.DeletedAt == nil && !s.MemberSince.IsZero()
}

func (s storedPublic) publiclyVisible() bool {
	return s.accountPresent() && !s.Profile.ModerationState.hidesPublic()
}

type store interface {
	GetUser(ctx context.Context, id identity.ID) (identity.User, error)
	GetByUserID(ctx context.Context, userID identity.ID) (Profile, error)
	GetByPublicID(ctx context.Context, publicID identity.ID) (storedPublic, error)
	Insert(ctx context.Context, profile Profile) error
	UpdateDisplayName(ctx context.Context, userID identity.ID, displayName *string, updatedAt time.Time) error
	UpdateModeration(ctx context.Context, next Profile, expectedUpdatedAt time.Time) error
}
