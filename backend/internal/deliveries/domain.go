package deliveries

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	errZeroID             = errors.New("deliveries id must not be zero")
	errInvalidDelivery    = errors.New("invalid delivery")
	errInvalidStatus      = errors.New("invalid delivery status")
	errInvalidTransition  = errors.New("invalid delivery status transition")
	errInvalidMethod      = errors.New("invalid delivery method")
	errInvalidNote        = errors.New("invalid delivery note")
	errInvalidEligibility = errors.New("invalid delivery eligibility")
	errStoreRequired      = errors.New("deliveries store required")
	errUnavailable        = errors.New("deliveries unavailable")
	errNotFound           = errors.New("delivery not found")
	errForbidden          = errors.New("delivery access denied")
	errConflict           = errors.New("delivery conflict")
	errNotEligible        = errors.New("transaction is not eligible for delivery")
)

var (
	ErrZeroID             = errZeroID
	ErrInvalidDelivery    = errInvalidDelivery
	ErrInvalidStatus      = errInvalidStatus
	ErrInvalidTransition  = errInvalidTransition
	ErrInvalidMethod      = errInvalidMethod
	ErrInvalidNote        = errInvalidNote
	ErrInvalidEligibility = errInvalidEligibility
	ErrStoreRequired      = errStoreRequired
	ErrUnavailable        = errUnavailable
	ErrNotFound           = errNotFound
	ErrForbidden          = errForbidden
	ErrConflict           = errConflict
	ErrNotEligible        = errNotEligible
)

const (
	MaxNoteRunes = 500

	EligibilityRequired = "delivery_required"
	EligibilityNone     = "no_delivery_required"
)

// V1CreateRule documents how a Delivery is created.
// A participant POSTs /v1/transactions/{transactionId}/delivery for an eligible
// Transaction. The server does not auto-create Delivery for service Transactions.
// Parties are resolved from the Transactions contract, never from the client body.
// MarkDelivered is provider logistics state only; requester acceptance is not implied.
const V1CreateRule = "participant-requested for eligible transaction"

type Status string

const (
	StatusPending   Status = "pending"
	StatusReady     Status = "ready"
	StatusInTransit Status = "in_transit"
	StatusDelivered Status = "delivered"
	StatusCancelled Status = "cancelled"
)

func (s Status) valid() bool {
	switch s {
	case StatusPending, StatusReady, StatusInTransit, StatusDelivered, StatusCancelled:
		return true
	default:
		return false
	}
}

func (s Status) Terminal() bool {
	return s == StatusDelivered || s == StatusCancelled
}

type Method string

const (
	MethodHandoff Method = "handoff"
	MethodCourier Method = "courier"
	MethodPickup  Method = "pickup"
)

func (m Method) valid() bool {
	switch m {
	case MethodHandoff, MethodCourier, MethodPickup:
		return true
	default:
		return false
	}
}

func (m Method) skipsInTransit() bool {
	return m == MethodHandoff || m == MethodPickup
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
		return ID{}, errInvalidDelivery
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidDelivery
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

type Delivery struct {
	ID              ID
	TransactionID   ID
	RequesterUserID ID
	ProviderUserID  ID
	Eligibility     string
	Status          Status
	Method          *Method
	Note            *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DispatchedAt    *time.Time
	DeliveredAt     *time.Time
	CancelledAt     *time.Time
}

func (d Delivery) Validate() error {
	if d.ID.IsZero() || d.TransactionID.IsZero() || d.RequesterUserID.IsZero() || d.ProviderUserID.IsZero() {
		return errZeroID
	}
	if d.RequesterUserID == d.ProviderUserID {
		return errInvalidDelivery
	}
	if d.Eligibility != EligibilityRequired {
		return errInvalidEligibility
	}
	if !d.Status.valid() {
		return errInvalidStatus
	}
	if d.Method != nil && !d.Method.valid() {
		return errInvalidMethod
	}
	if d.Note != nil {
		if _, err := normalizeNote(*d.Note); err != nil {
			return err
		}
	}
	if d.CreatedAt.IsZero() || d.UpdatedAt.Before(d.CreatedAt) {
		return errInvalidDelivery
	}
	switch d.Status {
	case StatusPending:
		if d.DispatchedAt != nil || d.DeliveredAt != nil || d.CancelledAt != nil {
			return errInvalidDelivery
		}
	case StatusReady:
		if d.DispatchedAt != nil || d.DeliveredAt != nil || d.CancelledAt != nil {
			return errInvalidDelivery
		}
	case StatusInTransit:
		if d.DispatchedAt == nil || d.DeliveredAt != nil || d.CancelledAt != nil {
			return errInvalidDelivery
		}
		if d.DispatchedAt.Before(d.CreatedAt) {
			return errInvalidDelivery
		}
		if d.skipsInTransit() {
			return errInvalidDelivery
		}
	case StatusDelivered:
		if d.DeliveredAt == nil || d.CancelledAt != nil {
			return errInvalidDelivery
		}
		if d.DeliveredAt.Before(d.CreatedAt) {
			return errInvalidDelivery
		}
		if d.skipsInTransit() {
			if d.DispatchedAt != nil {
				return errInvalidDelivery
			}
		} else if d.DispatchedAt == nil || d.DeliveredAt.Before(*d.DispatchedAt) {
			return errInvalidDelivery
		}
	case StatusCancelled:
		if d.CancelledAt == nil || d.DeliveredAt != nil {
			return errInvalidDelivery
		}
		if d.CancelledAt.Before(d.CreatedAt) {
			return errInvalidDelivery
		}
	}
	return nil
}

func (d Delivery) skipsInTransit() bool {
	return d.Method != nil && d.Method.skipsInTransit()
}

func (d Delivery) Participant(userID ID) bool {
	if userID.IsZero() {
		return false
	}
	return d.RequesterUserID == userID || d.ProviderUserID == userID
}

func (d Delivery) Provider(userID ID) bool {
	return !userID.IsZero() && d.ProviderUserID == userID
}

type CreateCommand struct {
	TransactionID   ID
	RequesterUserID ID
	ProviderUserID  ID
	Eligibility     string
	Method          *Method
	Note            *string
}

func CreatePending(cmd CreateCommand, now time.Time) (Delivery, error) {
	if cmd.TransactionID.IsZero() || cmd.RequesterUserID.IsZero() || cmd.ProviderUserID.IsZero() {
		return Delivery{}, errZeroID
	}
	if now.IsZero() {
		return Delivery{}, errInvalidDelivery
	}
	eligibility := strings.TrimSpace(cmd.Eligibility)
	if eligibility == "" {
		eligibility = EligibilityRequired
	}
	if eligibility == EligibilityNone {
		return Delivery{}, errNotEligible
	}
	if eligibility != EligibilityRequired {
		return Delivery{}, errInvalidEligibility
	}
	method, err := normalizeMethod(cmd.Method)
	if err != nil {
		return Delivery{}, err
	}
	note, err := cloneNormalizedNote(cmd.Note)
	if err != nil {
		return Delivery{}, err
	}
	id, err := NewID()
	if err != nil {
		return Delivery{}, errUnavailable
	}
	stamp := now.UTC()
	d := Delivery{
		ID:              id,
		TransactionID:   cmd.TransactionID,
		RequesterUserID: cmd.RequesterUserID,
		ProviderUserID:  cmd.ProviderUserID,
		Eligibility:     EligibilityRequired,
		Status:          StatusPending,
		Method:          method,
		Note:            note,
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
	}
	if err := d.Validate(); err != nil {
		return Delivery{}, err
	}
	return d, nil
}

func (d Delivery) MarkReady(now time.Time) (Delivery, bool, error) {
	if d.Status == StatusReady {
		return d, false, nil
	}
	if d.Status != StatusPending {
		return Delivery{}, false, errInvalidTransition
	}
	next := d.withStatus(StatusReady, now)
	if err := next.Validate(); err != nil {
		return Delivery{}, false, err
	}
	return next, true, nil
}

func (d Delivery) MarkInTransit(now time.Time) (Delivery, bool, error) {
	if d.Status == StatusInTransit {
		return d, false, nil
	}
	if d.skipsInTransit() {
		return Delivery{}, false, errInvalidTransition
	}
	if d.Status != StatusReady {
		return Delivery{}, false, errInvalidTransition
	}
	next := d.withStatus(StatusInTransit, now)
	stamp := next.UpdatedAt
	next.DispatchedAt = &stamp
	if err := next.Validate(); err != nil {
		return Delivery{}, false, err
	}
	return next, true, nil
}

// MarkDelivered records provider-side logistics completion.
// It is not requester acceptance or payment settlement.
func (d Delivery) MarkDelivered(now time.Time) (Delivery, bool, error) {
	if d.Status == StatusDelivered {
		return d, false, nil
	}
	if d.skipsInTransit() {
		if d.Status != StatusReady {
			return Delivery{}, false, errInvalidTransition
		}
	} else if d.Status != StatusInTransit {
		return Delivery{}, false, errInvalidTransition
	}
	next := d.withStatus(StatusDelivered, now)
	stamp := next.UpdatedAt
	next.DeliveredAt = &stamp
	if err := next.Validate(); err != nil {
		return Delivery{}, false, err
	}
	return next, true, nil
}

func (d Delivery) Cancel(now time.Time) (Delivery, bool, error) {
	if d.Status == StatusCancelled {
		return d, false, nil
	}
	if d.Status != StatusPending && d.Status != StatusReady && d.Status != StatusInTransit {
		return Delivery{}, false, errInvalidTransition
	}
	next := d.withStatus(StatusCancelled, now)
	stamp := next.UpdatedAt
	next.CancelledAt = &stamp
	if err := next.Validate(); err != nil {
		return Delivery{}, false, err
	}
	return next, true, nil
}

func (d Delivery) withStatus(next Status, now time.Time) Delivery {
	d.Status = next
	d.UpdatedAt = now.UTC()
	return d
}

func normalizeMethod(m *Method) (*Method, error) {
	if m == nil {
		return nil, nil
	}
	v := Method(strings.TrimSpace(string(*m)))
	if v == "" {
		return nil, nil
	}
	if !v.valid() {
		return nil, errInvalidMethod
	}
	return &v, nil
}

func normalizeNote(s string) (string, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return "", errInvalidNote
	}
	if utf8.RuneCountInString(v) > MaxNoteRunes {
		return "", errInvalidNote
	}
	return v, nil
}

func cloneNormalizedNote(s *string) (*string, error) {
	if s == nil {
		return nil, nil
	}
	v, err := normalizeNote(*s)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func cloneString(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func cloneMethod(m *Method) *Method {
	if m == nil {
		return nil
	}
	v := *m
	return &v
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

func cloneDelivery(d Delivery) Delivery {
	d.Method = cloneMethod(d.Method)
	d.Note = cloneString(d.Note)
	d.DispatchedAt = cloneTime(d.DispatchedAt)
	d.DeliveredAt = cloneTime(d.DeliveredAt)
	d.CancelledAt = cloneTime(d.CancelledAt)
	return d
}
