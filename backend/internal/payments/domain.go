package payments

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
	errZeroID            = errors.New("payments id must not be zero")
	errInvalidPayment    = errors.New("invalid payment")
	errInvalidStatus     = errors.New("invalid payment status")
	errInvalidTransition = errors.New("invalid payment status transition")
	errInvalidAmount     = errors.New("invalid payment amount")
	errStoreRequired     = errors.New("payments store required")
	errUnavailable       = errors.New("payments unavailable")
	errNotFound          = errors.New("payment not found")
	errForbidden         = errors.New("payment access denied")
	errConflict          = errors.New("payment conflict")
	errNotPriced         = errors.New("transaction is not priced")
	errNotPayable        = errors.New("transaction is not payable")
)

var (
	ErrZeroID            = errZeroID
	ErrInvalidPayment    = errInvalidPayment
	ErrInvalidStatus     = errInvalidStatus
	ErrInvalidTransition = errInvalidTransition
	ErrInvalidAmount     = errInvalidAmount
	ErrStoreRequired     = errStoreRequired
	ErrUnavailable       = errUnavailable
	ErrNotFound          = errNotFound
	ErrForbidden         = errForbidden
	ErrConflict          = errConflict
	ErrNotPriced         = errNotPriced
	ErrNotPayable        = errNotPayable
)

var (
	amountPattern   = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,8})?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusAuthorized Status = "authorized"
	StatusCaptured   Status = "captured"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
)

func (s Status) valid() bool {
	switch s {
	case StatusPending, StatusAuthorized, StatusCaptured, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

func (s Status) Terminal() bool {
	return s == StatusCaptured || s == StatusFailed || s == StatusCancelled
}

func (s Status) ActiveIntent() bool {
	return s == StatusPending || s == StatusAuthorized
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
		return ID{}, errInvalidPayment
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidPayment
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

type Money struct {
	Amount   string
	Currency string
}

func (m Money) normalized() (Money, error) {
	amt := strings.TrimSpace(m.Amount)
	cur := strings.TrimSpace(m.Currency)
	if amt == "" || cur == "" {
		return Money{}, errInvalidAmount
	}
	if !amountPattern.MatchString(amt) || !currencyPattern.MatchString(cur) {
		return Money{}, errInvalidAmount
	}
	if _, ok := new(big.Rat).SetString(amt); !ok {
		return Money{}, errInvalidAmount
	}
	return Money{Amount: amt, Currency: cur}, nil
}

type Payment struct {
	ID                ID
	TransactionID     ID
	PayerUserID       ID
	PayeeUserID       ID
	Amount            Money
	Status            Status
	Provider          *string
	ProviderReference *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	AuthorizedAt      *time.Time
	CapturedAt        *time.Time
	CancelledAt       *time.Time
	FailedAt          *time.Time
}

func (p Payment) Validate() error {
	if p.ID.IsZero() || p.TransactionID.IsZero() || p.PayerUserID.IsZero() || p.PayeeUserID.IsZero() {
		return errZeroID
	}
	if p.PayerUserID == p.PayeeUserID {
		return errInvalidPayment
	}
	if !p.Status.valid() {
		return errInvalidStatus
	}
	if _, err := p.Amount.normalized(); err != nil {
		return err
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) {
		return errInvalidPayment
	}
	if err := validateProviderFields(p.Provider, p.ProviderReference); err != nil {
		return err
	}
	switch p.Status {
	case StatusPending:
		if p.AuthorizedAt != nil || p.CapturedAt != nil || p.CancelledAt != nil || p.FailedAt != nil {
			return errInvalidPayment
		}
	case StatusAuthorized:
		if p.AuthorizedAt == nil || p.CapturedAt != nil || p.CancelledAt != nil || p.FailedAt != nil {
			return errInvalidPayment
		}
		if p.AuthorizedAt.Before(p.CreatedAt) {
			return errInvalidPayment
		}
	case StatusCaptured:
		if p.AuthorizedAt == nil || p.CapturedAt == nil || p.CancelledAt != nil || p.FailedAt != nil {
			return errInvalidPayment
		}
		if p.CapturedAt.Before(*p.AuthorizedAt) {
			return errInvalidPayment
		}
	case StatusCancelled:
		if p.CancelledAt == nil || p.CapturedAt != nil || p.FailedAt != nil {
			return errInvalidPayment
		}
		if p.CancelledAt.Before(p.CreatedAt) {
			return errInvalidPayment
		}
	case StatusFailed:
		if p.FailedAt == nil || p.CapturedAt != nil || p.CancelledAt != nil {
			return errInvalidPayment
		}
		if p.FailedAt.Before(p.CreatedAt) {
			return errInvalidPayment
		}
	}
	return nil
}

func validateProviderFields(provider, ref *string) error {
	if provider != nil {
		v := strings.TrimSpace(*provider)
		if v == "" {
			return errInvalidPayment
		}
	}
	if ref != nil {
		v := strings.TrimSpace(*ref)
		if v == "" {
			return errInvalidPayment
		}
	}
	return nil
}

func (p Payment) Participant(userID ID) bool {
	if userID.IsZero() {
		return false
	}
	return p.PayerUserID == userID || p.PayeeUserID == userID
}

type CreateCommand struct {
	TransactionID ID
	PayerUserID   ID
	PayeeUserID   ID
	Amount        Money
}

func CreatePending(cmd CreateCommand, now time.Time) (Payment, error) {
	if cmd.TransactionID.IsZero() || cmd.PayerUserID.IsZero() || cmd.PayeeUserID.IsZero() {
		return Payment{}, errZeroID
	}
	if now.IsZero() {
		return Payment{}, errInvalidPayment
	}
	amount, err := cmd.Amount.normalized()
	if err != nil {
		return Payment{}, err
	}
	id, err := NewID()
	if err != nil {
		return Payment{}, errUnavailable
	}
	stamp := now.UTC()
	p := Payment{
		ID:            id,
		TransactionID: cmd.TransactionID,
		PayerUserID:   cmd.PayerUserID,
		PayeeUserID:   cmd.PayeeUserID,
		Amount:        amount,
		Status:        StatusPending,
		CreatedAt:     stamp,
		UpdatedAt:     stamp,
	}
	if err := p.Validate(); err != nil {
		return Payment{}, err
	}
	return p, nil
}

func (p Payment) Authorize(now time.Time, signal ProviderSignal) (Payment, bool, error) {
	if p.Status == StatusAuthorized {
		return p, false, nil
	}
	if p.Status != StatusPending {
		return Payment{}, false, errInvalidTransition
	}
	next := p.withStatus(StatusAuthorized, now)
	stamp := next.UpdatedAt
	next.AuthorizedAt = &stamp
	next.applySignal(signal)
	if err := next.Validate(); err != nil {
		return Payment{}, false, err
	}
	return next, true, nil
}

func (p Payment) Capture(now time.Time, signal ProviderSignal) (Payment, bool, error) {
	if p.Status == StatusCaptured {
		return p, false, nil
	}
	if p.Status != StatusAuthorized {
		return Payment{}, false, errInvalidTransition
	}
	next := p.withStatus(StatusCaptured, now)
	stamp := next.UpdatedAt
	next.CapturedAt = &stamp
	next.applySignal(signal)
	if err := next.Validate(); err != nil {
		return Payment{}, false, err
	}
	return next, true, nil
}

func (p Payment) Cancel(now time.Time, signal ProviderSignal) (Payment, bool, error) {
	if p.Status == StatusCancelled {
		return p, false, nil
	}
	if p.Status != StatusPending && p.Status != StatusAuthorized {
		return Payment{}, false, errInvalidTransition
	}
	next := p.withStatus(StatusCancelled, now)
	stamp := next.UpdatedAt
	next.CancelledAt = &stamp
	next.applySignal(signal)
	if err := next.Validate(); err != nil {
		return Payment{}, false, err
	}
	return next, true, nil
}

func (p Payment) Fail(now time.Time, signal ProviderSignal) (Payment, bool, error) {
	if p.Status == StatusFailed {
		return p, false, nil
	}
	if p.Status != StatusPending && p.Status != StatusAuthorized {
		return Payment{}, false, errInvalidTransition
	}
	next := p.withStatus(StatusFailed, now)
	stamp := next.UpdatedAt
	next.FailedAt = &stamp
	next.applySignal(signal)
	if err := next.Validate(); err != nil {
		return Payment{}, false, err
	}
	return next, true, nil
}

func (p Payment) withStatus(next Status, now time.Time) Payment {
	p.Status = next
	p.UpdatedAt = now.UTC()
	return p
}

func (p *Payment) applySignal(signal ProviderSignal) {
	if signal.Provider != nil {
		v := strings.TrimSpace(*signal.Provider)
		if v != "" {
			p.Provider = &v
		}
	}
	if signal.ProviderReference != nil {
		v := strings.TrimSpace(*signal.ProviderReference)
		if v != "" {
			p.ProviderReference = &v
		}
	}
}

func cloneString(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

func clonePayment(p Payment) Payment {
	p.Provider = cloneString(p.Provider)
	p.ProviderReference = cloneString(p.ProviderReference)
	p.AuthorizedAt = cloneTime(p.AuthorizedAt)
	p.CapturedAt = cloneTime(p.CapturedAt)
	p.CancelledAt = cloneTime(p.CancelledAt)
	p.FailedAt = cloneTime(p.FailedAt)
	return p
}
