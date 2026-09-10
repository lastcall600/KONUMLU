package moderation

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	MaxDescriptionBytes         = 2000
	MaxStaffNoteBytes           = 2000
	MaxCaseTitleBytes           = 200
	MaxEvidenceTitleBytes       = 200
	MaxEvidenceDescriptionBytes = 2000
	MaxEvidenceReferenceBytes   = 512
	MaxActionRationaleBytes     = 2000
	MaxAppealStatementBytes     = 2000
	DefaultActionLimit          = 50
	MaxActionLimit              = 100
	DefaultAppealLimit          = 50
	MaxAppealLimit              = 100
	MaxMineReports              = 50
	MaxMineAppeals              = 50
	DefaultQueueLimit           = 20
	MaxQueueLimit               = 50
	DefaultCaseLimit            = 20
	MaxCaseLimit                = 50
	DefaultEvidenceLimit        = 50
	MaxEvidenceLimit            = 100
	DefaultDuplicateWindow      = 24 * time.Hour
	DefaultAppealWindow         = 14 * 24 * time.Hour

	QueueNewest QueueOrder = "newest"
	QueueOldest QueueOrder = "oldest"

	TargetListing       TargetType = "listing"
	TargetPublicProfile TargetType = "public_profile"

	StatusSubmitted Status = "submitted"
	StatusTriaged   Status = "triaged"
	StatusClosed    Status = "closed"

	ReasonSpam                 ReasonCode = "spam"
	ReasonScamOrFraud          ReasonCode = "scam_or_fraud"
	ReasonProhibitedItem       ReasonCode = "prohibited_item"
	ReasonHarassment           ReasonCode = "harassment"
	ReasonImpersonation        ReasonCode = "impersonation"
	ReasonInappropriateContent ReasonCode = "inappropriate_content"
	ReasonOther                ReasonCode = "other"
)

var (
	errZeroID              = errors.New("moderation id must not be zero")
	errStoreRequired       = errors.New("moderation store required")
	errUnavailable         = errors.New("moderation unavailable")
	errNotFound            = errors.New("moderation target not found")
	errListingsReq         = errors.New("listings source required")
	errProfilesReq         = errors.New("public profile source required")
	errInvalidTarget       = errors.New("invalid moderation target")
	errInvalidReason       = errors.New("invalid moderation reason")
	errInvalidStatus       = errors.New("invalid moderation status")
	errInvalidReport       = errors.New("invalid moderation report")
	errInvalidPolicy       = errors.New("invalid moderation policy")
	errInvalidBody         = errors.New("invalid moderation description")
	errSelfReport          = errors.New("self report")
	errConflict            = errors.New("moderation report conflict")
	errInvalidQuery        = errors.New("invalid moderation query")
	errInvalidTransition   = errors.New("invalid moderation status transition")
	errInvalidPriority     = errors.New("invalid moderation case priority")
	errInvalidCaseStatus   = errors.New("invalid moderation case status")
	errInvalidCase         = errors.New("invalid moderation case")
	errInvalidAttachment   = errors.New("invalid moderation case attachment")
	errInvalidHistory      = errors.New("invalid moderation case history")
	errInvalidEvidence     = errors.New("invalid moderation case evidence")
	errInvalidAction       = errors.New("invalid moderation action")
	errInvalidActionStatus = errors.New("invalid moderation action status")
	errInvalidAppeal       = errors.New("invalid moderation appeal")
	errInvalidAppealStatus = errors.New("invalid moderation appeal status")
	errUnsupportedAction   = errors.New("moderation action not enforceable")
)

var (
	ErrZeroID              = errZeroID
	ErrStoreRequired       = errStoreRequired
	ErrUnavailable         = errUnavailable
	ErrNotFound            = errNotFound
	ErrListingsReq         = errListingsReq
	ErrProfilesReq         = errProfilesReq
	ErrInvalidTarget       = errInvalidTarget
	ErrInvalidReason       = errInvalidReason
	ErrInvalidStatus       = errInvalidStatus
	ErrInvalidBody         = errInvalidBody
	ErrSelfReport          = errSelfReport
	ErrConflict            = errConflict
	ErrInvalidQuery        = errInvalidQuery
	ErrInvalidTransition   = errInvalidTransition
	ErrInvalidPriority     = errInvalidPriority
	ErrInvalidCaseStatus   = errInvalidCaseStatus
	ErrInvalidCase         = errInvalidCase
	ErrInvalidAttachment   = errInvalidAttachment
	ErrInvalidHistory      = errInvalidHistory
	ErrInvalidEvidence     = errInvalidEvidence
	ErrInvalidAction       = errInvalidAction
	ErrInvalidActionStatus = errInvalidActionStatus
	ErrInvalidAppeal       = errInvalidAppeal
	ErrInvalidAppealStatus = errInvalidAppealStatus
	ErrUnsupportedAction   = errUnsupportedAction
)

// ID is a report, target, or user UUID. Moderation does not own identity or listing tables.
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

type TargetType string

func ParseTargetType(raw string) (TargetType, error) {
	switch TargetType(strings.TrimSpace(raw)) {
	case TargetListing, TargetPublicProfile:
		return TargetType(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidTarget
	}
}

type ReasonCode string

func ParseReasonCode(raw string) (ReasonCode, error) {
	switch ReasonCode(strings.TrimSpace(raw)) {
	case ReasonSpam, ReasonScamOrFraud, ReasonProhibitedItem, ReasonHarassment,
		ReasonImpersonation, ReasonInappropriateContent, ReasonOther:
		return ReasonCode(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidReason
	}
}

type QueueOrder string

func ParseQueueOrder(raw string) (QueueOrder, error) {
	switch QueueOrder(strings.TrimSpace(raw)) {
	case "", QueueNewest:
		return QueueNewest, nil
	case QueueOldest:
		return QueueOldest, nil
	default:
		return "", errInvalidQuery
	}
}

type Status string

func ParseStatus(raw string) (Status, error) {
	switch Status(strings.TrimSpace(raw)) {
	case StatusSubmitted, StatusTriaged, StatusClosed:
		return Status(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidStatus
	}
}

type Policy struct {
	DuplicateWindow time.Duration
	AppealWindow    time.Duration
}

func (p Policy) Validate() error {
	if p.DuplicateWindow <= 0 || p.AppealWindow <= 0 {
		return errInvalidPolicy
	}
	return nil
}

func DefaultPolicy() Policy {
	return Policy{DuplicateWindow: DefaultDuplicateWindow, AppealWindow: DefaultAppealWindow}
}

// Report is a user-submitted moderation signal. It is not a staff case or a punishment.
type Report struct {
	ID              ID
	ReporterUserID  ID
	TargetType      TargetType
	TargetID        ID
	ReasonCode      ReasonCode
	Description     *string
	Status          Status
	StaffNote       *string
	StatusChangedBy *ID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (r Report) Validate() error {
	if r.ID.IsZero() || r.ReporterUserID.IsZero() || r.TargetID.IsZero() {
		return errZeroID
	}
	if _, err := ParseTargetType(string(r.TargetType)); err != nil {
		return err
	}
	if _, err := ParseReasonCode(string(r.ReasonCode)); err != nil {
		return err
	}
	if _, err := ParseStatus(string(r.Status)); err != nil {
		return err
	}
	if r.Description != nil {
		if _, err := NormalizeDescription(*r.Description); err != nil {
			return err
		}
	}
	if r.StaffNote != nil {
		if _, err := NormalizeStaffNote(*r.StaffNote); err != nil {
			return err
		}
	}
	if r.StatusChangedBy != nil && r.StatusChangedBy.IsZero() {
		return errZeroID
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return errInvalidReport
	}
	return nil
}

func CanTransition(from, to Status) bool {
	return (from == StatusSubmitted && to == StatusTriaged) || (from == StatusTriaged && to == StatusClosed)
}

type CreateInput struct {
	TargetType  TargetType
	TargetID    ID
	ReasonCode  ReasonCode
	Description string
}

func NormalizeDescription(raw string) (*string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return nil, nil
	}
	if len(body) > MaxDescriptionBytes {
		return nil, errInvalidBody
	}
	return &body, nil
}

func NormalizeStaffNote(raw string) (*string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return nil, nil
	}
	if len(body) > MaxStaffNoteBytes {
		return nil, errInvalidBody
	}
	return &body, nil
}

type QueueQuery struct {
	Status     *Status
	TargetType *TargetType
	ReasonCode *ReasonCode
	Order      QueueOrder
	Cursor     string
	Limit      int
}

type QueuePage struct {
	Reports    []Report
	NextCursor string
}

type TransitionInput struct {
	To        Status
	StaffNote string
	ActorID   *ID
}
