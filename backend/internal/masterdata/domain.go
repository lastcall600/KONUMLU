package masterdata

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var (
	errZeroID                 = errors.New("master data id must not be zero")
	errInvalidCategory        = errors.New("invalid category")
	errInvalidSchema          = errors.New("invalid category schema")
	errInvalidAttribute       = errors.New("invalid attribute")
	errInvalidOption          = errors.New("invalid attribute option")
	errInvalidLabel           = errors.New("invalid label")
	errInvalidLocale          = errors.New("invalid locale")
	errInvalidCode            = errors.New("invalid master data code")
	errInvalidEIDSRequirement = errors.New("invalid eids category requirement")
	errInvalidStatus          = errors.New("invalid master data status")
	errInvalidTransition      = errors.New("invalid master data status transition")
	errInvalidConstraints     = errors.New("invalid attribute constraints")
	errImmutablePublished     = errors.New("published schema is immutable")
	errEnumRequiresOptions    = errors.New("enum attribute requires options")
	errOptionsNotAllowed      = errors.New("non-enum attribute cannot have options")
	errStoreRequired          = errors.New("master data store required")
	errUnavailable            = errors.New("master data unavailable")
	errNotFound               = errors.New("master data not found")
	errConflict               = errors.New("master data conflict")
)

// Exported sentinels for tests and later HTTP adapters.
var (
	ErrZeroID                 = errZeroID
	ErrInvalidCategory        = errInvalidCategory
	ErrInvalidSchema          = errInvalidSchema
	ErrInvalidAttribute       = errInvalidAttribute
	ErrInvalidOption          = errInvalidOption
	ErrInvalidLabel           = errInvalidLabel
	ErrInvalidLocale          = errInvalidLocale
	ErrInvalidCode            = errInvalidCode
	ErrInvalidEIDSRequirement = errInvalidEIDSRequirement
	ErrInvalidStatus          = errInvalidStatus
	ErrInvalidTransition      = errInvalidTransition
	ErrInvalidConstraints     = errInvalidConstraints
	ErrImmutablePublished     = errImmutablePublished
	ErrEnumRequiresOptions    = errEnumRequiresOptions
	ErrOptionsNotAllowed      = errOptionsNotAllowed
	ErrStoreRequired          = errStoreRequired
	ErrUnavailable            = errUnavailable
	ErrNotFound               = errNotFound
	ErrConflict               = errConflict
)

// Status is the DRAFT → REVIEW → APPROVED → PUBLISHED lifecycle.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusReview    Status = "review"
	StatusApproved  Status = "approved"
	StatusPublished Status = "published"
)

func (s Status) valid() bool {
	switch s {
	case StatusDraft, StatusReview, StatusApproved, StatusPublished:
		return true
	default:
		return false
	}
}

func (s Status) editable() bool {
	return s == StatusDraft
}

// EIDSRequirement is the server-side listing EİDS policy for a category.
// Property and vehicle stay separate. This is not person/e-Devlet identity.
type EIDSRequirement string

const (
	EIDSRequirementNone     EIDSRequirement = "none"
	EIDSRequirementProperty EIDSRequirement = "property"
	EIDSRequirementVehicle  EIDSRequirement = "vehicle"
)

func (r EIDSRequirement) normalized() EIDSRequirement {
	if r == "" {
		return EIDSRequirementNone
	}
	return r
}

func (r EIDSRequirement) valid() bool {
	switch r.normalized() {
	case EIDSRequirementNone, EIDSRequirementProperty, EIDSRequirementVehicle:
		return true
	default:
		return false
	}
}

func ParseEIDSRequirement(raw string) (EIDSRequirement, error) {
	r := EIDSRequirement(strings.TrimSpace(raw))
	if r == "" {
		return EIDSRequirementNone, nil
	}
	if !r.valid() {
		return "", errInvalidEIDSRequirement
	}
	return r, nil
}

// Locale is a supported taxonomy label language (D-017 / ADR-012).
type Locale string

const (
	LocaleTR Locale = "tr"
	LocaleEN Locale = "en"
	LocaleRU Locale = "ru"
	LocaleAR Locale = "ar"
)

func (l Locale) valid() bool {
	switch l {
	case LocaleTR, LocaleEN, LocaleRU, LocaleAR:
		return true
	default:
		return false
	}
}

// ParseLocale accepts tr|en|ru|ar. Empty defaults to tr. Values are not case-folded.
func ParseLocale(s string) (Locale, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return LocaleTR, nil
	}
	l := Locale(s)
	if !l.valid() {
		return "", errInvalidLocale
	}
	return l, nil
}

// ValueType is a closed attribute type set. Extensions require a new schema version.
type ValueType string

const (
	ValueTypeText    ValueType = "text"
	ValueTypeInteger ValueType = "integer"
	ValueTypeDecimal ValueType = "decimal"
	ValueTypeBoolean ValueType = "boolean"
	ValueTypeEnum    ValueType = "enum"
)

func (t ValueType) valid() bool {
	switch t {
	case ValueTypeText, ValueTypeInteger, ValueTypeDecimal, ValueTypeBoolean, ValueTypeEnum:
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
		return ID{}, errInvalidCategory
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidCategory
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

var codePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func NormalizeCode(code string) (string, error) {
	code = strings.TrimSpace(code)
	if !codePattern.MatchString(code) {
		return "", errInvalidCode
	}
	return code, nil
}

// Constraints is a controlled JSON object on an attribute definition. Not EAV.
type Constraints map[string]any

func ValidateConstraints(c Constraints) error {
	if c == nil {
		return errInvalidConstraints
	}
	for k, v := range c {
		if !codePattern.MatchString(k) {
			return errInvalidConstraints
		}
		if !validConstraintValue(v) {
			return errInvalidConstraints
		}
	}
	return nil
}

func validConstraintValue(v any) bool {
	switch v.(type) {
	case string, bool, json.Number, float64, float32, int, int32, int64, uint, uint32, uint64:
		return true
	default:
		return false
	}
}

func cloneConstraints(in Constraints) Constraints {
	if in == nil {
		return Constraints{}
	}
	out := make(Constraints, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneIDPtr(id *ID) *ID {
	if id == nil {
		return nil
	}
	cp := *id
	return &cp
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

func trimLabel(s string) string {
	return strings.TrimSpace(s)
}

func validateLabelText(s string) error {
	if trimLabel(s) == "" {
		return errInvalidLabel
	}
	return nil
}

// PreserveTurkishRunes keeps dotted/dotless I intact. English ToLower is forbidden on TR labels.
func PreserveTurkishRunes(s string) string {
	return s
}

// TurkishFold maps Turkish İ/I/i/ı without Unicode default I→i corruption.
func TurkishFold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 'I':
			b.WriteRune('ı')
		case 'İ':
			b.WriteRune('i')
		case 'ı', 'i':
			b.WriteRune(r)
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func EqualTurkishFold(a, b string) bool {
	return TurkishFold(a) == TurkishFold(b)
}

// Category is a taxonomy node. Listings store this ID, not the tree.
type Category struct {
	ID              ID
	Code            string
	ParentID        *ID
	Status          Status
	EIDSRequirement EIDSRequirement
	CreatedAt       time.Time
	UpdatedAt       time.Time
	PublishedAt     *time.Time
}

func (c Category) Validate() error {
	if c.ID.IsZero() {
		return errZeroID
	}
	if _, err := NormalizeCode(c.Code); err != nil {
		return err
	}
	if c.ParentID != nil && (c.ParentID.IsZero() || *c.ParentID == c.ID) {
		return errInvalidCategory
	}
	if !c.Status.valid() {
		return errInvalidStatus
	}
	req := c.EIDSRequirement
	if req == "" {
		req = EIDSRequirementNone
	}
	if !req.valid() {
		return errInvalidEIDSRequirement
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		return errInvalidCategory
	}
	if c.Status == StatusPublished {
		if c.PublishedAt == nil || c.PublishedAt.Before(c.CreatedAt) {
			return errInvalidCategory
		}
	} else if c.PublishedAt != nil {
		return errInvalidCategory
	}
	return nil
}

func NewCategory(code string, parentID *ID, now time.Time) (Category, error) {
	code, err := NormalizeCode(code)
	if err != nil {
		return Category{}, err
	}
	if parentID != nil && parentID.IsZero() {
		return Category{}, errZeroID
	}
	if now.IsZero() {
		return Category{}, errInvalidCategory
	}
	id, err := NewID()
	if err != nil {
		return Category{}, errUnavailable
	}
	c := Category{
		ID:              id,
		Code:            code,
		ParentID:        cloneIDPtr(parentID),
		Status:          StatusDraft,
		EIDSRequirement: EIDSRequirementNone,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := c.Validate(); err != nil {
		return Category{}, err
	}
	return c, nil
}

func (c Category) WithEIDSRequirement(req EIDSRequirement, now time.Time) (Category, error) {
	if !req.valid() {
		return Category{}, errInvalidEIDSRequirement
	}
	if now.Before(c.CreatedAt) {
		return Category{}, errInvalidCategory
	}
	c.EIDSRequirement = req
	c.UpdatedAt = now
	if err := c.Validate(); err != nil {
		return Category{}, err
	}
	return c, nil
}

func (c Category) SubmitForReview(now time.Time) (Category, error) {
	if c.Status != StatusDraft {
		return Category{}, errInvalidTransition
	}
	return c.withStatus(StatusReview, now, nil)
}

func (c Category) Approve(now time.Time) (Category, error) {
	if c.Status != StatusReview {
		return Category{}, errInvalidTransition
	}
	return c.withStatus(StatusApproved, now, nil)
}

func (c Category) Publish(now time.Time) (Category, error) {
	if c.Status != StatusApproved && c.Status != StatusPublished {
		return Category{}, errInvalidTransition
	}
	if c.Status == StatusPublished {
		return c, nil
	}
	at := now
	return c.withStatus(StatusPublished, now, &at)
}

func (c Category) withStatus(next Status, now time.Time, publishedAt *time.Time) (Category, error) {
	if now.Before(c.CreatedAt) {
		return Category{}, errInvalidCategory
	}
	c.Status = next
	c.UpdatedAt = now
	c.PublishedAt = cloneTimePtr(publishedAt)
	if err := c.Validate(); err != nil {
		return Category{}, err
	}
	return c, nil
}

// CategoryLabel is UTF-8 display text. Codes stay ASCII; labels are not English-lowercased.
type CategoryLabel struct {
	CategoryID  ID
	Locale      Locale
	Label       string
	Description *string
}

func (l CategoryLabel) Validate() error {
	if l.CategoryID.IsZero() {
		return errZeroID
	}
	if !l.Locale.valid() {
		return errInvalidLocale
	}
	if err := validateLabelText(l.Label); err != nil {
		return err
	}
	if l.Label != PreserveTurkishRunes(l.Label) {
		return errInvalidLabel
	}
	if l.Description != nil && strings.TrimSpace(*l.Description) != *l.Description {
		return errInvalidLabel
	}
	return nil
}

func NewCategoryLabel(categoryID ID, locale Locale, label string, description *string) (CategoryLabel, error) {
	label = PreserveTurkishRunes(trimLabel(label))
	var desc *string
	if description != nil {
		d := PreserveTurkishRunes(strings.TrimSpace(*description))
		if d == "" {
			desc = nil
		} else {
			desc = &d
		}
	}
	l := CategoryLabel{CategoryID: categoryID, Locale: locale, Label: label, Description: desc}
	if err := l.Validate(); err != nil {
		return CategoryLabel{}, err
	}
	return l, nil
}

// Schema is an immutable snapshot once published. Changes require a new version.
type Schema struct {
	ID          ID
	CategoryID  ID
	Version     int
	Status      Status
	CreatedAt   time.Time
	PublishedAt *time.Time
}

func (s Schema) Validate() error {
	if s.ID.IsZero() || s.CategoryID.IsZero() {
		return errZeroID
	}
	if s.Version <= 0 {
		return errInvalidSchema
	}
	if !s.Status.valid() {
		return errInvalidStatus
	}
	if s.CreatedAt.IsZero() {
		return errInvalidSchema
	}
	if s.Status == StatusPublished {
		if s.PublishedAt == nil || s.PublishedAt.Before(s.CreatedAt) {
			return errInvalidSchema
		}
	} else if s.PublishedAt != nil {
		return errInvalidSchema
	}
	return nil
}

func NewSchema(categoryID ID, version int, now time.Time) (Schema, error) {
	if categoryID.IsZero() {
		return Schema{}, errZeroID
	}
	if version <= 0 {
		return Schema{}, errInvalidSchema
	}
	if now.IsZero() {
		return Schema{}, errInvalidSchema
	}
	id, err := NewID()
	if err != nil {
		return Schema{}, errUnavailable
	}
	s := Schema{
		ID:         id,
		CategoryID: categoryID,
		Version:    version,
		Status:     StatusDraft,
		CreatedAt:  now,
	}
	if err := s.Validate(); err != nil {
		return Schema{}, err
	}
	return s, nil
}

func (s Schema) SubmitForReview(now time.Time) (Schema, error) {
	if s.Status == StatusPublished {
		return Schema{}, errImmutablePublished
	}
	if s.Status != StatusDraft {
		return Schema{}, errInvalidTransition
	}
	return s.withStatus(StatusReview, now, nil)
}

func (s Schema) Approve(now time.Time) (Schema, error) {
	if s.Status == StatusPublished {
		return Schema{}, errImmutablePublished
	}
	if s.Status != StatusReview {
		return Schema{}, errInvalidTransition
	}
	return s.withStatus(StatusApproved, now, nil)
}

func (s Schema) Publish(now time.Time) (Schema, error) {
	if s.Status == StatusPublished {
		return Schema{}, errImmutablePublished
	}
	if s.Status != StatusApproved {
		return Schema{}, errInvalidTransition
	}
	at := now
	return s.withStatus(StatusPublished, now, &at)
}

func (s Schema) withStatus(next Status, now time.Time, publishedAt *time.Time) (Schema, error) {
	if s.Status == StatusPublished {
		return Schema{}, errImmutablePublished
	}
	if now.Before(s.CreatedAt) {
		return Schema{}, errInvalidSchema
	}
	s.Status = next
	s.PublishedAt = cloneTimePtr(publishedAt)
	if err := s.Validate(); err != nil {
		return Schema{}, err
	}
	return s, nil
}

func (s Schema) assertDraft() error {
	if s.Status == StatusPublished {
		return errImmutablePublished
	}
	if !s.Status.editable() {
		return errInvalidTransition
	}
	return nil
}

// Attribute is a relational definition bound to one schema version.
type Attribute struct {
	ID          ID
	SchemaID    ID
	Code        string
	ValueType   ValueType
	Required    bool
	Filterable  bool
	Sortable    bool
	SortOrder   int
	Constraints Constraints
}

func (a Attribute) Validate() error {
	if a.ID.IsZero() || a.SchemaID.IsZero() {
		return errZeroID
	}
	if _, err := NormalizeCode(a.Code); err != nil {
		return err
	}
	if !a.ValueType.valid() {
		return errInvalidAttribute
	}
	if a.SortOrder < 0 {
		return errInvalidAttribute
	}
	if a.Sortable && (a.ValueType == ValueTypeBoolean || a.ValueType == ValueTypeEnum) {
		return errInvalidAttribute
	}
	if err := ValidateConstraints(a.Constraints); err != nil {
		return err
	}
	return nil
}

func NewAttribute(schemaID ID, code string, valueType ValueType, required, filterable, sortable bool, sortOrder int, constraints Constraints) (Attribute, error) {
	code, err := NormalizeCode(code)
	if err != nil {
		return Attribute{}, err
	}
	if constraints == nil {
		constraints = Constraints{}
	}
	id, err := NewID()
	if err != nil {
		return Attribute{}, errUnavailable
	}
	a := Attribute{
		ID:          id,
		SchemaID:    schemaID,
		Code:        code,
		ValueType:   valueType,
		Required:    required,
		Filterable:  filterable,
		Sortable:    sortable,
		SortOrder:   sortOrder,
		Constraints: cloneConstraints(constraints),
	}
	if err := a.Validate(); err != nil {
		return Attribute{}, err
	}
	return a, nil
}

type AttributeLabel struct {
	AttributeID ID
	Locale      Locale
	Label       string
	HelpText    *string
}

func (l AttributeLabel) Validate() error {
	if l.AttributeID.IsZero() {
		return errZeroID
	}
	if !l.Locale.valid() {
		return errInvalidLocale
	}
	if err := validateLabelText(l.Label); err != nil {
		return err
	}
	return nil
}

func NewAttributeLabel(attributeID ID, locale Locale, label string, helpText *string) (AttributeLabel, error) {
	label = PreserveTurkishRunes(trimLabel(label))
	var help *string
	if helpText != nil {
		h := PreserveTurkishRunes(strings.TrimSpace(*helpText))
		if h != "" {
			help = &h
		}
	}
	l := AttributeLabel{AttributeID: attributeID, Locale: locale, Label: label, HelpText: help}
	if err := l.Validate(); err != nil {
		return AttributeLabel{}, err
	}
	return l, nil
}

type AttributeOption struct {
	ID          ID
	AttributeID ID
	Code        string
	SortOrder   int
}

func (o AttributeOption) Validate() error {
	if o.ID.IsZero() || o.AttributeID.IsZero() {
		return errZeroID
	}
	if _, err := NormalizeCode(o.Code); err != nil {
		return err
	}
	if o.SortOrder < 0 {
		return errInvalidOption
	}
	return nil
}

func NewAttributeOption(attributeID ID, code string, sortOrder int) (AttributeOption, error) {
	code, err := NormalizeCode(code)
	if err != nil {
		return AttributeOption{}, err
	}
	id, err := NewID()
	if err != nil {
		return AttributeOption{}, errUnavailable
	}
	o := AttributeOption{ID: id, AttributeID: attributeID, Code: code, SortOrder: sortOrder}
	if err := o.Validate(); err != nil {
		return AttributeOption{}, err
	}
	return o, nil
}

type OptionLabel struct {
	OptionID ID
	Locale   Locale
	Label    string
}

func (l OptionLabel) Validate() error {
	if l.OptionID.IsZero() {
		return errZeroID
	}
	if !l.Locale.valid() {
		return errInvalidLocale
	}
	return validateLabelText(l.Label)
}

func NewOptionLabel(optionID ID, locale Locale, label string) (OptionLabel, error) {
	l := OptionLabel{OptionID: optionID, Locale: locale, Label: PreserveTurkishRunes(trimLabel(label))}
	if err := l.Validate(); err != nil {
		return OptionLabel{}, err
	}
	return l, nil
}

// FormDefinition is the schema-driven listing form for a published category version.
type FormDefinition struct {
	CategoryID     ID
	CategoryCode   string
	SchemaID       ID
	SchemaVersion  int
	CategoryLabels []CategoryLabel
	Fields         []FormField
}

type FormField struct {
	AttributeID ID
	Code        string
	ValueType   ValueType
	Required    bool
	Filterable  bool
	Sortable    bool
	SortOrder   int
	Constraints Constraints
	Labels      []AttributeLabel
	Options     []FormOption
}

type FormOption struct {
	OptionID  ID
	Code      string
	SortOrder int
	Labels    []OptionLabel
}
