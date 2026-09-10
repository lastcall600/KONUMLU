package moderation

import (
	"context"
	"strings"
	"time"
)

func (s *Service) CreateCase(ctx context.Context, in CreateCaseInput) (CaseDetail, error) {
	if s == nil || s.store == nil {
		return CaseDetail{}, errStoreRequired
	}
	if in.SubjectID.IsZero() {
		return CaseDetail{}, errZeroID
	}
	subjectType, err := ParseTargetType(string(in.SubjectType))
	if err != nil {
		return CaseDetail{}, err
	}
	title, err := NormalizeCaseTitle(in.Title)
	if err != nil {
		return CaseDetail{}, err
	}
	priority, err := NormalizeCasePriority(in.Priority)
	if err != nil {
		return CaseDetail{}, err
	}
	reportIDs, err := uniqueReportIDs(in.ReportIDs)
	if err != nil {
		return CaseDetail{}, err
	}
	id, err := NewID()
	if err != nil {
		return CaseDetail{}, errUnavailable
	}
	now := s.now().UTC()
	row := Case{
		ID:              id,
		Status:          CaseStatusOpen,
		Priority:        priority,
		SubjectType:     subjectType,
		SubjectID:       in.SubjectID,
		Title:           title,
		AssignedStaffID: optionalID(in.AssignedStaffID),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := row.Validate(); err != nil {
		return CaseDetail{}, err
	}
	actor := optionalID(in.ActorID)
	history := make([]CaseHistory, 0, 1+len(reportIDs))
	createdHist, err := s.newHistory(id, HistoryCaseCreated, actor, now)
	if err != nil {
		return CaseDetail{}, err
	}
	history = append(history, createdHist)
	for i, reportID := range reportIDs {
		hist, err := s.newHistory(id, HistoryReportAttached, actor, now.Add(time.Duration(i+1)*time.Nanosecond))
		if err != nil {
			return CaseDetail{}, err
		}
		rid := reportID
		hist.ReportID = &rid
		if err := hist.Validate(); err != nil {
			return CaseDetail{}, err
		}
		history = append(history, hist)
	}
	if err := s.store.InsertCase(ctx, row, reportIDs, history); err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	return s.GetCase(ctx, id)
}

func (s *Service) GetCase(ctx context.Context, id ID) (CaseDetail, error) {
	if s == nil || s.store == nil {
		return CaseDetail{}, errStoreRequired
	}
	if id.IsZero() {
		return CaseDetail{}, errZeroID
	}
	row, err := s.store.GetCase(ctx, id)
	if err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	reportIDs, err := s.store.ListCaseReportIDs(ctx, id)
	if err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	history, err := s.store.ListCaseHistory(ctx, id)
	if err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	return CaseDetail{Case: row, ReportIDs: reportIDs, History: history}, nil
}

func (s *Service) ListCases(ctx context.Context, q CaseQuery) (CasePage, error) {
	if s == nil || s.store == nil {
		return CasePage{}, errStoreRequired
	}
	q, err := normalizeCaseQuery(q)
	if err != nil {
		return CasePage{}, err
	}
	var cursor *caseCursor
	if strings.TrimSpace(q.Cursor) != "" {
		decoded, err := decodeCaseCursor(q.Cursor)
		if err != nil {
			return CasePage{}, err
		}
		cursor = &decoded
	}
	rows, err := s.store.ListCases(ctx, q, cursor, q.Limit)
	if err != nil {
		return CasePage{}, mapStoreErr(err)
	}
	page := CasePage{Cases: rows}
	if len(rows) == q.Limit {
		next, err := encodeCaseCursor(rows[len(rows)-1])
		if err != nil {
			return CasePage{}, err
		}
		page.NextCursor = next
	}
	return page, nil
}

func (s *Service) AttachReports(ctx context.Context, caseID ID, in AttachReportsInput) (CaseDetail, error) {
	if s == nil || s.store == nil {
		return CaseDetail{}, errStoreRequired
	}
	if caseID.IsZero() {
		return CaseDetail{}, errZeroID
	}
	reportIDs, err := uniqueReportIDs(in.ReportIDs)
	if err != nil {
		return CaseDetail{}, err
	}
	if len(reportIDs) == 0 {
		return CaseDetail{}, errInvalidAttachment
	}
	row, err := s.store.GetCase(ctx, caseID)
	if err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	if !row.Status.IsActive() {
		return CaseDetail{}, errInvalidAttachment
	}
	now := s.now().UTC()
	actor := optionalID(in.ActorID)
	history := make([]CaseHistory, 0, len(reportIDs))
	for i, reportID := range reportIDs {
		hist, err := s.newHistory(caseID, HistoryReportAttached, actor, now.Add(time.Duration(i)*time.Nanosecond))
		if err != nil {
			return CaseDetail{}, err
		}
		rid := reportID
		hist.ReportID = &rid
		if err := hist.Validate(); err != nil {
			return CaseDetail{}, err
		}
		history = append(history, hist)
	}
	if err := s.store.AttachCaseReports(ctx, caseID, reportIDs, now, history); err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	return s.GetCase(ctx, caseID)
}

func (s *Service) TransitionCase(ctx context.Context, caseID ID, in CaseTransitionInput) (CaseDetail, error) {
	if s == nil || s.store == nil {
		return CaseDetail{}, errStoreRequired
	}
	if caseID.IsZero() {
		return CaseDetail{}, errZeroID
	}
	to, err := ParseCaseStatus(string(in.To))
	if err != nil {
		return CaseDetail{}, err
	}
	row, err := s.store.GetCase(ctx, caseID)
	if err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	if !CanTransitionCase(row.Status, to) {
		return CaseDetail{}, errInvalidTransition
	}
	now := s.now().UTC()
	fromStatus := row.Status
	updated := cloneCase(row)
	updated.Status = to
	updated.UpdatedAt = now
	hist, err := s.newHistory(caseID, HistoryStatusChanged, optionalID(in.ActorID), now)
	if err != nil {
		return CaseDetail{}, err
	}
	hist.FromStatus = &fromStatus
	hist.ToStatus = &to
	if err := hist.Validate(); err != nil {
		return CaseDetail{}, err
	}
	if err := s.store.UpdateCase(ctx, row, updated, hist); err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	return s.GetCase(ctx, caseID)
}

func (s *Service) SetCasePriority(ctx context.Context, caseID ID, in CasePriorityInput) (CaseDetail, error) {
	if s == nil || s.store == nil {
		return CaseDetail{}, errStoreRequired
	}
	if caseID.IsZero() {
		return CaseDetail{}, errZeroID
	}
	priority, err := ParseCasePriority(string(in.Priority))
	if err != nil {
		return CaseDetail{}, err
	}
	row, err := s.store.GetCase(ctx, caseID)
	if err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	if row.Status == CaseStatusClosed {
		return CaseDetail{}, errInvalidTransition
	}
	if row.Priority == priority {
		return s.GetCase(ctx, caseID)
	}
	now := s.now().UTC()
	fromPriority := row.Priority
	updated := cloneCase(row)
	updated.Priority = priority
	updated.UpdatedAt = now
	hist, err := s.newHistory(caseID, HistoryPriorityChanged, optionalID(in.ActorID), now)
	if err != nil {
		return CaseDetail{}, err
	}
	hist.FromPriority = &fromPriority
	hist.ToPriority = &priority
	if err := hist.Validate(); err != nil {
		return CaseDetail{}, err
	}
	if err := s.store.UpdateCase(ctx, row, updated, hist); err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	return s.GetCase(ctx, caseID)
}

func (s *Service) AssignCase(ctx context.Context, caseID ID, in CaseAssignmentInput) (CaseDetail, error) {
	if s == nil || s.store == nil {
		return CaseDetail{}, errStoreRequired
	}
	if caseID.IsZero() {
		return CaseDetail{}, errZeroID
	}
	if in.AssignedStaffID != nil && in.AssignedStaffID.IsZero() {
		return CaseDetail{}, errZeroID
	}
	row, err := s.store.GetCase(ctx, caseID)
	if err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	if row.Status == CaseStatusClosed {
		return CaseDetail{}, errInvalidTransition
	}
	next := optionalID(in.AssignedStaffID)
	if sameOptionalID(row.AssignedStaffID, next) {
		return s.GetCase(ctx, caseID)
	}
	now := s.now().UTC()
	updated := cloneCase(row)
	updated.AssignedStaffID = next
	updated.UpdatedAt = now
	hist, err := s.newHistory(caseID, HistoryAssignmentChanged, optionalID(in.ActorID), now)
	if err != nil {
		return CaseDetail{}, err
	}
	hist.FromAssignedStaffID = cloneOptionalID(row.AssignedStaffID)
	hist.ToAssignedStaffID = cloneOptionalID(next)
	if err := hist.Validate(); err != nil {
		return CaseDetail{}, err
	}
	if err := s.store.UpdateCase(ctx, row, updated, hist); err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	return s.GetCase(ctx, caseID)
}

func (s *Service) AddCaseNote(ctx context.Context, caseID ID, in CaseNoteInput) (CaseDetail, error) {
	if s == nil || s.store == nil {
		return CaseDetail{}, errStoreRequired
	}
	if caseID.IsZero() {
		return CaseDetail{}, errZeroID
	}
	note, err := NormalizeStaffNote(in.Note)
	if err != nil {
		return CaseDetail{}, err
	}
	if note == nil {
		return CaseDetail{}, errInvalidBody
	}
	row, err := s.store.GetCase(ctx, caseID)
	if err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	now := s.now().UTC()
	updated := cloneCase(row)
	updated.UpdatedAt = now
	hist, err := s.newHistory(caseID, HistoryStaffNoteAdded, optionalID(in.ActorID), now)
	if err != nil {
		return CaseDetail{}, err
	}
	hist.Note = note
	if err := hist.Validate(); err != nil {
		return CaseDetail{}, err
	}
	if err := s.store.UpdateCase(ctx, row, updated, hist); err != nil {
		return CaseDetail{}, mapStoreErr(err)
	}
	return s.GetCase(ctx, caseID)
}

func (s *Service) newHistory(caseID ID, kind CaseHistoryKind, actor *ID, now time.Time) (CaseHistory, error) {
	id, err := NewID()
	if err != nil {
		return CaseHistory{}, errUnavailable
	}
	return CaseHistory{
		ID:           id,
		CaseID:       caseID,
		Kind:         kind,
		ActorStaffID: actor,
		CreatedAt:    now,
	}, nil
}

func (s *Service) AddEvidence(ctx context.Context, caseID ID, in AddEvidenceInput) (CaseEvidence, error) {
	if s == nil || s.store == nil {
		return CaseEvidence{}, errStoreRequired
	}
	if caseID.IsZero() {
		return CaseEvidence{}, errZeroID
	}
	kind, err := ParseEvidenceType(string(in.EvidenceType))
	if err != nil {
		return CaseEvidence{}, err
	}
	title, err := NormalizeEvidenceTitle(in.Title)
	if err != nil {
		return CaseEvidence{}, err
	}
	description, err := NormalizeEvidenceDescription(in.Description)
	if err != nil {
		return CaseEvidence{}, err
	}
	reference, err := NormalizeEvidenceReference(in.ReferenceValue)
	if err != nil {
		return CaseEvidence{}, err
	}
	if err := validateEvidenceFields(kind, reference); err != nil {
		return CaseEvidence{}, err
	}
	if _, err := s.store.GetCase(ctx, caseID); err != nil {
		return CaseEvidence{}, mapStoreErr(err)
	}
	id, err := NewID()
	if err != nil {
		return CaseEvidence{}, errUnavailable
	}
	now := s.now().UTC()
	row := CaseEvidence{
		ID:             id,
		CaseID:         caseID,
		EvidenceType:   kind,
		Title:          title,
		Description:    description,
		ReferenceValue: reference,
		ActorStaffID:   optionalID(in.ActorID),
		CreatedAt:      now,
	}
	if err := row.Validate(); err != nil {
		return CaseEvidence{}, err
	}
	hist, err := s.newHistory(caseID, HistoryEvidenceAdded, optionalID(in.ActorID), now)
	if err != nil {
		return CaseEvidence{}, err
	}
	hist.EvidenceID = &id
	if err := hist.Validate(); err != nil {
		return CaseEvidence{}, err
	}
	if err := s.store.InsertEvidence(ctx, row, hist); err != nil {
		return CaseEvidence{}, mapStoreErr(err)
	}
	return cloneEvidence(row), nil
}

func (s *Service) ListEvidence(ctx context.Context, caseID ID, limit int) ([]CaseEvidence, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	if limit < 0 || limit > MaxEvidenceLimit {
		return nil, errInvalidQuery
	}
	if limit == 0 {
		limit = DefaultEvidenceLimit
	}
	if _, err := s.store.GetCase(ctx, caseID); err != nil {
		return nil, mapStoreErr(err)
	}
	rows, err := s.store.ListEvidence(ctx, caseID, limit)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return rows, nil
}

func uniqueReportIDs(ids []ID) ([]ID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	seen := make(map[ID]struct{}, len(ids))
	out := make([]ID, 0, len(ids))
	for _, id := range ids {
		if id.IsZero() {
			return nil, errZeroID
		}
		if _, ok := seen[id]; ok {
			return nil, errConflict
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}
