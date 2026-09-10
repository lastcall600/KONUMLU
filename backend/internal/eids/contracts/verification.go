package contracts

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrZeroID       = errors.New("eids id must not be zero")
	ErrNotFound     = errors.New("eids verification not found")
	ErrNotRequired  = errors.New("listing category does not require eids")
	ErrWrongType    = errors.New("eids verification type does not match category policy")
	ErrUnavailable  = errors.New("eids unavailable")
	ErrInvalidType  = errors.New("invalid eids verification type")
	ErrInvalidInput = errors.New("invalid eids verification input")
)

const (
	TypeProperty VerificationType = "property"
	TypeVehicle  VerificationType = "vehicle"

	StatusPending     Status = "pending"
	StatusInProgress  Status = "in_progress"
	StatusVerified    Status = "verified"
	StatusFailed      Status = "failed"
	StatusUnavailable Status = "unavailable"
	StatusExpired     Status = "expired"
)

type VerificationType string
type Status string

func (t VerificationType) Valid() bool {
	switch t {
	case TypeProperty, TypeVehicle:
		return true
	default:
		return false
	}
}

func ParseType(raw string) (VerificationType, error) {
	t := VerificationType(strings.TrimSpace(raw))
	if !t.Valid() {
		return "", ErrInvalidType
	}
	return t, nil
}

type ID [16]byte

func (id ID) IsZero() bool {
	return id == ID{}
}

// VerificationView is the owner-safe EİDS record. Provider payloads and
// providerReference are never included.
type VerificationView struct {
	VerificationID   ID
	ListingID        ID
	VerificationType VerificationType
	Status           Status
	FailureCode      string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	RequestedAt      *time.Time
	VerifiedAt       *time.Time
	FailedAt         *time.Time
}

// OwnerVerification is the owner-facing start/read surface. Type is derived
// from Master Data policy, not client authority.
type OwnerVerification interface {
	StartForOwner(ctx context.Context, ownerUserID, listingID ID, clientType string) (VerificationView, error)
	CurrentForOwner(ctx context.Context, ownerUserID, listingID ID) (VerificationView, error)
}

// ListingPublishGate is the only EİDS read Listings may use at publish time.
// A kind-less boolean is forbidden. Unavailable/failed/expired/pending are not verified.
type ListingPublishGate interface {
	IsVerified(ctx context.Context, listingID ID, kind VerificationType) (bool, error)
}
