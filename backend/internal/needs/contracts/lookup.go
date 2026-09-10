package contracts

import (
	"context"
	"errors"
	"time"
)

var (
	ErrZeroID      = errors.New("needs id must not be zero")
	ErrNotFound    = errors.New("need not found")
	ErrForbidden   = errors.New("need access denied")
	ErrUnavailable = errors.New("needs unavailable")
	ErrConflict    = errors.New("need conflict")
)

// DefaultMatchRadiusKm is applied when Need.radiusKm is absent.
// Offers and candidate listing must use this same default.
const DefaultMatchRadiusKm = 10.0

// ID is a Need or requester UUID. Other domains must not treat this as a table handle.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// Budget is optional Need range pricing. It has no tax or payment semantics.
type Budget struct {
	MinAmount string
	MaxAmount string
	Currency  string
}

// NeedRef is the cross-domain Need read surface.
// RequesterUserID is internal. Callers must not expose it on consumer HTTP.
type NeedRef struct {
	ID              ID
	RequesterUserID ID
	Status          string
	Title           string
	Description     string
	CategoryID      *ID
	Latitude        float64
	Longitude       float64
	RadiusKm        *float64
	Budget          *Budget
	ExpiresAt       *time.Time
}

// Lookup is the Needs-owned read surface. Callers must not import needs impl.
type Lookup interface {
	GetNeed(ctx context.Context, needID ID) (NeedRef, error)
	AssertOwnedBy(ctx context.Context, needID, userID ID) error
}
