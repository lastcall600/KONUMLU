package eids

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	errZeroID              = errors.New("eids id must not be zero")
	errInvalidVerification = errors.New("invalid eids verification")
	errInvalidStatus       = errors.New("invalid eids verification status")
	errInvalidType         = errors.New("invalid eids verification type")
	errInvalidTransition   = errors.New("invalid eids verification status transition")
	errInvalidFailure      = errors.New("invalid eids failure code")
	errWrongType           = errors.New("eids verification type does not match category policy")
	errNotRequired         = errors.New("listing category does not require eids")
	errStoreRequired       = errors.New("eids store required")
	errGatewayRequired     = errors.New("eids gateway required")
	errUnavailable         = errors.New("eids unavailable")
	errNotFound            = errors.New("eids verification not found")
	errForbidden           = errors.New("eids access denied")
	errConflict            = errors.New("eids verification conflict")
	errReplayConflict      = errors.New("eids signed decision conflict")
	errUnmappedSubject     = errors.New("eids subject_ref not mapped")
	errDecisionRejected    = errors.New("eids signed decision rejected")
)

var (
	ErrZeroID              = errZeroID
	ErrInvalidVerification = errInvalidVerification
	ErrInvalidStatus       = errInvalidStatus
	ErrInvalidType         = errInvalidType
	ErrInvalidTransition   = errInvalidTransition
	ErrInvalidFailure      = errInvalidFailure
	ErrWrongType           = errWrongType
	ErrNotRequired         = errNotRequired
	ErrStoreRequired       = errStoreRequired
	ErrGatewayRequired     = errGatewayRequired
	ErrUnavailable         = errUnavailable
	ErrNotFound            = errNotFound
	ErrForbidden           = errForbidden
	ErrConflict            = errConflict
	ErrReplayConflict      = errReplayConflict
	ErrUnmappedSubject     = errUnmappedSubject
	ErrDecisionRejected    = errDecisionRejected
)

// VerificationType is listing EİDS kind. Property and vehicle never substitute.
type VerificationType string

const (
	TypeProperty VerificationType = "property"
	TypeVehicle  VerificationType = "vehicle"
)

func (t VerificationType) valid() bool {
	switch t {
	case TypeProperty, TypeVehicle:
		return true
	default:
		return false
	}
}

func ParseType(raw string) (VerificationType, error) {
	t := VerificationType(strings.TrimSpace(raw))
	if !t.valid() {
		return "", errInvalidType
	}
	return t, nil
}

type Status string

const (
	StatusPending     Status = "pending"
	StatusInProgress  Status = "in_progress"
	StatusVerified    Status = "verified"
	StatusFailed      Status = "failed"
	StatusUnavailable Status = "unavailable"
	StatusExpired     Status = "expired"
)

func (s Status) valid() bool {
	switch s {
	case StatusPending, StatusInProgress, StatusVerified, StatusFailed, StatusUnavailable, StatusExpired:
		return true
	default:
		return false
	}
}

func (s Status) Open() bool {
	return s == StatusPending || s == StatusInProgress || s == StatusUnavailable
}

func (s Status) Terminal() bool {
	return s == StatusVerified || s == StatusFailed || s == StatusExpired
}

func (s Status) PublishEligible() bool {
	return s == StatusVerified
}

type FailureCode string

const (
	FailureProviderUnavailable FailureCode = "provider_unavailable"
	FailureVerificationFailed  FailureCode = "verification_failed"
	FailureExpired             FailureCode = "expired"
)

func (c FailureCode) valid() bool {
	switch c {
	case "", FailureProviderUnavailable, FailureVerificationFailed, FailureExpired:
		return true
	default:
		return false
	}
}

type ID [16]byte

func NewID() (ID, error) {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		return ID{}, errUnavailable
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id, nil
}

func NewSubjectRef() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errUnavailable
	}
	return hex.EncodeToString(b[:]), nil
}

func ParseID(s string) (ID, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(s) != 32 {
		return ID{}, errInvalidVerification
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidVerification
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

// Verification is the Compliance/EİDS source of truth for one listing+kind attempt.
type Verification struct {
	ID                ID
	ListingID         ID
	VerificationType  VerificationType
	Status            Status
	ProviderReference *string
	FailureCode       FailureCode
	CreatedAt         time.Time
	UpdatedAt         time.Time
	RequestedAt       *time.Time
	VerifiedAt        *time.Time
	FailedAt          *time.Time
	ExpiresAt         *time.Time
}

func (v Verification) Validate() error {
	if v.ID.IsZero() || v.ListingID.IsZero() {
		return errZeroID
	}
	if !v.VerificationType.valid() {
		return errInvalidType
	}
	if !v.Status.valid() {
		return errInvalidStatus
	}
	if !v.FailureCode.valid() {
		return errInvalidFailure
	}
	if v.CreatedAt.IsZero() || v.UpdatedAt.Before(v.CreatedAt) {
		return errInvalidVerification
	}
	if v.ProviderReference != nil && strings.TrimSpace(*v.ProviderReference) == "" {
		return errInvalidVerification
	}
	if v.RequestedAt != nil && v.RequestedAt.Before(v.CreatedAt) {
		return errInvalidVerification
	}
	if v.ExpiresAt != nil && v.ExpiresAt.Before(v.CreatedAt) {
		return errInvalidVerification
	}
	switch v.Status {
	case StatusPending:
		if v.VerifiedAt != nil || v.FailedAt != nil {
			return errInvalidVerification
		}
		if v.FailureCode != "" {
			return errInvalidVerification
		}
	case StatusInProgress:
		if v.VerifiedAt != nil || v.FailedAt != nil {
			return errInvalidVerification
		}
		if v.RequestedAt == nil {
			return errInvalidVerification
		}
		if v.FailureCode != "" {
			return errInvalidVerification
		}
	case StatusVerified:
		if v.VerifiedAt == nil || v.FailedAt != nil {
			return errInvalidVerification
		}
		if v.FailureCode != "" {
			return errInvalidVerification
		}
	case StatusFailed:
		if v.FailedAt == nil || v.VerifiedAt != nil {
			return errInvalidVerification
		}
		if v.FailureCode != FailureVerificationFailed {
			return errInvalidVerification
		}
	case StatusUnavailable:
		if v.VerifiedAt != nil {
			return errInvalidVerification
		}
		if v.FailureCode != FailureProviderUnavailable {
			return errInvalidVerification
		}
	case StatusExpired:
		if v.VerifiedAt != nil {
			return errInvalidVerification
		}
		if v.FailureCode != FailureExpired {
			return errInvalidVerification
		}
	}
	return nil
}

func NewPending(listingID ID, kind VerificationType, now time.Time) (Verification, error) {
	if listingID.IsZero() {
		return Verification{}, errZeroID
	}
	if !kind.valid() {
		return Verification{}, errInvalidType
	}
	if now.IsZero() {
		return Verification{}, errInvalidVerification
	}
	id, err := NewID()
	if err != nil {
		return Verification{}, err
	}
	requested := now
	v := Verification{
		ID:               id,
		ListingID:        listingID,
		VerificationType: kind,
		Status:           StatusPending,
		CreatedAt:        now,
		UpdatedAt:        now,
		RequestedAt:      &requested,
	}
	if err := v.Validate(); err != nil {
		return Verification{}, err
	}
	return v, nil
}

func allowedTransition(from, to Status) bool {
	if from == to {
		return true
	}
	switch from {
	case StatusPending:
		switch to {
		case StatusInProgress, StatusVerified, StatusFailed, StatusUnavailable, StatusExpired:
			return true
		}
	case StatusInProgress:
		switch to {
		case StatusVerified, StatusFailed, StatusUnavailable, StatusExpired:
			return true
		}
	case StatusUnavailable:
		switch to {
		case StatusPending, StatusInProgress, StatusVerified, StatusFailed, StatusExpired:
			return true
		}
	case StatusVerified, StatusFailed, StatusExpired:
		return false
	}
	return false
}

// ApplySignedDecision sets this attempt from a TR signed decision.
// Gateway ApplyOutcome transitions stay unchanged; signed ingest may supersede
// an older applied state on the same verification row when issued_at is newer.
func (v Verification) ApplySignedDecision(approved bool, expiresAt *time.Time, now time.Time) (Verification, error) {
	if now.Before(v.CreatedAt) {
		return Verification{}, errInvalidVerification
	}
	next := v
	next.UpdatedAt = now
	requested := now
	if next.RequestedAt == nil {
		next.RequestedAt = &requested
	}
	at := now
	if approved {
		next.Status = StatusVerified
		next.VerifiedAt = &at
		next.FailedAt = nil
		next.FailureCode = ""
		if expiresAt != nil {
			next.ExpiresAt = cloneTimePtr(expiresAt)
		}
	} else {
		next.Status = StatusFailed
		next.FailedAt = &at
		next.VerifiedAt = nil
		next.FailureCode = FailureVerificationFailed
	}
	if err := next.Validate(); err != nil {
		return Verification{}, err
	}
	return next, nil
}

func (v Verification) ApplyOutcome(out ProviderOutcome, now time.Time) (Verification, error) {
	if now.Before(v.CreatedAt) {
		return Verification{}, errInvalidVerification
	}
	next := v
	next.UpdatedAt = now
	if out.ProviderReference != "" {
		ref := strings.TrimSpace(out.ProviderReference)
		if ref == "" {
			return Verification{}, errInvalidVerification
		}
		next.ProviderReference = &ref
	}
	requested := now
	if next.RequestedAt == nil {
		next.RequestedAt = &requested
	}
	switch out.Outcome {
	case OutcomeVerified:
		if !allowedTransition(v.Status, StatusVerified) {
			return Verification{}, errInvalidTransition
		}
		at := now
		next.Status = StatusVerified
		next.VerifiedAt = &at
		next.FailedAt = nil
		next.FailureCode = ""
		if out.ExpiresAt != nil {
			next.ExpiresAt = cloneTimePtr(out.ExpiresAt)
		}
	case OutcomeFailed:
		if !allowedTransition(v.Status, StatusFailed) {
			return Verification{}, errInvalidTransition
		}
		at := now
		next.Status = StatusFailed
		next.FailedAt = &at
		next.VerifiedAt = nil
		next.FailureCode = FailureVerificationFailed
	case OutcomeUnavailable:
		if !allowedTransition(v.Status, StatusUnavailable) {
			return Verification{}, errInvalidTransition
		}
		next.Status = StatusUnavailable
		next.VerifiedAt = nil
		next.FailureCode = FailureProviderUnavailable
	case OutcomeInProgress:
		if !allowedTransition(v.Status, StatusInProgress) {
			return Verification{}, errInvalidTransition
		}
		next.Status = StatusInProgress
		next.VerifiedAt = nil
		next.FailedAt = nil
		next.FailureCode = ""
	default:
		return Verification{}, errInvalidStatus
	}
	if err := next.Validate(); err != nil {
		return Verification{}, err
	}
	return next, nil
}

func (v Verification) Expire(now time.Time) (Verification, error) {
	if now.Before(v.CreatedAt) {
		return Verification{}, errInvalidVerification
	}
	if v.Status == StatusExpired {
		return v, nil
	}
	if v.Status == StatusFailed {
		return Verification{}, errInvalidTransition
	}
	if !allowedTransition(v.Status, StatusExpired) && v.Status != StatusVerified {
		return Verification{}, errInvalidTransition
	}
	next := v
	next.Status = StatusExpired
	next.UpdatedAt = now
	next.VerifiedAt = nil
	next.FailureCode = FailureExpired
	if err := next.Validate(); err != nil {
		return Verification{}, err
	}
	return next, nil
}

func (v Verification) IsExpired(now time.Time) bool {
	if v.Status == StatusExpired {
		return true
	}
	if v.ExpiresAt == nil {
		return false
	}
	return !now.Before(*v.ExpiresAt)
}

func (v Verification) Effective(now time.Time) (Verification, error) {
	if !v.IsExpired(now) || v.Status == StatusExpired || v.Status == StatusFailed {
		return v, nil
	}
	return v.Expire(now)
}

func cloneTimePtr(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	t := *in
	return &t
}

func cloneStringPtr(in *string) *string {
	if in == nil {
		return nil
	}
	s := *in
	return &s
}

func cloneVerification(v Verification) Verification {
	v.ProviderReference = cloneStringPtr(v.ProviderReference)
	v.RequestedAt = cloneTimePtr(v.RequestedAt)
	v.VerifiedAt = cloneTimePtr(v.VerifiedAt)
	v.FailedAt = cloneTimePtr(v.FailedAt)
	v.ExpiresAt = cloneTimePtr(v.ExpiresAt)
	return v
}
