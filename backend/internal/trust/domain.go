package trust

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	verifiedcontracts "backend/internal/verified/contracts"
)

const (
	LevelNew         = "new"
	LevelVerified    = "verified"
	LevelEstablished = "established"

	RoleRequester = "requester"
	RoleProvider  = "provider"

	MinRating = 1
	MaxRating = 5
)

var (
	errZeroID         = errors.New("trust id must not be zero")
	errInvalidEvent   = errors.New("invalid trust event")
	errInvalidPolicy  = errors.New("invalid trust policy")
	errInvalidProfile = errors.New("invalid trust profile")
	errStoreRequired  = errors.New("trust store required")
	errUnavailable    = errors.New("trust unavailable")
	errNotFound       = errors.New("trust profile not found")
)

var (
	ErrZeroID         = errZeroID
	ErrInvalidEvent   = errInvalidEvent
	ErrInvalidPolicy  = errInvalidPolicy
	ErrInvalidProfile = errInvalidProfile
	ErrStoreRequired  = errStoreRequired
	ErrUnavailable    = errUnavailable
	ErrNotFound       = errNotFound
)

// ID is a user, interaction, listing, or outbox event UUID copied into the
// derived projection. Trust does not own identity or verified tables.
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

// LevelPolicy maps listing_inspection verified-interaction counts to a
// transparent level. Thresholds are provisional defaults, not a frozen
// product score. Transaction, delivery, unknown types, review ratings, and
// averages are never inputs to Level().
type LevelPolicy struct {
	VerifiedMin    int
	EstablishedMin int
}

func DefaultLevelPolicy() LevelPolicy {
	return LevelPolicy{VerifiedMin: 1, EstablishedMin: 5}
}

func (p LevelPolicy) Validate() error {
	if p.VerifiedMin < 1 || p.EstablishedMin <= p.VerifiedMin {
		return errInvalidPolicy
	}
	return nil
}

func (p LevelPolicy) Level(verifiedCount int) string {
	if verifiedCount >= p.EstablishedMin {
		return LevelEstablished
	}
	if verifiedCount >= p.VerifiedMin {
		return LevelVerified
	}
	return LevelNew
}

// UserProfile is a derived, rebuildable trust projection.
// TrustLevel is computed only from VerifiedInteractionCount, which counts
// listing_inspection completions only (the pre-0B-80 population).
// Transaction and delivery completions are ingested into history but do not
// increment these counters or change TrustLevel. Verified-review counters and
// ProviderServiceAverage are displayed separately and are not weighted into a
// hidden score. Listing accuracy is not stored here.
type UserProfile struct {
	UserID                            ID
	VerifiedInteractionCount          int
	ProviderVerifiedInteractionCount  int
	RequesterVerifiedInteractionCount int
	LastVerifiedInteractionAt         *time.Time
	VerifiedReviewCount               int
	ProviderServiceReviewCount        int
	ProviderServiceRatingSum          int
	LastVerifiedReviewAt              *time.Time
	LastProviderServiceReviewAt       *time.Time
	TrustLevel                        string
	UpdatedAt                         time.Time
}

func (p UserProfile) Validate() error {
	if p.UserID.IsZero() {
		return errZeroID
	}
	if p.VerifiedInteractionCount < 0 || p.ProviderVerifiedInteractionCount < 0 || p.RequesterVerifiedInteractionCount < 0 {
		return errInvalidProfile
	}
	if p.VerifiedInteractionCount != p.ProviderVerifiedInteractionCount+p.RequesterVerifiedInteractionCount {
		return errInvalidProfile
	}
	if p.VerifiedReviewCount < 0 || p.ProviderServiceReviewCount < 0 || p.ProviderServiceRatingSum < 0 {
		return errInvalidProfile
	}
	if p.ProviderServiceReviewCount == 0 {
		if p.ProviderServiceRatingSum != 0 {
			return errInvalidProfile
		}
	} else if p.ProviderServiceRatingSum < p.ProviderServiceReviewCount*MinRating ||
		p.ProviderServiceRatingSum > p.ProviderServiceReviewCount*MaxRating {
		return errInvalidProfile
	}
	if !validLevel(p.TrustLevel) {
		return errInvalidProfile
	}
	if p.UpdatedAt.IsZero() {
		return errInvalidProfile
	}
	return nil
}

// ProviderServiceAverage is a deterministic sum/count display value.
// Nil when the user has received no verified provider-service ratings.
func (p UserProfile) ProviderServiceAverage() *json.Number {
	if p.ProviderServiceReviewCount == 0 {
		return nil
	}
	n := json.Number(formatAverage(p.ProviderServiceRatingSum, p.ProviderServiceReviewCount))
	return &n
}

func validLevel(level string) bool {
	switch level {
	case LevelNew, LevelVerified, LevelEstablished:
		return true
	default:
		return false
	}
}

func ZeroProfile(userID ID, now time.Time) UserProfile {
	return UserProfile{
		UserID:     userID,
		TrustLevel: LevelNew,
		UpdatedAt:  now.UTC(),
	}
}

// PublicPassport is the public-safe Trust surface. It omits internal user id,
// requester-only counters, authored review counts, event ids, and history.
type PublicPassport struct {
	Level                            string
	VerifiedInteractionCount         int
	ProviderVerifiedInteractionCount int
	ProviderServiceReviewCount       int
	ProviderServiceAverage           *json.Number
	LastVerifiedInteractionAt        *time.Time
}

func ToPublicPassport(p UserProfile) PublicPassport {
	return PublicPassport{
		Level:                            p.TrustLevel,
		VerifiedInteractionCount:         p.VerifiedInteractionCount,
		ProviderVerifiedInteractionCount: p.ProviderVerifiedInteractionCount,
		ProviderServiceReviewCount:       p.ProviderServiceReviewCount,
		ProviderServiceAverage:           p.ProviderServiceAverage(),
		LastVerifiedInteractionAt:        p.LastVerifiedInteractionAt,
	}
}

type HistoryEntry struct {
	InteractionID      ID
	UserID             ID
	Role               string
	ListingID          ID
	InteractionType    string
	VerificationMethod string
	VerifiedAt         time.Time
}

func (h HistoryEntry) Validate() error {
	if h.InteractionID.IsZero() || h.UserID.IsZero() || h.ListingID.IsZero() {
		return errZeroID
	}
	if h.Role != RoleRequester && h.Role != RoleProvider {
		return errInvalidEvent
	}
	if !knownInteractionType(h.InteractionType) || strings.TrimSpace(h.VerificationMethod) == "" {
		return errInvalidEvent
	}
	if h.VerifiedAt.IsZero() {
		return errInvalidEvent
	}
	return nil
}

type CompletedInteraction struct {
	EventID            ID
	InteractionID      ID
	ListingID          ID
	RequesterUserID    ID
	ProviderUserID     ID
	InteractionType    string
	VerificationMethod string
	VerifiedAt         time.Time
}

func (c CompletedInteraction) Validate() error {
	if c.EventID.IsZero() || c.InteractionID.IsZero() || c.ListingID.IsZero() ||
		c.RequesterUserID.IsZero() || c.ProviderUserID.IsZero() {
		return errZeroID
	}
	if c.RequesterUserID == c.ProviderUserID {
		return errInvalidEvent
	}
	if !knownInteractionType(c.InteractionType) || strings.TrimSpace(c.VerificationMethod) == "" {
		return errInvalidEvent
	}
	if c.VerifiedAt.IsZero() {
		return errInvalidEvent
	}
	return nil
}

func knownInteractionType(typ string) bool {
	switch typ {
	case verifiedcontracts.InteractionTypeListingInspection,
		verifiedcontracts.InteractionTypeTransaction,
		verifiedcontracts.InteractionTypeDelivery:
		return true
	default:
		return false
	}
}

// countsTowardTrustLevel is the pre-0B-80 scoring population: listing_inspection only.
func countsTowardTrustLevel(typ string) bool {
	return typ == verifiedcontracts.InteractionTypeListingInspection
}

// VerifiedReview is projection input decoded from reviews.verified.created v1.
// ListingAccuracy is accepted from the contract DTO and discarded: it belongs
// to listing/review aggregates, not the user Trust profile.
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
	if e.ProviderService < MinRating || e.ProviderService > MaxRating {
		return errInvalidEvent
	}
	if e.ListingAccuracy < MinRating || e.ListingAccuracy > MaxRating {
		return errInvalidEvent
	}
	if e.CreatedAt.IsZero() {
		return errInvalidEvent
	}
	return nil
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
