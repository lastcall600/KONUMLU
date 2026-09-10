package businesses

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxDisplayNameRunes = 80
	MaxDescriptionRunes = 4000
)

var (
	errZeroID                = errors.New("businesses id must not be zero")
	errInvalidBusiness       = errors.New("invalid business")
	errInvalidStatus         = errors.New("invalid business status")
	errInvalidTransition     = errors.New("invalid business status transition")
	errInvalidContent        = errors.New("invalid business content")
	errStoreRequired         = errors.New("businesses store required")
	errUnavailable           = errors.New("businesses unavailable")
	errNotFound              = errors.New("business not found")
	errForbidden             = errors.New("business access denied")
	errConflict              = errors.New("business conflict")
	errInvalidOfferedService = errors.New("invalid offered service")
	errInvalidPrice          = errors.New("invalid offered service price")
	errInvalidLocation       = errors.New("invalid business location")
	errInvalidLatitude       = errors.New("invalid latitude")
	errInvalidLongitude      = errors.New("invalid longitude")
	errInvalidCategory       = errors.New("invalid offered service category")
	errInvalidRadius         = errors.New("invalid match radius")
)

var (
	ErrZeroID                = errZeroID
	ErrInvalidBusiness       = errInvalidBusiness
	ErrInvalidStatus         = errInvalidStatus
	ErrInvalidTransition     = errInvalidTransition
	ErrInvalidContent        = errInvalidContent
	ErrStoreRequired         = errStoreRequired
	ErrUnavailable           = errUnavailable
	ErrNotFound              = errNotFound
	ErrForbidden             = errForbidden
	ErrConflict              = errConflict
	ErrInvalidOfferedService = errInvalidOfferedService
	ErrInvalidPrice          = errInvalidPrice
	ErrInvalidLocation       = errInvalidLocation
	ErrInvalidLatitude       = errInvalidLatitude
	ErrInvalidLongitude      = errInvalidLongitude
	ErrInvalidCategory       = errInvalidCategory
	ErrInvalidRadius         = errInvalidRadius
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusClosed    Status = "closed"
)

func (s Status) valid() bool {
	switch s {
	case StatusDraft, StatusActive, StatusSuspended, StatusClosed:
		return true
	default:
		return false
	}
}

func (s Status) PubliclyReadable() bool {
	return s == StatusActive
}

func (s Status) ownerEditable() bool {
	return s == StatusDraft || s == StatusActive
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
		return ID{}, errInvalidBusiness
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidBusiness
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

// ProfileContent is owner-entered business text. No language-specific rewrite.
type ProfileContent struct {
	DisplayName string
	Description string
}

func (c ProfileContent) normalized() (ProfileContent, error) {
	name, err := normalizeDisplayName(c.DisplayName)
	if err != nil {
		return ProfileContent{}, err
	}
	if name == "" {
		return ProfileContent{}, errInvalidContent
	}
	desc, err := normalizeDescription(c.Description)
	if err != nil {
		return ProfileContent{}, err
	}
	return ProfileContent{DisplayName: name, Description: desc}, nil
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

type Profile struct {
	ID          ID
	OwnerUserID ID
	DisplayName string
	Description string
	Status      Status
	Location    *Coordinates
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (p Profile) Validate() error {
	if p.ID.IsZero() || p.OwnerUserID.IsZero() {
		return errZeroID
	}
	if !p.Status.valid() {
		return errInvalidStatus
	}
	content := ProfileContent{DisplayName: p.DisplayName, Description: p.Description}
	if _, err := content.normalized(); err != nil {
		return err
	}
	if p.DisplayName != strings.TrimSpace(p.DisplayName) {
		return errInvalidContent
	}
	if p.Description != "" && p.Description != strings.TrimSpace(p.Description) {
		return errInvalidContent
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) {
		return errInvalidBusiness
	}
	if p.Location != nil {
		if err := ValidateCoordinates(*p.Location); err != nil {
			return err
		}
	}
	return nil
}

func (p Profile) PubliclyReadable() bool {
	return p.Status.PubliclyReadable()
}

func NewDraft(ownerUserID ID, content ProfileContent, now time.Time) (Profile, error) {
	if ownerUserID.IsZero() {
		return Profile{}, errZeroID
	}
	if now.IsZero() {
		return Profile{}, errInvalidBusiness
	}
	content, err := content.normalized()
	if err != nil {
		return Profile{}, err
	}
	id, err := NewID()
	if err != nil {
		return Profile{}, errUnavailable
	}
	p := Profile{
		ID:          id,
		OwnerUserID: ownerUserID,
		DisplayName: content.DisplayName,
		Description: content.Description,
		Status:      StatusDraft,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func (p Profile) UpdateContent(content ProfileContent, now time.Time) (Profile, error) {
	if !p.Status.ownerEditable() {
		return Profile{}, errInvalidTransition
	}
	content, err := content.normalized()
	if err != nil {
		return Profile{}, err
	}
	if now.Before(p.CreatedAt) {
		return Profile{}, errInvalidBusiness
	}
	p.DisplayName = content.DisplayName
	p.Description = content.Description
	p.UpdatedAt = now
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func (p Profile) SetLocation(loc *Coordinates, now time.Time) (Profile, error) {
	if !p.Status.ownerEditable() {
		return Profile{}, errInvalidTransition
	}
	if now.Before(p.CreatedAt) {
		return Profile{}, errInvalidBusiness
	}
	if loc != nil {
		if err := ValidateCoordinates(*loc); err != nil {
			return Profile{}, err
		}
		cp := *loc
		p.Location = &cp
	} else {
		p.Location = nil
	}
	p.UpdatedAt = now
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func (p Profile) Activate(now time.Time) (Profile, error) {
	if p.Status != StatusDraft {
		return Profile{}, errInvalidTransition
	}
	if now.Before(p.CreatedAt) {
		return Profile{}, errInvalidBusiness
	}
	p.Status = StatusActive
	p.UpdatedAt = now
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func (p Profile) Close(now time.Time) (Profile, error) {
	if p.Status != StatusDraft && p.Status != StatusActive {
		return Profile{}, errInvalidTransition
	}
	if now.Before(p.CreatedAt) {
		return Profile{}, errInvalidBusiness
	}
	p.Status = StatusClosed
	p.UpdatedAt = now
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func normalizeDisplayName(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errInvalidContent
	}
	if utf8.RuneCountInString(trimmed) > MaxDisplayNameRunes {
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

func cloneCoords(c *Coordinates) *Coordinates {
	if c == nil {
		return nil
	}
	cp := *c
	return &cp
}

func cloneIDPtr(id *ID) *ID {
	if id == nil {
		return nil
	}
	cp := *id
	return &cp
}

func cloneProfile(p Profile) Profile {
	p.Location = cloneCoords(p.Location)
	return p
}
