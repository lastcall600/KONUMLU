package favorites

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	errZeroID        = errors.New("favorites id must not be zero")
	errStoreRequired = errors.New("favorites store required")
	errUnavailable   = errors.New("favorites unavailable")
	errNotFound      = errors.New("favorite listing not found")
	errListingsReq   = errors.New("listings source required")
)

var (
	ErrZeroID        = errZeroID
	ErrStoreRequired = errStoreRequired
	ErrUnavailable   = errUnavailable
	ErrNotFound      = errNotFound
	ErrListingsReq   = errListingsReq
)

// ID is a user or listing UUID. Favorites does not own identity or listing tables.
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

// Favorite is a user/listing save. Visibility is enforced at the service edge.
type Favorite struct {
	UserID    ID
	ListingID ID
	CreatedAt time.Time
}

func (f Favorite) Validate() error {
	if f.UserID.IsZero() || f.ListingID.IsZero() {
		return errZeroID
	}
	if f.CreatedAt.IsZero() {
		return errUnavailable
	}
	return nil
}
