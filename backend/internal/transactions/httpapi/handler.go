package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/transactions"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (transactions.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *transactions.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *transactions.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, transactions.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, transactions.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, transactions.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/offers/{offerId}/transaction", h.create)
	mux.HandleFunc("GET /v1/transactions", h.listMine)
	mux.HandleFunc("GET /v1/transactions/{transactionId}", h.get)
	mux.HandleFunc("POST /v1/transactions/{transactionId}/start", h.start)
	mux.HandleFunc("POST /v1/transactions/{transactionId}/complete", h.complete)
	mux.HandleFunc("POST /v1/transactions/{transactionId}/cancel", h.cancel)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	offerID, ok := parsePathID(w, r, "offerId")
	if !ok {
		return
	}
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	txn, err := h.svc.CreateFromAcceptedOffer(r.Context(), userID, offerID)
	if err != nil {
		writeTxnError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(txn))
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
		writeTxnError(w, err)
		return
	}
	out := make([]transactionDTO, 0, len(list))
	for _, txn := range list {
		out = append(out, toDTO(txn))
	}
	writeJSON(w, http.StatusOK, listDTO{Transactions: out})
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
	id, ok := parsePathID(w, r, "transactionId")
	if !ok {
		return
	}
	txn, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		writeTxnError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(txn))
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.svc.Start)
}

func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.svc.Complete)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	h.lifecycle(w, r, h.svc.Cancel)
}

func (h *Handler) lifecycle(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, userID, transactionID transactions.ID) (transactions.Transaction, error)) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r, "transactionId")
	if !ok {
		return
	}
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	txn, err := fn(r.Context(), userID, id)
	if err != nil {
		writeTxnError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(txn))
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

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (transactions.ID, bool) {
	if !h.requireOrigin(w, r) {
		return transactions.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return transactions.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return transactions.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (transactions.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return transactions.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return transactions.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return transactions.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return transactions.ID{}, false
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

func parsePathID(w http.ResponseWriter, r *http.Request, name string) (transactions.ID, bool) {
	raw := strings.TrimSpace(r.PathValue(name))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return transactions.ID{}, false
	}
	id, err := transactions.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return transactions.ID{}, false
	}
	return id, true
}

type spoofFields struct {
	ProviderUserID       string `json:"providerUserId"`
	ProviderUserIDSnake  string `json:"provider_user_id"`
	RequesterUserID      string `json:"requesterUserId"`
	RequesterUserIDSnake string `json:"requester_user_id"`
	OwnerUserID          string `json:"ownerUserId"`
	UserID               string `json:"userId"`
	Status               string `json:"status"`
	OfferID              string `json:"offerId"`
	NeedID               string `json:"needId"`
	TransactionID        string `json:"transactionId"`
	ID                   string `json:"id"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
	Fulfilled            string `json:"fulfilled"`
	NeedStatus           string `json:"needStatus"`
	NeedStatusSnake      string `json:"need_status"`
}

func spoofedFields(s spoofFields) bool {
	return strings.TrimSpace(s.ProviderUserID) != "" ||
		strings.TrimSpace(s.ProviderUserIDSnake) != "" ||
		strings.TrimSpace(s.RequesterUserID) != "" ||
		strings.TrimSpace(s.RequesterUserIDSnake) != "" ||
		strings.TrimSpace(s.OwnerUserID) != "" ||
		strings.TrimSpace(s.UserID) != "" ||
		strings.TrimSpace(s.Status) != "" ||
		strings.TrimSpace(s.OfferID) != "" ||
		strings.TrimSpace(s.NeedID) != "" ||
		strings.TrimSpace(s.TransactionID) != "" ||
		strings.TrimSpace(s.ID) != "" ||
		strings.TrimSpace(s.CreatedAt) != "" ||
		strings.TrimSpace(s.UpdatedAt) != "" ||
		strings.TrimSpace(s.Fulfilled) != "" ||
		strings.TrimSpace(s.NeedStatus) != "" ||
		strings.TrimSpace(s.NeedStatusSnake) != ""
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

type priceDTO struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type transactionDTO struct {
	TransactionID string    `json:"transactionId"`
	OfferID       string    `json:"offerId"`
	NeedID        string    `json:"needId"`
	BusinessID    string    `json:"providerBusinessId"`
	ServiceID     string    `json:"serviceId"`
	AgreedPrice   *priceDTO `json:"agreedPrice,omitempty"`
	Status        string    `json:"status"`
	CreatedAt     string    `json:"createdAt"`
	UpdatedAt     string    `json:"updatedAt"`
	CompletedAt   *string   `json:"completedAt,omitempty"`
	CancelledAt   *string   `json:"cancelledAt,omitempty"`
}

type listDTO struct {
	Transactions []transactionDTO `json:"transactions"`
}

func toDTO(t transactions.Transaction) transactionDTO {
	dto := transactionDTO{
		TransactionID: t.ID.String(),
		OfferID:       t.OfferID.String(),
		NeedID:        t.NeedID.String(),
		BusinessID:    t.ProviderBusinessID.String(),
		ServiceID:     t.ServiceID.String(),
		Status:        string(t.Status),
		CreatedAt:     t.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     t.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if t.Price != nil {
		dto.AgreedPrice = &priceDTO{Amount: t.Price.Amount, Currency: t.Price.Currency}
	}
	if t.CompletedAt != nil {
		s := t.CompletedAt.UTC().Format(time.RFC3339)
		dto.CompletedAt = &s
	}
	if t.CancelledAt != nil {
		s := t.CancelledAt.UTC().Format(time.RFC3339)
		dto.CancelledAt = &s
	}
	return dto
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeTxnError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, transactions.ErrInvalidTxn), errors.Is(err, transactions.ErrInvalidPrice),
		errors.Is(err, transactions.ErrInvalidStatus), errors.Is(err, transactions.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, transactions.ErrNotFound), errors.Is(err, transactions.ErrForbidden):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, transactions.ErrConflict), errors.Is(err, transactions.ErrInvalidTransition),
		errors.Is(err, transactions.ErrNotAccepted), errors.Is(err, transactions.ErrNeedDraft),
		errors.Is(err, transactions.ErrNeedCancelled), errors.Is(err, transactions.ErrNeedExpired),
		errors.Is(err, transactions.ErrNeedFulfilled):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, transactions.ErrUnavailable), errors.Is(err, transactions.ErrStoreRequired):
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
		strings.TrimSpace(q.Get("ownerUserId")) != "" || strings.TrimSpace(q.Get("owner_user_id")) != "" ||
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
