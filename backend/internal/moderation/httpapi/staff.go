package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/moderation"
	staffauth "backend/internal/staffauth/contracts"
)

var ErrNotStaff = errors.New("not staff")

type StaffActor struct {
	ID        moderation.ID
	principal staffauth.Principal
}

type staffAuthorizer interface {
	Authenticate(*http.Request) (staffauth.Principal, error)
	Allows(staffauth.Principal, staffauth.Permission) bool
}

type StaffHandler struct {
	staff staffAuthorizer
	svc   *moderation.Service
}

func NewStaff(staff staffAuthorizer, svc *moderation.Service) (*StaffHandler, error) {
	if staff == nil || svc == nil {
		return nil, moderation.ErrUnavailable
	}
	return &StaffHandler{staff: staff, svc: svc}, nil
}

func (h *StaffHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/staff/moderation/reports", h.list)
	mux.HandleFunc("GET /v1/staff/moderation/reports/{reportId}", h.get)
	mux.HandleFunc("POST /v1/staff/moderation/reports/{reportId}/status", h.transition)
	mux.HandleFunc("GET /v1/staff/moderation/cases", h.listCases)
	mux.HandleFunc("POST /v1/staff/moderation/cases", h.createCase)
	mux.HandleFunc("GET /v1/staff/moderation/cases/{caseId}", h.getCase)
	mux.HandleFunc("POST /v1/staff/moderation/cases/{caseId}/reports", h.attachReports)
	mux.HandleFunc("POST /v1/staff/moderation/cases/{caseId}/status", h.transitionCase)
	mux.HandleFunc("POST /v1/staff/moderation/cases/{caseId}/priority", h.setCasePriority)
	mux.HandleFunc("POST /v1/staff/moderation/cases/{caseId}/assignment", h.assignCase)
	mux.HandleFunc("POST /v1/staff/moderation/cases/{caseId}/notes", h.addCaseNote)
	mux.HandleFunc("POST /v1/staff/moderation/cases/{caseId}/evidence", h.addEvidence)
	mux.HandleFunc("GET /v1/staff/moderation/cases/{caseId}/evidence", h.listEvidence)
	mux.HandleFunc("POST /v1/staff/moderation/cases/{caseId}/actions", h.createAction)
	mux.HandleFunc("GET /v1/staff/moderation/cases/{caseId}/actions", h.listActions)
	mux.HandleFunc("GET /v1/staff/moderation/cases/{caseId}/actions/{actionId}", h.getAction)
	mux.HandleFunc("POST /v1/staff/moderation/cases/{caseId}/actions/{actionId}/status", h.transitionAction)
	mux.HandleFunc("GET /v1/staff/moderation/cases/{caseId}/appeals", h.listAppeals)
	mux.HandleFunc("GET /v1/staff/moderation/cases/{caseId}/appeals/{appealId}", h.getStaffAppeal)
	mux.HandleFunc("POST /v1/staff/moderation/cases/{caseId}/appeals/{appealId}/status", h.transitionAppeal)
}

func (h *StaffHandler) requireStaff(w http.ResponseWriter, r *http.Request, perm staffauth.Permission) (StaffActor, bool) {
	actor, ok := h.authenticateStaff(w, r)
	if !ok {
		return StaffActor{}, false
	}
	if !h.staff.Allows(staffPrincipal(actor), perm) {
		writeError(w, http.StatusForbidden, "forbidden")
		return StaffActor{}, false
	}
	return actor, true
}

func (h *StaffHandler) authenticateStaff(w http.ResponseWriter, r *http.Request) (StaffActor, bool) {
	principal, err := h.staff.Authenticate(r)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) || errors.Is(err, staffauth.ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return StaffActor{}, false
		}
		if errors.Is(err, ErrNotStaff) || errors.Is(err, staffauth.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return StaffActor{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return StaffActor{}, false
	}
	actor, err := staffActorFrom(principal)
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return StaffActor{}, false
	}
	return actor, true
}

func staffActorFrom(p staffauth.Principal) (StaffActor, error) {
	if p.StaffID.IsZero() {
		return StaffActor{}, ErrNotStaff
	}
	var id moderation.ID
	copy(id[:], p.StaffID[:])
	return StaffActor{ID: id, principal: p.Clone()}, nil
}

func staffPrincipal(actor StaffActor) staffauth.Principal {
	if len(actor.principal.Roles) > 0 || !actor.principal.StaffID.IsZero() {
		return actor.principal
	}
	var staffID staffauth.ID
	copy(staffID[:], actor.ID[:])
	return staffauth.Principal{StaffID: staffID}
}

func (h *StaffHandler) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermModerationReportRead); !ok {
		return
	}
	q, ok := parseQueueQuery(w, r)
	if !ok {
		return
	}
	page, err := h.svc.ListQueue(r.Context(), q)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	out := make([]staffReportDTO, 0, len(page.Reports))
	for _, row := range page.Reports {
		out = append(out, toStaffReportDTO(row))
	}
	writeJSON(w, http.StatusOK, staffReportListDTO{Reports: out, NextCursor: omitEmpty(page.NextCursor)})
}

func (h *StaffHandler) get(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermModerationReportRead); !ok {
		return
	}
	id, ok := parseReportID(w, r)
	if !ok {
		return
	}
	row, err := h.svc.GetReport(r.Context(), id)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffReportDTO(row))
}

func (h *StaffHandler) transition(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermModerationCaseWrite)
	if !ok {
		return
	}
	id, ok := parseReportID(w, r)
	if !ok {
		return
	}
	var req transitionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.ReporterUserID) > 0 || len(req.TargetType) > 0 || len(req.TargetID) > 0 ||
		len(req.ReasonCode) > 0 || len(req.Description) > 0 || len(req.AssignedToID) > 0 ||
		len(req.Decision) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	to, err := moderation.ParseStatus(req.Status)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	in := moderation.TransitionInput{To: to, StaffNote: req.StaffNote}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	row, err := h.svc.Transition(r.Context(), id, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffReportDTO(row))
}

func parseReportID(w http.ResponseWriter, r *http.Request) (moderation.ID, bool) {
	id, err := moderation.ParseID(strings.TrimSpace(r.PathValue("reportId")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return moderation.ID{}, false
	}
	return id, true
}

func parseQueueQuery(w http.ResponseWriter, r *http.Request) (moderation.QueueQuery, bool) {
	q := r.URL.Query()
	out := moderation.QueueQuery{
		Cursor: strings.TrimSpace(q.Get("cursor")),
	}
	if raw := strings.TrimSpace(q.Get("order")); raw != "" {
		order, err := moderation.ParseQueueOrder(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return moderation.QueueQuery{}, false
		}
		out.Order = order
	}
	if raw := strings.TrimSpace(q.Get("status")); raw != "" {
		st, err := moderation.ParseStatus(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return moderation.QueueQuery{}, false
		}
		out.Status = &st
	}
	if raw := strings.TrimSpace(q.Get("targetType")); raw != "" {
		tt, err := moderation.ParseTargetType(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return moderation.QueueQuery{}, false
		}
		out.TargetType = &tt
	}
	if raw := strings.TrimSpace(q.Get("targetId")); raw != "" {
		id, err := moderation.ParseID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return moderation.QueueQuery{}, false
		}
		out.TargetID = &id
	}
	if raw := strings.TrimSpace(q.Get("reasonCode")); raw != "" {
		rc, err := moderation.ParseReasonCode(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return moderation.QueueQuery{}, false
		}
		out.ReasonCode = &rc
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return moderation.QueueQuery{}, false
		}
		out.Limit = n
	}
	return out, true
}

type transitionRequest struct {
	Status         string          `json:"status"`
	StaffNote      string          `json:"staffNote"`
	ReporterUserID json.RawMessage `json:"reporterUserId"`
	TargetType     json.RawMessage `json:"targetType"`
	TargetID       json.RawMessage `json:"targetId"`
	ReasonCode     json.RawMessage `json:"reasonCode"`
	Description    json.RawMessage `json:"description"`
	AssignedToID   json.RawMessage `json:"assignedToId"`
	Decision       json.RawMessage `json:"decision"`
}

type staffReportDTO struct {
	ReportID        string  `json:"reportId"`
	TargetType      string  `json:"targetType"`
	TargetID        string  `json:"targetId"`
	ReasonCode      string  `json:"reasonCode"`
	Description     *string `json:"description,omitempty"`
	Status          string  `json:"status"`
	StaffNote       *string `json:"staffNote,omitempty"`
	StatusChangedBy *string `json:"statusChangedBy,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
}

type staffReportListDTO struct {
	Reports    []staffReportDTO `json:"reports"`
	NextCursor *string          `json:"nextCursor,omitempty"`
}

func toStaffReportDTO(row moderation.Report) staffReportDTO {
	out := staffReportDTO{
		ReportID:    row.ID.String(),
		TargetType:  string(row.TargetType),
		TargetID:    row.TargetID.String(),
		ReasonCode:  string(row.ReasonCode),
		Description: row.Description,
		Status:      string(row.Status),
		StaffNote:   row.StaffNote,
		CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.StatusChangedBy != nil && !row.StatusChangedBy.IsZero() {
		s := row.StatusChangedBy.String()
		out.StatusChangedBy = &s
	}
	return out
}

func omitEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
