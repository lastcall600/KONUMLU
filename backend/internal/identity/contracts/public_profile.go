package contracts

import (
	"context"
	"errors"
	"time"
)

var (
	ErrZeroID      = errors.New("identity public profile id must not be zero")
	ErrNotFound    = errors.New("identity public profile not found")
	ErrUnavailable = errors.New("identity public profile unavailable")
)

// ID is an opaque public-profile or user UUID as understood by Identity contracts.
// Other domains must not treat this as an Identity table handle.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// PublicProfile is the public-safe Identity surface for Trust passport, listing
// seller display, and future public reviewer identity. It omits internal user id,
// email, phone, credentials, and legal identity.
type PublicProfile struct {
	PublicProfileID ID
	DisplayName     *string
	MemberSince     time.Time
}

// PublicProfileResolver is the only Identity public-profile surface other domains may call.
// Implementations live in identity/publicprofile; callers must not import that package.
type PublicProfileResolver interface {
	ResolveByPublicID(ctx context.Context, publicProfileID ID) (PublicProfile, error)
	ResolveByUserID(ctx context.Context, userID ID) (PublicProfile, error)
	// ResolveUserIDByPublicID is a server-side mapping for composition.
	// Callers must not put the returned user id on public HTTP or URLs.
	ResolveUserIDByPublicID(ctx context.Context, publicProfileID ID) (ID, error)
}
