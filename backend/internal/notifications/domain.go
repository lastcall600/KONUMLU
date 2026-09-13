package notifications

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend/internal/notifications/contracts"
)

var (
	errZeroID           = errors.New("notifications id must not be zero")
	errInvalidDelivery  = errors.New("invalid notification delivery")
	errStoreRequired    = errors.New("notifications store required")
	errUnavailable      = errors.New("notifications unavailable")
	errInvalidEvent     = errors.New("invalid notification outbox event")
	errMaterialUnusable = errors.New("verification material not deliverable")
	errChannelMismatch  = errors.New("notification channel does not match verification kind")
	errProviderRequired = errors.New("notification provider required")
	errNotFound         = errors.New("notification resource not found")
	errInvalidQuery     = errors.New("invalid notification query")
)

// Exported sentinels for worker wiring and tests.
var (
	ErrInvalidDelivery  = errInvalidDelivery
	ErrStoreRequired    = errStoreRequired
	ErrUnavailable      = errUnavailable
	ErrInvalidEvent     = errInvalidEvent
	ErrSensitiveIntent  = contracts.ErrSensitivePayload
	ErrInvalidIntent    = contracts.ErrInvalidIntent
	ErrMaterialUnusable = errMaterialUnusable
	ErrChannelMismatch  = errChannelMismatch
	ErrProviderRequired = errProviderRequired
	ErrNotFound         = errNotFound
	ErrInvalidQuery     = errInvalidQuery
)

type Status string

const (
	StatusPending Status = "pending"
	StatusSending Status = "sending"
	StatusSent    Status = "sent"
	StatusFailed  Status = "failed"
)

func (s Status) valid() bool {
	switch s {
	case StatusPending, StatusSending, StatusSent, StatusFailed:
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
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 32 {
		return ID{}, errInvalidDelivery
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidDelivery
	}
	var id ID
	copy(id[:], b)
	return id, nil
}

func (id ID) IsZero() bool {
	return id == ID{}
}

func (id ID) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:])
}

// Delivery is durable per-channel work. It never stores verification secrets.
type Delivery struct {
	ID              ID
	IntentID        ID
	Channel         contracts.Channel
	TemplateCode    string
	TemplateVersion int
	Locale          string
	RecipientKind   contracts.RecipientKind
	RecipientID     ID
	Status          Status
	Attempts        int
	ProviderRef     *string
	CorrelationID   *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	LastAttemptAt   *time.Time
	CompletedAt     *time.Time
}

// WarningRecord is Notifications-owned user-safe warning content.
// Recipient identity lives on Delivery, not here.
type WarningRecord struct {
	IntentID     ID
	Locale       string
	TemplateCode string
	MessageKey   string
	TargetType   string
	TargetRef    ID
	ReasonCode   string
	ActionAt     time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (w WarningRecord) Validate() error {
	if w.IntentID.IsZero() || w.TargetRef.IsZero() {
		return errZeroID
	}
	if !contracts.ValidLocale(w.Locale) {
		return errInvalidDelivery
	}
	if w.TemplateCode != contracts.TemplateModerationWarningIssued {
		return errInvalidDelivery
	}
	if w.MessageKey != contracts.MessageKeyWarningListing && w.MessageKey != contracts.MessageKeyWarningPublicProfile {
		return errInvalidDelivery
	}
	if w.TargetType != contracts.WarningTargetListing && w.TargetType != contracts.WarningTargetPublicProfile {
		return errInvalidDelivery
	}
	if w.ReasonCode == "" || w.ActionAt.IsZero() || w.CreatedAt.IsZero() {
		return errInvalidDelivery
	}
	if w.UpdatedAt.Before(w.CreatedAt) {
		return errInvalidDelivery
	}
	return nil
}

func (d Delivery) Validate() error {
	if d.ID.IsZero() || d.IntentID.IsZero() || d.RecipientID.IsZero() {
		return errZeroID
	}
	if !contracts.ValidDeliveryChannel(d.Channel) {
		return errInvalidDelivery
	}
	if !contracts.ValidTemplateCode(d.TemplateCode) || d.TemplateVersion <= 0 {
		return errInvalidDelivery
	}
	if !contracts.ValidLocale(d.Locale) {
		return errInvalidDelivery
	}
	if err := (contracts.RecipientRef{Kind: d.RecipientKind, ID: d.RecipientID.String()}).Validate(); err != nil {
		return errInvalidDelivery
	}
	if !d.Status.valid() || d.Attempts < 0 {
		return errInvalidDelivery
	}
	if d.CreatedAt.IsZero() || d.UpdatedAt.Before(d.CreatedAt) {
		return errInvalidDelivery
	}
	if d.CompletedAt != nil && d.CompletedAt.Before(d.CreatedAt) {
		return errInvalidDelivery
	}
	return nil
}
