package transactions

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
)

var (
	errZeroID            = errors.New("transactions id must not be zero")
	errInvalidTxn        = errors.New("invalid transaction")
	errInvalidStatus     = errors.New("invalid transaction status")
	errInvalidTransition = errors.New("invalid transaction status transition")
	errInvalidPrice      = errors.New("invalid transaction price")
	errStoreRequired     = errors.New("transactions store required")
	errUnavailable       = errors.New("transactions unavailable")
	errNotFound          = errors.New("transaction not found")
	errForbidden         = errors.New("transaction access denied")
	errConflict          = errors.New("transaction conflict")
	errNotAccepted       = errors.New("offer is not accepted")
	errNeedDraft         = errors.New("need is draft")
	errNeedCancelled     = errors.New("need is cancelled")
	errNeedExpired       = errors.New("need is expired")
	errNeedFulfilled     = errors.New("need is fulfilled")
)

var (
	ErrZeroID            = errZeroID
	ErrInvalidTxn        = errInvalidTxn
	ErrInvalidStatus     = errInvalidStatus
	ErrInvalidTransition = errInvalidTransition
	ErrInvalidPrice      = errInvalidPrice
	ErrStoreRequired     = errStoreRequired
	ErrUnavailable       = errUnavailable
	ErrNotFound          = errNotFound
	ErrForbidden         = errForbidden
	ErrConflict          = errConflict
	ErrNotAccepted       = errNotAccepted
	ErrNeedDraft         = errNeedDraft
	ErrNeedCancelled     = errNeedCancelled
	ErrNeedExpired       = errNeedExpired
	ErrNeedFulfilled     = errNeedFulfilled
)

// V1CreateRule documents how a Transaction is created.
// Only the Need requester may create a Transaction from an already-accepted Offer
// via POST /v1/offers/{offerId}/transaction. The provider cannot create.
// The server does not auto-create on offer accept. Parties and source references
// are resolved from Offers and Needs contracts, never from the client body.
// Completing a Transaction does not write Needs tables. Fulfillment is a
// derived outbox effect processed by CompletionHandler through a Needs contract.
// Create and start require the source Need to be open. Complete allows open
// (then outbox fulfillment) or already-fulfilled; it rejects cancelled/expired/draft.
const V1CreateRule = "requester-triggered from accepted offer"

var (
	amountPattern   = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,8})?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
)

func (s Status) valid() bool {
	switch s {
	case StatusPending, StatusActive, StatusCompleted, StatusCancelled:
		return true
	default:
		return false
	}
}

func (s Status) Terminal() bool {
	return s == StatusCompleted || s == StatusCancelled
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
		return ID{}, errInvalidTxn
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidTxn
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

// Price is an optional agreed amount copied from an accepted Offer.
// It has no tax, escrow, or payment semantics.
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

type Transaction struct {
	ID                 ID
	OfferID            ID
	NeedID             ID
	RequesterUserID    ID
	ProviderUserID     ID
	ProviderBusinessID ID
	ServiceID          ID
	Price              *Price
	Status             Status
	CreatedAt          time.Time
	UpdatedAt          time.Time
	CompletedAt        *time.Time
	CancelledAt        *time.Time
}

func (t Transaction) Validate() error {
	if t.ID.IsZero() || t.OfferID.IsZero() || t.NeedID.IsZero() ||
		t.RequesterUserID.IsZero() || t.ProviderUserID.IsZero() ||
		t.ProviderBusinessID.IsZero() || t.ServiceID.IsZero() {
		return errZeroID
	}
	if t.RequesterUserID == t.ProviderUserID {
		return errInvalidTxn
	}
	if !t.Status.valid() {
		return errInvalidStatus
	}
	if _, err := t.Price.normalized(); err != nil {
		return err
	}
	if t.CreatedAt.IsZero() || t.UpdatedAt.Before(t.CreatedAt) {
		return errInvalidTxn
	}
	switch t.Status {
	case StatusCompleted:
		if t.CompletedAt == nil || t.CancelledAt != nil {
			return errInvalidTxn
		}
		if t.CompletedAt.Before(t.CreatedAt) {
			return errInvalidTxn
		}
	case StatusCancelled:
		if t.CancelledAt == nil || t.CompletedAt != nil {
			return errInvalidTxn
		}
		if t.CancelledAt.Before(t.CreatedAt) {
			return errInvalidTxn
		}
	default:
		if t.CompletedAt != nil || t.CancelledAt != nil {
			return errInvalidTxn
		}
	}
	return nil
}

func (t Transaction) Participant(userID ID) bool {
	if userID.IsZero() {
		return false
	}
	return t.RequesterUserID == userID || t.ProviderUserID == userID
}

type AcceptedSource struct {
	OfferID            ID
	NeedID             ID
	RequesterUserID    ID
	ProviderUserID     ID
	ProviderBusinessID ID
	ServiceID          ID
	Price              *Price
}

func CreatePending(src AcceptedSource, now time.Time) (Transaction, error) {
	if src.OfferID.IsZero() || src.NeedID.IsZero() || src.RequesterUserID.IsZero() ||
		src.ProviderUserID.IsZero() || src.ProviderBusinessID.IsZero() || src.ServiceID.IsZero() {
		return Transaction{}, errZeroID
	}
	if now.IsZero() {
		return Transaction{}, errInvalidTxn
	}
	price, err := src.Price.normalized()
	if err != nil {
		return Transaction{}, err
	}
	id, err := NewID()
	if err != nil {
		return Transaction{}, errUnavailable
	}
	stamp := now.UTC()
	txn := Transaction{
		ID:                 id,
		OfferID:            src.OfferID,
		NeedID:             src.NeedID,
		RequesterUserID:    src.RequesterUserID,
		ProviderUserID:     src.ProviderUserID,
		ProviderBusinessID: src.ProviderBusinessID,
		ServiceID:          src.ServiceID,
		Price:              clonePrice(price),
		Status:             StatusPending,
		CreatedAt:          stamp,
		UpdatedAt:          stamp,
	}
	if err := txn.Validate(); err != nil {
		return Transaction{}, err
	}
	return txn, nil
}

func (t Transaction) Start(now time.Time) (Transaction, bool, error) {
	if t.Status == StatusActive {
		return t, false, nil
	}
	if t.Status != StatusPending {
		return Transaction{}, false, errInvalidTransition
	}
	next := t.withStatus(StatusActive, now)
	if err := next.Validate(); err != nil {
		return Transaction{}, false, err
	}
	return next, true, nil
}

func (t Transaction) Complete(now time.Time) (Transaction, bool, error) {
	if t.Status == StatusCompleted {
		return t, false, nil
	}
	if t.Status != StatusActive {
		return Transaction{}, false, errInvalidTransition
	}
	next := t.withStatus(StatusCompleted, now)
	stamp := next.UpdatedAt
	next.CompletedAt = &stamp
	if err := next.Validate(); err != nil {
		return Transaction{}, false, err
	}
	return next, true, nil
}

func (t Transaction) Cancel(now time.Time) (Transaction, bool, error) {
	if t.Status == StatusCancelled {
		return t, false, nil
	}
	if t.Status != StatusPending && t.Status != StatusActive {
		return Transaction{}, false, errInvalidTransition
	}
	next := t.withStatus(StatusCancelled, now)
	stamp := next.UpdatedAt
	next.CancelledAt = &stamp
	if err := next.Validate(); err != nil {
		return Transaction{}, false, err
	}
	return next, true, nil
}

func (t Transaction) withStatus(next Status, now time.Time) Transaction {
	t.Status = next
	t.UpdatedAt = now.UTC()
	return t
}

func clonePrice(p *Price) *Price {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

func cloneTxn(t Transaction) Transaction {
	t.Price = clonePrice(t.Price)
	t.CompletedAt = cloneTime(t.CompletedAt)
	t.CancelledAt = cloneTime(t.CancelledAt)
	return t
}
