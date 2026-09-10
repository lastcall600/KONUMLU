package contracts

import (
	"context"
	"errors"
)

var (
	ErrZeroID      = errors.New("master data id must not be zero")
	ErrNotFound    = errors.New("published master data form not found")
	ErrUnavailable = errors.New("master data unavailable")
)

// Value types on published attribute definitions. Extensions are new schema versions.
const (
	ValueTypeText    = "text"
	ValueTypeInteger = "integer"
	ValueTypeDecimal = "decimal"
	ValueTypeBoolean = "boolean"
	ValueTypeEnum    = "enum"
)

// ID is a Master Data category UUID. Callers must not treat this as a table handle.
type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// PublishedField is the immutable validation surface for one attribute on a published schema.
type PublishedField struct {
	Code            string
	ValueType       string
	Required        bool
	Constraints     map[string]any
	EnumOptionCodes []string
}

// PublishedForm is the minimal cross-domain DTO for listing attribute validation.
type PublishedForm struct {
	CategoryID    ID
	SchemaVersion int64
	Fields        []PublishedField
}

// PublishedFormResolver is the only Master Data read Listings may call.
type PublishedFormResolver interface {
	ResolvePublishedForm(ctx context.Context, categoryID ID, schemaVersion int64) (PublishedForm, error)
}
