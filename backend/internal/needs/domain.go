package needs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxTitleRunes       = 120
	MaxDescriptionRunes = 4000
	MinRadiusKm         = 0.1
	MaxRadiusKm         = 50
)

var (
	errZeroID            = errors.New("needs id must not be zero")
	errInvalidNeed       = errors.New("invalid need")
	errInvalidStatus     = errors.New("invalid need status")
	errInvalidTransition = errors.New("invalid need status transition")
	errInvalidContent    = errors.New("invalid need content")
	errInvalidLocation   = errors.New("invalid need location")
	errInvalidLatitude   = errors.New("invalid latitude")
	errInvalidLongitude  = errors.New("invalid longitude")
	errInvalidBudget     = errors.New("invalid need budget")
	errInvalidRadius     = errors.New("invalid need radius")
	errInvalidExpiry     = errors.New("invalid need expiry")
	errInvalidCategory   = errors.New("invalid need category")
	errStoreRequired     = errors.New("needs store required")
	errUnavailable       = errors.New("needs unavailable")
	errNotFound          = errors.New("need not found")
	errForbidden         = errors.New("need access denied")
	errConflict          = errors.New("need conflict")
)

var (
	ErrZeroID            = errZeroID
	ErrInvalidNeed       = errInvalidNeed
	ErrInvalidStatus     = errInvalidStatus
	ErrInvalidTransition = errInvalidTransition
	ErrInvalidContent    = errInvalidContent
	ErrInvalidLocation   = errInvalidLocation
	ErrInvalidLatitude   = errInvalidLatitude
	ErrInvalidLongitude  = errInvalidLongitude
	ErrInvalidBudget     = errInvalidBudget
	ErrInvalidRadius     = errInvalidRadius
	ErrInvalidExpiry     = errInvalidExpiry
	ErrInvalidCategory   = errInvalidCategory
	ErrStoreRequired     = errStoreRequired
	ErrUnavailable       = errUnavailable
	ErrNotFound          = errNotFound
	ErrForbidden         = errForbidden
	ErrConflict          = errConflict
)

var (
	amountPattern   = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,8})?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusOpen      Status = "open"
	StatusFulfilled Status = "fulfilled"
	StatusCancelled Status = "cancelled"
	StatusExpired   Status = "expired"
)

func (s Status) valid() bool {
	switch s {
	case StatusDraft, StatusOpen, StatusFulfilled, StatusCancelled, StatusExpired:
		return true
	default:
		return false
	}
}

func (s Status) Terminal() bool {
	return s == StatusFulfilled || s == StatusCancelled || s == StatusExpired
}

func (s Status) ownerEditable() bool {
	return s == StatusDraft || s == StatusOpen
}

// ID is an application-generated UUID. The database does not mint IDs.
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
		return ID{}, errInvalidNeed
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidNeed
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

// Coordinates are WGS84 decimal degrees stored as PostGIS geography Point 4326.
type Coordinates struct {
	Latitude  float64
	Longitude float64
}

func ValidateCoordinates(c Coordinates) error {
	if math.IsNaN(c.Latitude) || math.IsInf(c.Latitude, 0) {
		return errInvalidLatitude
	}
	if math.IsNaN(c.Longitude) || math.IsInf(c.Longitude, 0) {
		return errInvalidLongitude
	}
	if c.Latitude < -90 || c.Latitude > 90 {
		return errInvalidLatitude
	}
	if c.Longitude < -180 || c.Longitude > 180 {
		return errInvalidLongitude
	}
	return nil
}

type Budget struct {
	MinAmount string
	MaxAmount string
	Currency  string
}

func (b *Budget) normalized() (*Budget, error) {
	if b == nil {
		return nil, nil
	}
	minAmt := strings.TrimSpace(b.MinAmount)
	maxAmt := strings.TrimSpace(b.MaxAmount)
	cur := strings.TrimSpace(b.Currency)
	if minAmt == "" && maxAmt == "" && cur == "" {
		return nil, nil
	}
	if minAmt == "" || maxAmt == "" || cur == "" {
		return nil, errInvalidBudget
	}
	if !amountPattern.MatchString(minAmt) || !amountPattern.MatchString(maxAmt) || !currencyPattern.MatchString(cur) {
		return nil, errInvalidBudget
	}
	minRat, okMin := new(big.Rat).SetString(minAmt)
	maxRat, okMax := new(big.Rat).SetString(maxAmt)
	if !okMin || !okMax || minRat.Cmp(maxRat) > 0 {
		return nil, errInvalidBudget
	}
	return &Budget{MinAmount: minAmt, MaxAmount: maxAmt, Currency: cur}, nil
}

type Content struct {
	Title       string
	Description string
	CategoryID  *ID
	Budget      *Budget
	Location    Coordinates
	RadiusKm    *float64
	ExpiresAt   *time.Time
}

func (c Content) normalized() (Content, error) {
	title, err := normalizeTitle(c.Title)
	if err != nil {
		return Content{}, err
	}
	if title == "" {
		return Content{}, errInvalidContent
	}
	desc, err := normalizeDescription(c.Description)
	if err != nil {
		return Content{}, err
	}
	if err := ValidateCoordinates(c.Location); err != nil {
		return Content{}, err
	}
	budget, err := c.Budget.normalized()
	if err != nil {
		return Content{}, err
	}
	radius, err := normalizeRadius(c.RadiusKm)
	if err != nil {
		return Content{}, err
	}
	var category *ID
	if c.CategoryID != nil {
		if c.CategoryID.IsZero() {
			return Content{}, errZeroID
		}
		id := *c.CategoryID
		category = &id
	}
	var expires *time.Time
	if c.ExpiresAt != nil {
		if c.ExpiresAt.IsZero() {
			return Content{}, errInvalidExpiry
		}
		t := c.ExpiresAt.UTC()
		expires = &t
	}
	return Content{
		Title:       title,
		Description: desc,
		CategoryID:  category,
		Budget:      budget,
		Location:    c.Location,
		RadiusKm:    radius,
		ExpiresAt:   expires,
	}, nil
}

type Need struct {
	ID              ID
	RequesterUserID ID
	Title           string
	Description     string
	CategoryID      *ID
	Status          Status
	Budget          *Budget
	Latitude        float64
	Longitude       float64
	RadiusKm        *float64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ExpiresAt       *time.Time
}

func (n Need) Validate() error {
	if n.ID.IsZero() || n.RequesterUserID.IsZero() {
		return errZeroID
	}
	if !n.Status.valid() {
		return errInvalidStatus
	}
	content := Content{
		Title:       n.Title,
		Description: n.Description,
		CategoryID:  n.CategoryID,
		Budget:      n.Budget,
		Location:    Coordinates{Latitude: n.Latitude, Longitude: n.Longitude},
		RadiusKm:    n.RadiusKm,
		ExpiresAt:   n.ExpiresAt,
	}
	normalized, err := content.normalized()
	if err != nil {
		return err
	}
	if n.Title != normalized.Title {
		return errInvalidContent
	}
	if n.Description != normalized.Description {
		return errInvalidContent
	}
	if n.CreatedAt.IsZero() || n.UpdatedAt.Before(n.CreatedAt) {
		return errInvalidNeed
	}
	if n.ExpiresAt != nil && !n.ExpiresAt.After(n.CreatedAt) {
		return errInvalidExpiry
	}
	return nil
}

func NewDraft(requesterUserID ID, content Content, now time.Time) (Need, error) {
	if requesterUserID.IsZero() {
		return Need{}, errZeroID
	}
	if now.IsZero() {
		return Need{}, errInvalidNeed
	}
	content, err := content.normalized()
	if err != nil {
		return Need{}, err
	}
	if content.ExpiresAt != nil && !content.ExpiresAt.After(now.UTC()) {
		return Need{}, errInvalidExpiry
	}
	id, err := NewID()
	if err != nil {
		return Need{}, errUnavailable
	}
	n := Need{
		ID:              id,
		RequesterUserID: requesterUserID,
		Title:           content.Title,
		Description:     content.Description,
		CategoryID:      cloneIDPtr(content.CategoryID),
		Status:          StatusDraft,
		Budget:          cloneBudget(content.Budget),
		Latitude:        content.Location.Latitude,
		Longitude:       content.Location.Longitude,
		RadiusKm:        cloneRadius(content.RadiusKm),
		CreatedAt:       now.UTC(),
		UpdatedAt:       now.UTC(),
		ExpiresAt:       cloneTime(content.ExpiresAt),
	}
	if err := n.Validate(); err != nil {
		return Need{}, err
	}
	return n, nil
}

func (n Need) UpdateContent(content Content, now time.Time) (Need, error) {
	if !n.Status.ownerEditable() {
		return Need{}, errInvalidTransition
	}
	content, err := content.normalized()
	if err != nil {
		return Need{}, err
	}
	if now.Before(n.CreatedAt) {
		return Need{}, errInvalidNeed
	}
	if content.ExpiresAt != nil && !content.ExpiresAt.After(n.CreatedAt) {
		return Need{}, errInvalidExpiry
	}
	n.Title = content.Title
	n.Description = content.Description
	n.CategoryID = cloneIDPtr(content.CategoryID)
	n.Budget = cloneBudget(content.Budget)
	n.Latitude = content.Location.Latitude
	n.Longitude = content.Location.Longitude
	n.RadiusKm = cloneRadius(content.RadiusKm)
	n.ExpiresAt = cloneTime(content.ExpiresAt)
	n.UpdatedAt = now.UTC()
	if err := n.Validate(); err != nil {
		return Need{}, err
	}
	return n, nil
}

func (n Need) Open(now time.Time) (Need, error) {
	if n.Status != StatusDraft {
		return Need{}, errInvalidTransition
	}
	return n.transition(StatusOpen, now)
}

func (n Need) Fulfill(now time.Time) (Need, error) {
	if n.Status != StatusOpen {
		return Need{}, errInvalidTransition
	}
	return n.transition(StatusFulfilled, now)
}

// ApplyCompletedTransactionFulfillment is the transaction-driven Need outcome.
// open -> fulfilled. already fulfilled is idempotent. cancelled/expired/draft
// are not rewritten or resurrected.
func (n Need) ApplyCompletedTransactionFulfillment(now time.Time) (Need, string, error) {
	switch n.Status {
	case StatusOpen:
		next, err := n.Fulfill(now)
		if err != nil {
			return Need{}, "", err
		}
		return next, "fulfilled", nil
	case StatusFulfilled:
		return n, "already_fulfilled", nil
	default:
		return n, "not_fulfillable", nil
	}
}

func (n Need) Cancel(now time.Time) (Need, error) {
	if n.Status != StatusDraft && n.Status != StatusOpen {
		return Need{}, errInvalidTransition
	}
	return n.transition(StatusCancelled, now)
}

// Expire applies server-side expiry policy. Clients must not set status expired.
func (n Need) Expire(now time.Time) (Need, error) {
	if n.Status != StatusDraft && n.Status != StatusOpen {
		return Need{}, errInvalidTransition
	}
	if n.ExpiresAt == nil || now.UTC().Before(*n.ExpiresAt) {
		return Need{}, errInvalidTransition
	}
	return n.transition(StatusExpired, now)
}

func (n Need) transition(next Status, now time.Time) (Need, error) {
	if now.Before(n.CreatedAt) {
		return Need{}, errInvalidNeed
	}
	n.Status = next
	n.UpdatedAt = now.UTC()
	if err := n.Validate(); err != nil {
		return Need{}, err
	}
	return n, nil
}

func normalizeTitle(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errInvalidContent
	}
	if utf8.RuneCountInString(trimmed) > MaxTitleRunes {
		return "", errInvalidContent
	}
	for _, r := range trimmed {
		if r == 0x7f || unicode.IsControl(r) {
			return "", errInvalidContent
		}
	}
	return trimmed, nil
}

func normalizeDescription(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if utf8.RuneCountInString(trimmed) > MaxDescriptionRunes {
		return "", errInvalidContent
	}
	for _, r := range trimmed {
		if r == 0 || r == 0x7f {
			return "", errInvalidContent
		}
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return "", errInvalidContent
		}
	}
	return trimmed, nil
}

func normalizeRadius(raw *float64) (*float64, error) {
	if raw == nil {
		return nil, nil
	}
	v := *raw
	if math.IsNaN(v) || math.IsInf(v, 0) || v < MinRadiusKm || v > MaxRadiusKm {
		return nil, errInvalidRadius
	}
	return &v, nil
}

func cloneIDPtr(id *ID) *ID {
	if id == nil {
		return nil
	}
	cp := *id
	return &cp
}

func cloneBudget(b *Budget) *Budget {
	if b == nil {
		return nil
	}
	cp := *b
	return &cp
}

func cloneRadius(r *float64) *float64 {
	if r == nil {
		return nil
	}
	v := *r
	return &v
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cp := t.UTC()
	return &cp
}

func cloneNeed(n Need) Need {
	n.CategoryID = cloneIDPtr(n.CategoryID)
	n.Budget = cloneBudget(n.Budget)
	n.RadiusKm = cloneRadius(n.RadiusKm)
	n.ExpiresAt = cloneTime(n.ExpiresAt)
	return n
}
