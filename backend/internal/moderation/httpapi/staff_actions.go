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

func (h *StaffHandler) createAction(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermModerationCaseWrite)
	if !ok {
		return
	}
	caseID, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	var req createActionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.ActorStaffID) > 0 || len(req.Status) > 0 || len(req.ActionID) > 0 ||
		len(req.RecipientUserID) > 0 || len(req.Recipient) > 0 || len(req.UserID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	targetType, err := moderation.ParseTargetType(req.TargetType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	targetID, err := moderation.ParseID(strings.TrimSpace(req.TargetID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	actionType, err := moderation.ParseActionType(req.ActionType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	reason, err := moderation.ParseActionReasonCode(req.ReasonCode)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	in := moderation.CreateActionInput{
		TargetType: targetType,
		TargetID:   targetID,
		ActionType: actionType,
		ReasonCode: reason,
		Rationale:  req.Rationale,
	}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	row, err := h.svc.CreateAction(r.Context(), caseID, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffActionDTO(row))
}

func (h *StaffHandler) listActions(w http.ResponseWriter, r *http.Request) {
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
	rows, err := h.svc.ListActions(r.Context(), caseID, limit)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	out := make([]staffActionDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toStaffActionDTO(row))
	}
	writeJSON(w, http.StatusOK, staffActionListDTO{Actions: out})
}

func (h *StaffHandler) getAction(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermModerationCaseRead); !ok {
		return
	}
	caseID, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	actionID, ok := parseActionID(w, r)
	if !ok {
		return
	}
	row, err := h.svc.GetAction(r.Context(), caseID, actionID)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffActionDTO(row))
}

func (h *StaffHandler) transitionAction(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.authenticateStaff(w, r)
	if !ok {
		return
	}
	caseID, ok := parseCaseID(w, r)
	if !ok {
		return
	}
	actionID, ok := parseActionID(w, r)
	if !ok {
		return
	}
	var req actionStatusRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.ActorStaffID) > 0 || len(req.ActionType) > 0 || len(req.TargetType) > 0 ||
		len(req.TargetID) > 0 || len(req.ReasonCode) > 0 || len(req.Rationale) > 0 ||
		len(req.RecipientUserID) > 0 || len(req.Recipient) > 0 || len(req.UserID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	to, err := moderation.ParseActionStatus(req.Status)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	perm := staffauth.PermModerationCaseWrite
	if to == moderation.ActionStatusApproved {
		perm = staffauth.PermModerationActionApprove
	}
	if !h.staff.Allows(staffPrincipal(actor), perm) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	in := moderation.ActionTransitionInput{To: to}
	if !actor.ID.IsZero() {
		idCopy := actor.ID
		in.ActorID = &idCopy
	}
	row, err := h.svc.TransitionAction(r.Context(), caseID, actionID, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffActionDTO(row))
}

func parseActionID(w http.ResponseWriter, r *http.Request) (moderation.ID, bool) {
	id, err := moderation.ParseID(strings.TrimSpace(r.PathValue("actionId")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return moderation.ID{}, false
	}
	return id, true
}

type createActionRequest struct {
	TargetType      string          `json:"targetType"`
	TargetID        string          `json:"targetId"`
	ActionType      string          `json:"actionType"`
	ReasonCode      string          `json:"reasonCode"`
	Rationale       string          `json:"rationale"`
	ActorStaffID    json.RawMessage `json:"actorStaffId"`
	Status          json.RawMessage `json:"status"`
	ActionID        json.RawMessage `json:"actionId"`
	RecipientUserID json.RawMessage `json:"recipientUserId"`
	Recipient       json.RawMessage `json:"recipient"`
	UserID          json.RawMessage `json:"userId"`
}

type actionStatusRequest struct {
	Status          string          `json:"status"`
	ActorStaffID    json.RawMessage `json:"actorStaffId"`
	ActionType      json.RawMessage `json:"actionType"`
	TargetType      json.RawMessage `json:"targetType"`
	TargetID        json.RawMessage `json:"targetId"`
	ReasonCode      json.RawMessage `json:"reasonCode"`
	Rationale       json.RawMessage `json:"rationale"`
	RecipientUserID json.RawMessage `json:"recipientUserId"`
	Recipient       json.RawMessage `json:"recipient"`
	UserID          json.RawMessage `json:"userId"`
}

type staffActionDTO struct {
	ActionID     string  `json:"actionId"`
	CaseID       string  `json:"caseId"`
	TargetType   string  `json:"targetType"`
	TargetID     string  `json:"targetId"`
	ActionType   string  `json:"actionType"`
	Status       string  `json:"status"`
	ReasonCode   string  `json:"reasonCode"`
	Rationale    *string `json:"rationale,omitempty"`
	ActorStaffID *string `json:"actorStaffId,omitempty"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
}

type staffActionListDTO struct {
	Actions []staffActionDTO `json:"actions"`
}

func toStaffActionDTO(row moderation.CaseAction) staffActionDTO {
	out := staffActionDTO{
		ActionID:   row.ID.String(),
		CaseID:     row.CaseID.String(),
		TargetType: string(row.TargetType),
		TargetID:   row.TargetID.String(),
		ActionType: string(row.ActionType),
		Status:     string(row.Status),
		ReasonCode: string(row.ReasonCode),
		Rationale:  row.Rationale,
		CreatedAt:  row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	out.ActorStaffID = idString(row.ActorStaffID)
	return out
}
