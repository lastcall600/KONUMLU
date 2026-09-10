package disputes

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
	errZeroID            = errors.New("disputes id must not be zero")
	errInvalidDispute    = errors.New("invalid dispute")
	errInvalidStatus     = errors.New("invalid dispute status")
	errInvalidTransition = errors.New("invalid dispute status transition")
	errInvalidReason     = errors.New("invalid dispute reason")
	errInvalidStatement  = errors.New("invalid dispute statement")
	errInvalidResolution = errors.New("invalid dispute resolution")
	errInvalidEvidence   = errors.New("invalid dispute evidence")
	errInvalidPolicy     = errors.New("invalid dispute policy")
	errStoreRequired     = errors.New("disputes store required")
	errUnavailable       = errors.New("disputes unavailable")
	errNotFound          = errors.New("dispute not found")
	errForbidden         = errors.New("dispute access denied")
	errConflict          = errors.New("dispute conflict")
	errNotEligible       = errors.New("transaction is not eligible for dispute")
	errConcluded         = errors.New("transaction dispute is already concluded")
	errWindowClosed      = errors.New("dispute window is closed")
)

var (
	ErrZeroID            = errZeroID
	ErrInvalidDispute    = errInvalidDispute
	ErrInvalidStatus     = errInvalidStatus
	ErrInvalidTransition = errInvalidTransition
	ErrInvalidReason     = errInvalidReason
	ErrInvalidStatement  = errInvalidStatement
	ErrInvalidResolution = errInvalidResolution
	ErrInvalidEvidence   = errInvalidEvidence
	ErrInvalidPolicy     = errInvalidPolicy
	ErrStoreRequired     = errStoreRequired
	ErrUnavailable       = errUnavailable
	ErrNotFound          = errNotFound
	ErrForbidden         = errForbidden
	ErrConflict          = errConflict
	ErrNotEligible       = errNotEligible
	ErrConcluded         = errConcluded
	ErrWindowClosed      = errWindowClosed
)

const (
	MaxStatementRunes   = 2000
	MaxTitleRunes       = 200
	MaxDescriptionRunes = 2000
	MaxReferenceRunes   = 512

	DefaultWindow = 14 * 24 * time.Hour

	RoleRequester = "requester"
	RoleProvider  = "provider"
	RoleInternal  = "internal"
)

// V1CreateRule documents how a Dispute is created.
// A Transaction participant POSTs /v1/transactions/{transactionId}/dispute.
// Parties and openedBy are resolved from the session and Transactions contract.
// Resolution records an internal outcome only; it does not refund, reverse,
// cancel the Transaction, change Trust, or invoke Moderation.
const V1CreateRule = "participant-opened for eligible transaction within window"

type Status string

const (
	StatusOpen        Status = "open"
	StatusUnderReview Status = "under_review"
	StatusResolved    Status = "resolved"
	StatusClosed      Status = "closed"
	StatusWithdrawn   Status = "withdrawn"
)

func (s Status) valid() bool {
	switch s {
	case StatusOpen, StatusUnderReview, StatusResolved, StatusClosed, StatusWithdrawn:
		return true
	default:
		return false
	}
}

func (s Status) Active() bool {
	return s == StatusOpen || s == StatusUnderReview
}

func (s Status) Terminal() bool {
	return s == StatusClosed || s == StatusWithdrawn
}

func (s Status) Concluded() bool {
	return s == StatusResolved || s == StatusClosed
}

type ReasonCode string

const (
	ReasonItemOrServiceNotAsDescribed ReasonCode = "item_or_service_not_as_described"
	ReasonNonDelivery                 ReasonCode = "non_delivery"
	ReasonDamagedOrIncomplete         ReasonCode = "damaged_or_incomplete"
	ReasonPaymentIssue                ReasonCode = "payment_issue"
	ReasonCancellationIssue           ReasonCode = "cancellation_issue"
	ReasonOther                       ReasonCode = "other"
)

func (r ReasonCode) valid() bool {
	switch r {
	case ReasonItemOrServiceNotAsDescribed, ReasonNonDelivery, ReasonDamagedOrIncomplete,
		ReasonPaymentIssue, ReasonCancellationIssue, ReasonOther:
		return true
	default:
		return false
	}
}

type ResolutionCode string

const (
	ResolutionNoAction             ResolutionCode = "no_action"
	ResolutionBuyerFavored         ResolutionCode = "buyer_favored"
	ResolutionProviderFavored      ResolutionCode = "provider_favored"
	ResolutionMutualResolution     ResolutionCode = "mutual_resolution"
	ResolutionInsufficientEvidence ResolutionCode = "insufficient_evidence"
	ResolutionOther                ResolutionCode = "other"
)

func (r ResolutionCode) valid() bool {
	switch r {
	case ResolutionNoAction, ResolutionBuyerFavored, ResolutionProviderFavored,
		ResolutionMutualResolution, ResolutionInsufficientEvidence, ResolutionOther:
		return true
	default:
		return false
	}
}

type EvidenceType string

const (
	EvidencePartyStatement    EvidenceType = "party_statement"
	EvidenceExternalReference EvidenceType = "external_reference"
	EvidenceInternalReference EvidenceType = "internal_reference"
)

func (t EvidenceType) valid() bool {
	switch t {
	case EvidencePartyStatement, EvidenceExternalReference, EvidenceInternalReference:
		return true
	default:
		return false
	}
}

func (t EvidenceType) requiresReference() bool {
	return t == EvidenceExternalReference || t == EvidenceInternalReference
}

func (t EvidenceType) forbidsReference() bool {
	return t == EvidencePartyStatement
}

type Policy struct {
	Window time.Duration
}

func DefaultPolicy() Policy {
	return Policy{Window: DefaultWindow}
}

func (p Policy) normalized() (Policy, error) {
	if p.Window <= 0 {
		return Policy{}, errInvalidPolicy
	}
	return Policy{Window: p.Window}, nil
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
		return ID{}, errInvalidDispute
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidDispute
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

type Dispute struct {
	ID              ID
	TransactionID   ID
	RequesterUserID ID
	ProviderUserID  ID
	OpenedByUserID  ID
	ReasonCode      ReasonCode
	Statement       *string
	Status          Status
	ResolutionCode  *ResolutionCode
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ResolvedAt      *time.Time
}

func (d Dispute) Validate() error {
	if d.ID.IsZero() || d.TransactionID.IsZero() || d.RequesterUserID.IsZero() ||
		d.ProviderUserID.IsZero() || d.OpenedByUserID.IsZero() {
		return errZeroID
	}
	if d.RequesterUserID == d.ProviderUserID {
		return errInvalidDispute
	}
	if !d.Participant(d.OpenedByUserID) {
		return errInvalidDispute
	}
	if !d.ReasonCode.valid() {
		return errInvalidReason
	}
	if d.Statement != nil {
		if _, err := normalizeStatement(*d.Statement); err != nil {
			return err
		}
	}
	if !d.Status.valid() {
		return errInvalidStatus
	}
	if d.CreatedAt.IsZero() || d.UpdatedAt.Before(d.CreatedAt) {
		return errInvalidDispute
	}
	switch d.Status {
	case StatusOpen, StatusUnderReview, StatusWithdrawn:
		if d.ResolvedAt != nil || d.ResolutionCode != nil {
			return errInvalidDispute
		}
	case StatusResolved, StatusClosed:
		if d.ResolvedAt == nil || d.ResolutionCode == nil {
			return errInvalidDispute
		}
		if !d.ResolutionCode.valid() {
			return errInvalidResolution
		}
		if d.ResolvedAt.Before(d.CreatedAt) {
			return errInvalidDispute
		}
	}
	return nil
}

func (d Dispute) Participant(userID ID) bool {
	if userID.IsZero() {
		return false
	}
	return d.RequesterUserID == userID || d.ProviderUserID == userID
}

func (d Dispute) OpenedByRole() string {
	switch d.OpenedByUserID {
	case d.RequesterUserID:
		return RoleRequester
	case d.ProviderUserID:
		return RoleProvider
	default:
		return ""
	}
}

func (d Dispute) RoleOf(userID ID) string {
	switch userID {
	case d.RequesterUserID:
		return RoleRequester
	case d.ProviderUserID:
		return RoleProvider
	default:
		return ""
	}
}

type CreateCommand struct {
	TransactionID   ID
	RequesterUserID ID
	ProviderUserID  ID
	OpenedByUserID  ID
	ReasonCode      ReasonCode
	Statement       *string
}

func CreateOpen(cmd CreateCommand, now time.Time) (Dispute, error) {
	if cmd.TransactionID.IsZero() || cmd.RequesterUserID.IsZero() ||
		cmd.ProviderUserID.IsZero() || cmd.OpenedByUserID.IsZero() {
		return Dispute{}, errZeroID
	}
	if now.IsZero() {
		return Dispute{}, errInvalidDispute
	}
	statement, err := cloneNormalizedStatement(cmd.Statement)
	if err != nil {
		return Dispute{}, err
	}
	id, err := NewID()
	if err != nil {
		return Dispute{}, errUnavailable
	}
	stamp := now.UTC()
	d := Dispute{
		ID:              id,
		TransactionID:   cmd.TransactionID,
		RequesterUserID: cmd.RequesterUserID,
		ProviderUserID:  cmd.ProviderUserID,
		OpenedByUserID:  cmd.OpenedByUserID,
		ReasonCode:      cmd.ReasonCode,
		Statement:       statement,
		Status:          StatusOpen,
		CreatedAt:       stamp,
		UpdatedAt:       stamp,
	}
	if err := d.Validate(); err != nil {
		return Dispute{}, err
	}
	return d, nil
}

func (d Dispute) StartReview(now time.Time) (Dispute, bool, error) {
	if d.Status == StatusUnderReview {
		return d, false, nil
	}
	if d.Status != StatusOpen {
		return Dispute{}, false, errInvalidTransition
	}
	next := d.withStatus(StatusUnderReview, now)
	if err := next.Validate(); err != nil {
		return Dispute{}, false, err
	}
	return next, true, nil
}

func (d Dispute) Resolve(code ResolutionCode, now time.Time) (Dispute, bool, error) {
	if !code.valid() {
		return Dispute{}, false, errInvalidResolution
	}
	if d.Status == StatusResolved {
		if d.ResolutionCode != nil && *d.ResolutionCode == code {
			return d, false, nil
		}
		return Dispute{}, false, errConflict
	}
	if d.Status != StatusUnderReview {
		return Dispute{}, false, errInvalidTransition
	}
	next := d.withStatus(StatusResolved, now)
	stamp := next.UpdatedAt
	next.ResolvedAt = &stamp
	next.ResolutionCode = &code
	if err := next.Validate(); err != nil {
		return Dispute{}, false, err
	}
	return next, true, nil
}

func (d Dispute) Close(now time.Time) (Dispute, bool, error) {
	if d.Status == StatusClosed {
		return d, false, nil
	}
	if d.Status != StatusResolved {
		return Dispute{}, false, errInvalidTransition
	}
	next := d.withStatus(StatusClosed, now)
	if err := next.Validate(); err != nil {
		return Dispute{}, false, err
	}
	return next, true, nil
}

func (d Dispute) Withdraw(now time.Time) (Dispute, bool, error) {
	if d.Status == StatusWithdrawn {
		return d, false, nil
	}
	if d.Status != StatusOpen {
		return Dispute{}, false, errInvalidTransition
	}
	next := d.withStatus(StatusWithdrawn, now)
	if err := next.Validate(); err != nil {
		return Dispute{}, false, err
	}
	return next, true, nil
}

func (d Dispute) AcceptsEvidence() bool {
	return d.Status == StatusOpen || d.Status == StatusUnderReview
}

func (d Dispute) withStatus(next Status, now time.Time) Dispute {
	d.Status = next
	d.UpdatedAt = now.UTC()
	return d
}

type Evidence struct {
	ID             ID
	DisputeID      ID
	EvidenceType   EvidenceType
	Title          string
	Description    *string
	ReferenceValue *string
	ActorUserID    *ID
	ActorRole      string
	CreatedAt      time.Time
}

func (e Evidence) Validate() error {
	if e.ID.IsZero() || e.DisputeID.IsZero() {
		return errZeroID
	}
	if !e.EvidenceType.valid() {
		return errInvalidEvidence
	}
	title, err := normalizeTitle(e.Title)
	if err != nil {
		return err
	}
	if title != e.Title {
		return errInvalidEvidence
	}
	if e.Description != nil {
		if _, err := normalizeOptionalText(*e.Description, MaxDescriptionRunes, errInvalidEvidence); err != nil {
			return err
		}
	}
	if e.EvidenceType.forbidsReference() && e.ReferenceValue != nil {
		return errInvalidEvidence
	}
	if e.EvidenceType.requiresReference() {
		if e.ReferenceValue == nil {
			return errInvalidEvidence
		}
		if _, err := normalizeOptionalText(*e.ReferenceValue, MaxReferenceRunes, errInvalidEvidence); err != nil {
			return err
		}
	}
	switch e.ActorRole {
	case RoleRequester, RoleProvider, RoleInternal:
	default:
		return errInvalidEvidence
	}
	if e.ActorRole == RoleInternal {
		if e.EvidenceType != EvidenceInternalReference {
			return errInvalidEvidence
		}
	} else if e.EvidenceType == EvidenceInternalReference {
		return errInvalidEvidence
	}
	if e.CreatedAt.IsZero() {
		return errInvalidEvidence
	}
	return nil
}

type EvidenceCommand struct {
	DisputeID      ID
	EvidenceType   EvidenceType
	Title          string
	Description    *string
	ReferenceValue *string
	ActorUserID    *ID
	ActorRole      string
}

func CreateEvidence(cmd EvidenceCommand, now time.Time) (Evidence, error) {
	if cmd.DisputeID.IsZero() {
		return Evidence{}, errZeroID
	}
	if now.IsZero() {
		return Evidence{}, errInvalidEvidence
	}
	title, err := normalizeTitle(cmd.Title)
	if err != nil {
		return Evidence{}, err
	}
	desc, err := cloneNormalizedOptional(cmd.Description, MaxDescriptionRunes, errInvalidEvidence)
	if err != nil {
		return Evidence{}, err
	}
	ref, err := cloneNormalizedOptional(cmd.ReferenceValue, MaxReferenceRunes, errInvalidEvidence)
	if err != nil {
		return Evidence{}, err
	}
	id, err := NewID()
	if err != nil {
		return Evidence{}, errUnavailable
	}
	e := Evidence{
		ID:             id,
		DisputeID:      cmd.DisputeID,
		EvidenceType:   cmd.EvidenceType,
		Title:          title,
		Description:    desc,
		ReferenceValue: ref,
		ActorUserID:    cloneID(cmd.ActorUserID),
		ActorRole:      strings.TrimSpace(cmd.ActorRole),
		CreatedAt:      now.UTC(),
	}
	if err := e.Validate(); err != nil {
		return Evidence{}, err
	}
	return e, nil
}

func normalizeStatement(s string) (string, error) {
	return normalizeOptionalText(s, MaxStatementRunes, errInvalidStatement)
}

func cloneNormalizedStatement(s *string) (*string, error) {
	return cloneNormalizedOptional(s, MaxStatementRunes, errInvalidStatement)
}

func normalizeTitle(s string) (string, error) {
	return normalizeOptionalText(s, MaxTitleRunes, errInvalidEvidence)
}

func normalizeOptionalText(s string, max int, invalid error) (string, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return "", invalid
	}
	if utf8.RuneCountInString(v) > max {
		return "", invalid
	}
	return v, nil
}

func cloneNormalizedOptional(s *string, max int, invalid error) (*string, error) {
	if s == nil {
		return nil, nil
	}
	v, err := normalizeOptionalText(*s, max, invalid)
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

func cloneResolution(c *ResolutionCode) *ResolutionCode {
	if c == nil {
		return nil
	}
	v := *c
	return &v
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}

func cloneID(id *ID) *ID {
	if id == nil {
		return nil
	}
	v := *id
	return &v
}

func cloneDispute(d Dispute) Dispute {
	d.Statement = cloneString(d.Statement)
	d.ResolutionCode = cloneResolution(d.ResolutionCode)
	d.ResolvedAt = cloneTime(d.ResolvedAt)
	return d
}

func cloneEvidence(e Evidence) Evidence {
	e.Description = cloneString(e.Description)
	e.ReferenceValue = cloneString(e.ReferenceValue)
	e.ActorUserID = cloneID(e.ActorUserID)
	return e
}
