package savedsearch

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxNameLen  = 80
	maxQueryLen = 200
)

var (
	errZeroID        = errors.New("saved search id must not be zero")
	errStoreRequired = errors.New("saved search store required")
	errUnavailable   = errors.New("saved search unavailable")
	errNotFound      = errors.New("saved search not found")
	errInvalid       = errors.New("invalid saved search")
)

var (
	ErrZeroID        = errZeroID
	ErrStoreRequired = errStoreRequired
	ErrUnavailable   = errUnavailable
	ErrNotFound      = errNotFound
	ErrInvalid       = errInvalid
)

var (
	priceAmountPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,8})?$`)
	currencyPattern    = regexp.MustCompile(`^[A-Z]{3}$`)
)

// ID is a saved-search or user UUID. Saved Search does not own identity tables.
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

type Viewport struct {
	North float64
	South float64
	East  float64
	West  float64
}

// Filters are the supported public Search filters. Cursor and result IDs are not stored.
type Filters struct {
	Q          *string
	CategoryID *ID
	MinPrice   *string
	MaxPrice   *string
	Currency   *string
	Viewport   *Viewport
}

type SavedSearch struct {
	ID        ID
	UserID    ID
	Name      string
	Filters   Filters
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s SavedSearch) Validate() error {
	if s.ID.IsZero() || s.UserID.IsZero() {
		return errZeroID
	}
	if err := validateName(s.Name); err != nil {
		return err
	}
	if err := s.Filters.Normalize(); err != nil {
		return err
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() {
		return errUnavailable
	}
	return nil
}

func validateName(name string) error {
	n := strings.TrimSpace(name)
	if n == "" || utf8.RuneCountInString(n) > maxNameLen {
		return errInvalid
	}
	if n != name {
		return errInvalid
	}
	return nil
}

func (f *Filters) Normalize() error {
	if f == nil {
		return errInvalid
	}
	if f.Q != nil {
		text := strings.TrimSpace(*f.Q)
		if text == "" {
			f.Q = nil
		} else {
			if len(text) > maxQueryLen {
				return errInvalid
			}
			if len(tokenize(text)) == 0 {
				return errInvalid
			}
			f.Q = &text
		}
	}
	if f.CategoryID != nil {
		if f.CategoryID.IsZero() {
			return errInvalid
		}
	}
	minP, err := parseOptionalPrice(f.MinPrice)
	if err != nil {
		return err
	}
	maxP, err := parseOptionalPrice(f.MaxPrice)
	if err != nil {
		return err
	}
	if minP != nil && maxP != nil && minP.Cmp(maxP) > 0 {
		return errInvalid
	}
	if f.MinPrice != nil {
		s := strings.TrimSpace(*f.MinPrice)
		f.MinPrice = &s
	}
	if f.MaxPrice != nil {
		s := strings.TrimSpace(*f.MaxPrice)
		f.MaxPrice = &s
	}
	if f.Currency != nil {
		cur := strings.TrimSpace(*f.Currency)
		if cur == "" {
			f.Currency = nil
		} else {
			if !currencyPattern.MatchString(cur) {
				return errInvalid
			}
			f.Currency = &cur
		}
	}
	if f.Viewport != nil {
		if err := f.Viewport.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (v Viewport) validate() error {
	if v.North < -90 || v.North > 90 || v.South < -90 || v.South > 90 {
		return errInvalid
	}
	if v.East < -180 || v.East > 180 || v.West < -180 || v.West > 180 {
		return errInvalid
	}
	if v.North <= v.South || v.East <= v.West {
		return errInvalid
	}
	return nil
}

func parseOptionalPrice(raw *string) (*big.Rat, error) {
	if raw == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" || !priceAmountPattern.MatchString(s) {
		return nil, errInvalid
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return nil, errInvalid
	}
	return r, nil
}

func tokenize(q string) []string {
	fields := strings.Fields(strings.ToLower(q))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
