package contracts

import (
	"context"
	"errors"
)

var (
	ErrZeroID      = errors.New("businesses id must not be zero")
	ErrNotFound    = errors.New("business not found")
	ErrForbidden   = errors.New("business access denied")
	ErrUnavailable = errors.New("businesses unavailable")
)

// ID is a business or owner UUID. Other domains must not treat this as a table handle.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// ProfileRef is the minimal cross-domain business read surface.
// OwnerUserID is internal. Callers must not expose it on public HTTP.
type ProfileRef struct {
	ID          ID
	OwnerUserID ID
	Status      string
	DisplayName string
}

// Lookup is the Business Profiles read surface. Callers must not import businesses impl.
type Lookup interface {
	GetBusiness(ctx context.Context, businessID ID) (ProfileRef, error)
	AssertOwnedBy(ctx context.Context, businessID, userID ID) error
}
