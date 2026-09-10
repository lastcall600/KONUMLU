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

	"backend/internal/verified"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
	maxJSONBytes      = 16 << 10
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (verified.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *verified.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *verified.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, verified.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, verified.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, verified.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/verified/appointments", h.create)
	mux.HandleFunc("GET /v1/verified/appointments", h.list)
	mux.HandleFunc("GET /v1/verified/appointments/{appointmentId}", h.get)
	mux.HandleFunc("POST /v1/verified/appointments/{appointmentId}/accept", h.accept)
	mux.HandleFunc("POST /v1/verified/appointments/{appointmentId}/reject", h.reject)
	mux.HandleFunc("POST /v1/verified/appointments/{appointmentId}/cancel", h.cancel)
	mux.HandleFunc("POST /v1/verified/appointments/{appointmentId}/verification/start", h.start)
	mux.HandleFunc("POST /v1/verified/appointments/{appointmentId}/verification/finish", h.finish)
	mux.HandleFunc("POST /v1/verified/transaction/verification/start", h.startTransaction)
	mux.HandleFunc("POST /v1/verified/delivery/verification/start", h.startDelivery)
	mux.HandleFunc("GET /v1/verified/verification-flows", h.listFlows)
	mux.HandleFunc("GET /v1/verified/verification-flows/{flowId}", h.getFlow)
	mux.HandleFunc("POST /v1/verified/verification-flows/{flowId}/verification/finish", h.finishFlow)
	mux.HandleFunc("GET /v1/verified/interactions/{interactionId}", h.getInteraction)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	listingID, scheduledAt, ok := parseCreate(w, r)
	if !ok {
		return
	}
	appt, err := h.svc.CreateAppointment(r.Context(), userID, listingID, scheduledAt)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAppointmentDTO(appt))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.ListAppointments(r.Context(), userID)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	out := make([]appointmentDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAppointmentDTO(row))
	}
	writeJSON(w, http.StatusOK, appointmentListDTO{Appointments: out})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	appointmentID, ok := parseAppointmentID(w, r)
	if !ok {
		return
	}
	appt, err := h.svc.GetAppointment(r.Context(), userID, appointmentID)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAppointmentDTO(appt))
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	h.mutateAppointment(w, r, h.svc.AcceptAppointment)
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	h.mutateAppointment(w, r, h.svc.RejectAppointment)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	h.mutateAppointment(w, r, h.svc.CancelAppointment)
}

func (h *Handler) mutateAppointment(w http.ResponseWriter, r *http.Request, fn func(context.Context, verified.ID, verified.ID) (verified.Appointment, error)) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	appointmentID, ok := parseAppointmentID(w, r)
	if !ok {
		return
	}
	appt, err := fn(r.Context(), userID, appointmentID)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAppointmentDTO(appt))
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	appointmentID, ok := parseAppointmentID(w, r)
	if !ok {
		return
	}
	method, ok := parseStart(w, r)
	if !ok {
		return
	}
	issued, err := h.svc.StartVerification(r.Context(), userID, appointmentID, method)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toIssuedChallengeDTO(issued))
}

func (h *Handler) startTransaction(w http.ResponseWriter, r *http.Request) {
	h.startFlow(w, r, h.svc.StartTransactionVerification)
}

func (h *Handler) startDelivery(w http.ResponseWriter, r *http.Request) {
	h.startFlow(w, r, h.svc.StartDeliveryVerification)
}

func (h *Handler) startFlow(w http.ResponseWriter, r *http.Request, fn func(context.Context, verified.ID, verified.ID, verified.ID, string) (verified.IssuedChallenge, error)) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	listingID, requesterID, method, ok := parseFlowStart(w, r)
	if !ok {
		return
	}
	issued, err := fn(r.Context(), userID, listingID, requesterID, method)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toIssuedChallengeDTO(issued))
}

func (h *Handler) listFlows(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	listingID, ok := parseOptionalListingID(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.ListFlows(r.Context(), userID, listingID)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	out := make([]verificationFlowDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toVerificationFlowDTO(row))
	}
	writeJSON(w, http.StatusOK, verificationFlowListDTO{Flows: out})
}

func (h *Handler) getFlow(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	flowID, ok := parseFlowID(w, r)
	if !ok {
		return
	}
	flow, err := h.svc.GetFlow(r.Context(), userID, flowID)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toVerificationFlowDTO(flow))
}

func (h *Handler) finishFlow(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	flowID, ok := parseFlowID(w, r)
	if !ok {
		return
	}
	token, ok := parseFinish(w, r)
	if !ok {
		return
	}
	row, err := h.svc.FinishFlowVerification(r.Context(), userID, flowID, token)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toInteractionDTO(row))
}

func (h *Handler) finish(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	appointmentID, ok := parseAppointmentID(w, r)
	if !ok {
		return
	}
	token, ok := parseFinish(w, r)
	if !ok {
		return
	}
	row, err := h.svc.FinishVerification(r.Context(), userID, appointmentID, token)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toInteractionDTO(row))
}

func (h *Handler) getInteraction(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	raw := strings.TrimSpace(r.PathValue("interactionId"))
	id, ok := parseID(w, raw)
	if !ok {
		return
	}
	row, err := h.svc.GetInteraction(r.Context(), userID, id)
	if err != nil {
		writeVerifiedError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toInteractionDTO(row))
}

func parseCreate(w http.ResponseWriter, r *http.Request) (verified.ID, *time.Time, bool) {
	var req createRequest
	if !decodeJSON(w, r, &req) {
		return verified.ID{}, nil, false
	}
	if len(req.UserID) > 0 || len(req.RequesterUserID) > 0 || len(req.ProviderUserID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return verified.ID{}, nil, false
	}
	id, err := verified.ParseID(strings.TrimSpace(req.ListingID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return verified.ID{}, nil, false
	}
	var scheduled *time.Time
	if strings.TrimSpace(req.ScheduledAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ScheduledAt))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return verified.ID{}, nil, false
		}
		utc := t.UTC()
		scheduled = &utc
	}
	return id, scheduled, true
}

func parseStart(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req startRequest
	if !decodeJSON(w, r, &req) {
		return "", false
	}
	if len(req.UserID) > 0 || len(req.ProviderUserID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return "", false
	}
	if strings.TrimSpace(req.Method) == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return "", false
	}
	return req.Method, true
}

func parseFinish(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req finishRequest
	if !decodeJSON(w, r, &req) {
		return "", false
	}
	if len(req.UserID) > 0 || len(req.RequesterUserID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return "", false
	}
	if strings.TrimSpace(req.Token) == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return "", false
	}
	return req.Token, true
}

func parseFlowStart(w http.ResponseWriter, r *http.Request) (verified.ID, verified.ID, string, bool) {
	var req flowStartRequest
	if !decodeJSON(w, r, &req) {
		return verified.ID{}, verified.ID{}, "", false
	}
	if len(req.UserID) > 0 || len(req.ProviderUserID) > 0 || len(req.InteractionType) > 0 || len(req.AppointmentID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return verified.ID{}, verified.ID{}, "", false
	}
	listingID, err := verified.ParseID(strings.TrimSpace(req.ListingID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return verified.ID{}, verified.ID{}, "", false
	}
	requesterID, err := verified.ParseID(strings.TrimSpace(req.RequesterUserID))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return verified.ID{}, verified.ID{}, "", false
	}
	if strings.TrimSpace(req.Method) == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return verified.ID{}, verified.ID{}, "", false
	}
	return listingID, requesterID, req.Method, true
}

func parseOptionalListingID(w http.ResponseWriter, r *http.Request) (verified.ID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("listingId"))
	if raw == "" {
		return verified.ID{}, true
	}
	if strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return verified.ID{}, false
	}
	id, err := verified.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return verified.ID{}, false
	}
	return id, true
}

func parseAppointmentID(w http.ResponseWriter, r *http.Request) (verified.ID, bool) {
	return parseID(w, r.PathValue("appointmentId"))
}

func parseFlowID(w http.ResponseWriter, r *http.Request) (verified.ID, bool) {
	return parseID(w, r.PathValue("flowId"))
}

func parseID(w http.ResponseWriter, raw string) (verified.ID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return verified.ID{}, false
	}
	id, err := verified.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return verified.ID{}, false
	}
	return id, true
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

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (verified.ID, bool) {
	if !h.requireOrigin(w, r) {
		return verified.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return verified.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return verified.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (verified.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return verified.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return verified.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return verified.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return verified.ID{}, false
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
	ListingID       string          `json:"listingId"`
	ScheduledAt     string          `json:"scheduledAt"`
	UserID          json.RawMessage `json:"userId"`
	RequesterUserID json.RawMessage `json:"requesterUserId"`
	ProviderUserID  json.RawMessage `json:"providerUserId"`
}

type startRequest struct {
	Method         string          `json:"method"`
	UserID         json.RawMessage `json:"userId"`
	ProviderUserID json.RawMessage `json:"providerUserId"`
}

type finishRequest struct {
	Token           string          `json:"token"`
	UserID          json.RawMessage `json:"userId"`
	RequesterUserID json.RawMessage `json:"requesterUserId"`
}

type flowStartRequest struct {
	ListingID       string          `json:"listingId"`
	RequesterUserID string          `json:"requesterUserId"`
	Method          string          `json:"method"`
	UserID          json.RawMessage `json:"userId"`
	ProviderUserID  json.RawMessage `json:"providerUserId"`
	InteractionType json.RawMessage `json:"interactionType"`
	AppointmentID   json.RawMessage `json:"appointmentId"`
}

type appointmentDTO struct {
	AppointmentID         string  `json:"appointmentId"`
	ListingID             string  `json:"listingId"`
	RequesterUserID       string  `json:"requesterUserId"`
	ProviderUserID        string  `json:"providerUserId"`
	Status                string  `json:"status"`
	RequestedAt           string  `json:"requestedAt"`
	ScheduledAt           *string `json:"scheduledAt"`
	UpdatedAt             string  `json:"updatedAt"`
	VerifiedInteractionID *string `json:"verifiedInteractionId"`
}

type appointmentListDTO struct {
	Appointments []appointmentDTO `json:"appointments"`
}

type verificationFlowDTO struct {
	FlowID                 string  `json:"flowId"`
	ListingID              string  `json:"listingId"`
	InteractionType        string  `json:"interactionType"`
	Status                 string  `json:"status"`
	CreatedAt              string  `json:"createdAt"`
	CompletedInteractionID *string `json:"completedInteractionId"`
}

type verificationFlowListDTO struct {
	Flows []verificationFlowDTO `json:"flows"`
}

type issuedChallengeDTO struct {
	ChallengeID     string `json:"challengeId"`
	AppointmentID   string `json:"appointmentId,omitempty"`
	FlowID          string `json:"flowId,omitempty"`
	Method          string `json:"method"`
	Token           string `json:"token"`
	ExpiresAt       string `json:"expiresAt"`
	InteractionType string `json:"interactionType,omitempty"`
	// QrPayload is the opaque token for client-side QR encoding. Present only for method=qr. Never an image.
	QrPayload string `json:"qrPayload,omitempty"`
}

type interactionDTO struct {
	InteractionID      string `json:"interactionId"`
	AppointmentID      string `json:"appointmentId,omitempty"`
	FlowID             string `json:"flowId,omitempty"`
	ListingID          string `json:"listingId"`
	RequesterUserID    string `json:"requesterUserId"`
	ProviderUserID     string `json:"providerUserId"`
	InteractionType    string `json:"interactionType"`
	VerificationMethod string `json:"verificationMethod"`
	VerifiedAt         string `json:"verifiedAt"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toAppointmentDTO(appt verified.Appointment) appointmentDTO {
	dto := appointmentDTO{
		AppointmentID:   appt.ID.String(),
		ListingID:       appt.ListingID.String(),
		RequesterUserID: appt.RequesterUserID.String(),
		ProviderUserID:  appt.ProviderUserID.String(),
		Status:          appt.Status,
		RequestedAt:     appt.RequestedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       appt.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if appt.ScheduledAt != nil {
		at := appt.ScheduledAt.UTC().Format(time.RFC3339)
		dto.ScheduledAt = &at
	}
	if appt.Status == verified.StatusCompleted && appt.VerifiedInteractionID != nil && !appt.VerifiedInteractionID.IsZero() {
		id := appt.VerifiedInteractionID.String()
		dto.VerifiedInteractionID = &id
	}
	return dto
}

func toVerificationFlowDTO(flow verified.VerificationFlow) verificationFlowDTO {
	dto := verificationFlowDTO{
		FlowID:          flow.ID.String(),
		ListingID:       flow.ListingID.String(),
		InteractionType: flow.InteractionType,
		Status:          flow.Status,
		CreatedAt:       flow.CreatedAt.UTC().Format(time.RFC3339),
	}
	if flow.Status == verified.FlowCompleted && flow.CompletedInteractionID != nil && !flow.CompletedInteractionID.IsZero() {
		id := flow.CompletedInteractionID.String()
		dto.CompletedInteractionID = &id
	}
	return dto
}

func toIssuedChallengeDTO(issued verified.IssuedChallenge) issuedChallengeDTO {
	dto := issuedChallengeDTO{
		ChallengeID:     issued.Challenge.ID.String(),
		Method:          issued.Challenge.Method,
		Token:           issued.RawToken,
		ExpiresAt:       issued.Challenge.ExpiresAt.UTC().Format(time.RFC3339),
		InteractionType: issued.InteractionType,
	}
	if !issued.Challenge.AppointmentID.IsZero() {
		dto.AppointmentID = issued.Challenge.AppointmentID.String()
	}
	if !issued.Challenge.FlowID.IsZero() {
		dto.FlowID = issued.Challenge.FlowID.String()
	}
	if issued.Challenge.Method == verified.MethodQR {
		dto.QrPayload = issued.RawToken
	}
	return dto
}

func toInteractionDTO(row verified.VerifiedInteraction) interactionDTO {
	dto := interactionDTO{
		InteractionID:      row.ID.String(),
		ListingID:          row.ListingID.String(),
		RequesterUserID:    row.RequesterUserID.String(),
		ProviderUserID:     row.ProviderUserID.String(),
		InteractionType:    row.InteractionType,
		VerificationMethod: row.VerificationMethod,
		VerifiedAt:         row.VerifiedAt.UTC().Format(time.RFC3339),
	}
	if !row.AppointmentID.IsZero() {
		dto.AppointmentID = row.AppointmentID.String()
	}
	if !row.FlowID.IsZero() {
		dto.FlowID = row.FlowID.String()
	}
	return dto
}

func writeVerifiedError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, verified.ErrZeroID), errors.Is(err, verified.ErrSelfAppointment),
		errors.Is(err, verified.ErrInvalidMethod), errors.Is(err, verified.ErrInvalidToken),
		errors.Is(err, verified.ErrExpiredChallenge), errors.Is(err, verified.ErrInvalidAppointment),
		errors.Is(err, verified.ErrInvalidInteraction):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, verified.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, verified.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, verified.ErrInvalidTransition), errors.Is(err, verified.ErrInvalidStatus),
		errors.Is(err, verified.ErrChallengeConsumed), errors.Is(err, verified.ErrConflict):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, verified.ErrUnavailable), errors.Is(err, verified.ErrStoreRequired),
		errors.Is(err, verified.ErrListingsReq), errors.Is(err, verified.ErrOutboxRequired):
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
