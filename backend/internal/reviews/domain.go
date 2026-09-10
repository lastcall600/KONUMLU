package reviews

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	reviewscontracts "backend/internal/reviews/contracts"
)

const (
	MaxBodyBytes          = 4000
	MaxReviews            = 50
	MaxPublicReviews      = 20
	MinRating             = 1
	MaxRating             = 5
	DefaultCreationWindow = 24 * time.Hour

	EventTypeVerifiedCreated = reviewscontracts.EventTypeVerifiedCreated
	EventVersion             = reviewscontracts.EventVersion
)

var (
	errZeroID         = errors.New("reviews id must not be zero")
	errStoreRequired  = errors.New("reviews store required")
	errUnavailable    = errors.New("reviews unavailable")
	errNotFound       = errors.New("review not found")
	errVerifiedReq    = errors.New("verified source required")
	errOutboxRequired = errors.New("reviews outbox required")
	errInvalidBody    = errors.New("invalid review body")
	errInvalidRating  = errors.New("invalid review rating")
	errInvalidPolicy  = errors.New("invalid reviews policy")
	errInvalidReview  = errors.New("invalid review")
	errNotEligible    = errors.New("review not eligible")
	errConflict       = errors.New("review conflict")
	errSelfReview     = errors.New("self review")
	errListingsReq    = errors.New("listings source required")
	errInvalidQuery   = errors.New("invalid reviews query")
)

var (
	ErrZeroID         = errZeroID
	ErrStoreRequired  = errStoreRequired
	ErrUnavailable    = errUnavailable
	ErrNotFound       = errNotFound
	ErrVerifiedReq    = errVerifiedReq
	ErrOutboxRequired = errOutboxRequired
	ErrInvalidBody    = errInvalidBody
	ErrInvalidRating  = errInvalidRating
	ErrInvalidPolicy  = errInvalidPolicy
	ErrNotEligible    = errNotEligible
	ErrConflict       = errConflict
	ErrListingsReq    = errListingsReq
	ErrInvalidQuery   = errInvalidQuery
)

// ID is a review, interaction, listing, or user UUID. Reviews does not own those tables.
type ID [16]byte

func NewID() (ID, error) {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		return ID{}, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id, nil
}

func ParseID(s string) (ID, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(s) != 32 {
		return ID{}, errZeroID
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errZeroID
	}
	var id ID
	copy(id[:], b)
	if id.IsZero() {
		return ID{}, errZeroID
	}
	return id, nil
}

func (id ID) IsZero() bool {
	return id == ID{}
}

func (id ID) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:])
}

type Policy struct {
	CreationWindow time.Duration
}

func (p Policy) Validate() error {
	if p.CreationWindow <= 0 {
		return errInvalidPolicy
	}
	return nil
}

func DefaultPolicy() Policy {
	return Policy{CreationWindow: DefaultCreationWindow}
}

// Review is one main review for a verified listing_inspection interaction.
type Review struct {
	ID                    ID
	VerifiedInteractionID ID
	ListingID             ID
	ReviewerUserID        ID
	ProviderUserID        ID
	Body                  *string
	ListingAccuracy       int
	ProviderService       int
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// PublicReview is the public-safe listing review surface. It omits reviewer, provider, and interaction IDs.
type PublicReview struct {
	ID              ID
	Body            *string
	ListingAccuracy int
	ProviderService int
	CreatedAt       time.Time
}

type PublicListQuery struct {
	ListingID ID
	Cursor    string
	Limit     int
}

type PublicListingPage struct {
	Reviews    []PublicReview
	NextCursor string
}

func toPublicReview(row Review) PublicReview {
	out := PublicReview{
		ID:              row.ID,
		ListingAccuracy: row.ListingAccuracy,
		ProviderService: row.ProviderService,
		CreatedAt:       row.CreatedAt,
	}
	if row.Body != nil {
		v := *row.Body
		out.Body = &v
	}
	return out
}

func (r Review) Validate() error {
	if r.ID.IsZero() || r.VerifiedInteractionID.IsZero() || r.ListingID.IsZero() ||
		r.ReviewerUserID.IsZero() || r.ProviderUserID.IsZero() {
		return errZeroID
	}
	if r.ReviewerUserID == r.ProviderUserID {
		return errSelfReview
	}
	if err := ValidateRating(r.ListingAccuracy); err != nil {
		return err
	}
	if err := ValidateRating(r.ProviderService); err != nil {
		return err
	}
	if r.Body != nil {
		if _, err := NormalizeBody(*r.Body); err != nil {
			return err
		}
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return errInvalidReview
	}
	return nil
}

type Eligibility struct {
	Eligible        bool
	ExpiresAt       *time.Time
	AlreadyReviewed bool
}

type CreateInput struct {
	VerifiedInteractionID ID
	Body                  string
	ListingAccuracy       int
	ProviderService       int
}

func ValidateRating(n int) error {
	if n < MinRating || n > MaxRating {
		return errInvalidRating
	}
	return nil
}

func NormalizeBody(raw string) (*string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return nil, nil
	}
	if len(body) > MaxBodyBytes {
		return nil, errInvalidBody
	}
	return &body, nil
}

func (p Policy) ExpiresAt(verifiedAt time.Time) time.Time {
	return verifiedAt.UTC().Add(p.CreationWindow)
}

func (p Policy) WithinWindow(verifiedAt, now time.Time) bool {
	if verifiedAt.IsZero() {
		return false
	}
	return now.UTC().Before(p.ExpiresAt(verifiedAt))
}

type VerifiedCreatedPayload = reviewscontracts.VerifiedCreatedPayload

func DecodeVerifiedCreated(raw json.RawMessage) (VerifiedCreatedPayload, error) {
	return reviewscontracts.DecodeVerifiedCreated(raw)
}
