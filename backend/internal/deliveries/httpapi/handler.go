package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/deliveries"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (deliveries.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *deliveries.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *deliveries.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, deliveries.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, deliveries.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, deliveries.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/transactions/{transactionId}/delivery", h.create)
	mux.HandleFunc("GET /v1/deliveries", h.listMine)
	mux.HandleFunc("GET /v1/deliveries/{deliveryId}", h.get)
	mux.HandleFunc("POST /v1/deliveries/{deliveryId}/ready", h.ready)
	mux.HandleFunc("POST /v1/deliveries/{deliveryId}/in-transit", h.inTransit)
	mux.HandleFunc("POST /v1/deliveries/{deliveryId}/delivered", h.delivered)
	mux.HandleFunc("POST /v1/deliveries/{deliveryId}/cancel", h.cancel)
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
		writeDeliveryError(w, err)
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
		writeDeliveryError(w, err)
		return
	}
	out := make([]deliveryDTO, 0, len(list))
	for _, d := range list {
		out = append(out, toDTO(d))
	}
	writeJSON(w, http.StatusOK, listDTO{Deliveries: out})
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
	id, ok := parsePathID(w, r, "deliveryId")
	if !ok {
		return
	}
	d, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		writeDeliveryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(d))
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(ctx context.Context, userID, id deliveries.ID) (deliveries.Delivery, error) {
		return h.svc.MarkReady(ctx, userID, id)
	})
}

func (h *Handler) inTransit(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(ctx context.Context, userID, id deliveries.ID) (deliveries.Delivery, error) {
		return h.svc.MarkInTransit(ctx, userID, id)
	})
}

func (h *Handler) delivered(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(ctx context.Context, userID, id deliveries.ID) (deliveries.Delivery, error) {
		return h.svc.MarkDelivered(ctx, userID, id)
	})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(ctx context.Context, userID, id deliveries.ID) (deliveries.Delivery, error) {
		return h.svc.Cancel(ctx, userID, id)
	})
}

func (h *Handler) mutate(w http.ResponseWriter, r *http.Request, fn func(context.Context, deliveries.ID, deliveries.ID) (deliveries.Delivery, error)) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r, "deliveryId")
	if !ok {
		return
	}
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	d, err := fn(r.Context(), userID, id)
	if err != nil {
		writeDeliveryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(d))
}

func (h *Handler) decodeCreate(w http.ResponseWriter, r *http.Request) (deliveries.CreateInput, bool) {
	if r.Body == nil || r.ContentLength == 0 {
		return deliveries.CreateInput{}, true
	}
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return deliveries.CreateInput{}, false
	}
	if spoofedFields(req.Spoof) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return deliveries.CreateInput{}, false
	}
	in := deliveries.CreateInput{Eligibility: strings.TrimSpace(req.Eligibility)}
	if m := strings.TrimSpace(req.Method); m != "" {
		method := deliveries.Method(m)
		in.Method = &method
	}
	if n := strings.TrimSpace(req.Note); n != "" {
		in.Note = &n
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

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (deliveries.ID, bool) {
	if !h.requireOrigin(w, r) {
		return deliveries.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return deliveries.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return deliveries.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (deliveries.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return deliveries.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return deliveries.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return deliveries.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return deliveries.ID{}, false
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

func parsePathID(w http.ResponseWriter, r *http.Request, name string) (deliveries.ID, bool) {
	raw := strings.TrimSpace(r.PathValue(name))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return deliveries.ID{}, false
	}
	id, err := deliveries.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return deliveries.ID{}, false
	}
	return id, true
}

type spoofFields struct {
	RequesterUserID      string `json:"requesterUserId"`
	RequesterUserIDSnake string `json:"requester_user_id"`
	ProviderUserID       string `json:"providerUserId"`
	ProviderUserIDSnake  string `json:"provider_user_id"`
	UserID               string `json:"userId"`
	Status               string `json:"status"`
	DeliveryID           string `json:"deliveryId"`
	Address              string `json:"address"`
	Street               string `json:"street"`
	HomeAddress          string `json:"homeAddress"`
	Lat                  string `json:"lat"`
	Lng                  string `json:"lng"`
	Latitude             string `json:"latitude"`
	Longitude            string `json:"longitude"`
	GPS                  string `json:"gps"`
	Courier              string `json:"courier"`
	TrackingNumber       string `json:"trackingNumber"`
	Tracking             string `json:"tracking"`
}

func spoofedFields(s spoofFields) bool {
	return strings.TrimSpace(s.RequesterUserID) != "" ||
		strings.TrimSpace(s.RequesterUserIDSnake) != "" ||
		strings.TrimSpace(s.ProviderUserID) != "" ||
		strings.TrimSpace(s.ProviderUserIDSnake) != "" ||
		strings.TrimSpace(s.UserID) != "" ||
		strings.TrimSpace(s.Status) != "" ||
		strings.TrimSpace(s.DeliveryID) != "" ||
		strings.TrimSpace(s.Address) != "" ||
		strings.TrimSpace(s.Street) != "" ||
		strings.TrimSpace(s.HomeAddress) != "" ||
		strings.TrimSpace(s.Lat) != "" ||
		strings.TrimSpace(s.Lng) != "" ||
		strings.TrimSpace(s.Latitude) != "" ||
		strings.TrimSpace(s.Longitude) != "" ||
		strings.TrimSpace(s.GPS) != "" ||
		strings.TrimSpace(s.Courier) != "" ||
		strings.TrimSpace(s.TrackingNumber) != "" ||
		strings.TrimSpace(s.Tracking) != ""
}

type createRequest struct {
	Eligibility string `json:"eligibility"`
	Method      string `json:"method"`
	Note        string `json:"note"`
	Spoof       spoofFields
}

func (c *createRequest) UnmarshalJSON(b []byte) error {
	type alias struct {
		Eligibility string `json:"eligibility"`
		Method      string `json:"method"`
		Note        string `json:"note"`
	}
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	var spoof spoofFields
	if err := json.Unmarshal(b, &spoof); err != nil {
		return err
	}
	c.Eligibility = a.Eligibility
	c.Method = a.Method
	c.Note = a.Note
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

type deliveryDTO struct {
	DeliveryID    string  `json:"deliveryId"`
	TransactionID string  `json:"transactionId"`
	Eligibility   string  `json:"eligibility"`
	Status        string  `json:"status"`
	Method        *string `json:"method,omitempty"`
	Note          *string `json:"note,omitempty"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
	DispatchedAt  *string `json:"dispatchedAt,omitempty"`
	DeliveredAt   *string `json:"deliveredAt,omitempty"`
	CancelledAt   *string `json:"cancelledAt,omitempty"`
}

type listDTO struct {
	Deliveries []deliveryDTO `json:"deliveries"`
}

func toDTO(d deliveries.Delivery) deliveryDTO {
	dto := deliveryDTO{
		DeliveryID:    d.ID.String(),
		TransactionID: d.TransactionID.String(),
		Eligibility:   d.Eligibility,
		Status:        string(d.Status),
		CreatedAt:     d.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     d.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if d.Method != nil {
		m := string(*d.Method)
		dto.Method = &m
	}
	if d.Note != nil {
		n := *d.Note
		dto.Note = &n
	}
	if d.DispatchedAt != nil {
		s := d.DispatchedAt.UTC().Format(time.RFC3339)
		dto.DispatchedAt = &s
	}
	if d.DeliveredAt != nil {
		s := d.DeliveredAt.UTC().Format(time.RFC3339)
		dto.DeliveredAt = &s
	}
	if d.CancelledAt != nil {
		s := d.CancelledAt.UTC().Format(time.RFC3339)
		dto.CancelledAt = &s
	}
	return dto
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeDeliveryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, deliveries.ErrInvalidDelivery), errors.Is(err, deliveries.ErrInvalidMethod),
		errors.Is(err, deliveries.ErrInvalidNote), errors.Is(err, deliveries.ErrInvalidStatus),
		errors.Is(err, deliveries.ErrInvalidEligibility), errors.Is(err, deliveries.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, deliveries.ErrNotFound), errors.Is(err, deliveries.ErrForbidden):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, deliveries.ErrNotEligible):
		writeError(w, http.StatusConflict, "not_eligible")
	case errors.Is(err, deliveries.ErrConflict), errors.Is(err, deliveries.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, deliveries.ErrUnavailable), errors.Is(err, deliveries.ErrStoreRequired):
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
		strings.TrimSpace(q.Get("providerUserId")) != "" || strings.TrimSpace(q.Get("provider_user_id")) != ""
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
