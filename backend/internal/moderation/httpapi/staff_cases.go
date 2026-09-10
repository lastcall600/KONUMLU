package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/moderation"
	staffauth "backend/internal/staffauth/contracts"
)

func (h *StaffHandler) createCase(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermModerationCaseWrite)
	if !ok {
		return
	}
	var req createCaseRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	subjectType, err := moderation.ParseTargetType(req.SubjectType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	subjectID, err := moderation.ParseID(strings.TrimSpace(req.SubjectID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	reportIDs, ok := parseReportIDList(w, req.ReportIDs)
	if !ok {
		return
	}
	in := moderation.CreateCaseInput{
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Title:       req.Title,
		Priority:    req.Priority,
		ReportIDs:   reportIDs,
	}
	if assigned := strings.TrimSpace(req.AssignedStaffID); assigned != "" {
		id, err := moderation.ParseID(assigned)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		in.AssignedStaffID = &id
	}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	detail, err := h.svc.CreateCase(r.Context(), in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffCaseDTO(detail))
}

func (h *StaffHandler) listCases(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermModerationCaseRead); !ok {
		return
	}
	q, ok := parseCaseQuery(w, r)
	if !ok {
		return
	}
	page, err := h.svc.ListCases(r.Context(), q)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	out := make([]staffCaseSummaryDTO, 0, len(page.Cases))
	for _, row := range page.Cases {
		out = append(out, toStaffCaseSummaryDTO(row))
	}
	writeJSON(w, http.StatusOK, staffCaseListDTO{Cases: out, NextCursor: omitEmpty(page.NextCursor)})
}

func (h *StaffHandler) getCase(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermModerationCaseRead); !ok {
		return
	}
	id, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	detail, err := h.svc.GetCase(r.Context(), id)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffCaseDTO(detail))
}

func (h *StaffHandler) attachReports(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermModerationCaseWrite)
	if !ok {
		return
	}
	id, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	var req attachReportsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	reportIDs, ok := parseReportIDList(w, req.ReportIDs)
	if !ok {
		return
	}
	in := moderation.AttachReportsInput{ReportIDs: reportIDs}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	detail, err := h.svc.AttachReports(r.Context(), id, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffCaseDTO(detail))
}

func (h *StaffHandler) transitionCase(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermModerationCaseWrite)
	if !ok {
		return
	}
	id, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	var req caseStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	to, err := moderation.ParseCaseStatus(req.Status)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	in := moderation.CaseTransitionInput{To: to}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	detail, err := h.svc.TransitionCase(r.Context(), id, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffCaseDTO(detail))
}

func (h *StaffHandler) setCasePriority(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermModerationCaseWrite)
	if !ok {
		return
	}
	id, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	var req casePriorityRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	priority, err := moderation.ParseCasePriority(req.Priority)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	in := moderation.CasePriorityInput{Priority: priority}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	detail, err := h.svc.SetCasePriority(r.Context(), id, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffCaseDTO(detail))
}

func (h *StaffHandler) assignCase(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermModerationCaseWrite)
	if !ok {
		return
	}
	id, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	var req caseAssignmentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := moderation.CaseAssignmentInput{}
	if assigned := strings.TrimSpace(req.AssignedStaffID); assigned != "" {
		staffID, err := moderation.ParseID(assigned)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		in.AssignedStaffID = &staffID
	}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	detail, err := h.svc.AssignCase(r.Context(), id, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffCaseDTO(detail))
}

func (h *StaffHandler) addCaseNote(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermModerationCaseWrite)
	if !ok {
		return
	}
	id, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	var req caseNoteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := moderation.CaseNoteInput{Note: req.Note}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	detail, err := h.svc.AddCaseNote(r.Context(), id, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffCaseDTO(detail))
}

func (h *StaffHandler) addEvidence(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermModerationCaseWrite)
	if !ok {
		return
	}
	id, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	var req addEvidenceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	kind, err := moderation.ParseEvidenceType(req.EvidenceType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	in := moderation.AddEvidenceInput{
		EvidenceType:   kind,
		Title:          req.Title,
		Description:    req.Description,
		ReferenceValue: req.ReferenceValue,
	}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	row, err := h.svc.AddEvidence(r.Context(), id, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffEvidenceDTO(row))
}

func (h *StaffHandler) listEvidence(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermModerationCaseRead); !ok {
		return
	}
	id, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		limit = n
	}
	rows, err := h.svc.ListEvidence(r.Context(), id, limit)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	out := make([]staffEvidenceDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toStaffEvidenceDTO(row))
	}
	writeJSON(w, http.StatusOK, staffEvidenceListDTO{Evidence: out})
}

func parseCaseID(w http.ResponseWriter, r *http.Request) (moderation.ID, bool) {
	id, err := moderation.ParseID(strings.TrimSpace(r.PathValue("caseId")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return moderation.ID{}, false
	}
	return id, true
}

func parseReportIDList(w http.ResponseWriter, raw []string) ([]moderation.ID, bool) {
	out := make([]moderation.ID, 0, len(raw))
	for _, item := range raw {
		id, err := moderation.ParseID(strings.TrimSpace(item))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return nil, false
		}
		out = append(out, id)
	}
	return out, true
}

func parseCaseQuery(w http.ResponseWriter, r *http.Request) (moderation.CaseQuery, bool) {
	q := r.URL.Query()
	out := moderation.CaseQuery{Cursor: strings.TrimSpace(q.Get("cursor"))}
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		st, err := moderation.ParseCaseStatus(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return moderation.CaseQuery{}, false
		}
		out.Status = &st
	}
	if raw := strings.TrimSpace(q.Get("priority")); raw != "" {
		pr, err := moderation.ParseCasePriority(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return moderation.CaseQuery{}, false
		}
		out.Priority = &pr
	}
	if raw := strings.TrimSpace(q.Get("subjectType")); raw != "" {
		tt, err := moderation.ParseTargetType(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return moderation.CaseQuery{}, false
		}
		out.SubjectType = &tt
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return moderation.CaseQuery{}, false
		}
		out.Limit = n
	}
	return out, true
}

type createCaseRequest struct {
	SubjectType     string   `json:"subjectType"`
	SubjectID       string   `json:"subjectId"`
	Title           string   `json:"title"`
	Priority        string   `json:"priority"`
	ReportIDs       []string `json:"reportIds"`
	AssignedStaffID string   `json:"assignedStaffId"`
}

type attachReportsRequest struct {
	ReportIDs []string `json:"reportIds"`
}

type caseStatusRequest struct {
	Status string `json:"status"`
}

type casePriorityRequest struct {
	Priority string `json:"priority"`
}

type caseAssignmentRequest struct {
	AssignedStaffID string `json:"assignedStaffId"`
}

type caseNoteRequest struct {
	Note string `json:"note"`
}

type addEvidenceRequest struct {
	EvidenceType   string `json:"evidenceType"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	ReferenceValue string `json:"referenceValue"`
}

type staffEvidenceDTO struct {
	EvidenceID     string  `json:"evidenceId"`
	CaseID         string  `json:"caseId"`
	EvidenceType   string  `json:"evidenceType"`
	Title          string  `json:"title"`
	Description    *string `json:"description,omitempty"`
	ReferenceValue *string `json:"referenceValue,omitempty"`
	ActorStaffID   *string `json:"actorStaffId,omitempty"`
	CreatedAt      string  `json:"createdAt"`
}

type staffEvidenceListDTO struct {
	Evidence []staffEvidenceDTO `json:"evidence"`
}

type staffCaseSummaryDTO struct {
	CaseID          string  `json:"caseId"`
	Status          string  `json:"status"`
	Priority        string  `json:"priority"`
	SubjectType     string  `json:"subjectType"`
	SubjectID       string  `json:"subjectId"`
	Title           string  `json:"title"`
	AssignedStaffID *string `json:"assignedStaffId,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
}

type staffCaseHistoryDTO struct {
	Kind                string  `json:"kind"`
	ActorStaffID        *string `json:"actorStaffId,omitempty"`
	ReportID            *string `json:"reportId,omitempty"`
	FromStatus          *string `json:"fromStatus,omitempty"`
	ToStatus            *string `json:"toStatus,omitempty"`
	FromPriority        *string `json:"fromPriority,omitempty"`
	ToPriority          *string `json:"toPriority,omitempty"`
	FromAssignedStaffID *string `json:"fromAssignedStaffId,omitempty"`
	ToAssignedStaffID   *string `json:"toAssignedStaffId,omitempty"`
	Note                *string `json:"note,omitempty"`
	EvidenceID          *string `json:"evidenceId,omitempty"`
	ActionID            *string `json:"actionId,omitempty"`
	AppealID            *string `json:"appealId,omitempty"`
	CreatedAt           string  `json:"createdAt"`
}

type staffCaseDTO struct {
	staffCaseSummaryDTO
	ReportIDs []string              `json:"reportIds"`
	History   []staffCaseHistoryDTO `json:"history"`
}

type staffCaseListDTO struct {
	Cases      []staffCaseSummaryDTO `json:"cases"`
	NextCursor *string               `json:"nextCursor,omitempty"`
}

func toStaffCaseSummaryDTO(row moderation.Case) staffCaseSummaryDTO {
	out := staffCaseSummaryDTO{
		CaseID:      row.ID.String(),
		Status:      string(row.Status),
		Priority:    string(row.Priority),
		SubjectType: string(row.SubjectType),
		SubjectID:   row.SubjectID.String(),
		Title:       row.Title,
		CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.AssignedStaffID != nil && !row.AssignedStaffID.IsZero() {
		s := row.AssignedStaffID.String()
		out.AssignedStaffID = &s
	}
	return out
}

func toStaffCaseDTO(detail moderation.CaseDetail) staffCaseDTO {
	ids := make([]string, 0, len(detail.ReportIDs))
	for _, id := range detail.ReportIDs {
		ids = append(ids, id.String())
	}
	history := make([]staffCaseHistoryDTO, 0, len(detail.History))
	for _, row := range detail.History {
		history = append(history, toStaffCaseHistoryDTO(row))
	}
	return staffCaseDTO{
		staffCaseSummaryDTO: toStaffCaseSummaryDTO(detail.Case),
		ReportIDs:           ids,
		History:             history,
	}
}

func toStaffCaseHistoryDTO(row moderation.CaseHistory) staffCaseHistoryDTO {
	out := staffCaseHistoryDTO{
		Kind:      string(row.Kind),
		Note:      row.Note,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
	}
	out.ActorStaffID = idString(row.ActorStaffID)
	out.ReportID = idString(row.ReportID)
	out.EvidenceID = idString(row.EvidenceID)
	out.ActionID = idString(row.ActionID)
	out.AppealID = idString(row.AppealID)
	out.FromAssignedStaffID = idString(row.FromAssignedStaffID)
	out.ToAssignedStaffID = idString(row.ToAssignedStaffID)
	if row.FromStatus != nil {
		s := string(*row.FromStatus)
		out.FromStatus = &s
	}
	if row.ToStatus != nil {
		s := string(*row.ToStatus)
		out.ToStatus = &s
	}
	if row.FromPriority != nil {
		s := string(*row.FromPriority)
		out.FromPriority = &s
	}
	if row.ToPriority != nil {
		s := string(*row.ToPriority)
		out.ToPriority = &s
	}
	return out
}

func toStaffEvidenceDTO(row moderation.CaseEvidence) staffEvidenceDTO {
	out := staffEvidenceDTO{
		EvidenceID:     row.ID.String(),
		CaseID:         row.CaseID.String(),
		EvidenceType:   string(row.EvidenceType),
		Title:          row.Title,
		Description:    row.Description,
		ReferenceValue: row.ReferenceValue,
		CreatedAt:      row.CreatedAt.UTC().Format(time.RFC3339),
	}
	out.ActorStaffID = idString(row.ActorStaffID)
	return out
}

func idString(id *moderation.ID) *string {
	if id == nil || id.IsZero() {
		return nil
	}
	s := id.String()
	return &s
}
