package outbox

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

var (
	ErrUnavailable      = errors.New("outbox unavailable")
	ErrInvalidEvent     = errors.New("invalid outbox event")
	ErrInvalidPolicy    = errors.New("invalid outbox policy")
	ErrSensitivePayload = errors.New("outbox payload must not contain secrets")
	ErrConflict         = errors.New("outbox idempotency conflict")
	ErrNotFound         = errors.New("outbox event not found")
	ErrStoreRequired    = errors.New("outbox store required")
	ErrHandlerRequired  = errors.New("outbox handler required")
	ErrDuplicateHandler = errors.New("duplicate outbox handler")
	ErrRelayRequired    = errors.New("outbox relay required")
)

const (
	ErrorClassUnknownHandler = "unknown_handler"
	ErrorClassHandlerFailed  = "handler_failed"
)

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

func (id ID) IsZero() bool {
	return id == ID{}
}

func (id ID) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:])
}

// Event is a durable outbox row. Payload is structured JSON, never secrets.
type Event struct {
	ID             ID
	EventType      string
	EventVersion   int
	AggregateType  *string
	AggregateID    *string
	Payload        json.RawMessage
	IdempotencyKey *string
	CorrelationID  *string
	CreatedAt      time.Time
	AvailableAt    time.Time
	ClaimedAt      *time.Time
	ClaimUntil     *time.Time
	CompletedAt    *time.Time
	Attempts       int
	LastErrorClass *string
}

func (e Event) Validate() error {
	if e.ID.IsZero() {
		return ErrInvalidEvent
	}
	if strings.TrimSpace(e.EventType) == "" || e.EventType != strings.TrimSpace(e.EventType) {
		return ErrInvalidEvent
	}
	if e.EventVersion <= 0 {
		return ErrInvalidEvent
	}
	if e.CreatedAt.IsZero() || e.AvailableAt.Before(e.CreatedAt) {
		return ErrInvalidEvent
	}
	if e.Attempts < 0 {
		return ErrInvalidEvent
	}
	if (e.ClaimedAt == nil) != (e.ClaimUntil == nil) {
		return ErrInvalidEvent
	}
	if e.ClaimedAt != nil && !e.ClaimUntil.After(*e.ClaimedAt) {
		return ErrInvalidEvent
	}
	return validatePayload(e.Payload)
}

func validatePayload(raw json.RawMessage) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return ErrInvalidEvent
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ErrInvalidEvent
	}
	switch v.(type) {
	case map[string]any, []any:
	default:
		return ErrInvalidEvent
	}
	if err := rejectSensitive(v); err != nil {
		return err
	}
	return nil
}

func rejectSensitive(v any) error {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if sensitiveKey(k) {
				return ErrSensitivePayload
			}
			if err := rejectSensitive(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range t {
			if err := rejectSensitive(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func sensitiveKey(k string) bool {
	var b strings.Builder
	for _, r := range strings.ToLower(k) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	switch b.String() {
	case "password", "passwd", "otp", "token", "secret", "cookie", "authorization",
		"sessiontoken", "accesstoken", "refreshtoken", "apikey", "privatekey",
		"rawtoken", "rawsecret", "rawotp":
		return true
	default:
		return false
	}
}

func isClaimable(e Event, now time.Time) bool {
	if e.CompletedAt != nil {
		return false
	}
	if now.Before(e.AvailableAt) {
		return false
	}
	if e.ClaimUntil != nil && now.Before(*e.ClaimUntil) {
		return false
	}
	return true
}
