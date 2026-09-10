package listings

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	errZeroID                = errors.New("listings id must not be zero")
	errInvalidListing        = errors.New("invalid listing")
	errInvalidStatus         = errors.New("invalid listing status")
	errInvalidTransition     = errors.New("invalid listing status transition")
	errInvalidAttributes     = errors.New("invalid listing attributes")
	errInvalidPrice          = errors.New("invalid listing price")
	errInvalidContent        = errors.New("invalid listing content")
	errInvalidCategorySchema = errors.New("invalid listing category schema")
	errPublishNotEligible    = errors.New("listing publish eligibility required")
	errInvalidModeration     = errors.New("invalid listing moderation state")
	errStoreRequired         = errors.New("listings store required")
	errUnavailable           = errors.New("listings unavailable")
	errNotFound              = errors.New("listing not found")
	errForbidden             = errors.New("listing access denied")
	errConflict              = errors.New("listing conflict")
)

// Exported sentinels for tests and later HTTP adapters.
var (
	ErrZeroID                = errZeroID
	ErrInvalidListing        = errInvalidListing
	ErrInvalidStatus         = errInvalidStatus
	ErrInvalidTransition     = errInvalidTransition
	ErrInvalidAttributes     = errInvalidAttributes
	ErrInvalidPrice          = errInvalidPrice
	ErrInvalidContent        = errInvalidContent
	ErrInvalidCategorySchema = errInvalidCategorySchema
	ErrPublishNotEligible    = errPublishNotEligible
	ErrInvalidModeration     = errInvalidModeration
	ErrStoreRequired         = errStoreRequired
	ErrUnavailable           = errUnavailable
	ErrNotFound              = errNotFound
	ErrForbidden             = errForbidden
	ErrConflict              = errConflict
)

// Status is the listing lifecycle state. Listings owns this FSM; Compliance owns EİDS.
type Status string

const (
	StatusDraft               Status = "draft"
	StatusReady               Status = "ready"
	StatusVerificationPending Status = "verification_pending"
	StatusPublished           Status = "published"
	StatusArchived            Status = "archived"

	// ModerationState is staff visibility enforcement. It is not owner archive.
	ModerationNone       ModerationState = "none"
	ModerationRestricted ModerationState = "restricted"
	ModerationRemoved    ModerationState = "removed"
)

type ModerationState string

func ParseModerationState(raw string) (ModerationState, error) {
	switch ModerationState(strings.TrimSpace(raw)) {
	case "", ModerationNone:
		return ModerationNone, nil
	case ModerationRestricted, ModerationRemoved:
		return ModerationState(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidModeration
	}
}

func (s ModerationState) valid() bool {
	switch s {
	case "", ModerationNone, ModerationRestricted, ModerationRemoved:
		return true
	default:
		return false
	}
}

func (s ModerationState) Normalized() ModerationState {
	if s == "" {
		return ModerationNone
	}
	return s
}

func (s ModerationState) hidesPublic() bool {
	n := s.Normalized()
	return n == ModerationRestricted || n == ModerationRemoved
}

func (s Status) valid() bool {
	switch s {
	case StatusDraft, StatusReady, StatusVerificationPending, StatusPublished, StatusArchived:
		return true
	default:
		return false
	}
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
		return ID{}, errInvalidListing
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidListing
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

// Attributes is controlled JSONB bound to category_schema_version. Not an EAV table.
type Attributes map[string]any

var attrKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
var priceAmountPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,8})?$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// ValidateAttributesShape rejects unconstrained JSON (arrays, nested objects, unknown key shape).
func ValidateAttributesShape(attrs Attributes) error {
	if attrs == nil {
		return errInvalidAttributes
	}
	for k, v := range attrs {
		if !attrKeyPattern.MatchString(k) {
			return errInvalidAttributes
		}
		if !validAttributeValue(v) {
			return errInvalidAttributes
		}
	}
	return nil
}

func validAttributeValue(v any) bool {
	switch x := v.(type) {
	case string, bool, json.Number:
		return true
	case float64, int, int32, int64, uint, uint32, uint64:
		return true
	case []any:
		for _, el := range x {
			switch el.(type) {
			case string, bool, json.Number, float64, int, int32, int64:
				continue
			default:
				return false
			}
		}
		return true
	case map[string]any:
		return false
	default:
		return false
	}
}

func cloneAttributes(in Attributes) Attributes {
	if in == nil {
		return Attributes{}
	}
	out := make(Attributes, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// DraftContent is original seller-entered listing content. No AI rewrite.
type DraftContent struct {
	CategoryID            ID
	CategorySchemaVersion int64
	Title                 string
	Description           string
	PriceAmount           *string
	PriceCurrency         *string
	Attributes            Attributes
}

func (c DraftContent) normalized() (DraftContent, error) {
	c.Title = strings.TrimSpace(c.Title)
	c.Description = strings.TrimSpace(c.Description)
	if c.CategoryID.IsZero() || c.CategorySchemaVersion <= 0 {
		return DraftContent{}, errInvalidContent
	}
	if err := validatePrice(c.PriceAmount, c.PriceCurrency); err != nil {
		return DraftContent{}, err
	}
	attrs := cloneAttributes(c.Attributes)
	if err := ValidateAttributesShape(attrs); err != nil {
		return DraftContent{}, err
	}
	c.Attributes = attrs
	return c, nil
}

func validatePrice(amount, currency *string) error {
	if amount == nil && currency == nil {
		return nil
	}
	if amount == nil || currency == nil {
		return errInvalidPrice
	}
	a := strings.TrimSpace(*amount)
	cur := strings.TrimSpace(*currency)
	if a == "" || !priceAmountPattern.MatchString(a) || !currencyPattern.MatchString(cur) {
		return errInvalidPrice
	}
	*amount = a
	*currency = cur
	return nil
}

func clonePrice(amount, currency *string) (*string, *string) {
	if amount == nil && currency == nil {
		return nil, nil
	}
	var a, c *string
	if amount != nil {
		v := *amount
		a = &v
	}
	if currency != nil {
		v := *currency
		c = &v
	}
	return a, c
}

// Listing is the durable listing record. Geo, media, and EİDS state live in other domains.
type Listing struct {
	ID                    ID
	OwnerUserID           ID
	Status                Status
	CategoryID            ID
	CategorySchemaVersion int64
	Title                 string
	Description           string
	PriceAmount           *string
	PriceCurrency         *string
	Attributes            Attributes
	CreatedAt             time.Time
	UpdatedAt             time.Time
	PublishedAt           *time.Time
	ArchivedAt            *time.Time
	ModerationState       ModerationState
}

func (l Listing) Validate() error {
	if l.ID.IsZero() || l.OwnerUserID.IsZero() || l.CategoryID.IsZero() {
		return errZeroID
	}
	if !l.Status.valid() {
		return errInvalidStatus
	}
	if !l.ModerationState.valid() {
		return errInvalidModeration
	}
	if l.CategorySchemaVersion <= 0 {
		return errInvalidContent
	}
	if l.Title != strings.TrimSpace(l.Title) || l.Description != strings.TrimSpace(l.Description) {
		return errInvalidContent
	}
	if err := validatePrice(clonePrice(l.PriceAmount, l.PriceCurrency)); err != nil {
		return err
	}
	if err := ValidateAttributesShape(cloneAttributes(l.Attributes)); err != nil {
		return err
	}
	if l.CreatedAt.IsZero() || l.UpdatedAt.Before(l.CreatedAt) {
		return errInvalidListing
	}
	if l.PublishedAt != nil && l.PublishedAt.Before(l.CreatedAt) {
		return errInvalidListing
	}
	if l.ArchivedAt != nil && l.ArchivedAt.Before(l.CreatedAt) {
		return errInvalidListing
	}
	if l.Status == StatusPublished && l.PublishedAt == nil {
		return errInvalidListing
	}
	if l.Status == StatusArchived && l.ArchivedAt == nil {
		return errInvalidListing
	}
	return nil
}

func NewDraft(ownerUserID ID, content DraftContent, now time.Time) (Listing, error) {
	if ownerUserID.IsZero() {
		return Listing{}, errZeroID
	}
	if now.IsZero() {
		return Listing{}, errInvalidListing
	}
	content, err := content.normalized()
	if err != nil {
		return Listing{}, err
	}
	id, err := NewID()
	if err != nil {
		return Listing{}, errUnavailable
	}
	amount, currency := clonePrice(content.PriceAmount, content.PriceCurrency)
	l := Listing{
		ID:                    id,
		OwnerUserID:           ownerUserID,
		Status:                StatusDraft,
		ModerationState:       ModerationNone,
		CategoryID:            content.CategoryID,
		CategorySchemaVersion: content.CategorySchemaVersion,
		Title:                 content.Title,
		Description:           content.Description,
		PriceAmount:           amount,
		PriceCurrency:         currency,
		Attributes:            content.Attributes,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := l.Validate(); err != nil {
		return Listing{}, err
	}
	return l, nil
}

func (l Listing) UpdateDraft(content DraftContent, now time.Time) (Listing, error) {
	if l.Status != StatusDraft {
		return Listing{}, errInvalidTransition
	}
	content, err := content.normalized()
	if err != nil {
		return Listing{}, err
	}
	if now.Before(l.CreatedAt) {
		return Listing{}, errInvalidListing
	}
	l.CategoryID = content.CategoryID
	l.CategorySchemaVersion = content.CategorySchemaVersion
	l.Title = content.Title
	l.Description = content.Description
	l.PriceAmount, l.PriceCurrency = clonePrice(content.PriceAmount, content.PriceCurrency)
	l.Attributes = content.Attributes
	l.UpdatedAt = now
	if err := l.Validate(); err != nil {
		return Listing{}, err
	}
	return l, nil
}

func (l Listing) MarkReady(now time.Time) (Listing, error) {
	if l.Status != StatusDraft {
		return Listing{}, errInvalidTransition
	}
	if strings.TrimSpace(l.Title) == "" || strings.TrimSpace(l.Description) == "" {
		return Listing{}, errInvalidContent
	}
	if now.Before(l.CreatedAt) {
		return Listing{}, errInvalidListing
	}
	l.Status = StatusReady
	l.UpdatedAt = now
	if err := l.Validate(); err != nil {
		return Listing{}, err
	}
	return l, nil
}

func (l Listing) Archive(now time.Time) (Listing, error) {
	if l.Status == StatusArchived {
		return Listing{}, errInvalidTransition
	}
	if now.Before(l.CreatedAt) {
		return Listing{}, errInvalidListing
	}
	at := now
	l.Status = StatusArchived
	l.ArchivedAt = &at
	l.UpdatedAt = now
	if err := l.Validate(); err != nil {
		return Listing{}, err
	}
	return l, nil
}

func (l Listing) PubliclyReadable() bool {
	return l.Status == StatusPublished && !l.ModerationState.hidesPublic()
}

// ApplyModeration sets staff hide/remove without changing owner lifecycle status.
// none is not applied here; restoration uses ClearModeration. Same-state apply is allowed.
func (l Listing) ApplyModeration(state ModerationState, now time.Time) (Listing, error) {
	parsed, err := ParseModerationState(string(state))
	if err != nil {
		return Listing{}, err
	}
	if parsed == ModerationNone {
		return Listing{}, errInvalidModeration
	}
	if now.Before(l.CreatedAt) {
		return Listing{}, errInvalidListing
	}
	l.ModerationState = parsed
	l.UpdatedAt = now
	if err := l.Validate(); err != nil {
		return Listing{}, err
	}
	return l, nil
}

// ClearModeration resets staff visibility to none without changing owner status.
// Already-none is idempotent and still bumps UpdatedAt so search can heal.
func (l Listing) ClearModeration(now time.Time) (Listing, error) {
	if now.Before(l.CreatedAt) {
		return Listing{}, errInvalidListing
	}
	l.ModerationState = ModerationNone
	l.UpdatedAt = now
	if err := l.Validate(); err != nil {
		return Listing{}, err
	}
	return l, nil
}

// Publish requires caller-provided verification eligibility. Listings does not call EİDS.
func (l Listing) Publish(now time.Time, eligible bool) (Listing, error) {
	if !eligible {
		return Listing{}, errPublishNotEligible
	}
	if l.Status != StatusReady && l.Status != StatusVerificationPending {
		return Listing{}, errInvalidTransition
	}
	if strings.TrimSpace(l.Title) == "" || strings.TrimSpace(l.Description) == "" {
		return Listing{}, errInvalidContent
	}
	if now.Before(l.CreatedAt) {
		return Listing{}, errInvalidListing
	}
	at := now
	l.Status = StatusPublished
	l.PublishedAt = &at
	l.UpdatedAt = now
	if err := l.Validate(); err != nil {
		return Listing{}, err
	}
	return l, nil
}
