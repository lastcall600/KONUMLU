package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/internal/moderation"
	staffauth "backend/internal/staffauth/contracts"
)

func (h *StaffHandler) listAppeals(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermModerationCaseRead); !ok {
		return
	}
	caseID, ok := parseCaseID(w, r)
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
	rows, err := h.svc.ListAppeals(r.Context(), caseID, limit)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	out := make([]staffAppealDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toStaffAppealDTO(row))
	}
	writeJSON(w, http.StatusOK, staffAppealListDTO{Appeals: out})
}

func (h *StaffHandler) getStaffAppeal(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermModerationCaseRead); !ok {
		return
	}
	caseID, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	appealID, ok := parseAppealID(w, r)
	if !ok {
		return
	}
	row, err := h.svc.GetAppeal(r.Context(), caseID, appealID)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffAppealDTO(row))
}

func (h *StaffHandler) transitionAppeal(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermModerationAppealReview)
	if !ok {
		return
	}
	caseID, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	appealID, ok := parseAppealID(w, r)
	if !ok {
		return
	}
	var req appealStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.DecidedByStaffID) > 0 || len(req.AppellantUserID) > 0 || len(req.ActionID) > 0 ||
		len(req.Statement) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	to, err := moderation.ParseAppealStatus(req.Status)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	in := moderation.AppealTransitionInput{To: to}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	row, err := h.svc.TransitionAppeal(r.Context(), caseID, appealID, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffAppealDTO(row))
}

type appealStatusRequest struct {
	Status           string          `json:"status"`
	DecidedByStaffID json.RawMessage `json:"decidedByStaffId"`
	AppellantUserID  json.RawMessage `json:"appellantUserId"`
	ActionID         json.RawMessage `json:"actionId"`
	Statement        json.RawMessage `json:"statement"`
}

type staffAppealDTO struct {
	AppealID         string  `json:"appealId"`
	ActionID         string  `json:"actionId"`
	CaseID           string  `json:"caseId"`
	AppellantUserID  string  `json:"appellantUserId"`
	Statement        string  `json:"statement"`
	Status           string  `json:"status"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
	DecidedAt        *string `json:"decidedAt,omitempty"`
	DecidedByStaffID *string `json:"decidedByStaffId,omitempty"`
}

type staffAppealListDTO struct {
	Appeals []staffAppealDTO `json:"appeals"`
}

func toStaffAppealDTO(row moderation.Appeal) staffAppealDTO {
	out := staffAppealDTO{
		AppealID:        row.ID.String(),
		ActionID:        row.ActionID.String(),
		CaseID:          row.CaseID.String(),
		AppellantUserID: row.AppellantUserID.String(),
		Statement:       row.Statement,
		Status:          string(row.Status),
		CreatedAt:       row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.DecidedAt != nil {
		s := row.DecidedAt.UTC().Format(time.RFC3339)
		out.DecidedAt = &s
	}
	out.DecidedByStaffID = idString(row.DecidedByStaffID)
	return out
}
