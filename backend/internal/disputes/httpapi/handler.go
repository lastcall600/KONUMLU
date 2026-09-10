package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/disputes"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (disputes.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *disputes.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *disputes.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, disputes.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, disputes.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, disputes.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/transactions/{transactionId}/dispute", h.create)
	mux.HandleFunc("GET /v1/disputes", h.listMine)
	mux.HandleFunc("GET /v1/disputes/{disputeId}", h.get)
	mux.HandleFunc("GET /v1/disputes/{disputeId}/evidence", h.listEvidence)
	mux.HandleFunc("POST /v1/disputes/{disputeId}/evidence", h.addEvidence)
	mux.HandleFunc("POST /v1/disputes/{disputeId}/withdraw", h.withdraw)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	txnID, ok := parsePathID(w, r, "transactionId")
	if !ok {
		return
	}
	in, ok := h.decodeCreate(w, r)
	if !ok {
		return
	}
	d, err := h.svc.CreateForTransaction(r.Context(), userID, txnID, in)
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(d))
}

func (h *Handler) listMine(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	list, err := h.svc.ListMine(r.Context(), userID)
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	out := make([]disputeDTO, 0, len(list))
	for _, d := range list {
		out = append(out, toDTO(d))
	}
	writeJSON(w, http.StatusOK, listDTO{Disputes: out})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r, "disputeId")
	if !ok {
		return
	}
	d, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(d))
}

func (h *Handler) listEvidence(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r, "disputeId")
	if !ok {
		return
	}
	list, err := h.svc.ListEvidence(r.Context(), userID, id)
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	out := make([]evidenceDTO, 0, len(list))
	for _, e := range list {
		out = append(out, toEvidenceDTO(e))
	}
	writeJSON(w, http.StatusOK, evidenceListDTO{Evidence: out})
}

func (h *Handler) addEvidence(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r, "disputeId")
	if !ok {
		return
	}
	in, ok := h.decodeEvidence(w, r)
	if !ok {
		return
	}
	e, err := h.svc.AddEvidence(r.Context(), userID, id, in)
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toEvidenceDTO(e))
}

func (h *Handler) withdraw(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r, "disputeId")
	if !ok {
		return
	}
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	d, err := h.svc.Withdraw(r.Context(), userID, id)
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(d))
}

func (h *Handler) decodeCreate(w http.ResponseWriter, r *http.Request) (disputes.CreateInput, bool) {
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return disputes.CreateInput{}, false
	}
	if spoofedFields(req.Spoof) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return disputes.CreateInput{}, false
	}
	in := disputes.CreateInput{ReasonCode: disputes.ReasonCode(strings.TrimSpace(req.ReasonCode))}
	if n := strings.TrimSpace(req.Statement); n != "" {
		in.Statement = &n
	}
	return in, true
}

func (h *Handler) decodeEvidence(w http.ResponseWriter, r *http.Request) (disputes.EvidenceInput, bool) {
	var req evidenceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return disputes.EvidenceInput{}, false
	}
	if spoofedFields(req.Spoof) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return disputes.EvidenceInput{}, false
	}
	in := disputes.EvidenceInput{
		EvidenceType: disputes.EvidenceType(strings.TrimSpace(req.EvidenceType)),
		Title:        strings.TrimSpace(req.Title),
	}
	if n := strings.TrimSpace(req.Description); n != "" {
		in.Description = &n
	}
	if n := strings.TrimSpace(req.ReferenceValue); n != "" {
		in.ReferenceValue = &n
	}
	return in, true
}

func (h *Handler) rejectMutationSpoof(w http.ResponseWriter, r *http.Request) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	var req mutationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	if spoofedFields(req.Spoof) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (disputes.ID, bool) {
	if !h.requireOrigin(w, r) {
		return disputes.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return disputes.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return disputes.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (disputes.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return disputes.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return disputes.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return disputes.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return disputes.ID{}, false
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

func parsePathID(w http.ResponseWriter, r *http.Request, name string) (disputes.ID, bool) {
	raw := strings.TrimSpace(r.PathValue(name))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return disputes.ID{}, false
	}
	id, err := disputes.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return disputes.ID{}, false
	}
	return id, true
}

type spoofFields struct {
	OpenedByUserID       string `json:"openedByUserId"`
	OpenedByUserIDSnake  string `json:"opened_by_user_id"`
	RequesterUserID      string `json:"requesterUserId"`
	RequesterUserIDSnake string `json:"requester_user_id"`
	ProviderUserID       string `json:"providerUserId"`
	ProviderUserIDSnake  string `json:"provider_user_id"`
	UserID               string `json:"userId"`
	Status               string `json:"status"`
	DisputeID            string `json:"disputeId"`
	ActorUserID          string `json:"actorUserId"`
	ActorRole            string `json:"actorRole"`
	ResolutionCode       string `json:"resolutionCode"`
}

func spoofedFields(s spoofFields) bool {
	return strings.TrimSpace(s.OpenedByUserID) != "" ||
		strings.TrimSpace(s.OpenedByUserIDSnake) != "" ||
		strings.TrimSpace(s.RequesterUserID) != "" ||
		strings.TrimSpace(s.RequesterUserIDSnake) != "" ||
		strings.TrimSpace(s.ProviderUserID) != "" ||
		strings.TrimSpace(s.ProviderUserIDSnake) != "" ||
		strings.TrimSpace(s.UserID) != "" ||
		strings.TrimSpace(s.Status) != "" ||
		strings.TrimSpace(s.DisputeID) != "" ||
		strings.TrimSpace(s.ActorUserID) != "" ||
		strings.TrimSpace(s.ActorRole) != "" ||
		strings.TrimSpace(s.ResolutionCode) != ""
}

type createRequest struct {
	ReasonCode string `json:"reasonCode"`
	Statement  string `json:"statement"`
	Spoof      spoofFields
}

func (c *createRequest) UnmarshalJSON(b []byte) error {
	type alias struct {
		ReasonCode string `json:"reasonCode"`
		Statement  string `json:"statement"`
	}
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	var spoof spoofFields
	if err := json.Unmarshal(b, &spoof); err != nil {
		return err
	}
	c.ReasonCode = a.ReasonCode
	c.Statement = a.Statement
	c.Spoof = spoof
	return nil
}

type evidenceRequest struct {
	EvidenceType   string `json:"evidenceType"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	ReferenceValue string `json:"referenceValue"`
	Spoof          spoofFields
}

func (c *evidenceRequest) UnmarshalJSON(b []byte) error {
	type alias struct {
		EvidenceType   string `json:"evidenceType"`
		Title          string `json:"title"`
		Description    string `json:"description"`
		ReferenceValue string `json:"referenceValue"`
	}
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	var spoof spoofFields
	if err := json.Unmarshal(b, &spoof); err != nil {
		return err
	}
	c.EvidenceType = a.EvidenceType
	c.Title = a.Title
	c.Description = a.Description
	c.ReferenceValue = a.ReferenceValue
	c.Spoof = spoof
	return nil
}

type mutationRequest struct {
	Spoof spoofFields
}

func (c *mutationRequest) UnmarshalJSON(b []byte) error {
	var spoof spoofFields
	if err := json.Unmarshal(b, &spoof); err != nil {
		return err
	}
	c.Spoof = spoof
	return nil
}

type disputeDTO struct {
	DisputeID      string  `json:"disputeId"`
	TransactionID  string  `json:"transactionId"`
	ReasonCode     string  `json:"reasonCode"`
	Statement      *string `json:"statement,omitempty"`
	Status         string  `json:"status"`
	OpenedByRole   string  `json:"openedByRole"`
	ResolutionCode *string `json:"resolutionCode,omitempty"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
	ResolvedAt     *string `json:"resolvedAt,omitempty"`
}

type listDTO struct {
	Disputes []disputeDTO `json:"disputes"`
}

type evidenceDTO struct {
	EvidenceID     string  `json:"evidenceId"`
	DisputeID      string  `json:"disputeId"`
	EvidenceType   string  `json:"evidenceType"`
	Title          string  `json:"title"`
	Description    *string `json:"description,omitempty"`
	ReferenceValue *string `json:"referenceValue,omitempty"`
	ActorRole      string  `json:"actorRole"`
	CreatedAt      string  `json:"createdAt"`
}

type evidenceListDTO struct {
	Evidence []evidenceDTO `json:"evidence"`
}

func toDTO(d disputes.Dispute) disputeDTO {
	dto := disputeDTO{
		DisputeID:     d.ID.String(),
		TransactionID: d.TransactionID.String(),
		ReasonCode:    string(d.ReasonCode),
		Status:        string(d.Status),
		OpenedByRole:  d.OpenedByRole(),
		CreatedAt:     d.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     d.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if d.Statement != nil {
		n := *d.Statement
		dto.Statement = &n
	}
	if d.ResolutionCode != nil {
		c := string(*d.ResolutionCode)
		dto.ResolutionCode = &c
	}
	if d.ResolvedAt != nil {
		s := d.ResolvedAt.UTC().Format(time.RFC3339)
		dto.ResolvedAt = &s
	}
	return dto
}

func toEvidenceDTO(e disputes.Evidence) evidenceDTO {
	dto := evidenceDTO{
		EvidenceID:   e.ID.String(),
		DisputeID:    e.DisputeID.String(),
		EvidenceType: string(e.EvidenceType),
		Title:        e.Title,
		ActorRole:    e.ActorRole,
		CreatedAt:    e.CreatedAt.UTC().Format(time.RFC3339),
	}
	if e.Description != nil {
		n := *e.Description
		dto.Description = &n
	}
	if e.ReferenceValue != nil {
		n := *e.ReferenceValue
		dto.ReferenceValue = &n
	}
	return dto
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeDisputeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, disputes.ErrInvalidDispute), errors.Is(err, disputes.ErrInvalidReason),
		errors.Is(err, disputes.ErrInvalidStatement), errors.Is(err, disputes.ErrInvalidStatus),
		errors.Is(err, disputes.ErrInvalidEvidence), errors.Is(err, disputes.ErrInvalidResolution),
		errors.Is(err, disputes.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, disputes.ErrNotFound), errors.Is(err, disputes.ErrForbidden):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, disputes.ErrNotEligible), errors.Is(err, disputes.ErrWindowClosed):
		writeError(w, http.StatusConflict, "not_eligible")
	case errors.Is(err, disputes.ErrConflict), errors.Is(err, disputes.ErrInvalidTransition),
		errors.Is(err, disputes.ErrConcluded):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, disputes.ErrUnavailable), errors.Is(err, disputes.ErrStoreRequired):
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

func decodeJSON(r *http.Request, dest any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dest); err != nil {
		return err
	}
	return nil
}

func hasForeignUserID(r *http.Request) bool {
	q := r.URL.Query()
	return strings.TrimSpace(q.Get("userId")) != "" || strings.TrimSpace(q.Get("user_id")) != "" ||
		strings.TrimSpace(q.Get("requesterUserId")) != "" || strings.TrimSpace(q.Get("requester_user_id")) != "" ||
		strings.TrimSpace(q.Get("providerUserId")) != "" || strings.TrimSpace(q.Get("provider_user_id")) != "" ||
		strings.TrimSpace(q.Get("openedByUserId")) != ""
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
