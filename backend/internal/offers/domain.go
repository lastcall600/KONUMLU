package offers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxMessageRunes = 2000

var (
	errZeroID            = errors.New("offers id must not be zero")
	errInvalidOffer      = errors.New("invalid offer")
	errInvalidStatus     = errors.New("invalid offer status")
	errInvalidTransition = errors.New("invalid offer status transition")
	errInvalidMessage    = errors.New("invalid offer message")
	errInvalidPrice      = errors.New("invalid offer price")
	errStoreRequired     = errors.New("offers store required")
	errUnavailable       = errors.New("offers unavailable")
	errNotFound          = errors.New("offer not found")
	errForbidden         = errors.New("offer access denied")
	errConflict          = errors.New("offer conflict")
	errNotEligible       = errors.New("offer not eligible")
)

var (
	ErrZeroID            = errZeroID
	ErrInvalidOffer      = errInvalidOffer
	ErrInvalidStatus     = errInvalidStatus
	ErrInvalidTransition = errInvalidTransition
	ErrInvalidMessage    = errInvalidMessage
	ErrInvalidPrice      = errInvalidPrice
	ErrStoreRequired     = errStoreRequired
	ErrUnavailable       = errUnavailable
	ErrNotFound          = errNotFound
	ErrForbidden         = errForbidden
	ErrConflict          = errConflict
	ErrNotEligible       = errNotEligible
)

var (
	amountPattern   = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,8})?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

type Status string

const (
	StatusSubmitted Status = "submitted"
	StatusWithdrawn Status = "withdrawn"
	StatusAccepted  Status = "accepted"
	StatusRejected  Status = "rejected"
	StatusExpired   Status = "expired"
)

func (s Status) valid() bool {
	switch s {
	case StatusSubmitted, StatusWithdrawn, StatusAccepted, StatusRejected, StatusExpired:
		return true
	default:
		return false
	}
}

func (s Status) Terminal() bool {
	return s == StatusWithdrawn || s == StatusAccepted || s == StatusRejected || s == StatusExpired
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
		return ID{}, errInvalidOffer
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidOffer
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

// Price is an optional fixed amount. It has no tax, escrow, or payment semantics.
type Price struct {
	Amount   string
	Currency string
}

func (p *Price) normalized() (*Price, error) {
	if p == nil {
		return nil, nil
	}
	amt := strings.TrimSpace(p.Amount)
	cur := strings.TrimSpace(p.Currency)
	if amt == "" && cur == "" {
		return nil, nil
	}
	if amt == "" || cur == "" {
		return nil, errInvalidPrice
	}
	if !amountPattern.MatchString(amt) || !currencyPattern.MatchString(cur) {
		return nil, errInvalidPrice
	}
	if _, ok := new(big.Rat).SetString(amt); !ok {
		return nil, errInvalidPrice
	}
	return &Price{Amount: amt, Currency: cur}, nil
}

type Content struct {
	NeedID             ID
	ProviderBusinessID ID
	ServiceID          ID
	Message            string
	Price              *Price
}

func (c Content) normalized() (Content, error) {
	if c.NeedID.IsZero() || c.ProviderBusinessID.IsZero() || c.ServiceID.IsZero() {
		return Content{}, errZeroID
	}
	msg, err := normalizeMessage(c.Message)
	if err != nil {
		return Content{}, err
	}
	price, err := c.Price.normalized()
	if err != nil {
		return Content{}, err
	}
	return Content{
		NeedID:             c.NeedID,
		ProviderBusinessID: c.ProviderBusinessID,
		ServiceID:          c.ServiceID,
		Message:            msg,
		Price:              price,
	}, nil
}

type Offer struct {
	ID                 ID
	NeedID             ID
	ProviderBusinessID ID
	ServiceID          ID
	ProviderUserID     ID
	Message            string
	Price              *Price
	Status             Status
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (o Offer) Validate() error {
	if o.ID.IsZero() || o.NeedID.IsZero() || o.ProviderBusinessID.IsZero() || o.ServiceID.IsZero() || o.ProviderUserID.IsZero() {
		return errZeroID
	}
	if !o.Status.valid() {
		return errInvalidStatus
	}
	if _, err := normalizeMessage(o.Message); err != nil {
		return err
	}
	if o.Message != strings.TrimSpace(o.Message) {
		return errInvalidMessage
	}
	if _, err := o.Price.normalized(); err != nil {
		return err
	}
	if o.CreatedAt.IsZero() || o.UpdatedAt.Before(o.CreatedAt) {
		return errInvalidOffer
	}
	return nil
}

func Submit(providerUserID ID, content Content, now time.Time) (Offer, error) {
	if providerUserID.IsZero() {
		return Offer{}, errZeroID
	}
	if now.IsZero() {
		return Offer{}, errInvalidOffer
	}
	content, err := content.normalized()
	if err != nil {
		return Offer{}, err
	}
	id, err := NewID()
	if err != nil {
		return Offer{}, errUnavailable
	}
	o := Offer{
		ID:                 id,
		NeedID:             content.NeedID,
		ProviderBusinessID: content.ProviderBusinessID,
		ServiceID:          content.ServiceID,
		ProviderUserID:     providerUserID,
		Message:            content.Message,
		Price:              clonePrice(content.Price),
		Status:             StatusSubmitted,
		CreatedAt:          now.UTC(),
		UpdatedAt:          now.UTC(),
	}
	if err := o.Validate(); err != nil {
		return Offer{}, err
	}
	return o, nil
}

func (o Offer) Withdraw(now time.Time) (Offer, error) {
	if o.Status != StatusSubmitted {
		return Offer{}, errInvalidTransition
	}
	return o.transition(StatusWithdrawn, now)
}

func (o Offer) Accept(now time.Time) (Offer, error) {
	if o.Status != StatusSubmitted {
		return Offer{}, errInvalidTransition
	}
	return o.transition(StatusAccepted, now)
}

func (o Offer) Reject(now time.Time) (Offer, error) {
	if o.Status != StatusSubmitted {
		return Offer{}, errInvalidTransition
	}
	return o.transition(StatusRejected, now)
}

func (o Offer) Expire(now time.Time) (Offer, error) {
	if o.Status != StatusSubmitted {
		return Offer{}, errInvalidTransition
	}
	return o.transition(StatusExpired, now)
}

func (o Offer) transition(next Status, now time.Time) (Offer, error) {
	if now.Before(o.CreatedAt) {
		return Offer{}, errInvalidOffer
	}
	o.Status = next
	o.UpdatedAt = now.UTC()
	if err := o.Validate(); err != nil {
		return Offer{}, err
	}
	return o, nil
}

func normalizeMessage(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if utf8.RuneCountInString(trimmed) > MaxMessageRunes {
		return "", errInvalidMessage
	}
	for _, r := range trimmed {
		if r == 0 || r == 0x7f {
			return "", errInvalidMessage
		}
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return "", errInvalidMessage
		}
	}
	return trimmed, nil
}

func clonePrice(p *Price) *Price {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

func cloneOffer(o Offer) Offer {
	o.Price = clonePrice(o.Price)
	return o
}
