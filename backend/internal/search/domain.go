package search

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	listingcontracts "backend/internal/listings/contracts"
)

var (
	errZeroID         = errors.New("search listing id must not be zero")
	errInvalidDoc     = errors.New("invalid search document")
	errInvalidEvent   = errors.New("invalid search event")
	errInvalidQuery   = errors.New("invalid search query")
	errStoreRequired  = errors.New("search store required")
	errUnavailable    = errors.New("search unavailable")
	errNotFound       = errors.New("search document not found")
	errSourceRequired = errors.New("search source required")
)

var (
	ErrZeroID         = errZeroID
	ErrInvalidDoc     = errInvalidDoc
	ErrInvalidEvent   = errInvalidEvent
	ErrInvalidQuery   = errInvalidQuery
	ErrStoreRequired  = errStoreRequired
	ErrUnavailable    = errUnavailable
	ErrNotFound       = errNotFound
	ErrSourceRequired = errSourceRequired
)

const StatusPublished = "published"

// ID is a listing UUID copied into the derived projection.
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
		return ID{}, errInvalidEvent
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidEvent
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

// ListingDocument is a derived searchable listing. It is not canonical.
type ListingDocument struct {
	ListingID             ID
	Status                string
	ModerationState       string
	CategoryID            ID
	CategorySchemaVersion int64
	Title                 string
	Description           string
	PriceAmount           *string
	PriceCurrency         *string
	Attributes            map[string]any
	Latitude              *float64
	Longitude             *float64
	CatalogLocationID     *ID
	PublishedAt           *time.Time
	UpdatedAt             time.Time
}

func (d ListingDocument) Searchable() bool {
	return listingcontracts.PubliclyVisible(d.Status, d.ModerationState)
}

func (d ListingDocument) Validate() error {
	if d.ListingID.IsZero() || d.CategoryID.IsZero() {
		return errZeroID
	}
	if strings.TrimSpace(d.Status) == "" {
		return errInvalidDoc
	}
	if d.CategorySchemaVersion <= 0 {
		return errInvalidDoc
	}
	if d.UpdatedAt.IsZero() {
		return errInvalidDoc
	}
	if (d.PriceAmount == nil) != (d.PriceCurrency == nil) {
		return errInvalidDoc
	}
	if d.Attributes == nil {
		return errInvalidDoc
	}
	if (d.Latitude == nil) != (d.Longitude == nil) {
		return errInvalidDoc
	}
	if d.CatalogLocationID != nil && d.CatalogLocationID.IsZero() {
		return errZeroID
	}
	return nil
}

func cloneDoc(d ListingDocument) ListingDocument {
	d.PriceAmount, d.PriceCurrency = clonePrice(d.PriceAmount, d.PriceCurrency)
	if d.Attributes != nil {
		attrs := make(map[string]any, len(d.Attributes))
		for k, v := range d.Attributes {
			attrs[k] = v
		}
		d.Attributes = attrs
	}
	d.Latitude = cloneFloat(d.Latitude)
	d.Longitude = cloneFloat(d.Longitude)
	if d.CatalogLocationID != nil {
		id := *d.CatalogLocationID
		d.CatalogLocationID = &id
	}
	if d.PublishedAt != nil {
		t := *d.PublishedAt
		d.PublishedAt = &t
	}
	return d
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

func cloneFloat(v *float64) *float64 {
	if v == nil {
		return nil
	}
	x := *v
	return &x
}
