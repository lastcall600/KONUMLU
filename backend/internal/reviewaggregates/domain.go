package reviewaggregates

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const (
	MinRating = 1
	MaxRating = 5
)

var (
	errZeroID         = errors.New("review aggregates id must not be zero")
	errInvalidEvent   = errors.New("invalid review aggregates event")
	errInvalidRating  = errors.New("invalid review aggregates rating")
	errStoreRequired  = errors.New("review aggregates store required")
	errUnavailable    = errors.New("review aggregates unavailable")
	errNotFound       = errors.New("review aggregates not found")
	errInvalidSummary = errors.New("invalid review aggregates summary")
)

var (
	ErrZeroID         = errZeroID
	ErrInvalidEvent   = errInvalidEvent
	ErrInvalidRating  = errInvalidRating
	ErrStoreRequired  = errStoreRequired
	ErrUnavailable    = errUnavailable
	ErrNotFound       = errNotFound
	ErrInvalidSummary = errInvalidSummary
)

// ID is a listing, user, review, or outbox event UUID copied into the projection.
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

// RatingSummary is a derived count/sum/average for one rating dimension.
type RatingSummary struct {
	ReviewCount int
	RatingSum   int
	UpdatedAt   time.Time
}

func (s RatingSummary) Validate() error {
	if s.ReviewCount < 0 || s.RatingSum < 0 {
		return errInvalidSummary
	}
	if s.ReviewCount == 0 {
		if s.RatingSum != 0 {
			return errInvalidSummary
		}
		return nil
	}
	if s.RatingSum < s.ReviewCount*MinRating || s.RatingSum > s.ReviewCount*MaxRating {
		return errInvalidSummary
	}
	if s.UpdatedAt.IsZero() {
		return errInvalidSummary
	}
	return nil
}

func (s RatingSummary) AverageNumber() *json.Number {
	if s.ReviewCount == 0 {
		return nil
	}
	n := json.Number(formatAverage(s.RatingSum, s.ReviewCount))
	return &n
}

func ZeroSummary() RatingSummary {
	return RatingSummary{}
}

func formatAverage(sum, count int) string {
	if count <= 0 {
		return "0"
	}
	r := new(big.Rat).SetFrac64(int64(sum), int64(count))
	s := strings.TrimRight(r.FloatString(8), "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-" {
		return "0"
	}
	return s
}

func validRating(v int) bool {
	return v >= MinRating && v <= MaxRating
}

// VerifiedReview is the projection input decoded from reviews.verified.created v1.
type VerifiedReview struct {
	EventID               ID
	ReviewID              ID
	VerifiedInteractionID ID
	ListingID             ID
	ReviewerUserID        ID
	ProviderUserID        ID
	ListingAccuracy       int
	ProviderService       int
	CreatedAt             time.Time
}

func (e VerifiedReview) Validate() error {
	if e.EventID.IsZero() || e.ReviewID.IsZero() || e.VerifiedInteractionID.IsZero() ||
		e.ListingID.IsZero() || e.ReviewerUserID.IsZero() || e.ProviderUserID.IsZero() {
		return errZeroID
	}
	if e.ReviewerUserID == e.ProviderUserID {
		return errInvalidEvent
	}
	if !validRating(e.ListingAccuracy) || !validRating(e.ProviderService) {
		return errInvalidRating
	}
	if e.CreatedAt.IsZero() {
		return errInvalidEvent
	}
	return nil
}
