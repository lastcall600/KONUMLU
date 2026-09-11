package moderation

import (
	"strings"
	"time"
)

const (
	CaseStatusOpen          CaseStatus = "open"
	CaseStatusInvestigating CaseStatus = "investigating"
	CaseStatusResolved      CaseStatus = "resolved"
	CaseStatusClosed        CaseStatus = "closed"

	CasePriorityLow    CasePriority = "low"
	CasePriorityNormal CasePriority = "normal"
	CasePriorityHigh   CasePriority = "high"
	CasePriorityUrgent CasePriority = "urgent"

	HistoryCaseCreated       CaseHistoryKind = "case_created"
	HistoryReportAttached    CaseHistoryKind = "report_attached"
	HistoryStatusChanged     CaseHistoryKind = "status_changed"
	HistoryPriorityChanged   CaseHistoryKind = "priority_changed"
	HistoryAssignmentChanged CaseHistoryKind = "assignment_changed"
	HistoryStaffNoteAdded    CaseHistoryKind = "staff_note_added"
	HistoryEvidenceAdded     CaseHistoryKind = "evidence_added"
)

type CaseStatus string

func ParseCaseStatus(raw string) (CaseStatus, error) {
	switch CaseStatus(strings.TrimSpace(raw)) {
	case CaseStatusOpen, CaseStatusInvestigating, CaseStatusResolved, CaseStatusClosed:
		return CaseStatus(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidCaseStatus
	}
}

func (s CaseStatus) IsActive() bool {
	return s == CaseStatusOpen || s == CaseStatusInvestigating
}

func CanTransitionCase(from, to CaseStatus) bool {
	return (from == CaseStatusOpen && to == CaseStatusInvestigating) ||
		(from == CaseStatusInvestigating && to == CaseStatusResolved) ||
		(from == CaseStatusResolved && to == CaseStatusClosed)
}

type CasePriority string

func ParseCasePriority(raw string) (CasePriority, error) {
	switch CasePriority(strings.TrimSpace(raw)) {
	case CasePriorityLow, CasePriorityNormal, CasePriorityHigh, CasePriorityUrgent:
		return CasePriority(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidPriority
	}
}

func NormalizeCasePriority(raw string) (CasePriority, error) {
	if strings.TrimSpace(raw) == "" {
		return CasePriorityNormal, nil
	}
	return ParseCasePriority(raw)
}

type CaseHistoryKind string

func ParseCaseHistoryKind(raw string) (CaseHistoryKind, error) {
	switch CaseHistoryKind(strings.TrimSpace(raw)) {
	case HistoryCaseCreated, HistoryReportAttached, HistoryStatusChanged,
		HistoryPriorityChanged, HistoryAssignmentChanged, HistoryStaffNoteAdded,
		HistoryEvidenceAdded, HistoryActionProposed, HistoryActionApproved,
		HistoryActionExecuted, HistoryActionCancelled, HistoryAppealSubmitted,
		HistoryAppealReviewStarted, HistoryAppealAccepted, HistoryAppealRejected,
		HistoryAppealWithdrawn, HistoryAppealRestorationApplied:
		return CaseHistoryKind(strings.TrimSpace(raw)), nil
	default:
		return "", errInvalidHistory
	}
}

func NormalizeCaseTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", errInvalidCase
	}
	if len(title) > MaxCaseTitleBytes {
		return "", errInvalidBody
	}
	return title, nil
}

// Case is a Moderation-owned investigation record. It does not copy report bodies.
type Case struct {
	ID              ID
	Status          CaseStatus
	Priority        CasePriority
	SubjectType     TargetType
	SubjectID       ID
	Title           string
	AssignedStaffID *ID
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (c Case) Validate() error {
	if c.ID.IsZero() || c.SubjectID.IsZero() {
		return errZeroID
	}
	if _, err := ParseCaseStatus(string(c.Status)); err != nil {
		return err
	}
	if _, err := ParseCasePriority(string(c.Priority)); err != nil {
		return err
	}
	if _, err := ParseTargetType(string(c.SubjectType)); err != nil {
		return err
	}
	if _, err := NormalizeCaseTitle(c.Title); err != nil {
		return err
	}
	if c.AssignedStaffID != nil && c.AssignedStaffID.IsZero() {
		return errZeroID
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		return errInvalidCase
	}
	return nil
}

type CaseHistory struct {
	ID                  ID
	CaseID              ID
	Kind                CaseHistoryKind
	ActorStaffID        *ID
	ReportID            *ID
	FromStatus          *CaseStatus
	ToStatus            *CaseStatus
	FromPriority        *CasePriority
	ToPriority          *CasePriority
	FromAssignedStaffID *ID
	ToAssignedStaffID   *ID
	Note                *string
	EvidenceID          *ID
	ActionID            *ID
	AppealID            *ID
	CreatedAt           time.Time
}

func (h CaseHistory) Validate() error {
	if h.ID.IsZero() || h.CaseID.IsZero() {
		return errZeroID
	}
	if _, err := ParseCaseHistoryKind(string(h.Kind)); err != nil {
		return err
	}
	if h.ActorStaffID != nil && h.ActorStaffID.IsZero() {
		return errZeroID
	}
	if h.ReportID != nil && h.ReportID.IsZero() {
		return errZeroID
	}
	if h.EvidenceID != nil && h.EvidenceID.IsZero() {
		return errZeroID
	}
	if h.ActionID != nil && h.ActionID.IsZero() {
		return errZeroID
	}
	if h.AppealID != nil && h.AppealID.IsZero() {
		return errZeroID
	}
	if h.FromAssignedStaffID != nil && h.FromAssignedStaffID.IsZero() {
		return errZeroID
	}
	if h.ToAssignedStaffID != nil && h.ToAssignedStaffID.IsZero() {
		return errZeroID
	}
	if h.Note != nil {
		if _, err := NormalizeStaffNote(*h.Note); err != nil {
			return err
		}
	}
	if h.CreatedAt.IsZero() {
		return errInvalidHistory
	}
	switch h.Kind {
	case HistoryReportAttached:
		if h.ReportID == nil {
			return errInvalidHistory
		}
	case HistoryStatusChanged:
		if h.FromStatus == nil || h.ToStatus == nil {
			return errInvalidHistory
		}
	case HistoryPriorityChanged:
		if h.FromPriority == nil || h.ToPriority == nil {
			return errInvalidHistory
		}
	case HistoryStaffNoteAdded:
		if h.Note == nil {
			return errInvalidHistory
		}
	case HistoryEvidenceAdded:
		if h.EvidenceID == nil || h.ActionID != nil || h.AppealID != nil {
			return errInvalidHistory
		}
	case HistoryActionProposed, HistoryActionApproved, HistoryActionExecuted, HistoryActionCancelled:
		if h.ActionID == nil || h.EvidenceID != nil || h.AppealID != nil {
			return errInvalidHistory
		}
	case HistoryAppealRestorationApplied:
		if h.AppealID == nil || h.ActionID == nil || h.EvidenceID != nil {
			return errInvalidHistory
		}
	case HistoryAppealSubmitted, HistoryAppealReviewStarted, HistoryAppealAccepted, HistoryAppealRejected, HistoryAppealWithdrawn:
		if h.AppealID == nil || h.EvidenceID != nil || h.ActionID != nil {
			return errInvalidHistory
		}
	default:
		if h.ActionID != nil || h.AppealID != nil {
			return errInvalidHistory
		}
	}
	return nil
}

type CreateCaseInput struct {
	SubjectType     TargetType
	SubjectID       ID
	Title           string
	Priority        string
	ReportIDs       []ID
	AssignedStaffID *ID
	ActorID         *ID
}

type CaseQuery struct {
	Status      *CaseStatus
	Priority    *CasePriority
	SubjectType *TargetType
	SubjectID   *ID
	Cursor      string
	Limit       int
}

type CasePage struct {
	Cases      []Case
	NextCursor string
}

type CaseDetail struct {
	Case      Case
	ReportIDs []ID
	History   []CaseHistory
}

type AttachReportsInput struct {
	ReportIDs []ID
	ActorID   *ID
}

type CaseTransitionInput struct {
	To      CaseStatus
	ActorID *ID
}

type CasePriorityInput struct {
	Priority CasePriority
	ActorID  *ID
}

type CaseAssignmentInput struct {
	AssignedStaffID *ID
	ActorID         *ID
}

type CaseNoteInput struct {
	Note    string
	ActorID *ID
}

func optionalID(id *ID) *ID {
	if id == nil || id.IsZero() {
		return nil
	}
	copyID := *id
	return &copyID
}

func sameOptionalID(a, b *ID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func cloneOptionalID(id *ID) *ID {
	return optionalID(id)
}

func cloneCase(row Case) Case {
	out := row
	out.AssignedStaffID = cloneOptionalID(row.AssignedStaffID)
	return out
}

func cloneHistory(row CaseHistory) CaseHistory {
	out := row
	out.ActorStaffID = cloneOptionalID(row.ActorStaffID)
	out.ReportID = cloneOptionalID(row.ReportID)
	out.EvidenceID = cloneOptionalID(row.EvidenceID)
	out.ActionID = cloneOptionalID(row.ActionID)
	out.AppealID = cloneOptionalID(row.AppealID)
	out.FromAssignedStaffID = cloneOptionalID(row.FromAssignedStaffID)
	out.ToAssignedStaffID = cloneOptionalID(row.ToAssignedStaffID)
	if row.FromStatus != nil {
		v := *row.FromStatus
		out.FromStatus = &v
	}
	if row.ToStatus != nil {
		v := *row.ToStatus
		out.ToStatus = &v
	}
	if row.FromPriority != nil {
		v := *row.FromPriority
		out.FromPriority = &v
	}
	if row.ToPriority != nil {
		v := *row.ToPriority
		out.ToPriority = &v
	}
	if row.Note != nil {
		v := *row.Note
		out.Note = &v
	}
	return out
}
