package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"backend/internal/moderation"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
	maxJSONBytes      = 16 << 10
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (moderation.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *moderation.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *moderation.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, moderation.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, moderation.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, moderation.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/moderation/reports", h.create)
	mux.HandleFunc("GET /v1/moderation/reports/mine", h.mine)
	mux.HandleFunc("POST /v1/moderation/appeals", h.createAppeal)
	mux.HandleFunc("GET /v1/moderation/appeals/mine", h.mineAppeals)
	mux.HandleFunc("GET /v1/moderation/appeals/{appealId}", h.getAppeal)
	mux.HandleFunc("POST /v1/moderation/appeals/{appealId}/withdraw", h.withdrawAppeal)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	in, ok := parseCreate(w, r)
	if !ok {
		return
	}
	row, err := h.svc.Create(r.Context(), userID, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toReportDTO(row))
}

func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.ListMine(r.Context(), userID)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	out := make([]reportDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toReportDTO(row))
	}
	writeJSON(w, http.StatusOK, reportListDTO{Reports: out})
}

func (h *Handler) createAppeal(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	in, ok := parseCreateAppeal(w, r)
	if !ok {
		return
	}
	row, err := h.svc.SubmitAppeal(r.Context(), userID, in)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAppealDTO(row))
}

func (h *Handler) mineAppeals(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.ListMineAppeals(r.Context(), userID)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	out := make([]appealDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAppealDTO(row))
	}
	writeJSON(w, http.StatusOK, appealListDTO{Appeals: out})
}

func (h *Handler) getAppeal(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	id, ok := parseAppealID(w, r)
	if !ok {
		return
	}
	row, err := h.svc.GetAppealForUser(r.Context(), userID, id)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAppealDTO(row))
}

func (h *Handler) withdrawAppeal(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	id, ok := parseAppealID(w, r)
	if !ok {
		return
	}
	row, err := h.svc.WithdrawAppeal(r.Context(), userID, id)
	if err != nil {
		writeModerationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAppealDTO(row))
}

func parseCreateAppeal(w http.ResponseWriter, r *http.Request) (moderation.CreateAppealInput, bool) {
	var req createAppealRequest
	if !decodeJSON(w, r, &req) {
		return moderation.CreateAppealInput{}, false
	}
	if len(req.AppellantUserID) > 0 || len(req.UserID) > 0 || len(req.Status) > 0 ||
		len(req.CaseID) > 0 || len(req.DecidedByStaffID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return moderation.CreateAppealInput{}, false
	}
	actionID, err := moderation.ParseID(strings.TrimSpace(req.ActionID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return moderation.CreateAppealInput{}, false
	}
	return moderation.CreateAppealInput{ActionID: actionID, Statement: req.Statement}, true
}

func parseAppealID(w http.ResponseWriter, r *http.Request) (moderation.ID, bool) {
	id, err := moderation.ParseID(strings.TrimSpace(r.PathValue("appealId")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return moderation.ID{}, false
	}
	return id, true
}

func parseCreate(w http.ResponseWriter, r *http.Request) (moderation.CreateInput, bool) {
	var req createRequest
	if !decodeJSON(w, r, &req) {
		return moderation.CreateInput{}, false
	}
	if len(req.ReporterUserID) > 0 || len(req.UserID) > 0 || len(req.Status) > 0 ||
		len(req.AssignedToID) > 0 || len(req.Decision) > 0 || len(req.TargetOwnerUserID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return moderation.CreateInput{}, false
	}
	targetType, err := moderation.ParseTargetType(req.TargetType)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return moderation.CreateInput{}, false
	}
	targetID, err := moderation.ParseID(strings.TrimSpace(req.TargetID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return moderation.CreateInput{}, false
	}
	reason, err := moderation.ParseReasonCode(req.ReasonCode)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return moderation.CreateInput{}, false
	}
	return moderation.CreateInput{
		TargetType:  targetType,
		TargetID:    targetID,
		ReasonCode:  reason,
		Description: req.Description,
	}, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	limited := http.MaxBytesReader(w, r.Body, maxJSONBytes)
	dec := json.NewDecoder(limited)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (moderation.ID, bool) {
	if !h.requireOrigin(w, r) {
		return moderation.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return moderation.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return moderation.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (moderation.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return moderation.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return moderation.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return moderation.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return moderation.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || origin == "null" {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if _, ok := h.origins[origin]; !ok {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

type createRequest struct {
	TargetType        string          `json:"targetType"`
	TargetID          string          `json:"targetId"`
	ReasonCode        string          `json:"reasonCode"`
	Description       string          `json:"description"`
	ReporterUserID    json.RawMessage `json:"reporterUserId"`
	UserID            json.RawMessage `json:"userId"`
	Status            json.RawMessage `json:"status"`
	AssignedToID      json.RawMessage `json:"assignedToId"`
	Decision          json.RawMessage `json:"decision"`
	TargetOwnerUserID json.RawMessage `json:"targetOwnerUserId"`
}

type reportDTO struct {
	ReportID    string  `json:"reportId"`
	TargetType  string  `json:"targetType"`
	TargetID    string  `json:"targetId"`
	ReasonCode  string  `json:"reasonCode"`
	Description *string `json:"description,omitempty"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

type reportListDTO struct {
	Reports []reportDTO `json:"reports"`
}

type createAppealRequest struct {
	ActionID         string          `json:"actionId"`
	Statement        string          `json:"statement"`
	AppellantUserID  json.RawMessage `json:"appellantUserId"`
	UserID           json.RawMessage `json:"userId"`
	Status           json.RawMessage `json:"status"`
	CaseID           json.RawMessage `json:"caseId"`
	DecidedByStaffID json.RawMessage `json:"decidedByStaffId"`
}

type appealDTO struct {
	AppealID  string  `json:"appealId"`
	ActionID  string  `json:"actionId"`
	CaseID    string  `json:"caseId"`
	Statement string  `json:"statement"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
	DecidedAt *string `json:"decidedAt,omitempty"`
}

type appealListDTO struct {
	Appeals []appealDTO `json:"appeals"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toReportDTO(row moderation.Report) reportDTO {
	return reportDTO{
		ReportID:    row.ID.String(),
		TargetType:  string(row.TargetType),
		TargetID:    row.TargetID.String(),
		ReasonCode:  string(row.ReasonCode),
		Description: row.Description,
		Status:      string(row.Status),
		CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   row.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toAppealDTO(row moderation.Appeal) appealDTO {
	out := appealDTO{
		AppealID:  row.ID.String(),
		ActionID:  row.ActionID.String(),
		CaseID:    row.CaseID.String(),
		Statement: row.Statement,
		Status:    string(row.Status),
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.DecidedAt != nil {
		s := row.DecidedAt.UTC().Format(time.RFC3339)
		out.DecidedAt = &s
	}
	return out
}

func writeModerationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, moderation.ErrZeroID), errors.Is(err, moderation.ErrInvalidTarget),
		errors.Is(err, moderation.ErrInvalidReason), errors.Is(err, moderation.ErrInvalidBody),
		errors.Is(err, moderation.ErrInvalidQuery), errors.Is(err, moderation.ErrInvalidStatus),
		errors.Is(err, moderation.ErrInvalidPriority), errors.Is(err, moderation.ErrInvalidCaseStatus),
		errors.Is(err, moderation.ErrInvalidCase), errors.Is(err, moderation.ErrInvalidAttachment),
		errors.Is(err, moderation.ErrInvalidHistory), errors.Is(err, moderation.ErrInvalidEvidence),
		errors.Is(err, moderation.ErrInvalidAction), errors.Is(err, moderation.ErrInvalidActionStatus),
		errors.Is(err, moderation.ErrInvalidAppeal), errors.Is(err, moderation.ErrInvalidAppealStatus),
		errors.Is(err, moderation.ErrUnsupportedAction):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, moderation.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, moderation.ErrSelfReport):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, moderation.ErrConflict), errors.Is(err, moderation.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, moderation.ErrUnavailable), errors.Is(err, moderation.ErrStoreRequired),
		errors.Is(err, moderation.ErrListingsReq), errors.Is(err, moderation.ErrProfilesReq):
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorResponse{Error: code})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func readCookie(r *http.Request, name string) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil || c == nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

func csrfOK(r *http.Request) bool {
	cookie, ok := readCookie(r, csrfCookieName)
	if !ok {
		return false
	}
	header := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	if header == "" || len(header) != len(cookie) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(cookie)) == 1
}
