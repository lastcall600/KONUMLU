package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/payments"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (payments.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *payments.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *payments.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, payments.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, payments.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, payments.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/transactions/{transactionId}/payment", h.create)
	mux.HandleFunc("GET /v1/payments", h.listMine)
	mux.HandleFunc("GET /v1/payments/{paymentId}", h.get)
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
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	p, err := h.svc.CreateForTransaction(r.Context(), userID, txnID)
	if err != nil {
		writePayError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(p))
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
		writePayError(w, err)
		return
	}
	out := make([]paymentDTO, 0, len(list))
	for _, p := range list {
		out = append(out, toDTO(p))
	}
	writeJSON(w, http.StatusOK, listDTO{Payments: out})
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
	id, ok := parsePathID(w, r, "paymentId")
	if !ok {
		return
	}
	p, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		writePayError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(p))
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

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (payments.ID, bool) {
	if !h.requireOrigin(w, r) {
		return payments.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return payments.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return payments.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (payments.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return payments.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return payments.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return payments.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return payments.ID{}, false
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

func parsePathID(w http.ResponseWriter, r *http.Request, name string) (payments.ID, bool) {
	raw := strings.TrimSpace(r.PathValue(name))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return payments.ID{}, false
	}
	id, err := payments.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return payments.ID{}, false
	}
	return id, true
}

type spoofFields struct {
	PayerUserID            string `json:"payerUserId"`
	PayerUserIDSnake       string `json:"payer_user_id"`
	PayeeUserID            string `json:"payeeUserId"`
	PayeeUserIDSnake       string `json:"payee_user_id"`
	RequesterUserID        string `json:"requesterUserId"`
	ProviderUserID         string `json:"providerUserId"`
	UserID                 string `json:"userId"`
	Amount                 string `json:"amount"`
	Currency               string `json:"currency"`
	Status                 string `json:"status"`
	Provider               string `json:"provider"`
	ProviderReference      string `json:"providerReference"`
	ProviderReferenceSnake string `json:"provider_reference"`
	PAN                    string `json:"pan"`
	CVV                    string `json:"cvv"`
	CVC                    string `json:"cvc"`
	CardNumber             string `json:"cardNumber"`
	CardNumberSnake        string `json:"card_number"`
	Card                   string `json:"card"`
	IBAN                   string `json:"iban"`
	AccountNumber          string `json:"accountNumber"`
	PIN                    string `json:"pin"`
	Secret                 string `json:"secret"`
	PaymentSecret          string `json:"paymentSecret"`
}

func spoofedFields(s spoofFields) bool {
	return strings.TrimSpace(s.PayerUserID) != "" ||
		strings.TrimSpace(s.PayerUserIDSnake) != "" ||
		strings.TrimSpace(s.PayeeUserID) != "" ||
		strings.TrimSpace(s.PayeeUserIDSnake) != "" ||
		strings.TrimSpace(s.RequesterUserID) != "" ||
		strings.TrimSpace(s.ProviderUserID) != "" ||
		strings.TrimSpace(s.UserID) != "" ||
		strings.TrimSpace(s.Amount) != "" ||
		strings.TrimSpace(s.Currency) != "" ||
		strings.TrimSpace(s.Status) != "" ||
		strings.TrimSpace(s.Provider) != "" ||
		strings.TrimSpace(s.ProviderReference) != "" ||
		strings.TrimSpace(s.ProviderReferenceSnake) != "" ||
		strings.TrimSpace(s.PAN) != "" ||
		strings.TrimSpace(s.CVV) != "" ||
		strings.TrimSpace(s.CVC) != "" ||
		strings.TrimSpace(s.CardNumber) != "" ||
		strings.TrimSpace(s.CardNumberSnake) != "" ||
		strings.TrimSpace(s.Card) != "" ||
		strings.TrimSpace(s.IBAN) != "" ||
		strings.TrimSpace(s.AccountNumber) != "" ||
		strings.TrimSpace(s.PIN) != "" ||
		strings.TrimSpace(s.Secret) != "" ||
		strings.TrimSpace(s.PaymentSecret) != ""
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

type paymentDTO struct {
	PaymentID     string  `json:"paymentId"`
	TransactionID string  `json:"transactionId"`
	Amount        string  `json:"amount"`
	Currency      string  `json:"currency"`
	Status        string  `json:"status"`
	CreatedAt     string  `json:"createdAt"`
	UpdatedAt     string  `json:"updatedAt"`
	AuthorizedAt  *string `json:"authorizedAt,omitempty"`
	CapturedAt    *string `json:"capturedAt,omitempty"`
	CancelledAt   *string `json:"cancelledAt,omitempty"`
	FailedAt      *string `json:"failedAt,omitempty"`
}

type listDTO struct {
	Payments []paymentDTO `json:"payments"`
}

func toDTO(p payments.Payment) paymentDTO {
	dto := paymentDTO{
		PaymentID:     p.ID.String(),
		TransactionID: p.TransactionID.String(),
		Amount:        p.Amount.Amount,
		Currency:      p.Amount.Currency,
		Status:        string(p.Status),
		CreatedAt:     p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     p.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if p.AuthorizedAt != nil {
		s := p.AuthorizedAt.UTC().Format(time.RFC3339)
		dto.AuthorizedAt = &s
	}
	if p.CapturedAt != nil {
		s := p.CapturedAt.UTC().Format(time.RFC3339)
		dto.CapturedAt = &s
	}
	if p.CancelledAt != nil {
		s := p.CancelledAt.UTC().Format(time.RFC3339)
		dto.CancelledAt = &s
	}
	if p.FailedAt != nil {
		s := p.FailedAt.UTC().Format(time.RFC3339)
		dto.FailedAt = &s
	}
	return dto
}

type errorResponse struct {
	Error string `json:"error"`
}

func writePayError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, payments.ErrInvalidPayment), errors.Is(err, payments.ErrInvalidAmount),
		errors.Is(err, payments.ErrInvalidStatus), errors.Is(err, payments.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, payments.ErrNotFound), errors.Is(err, payments.ErrForbidden):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, payments.ErrNotPriced):
		writeError(w, http.StatusConflict, "not_priced")
	case errors.Is(err, payments.ErrNotPayable):
		writeError(w, http.StatusConflict, "not_payable")
	case errors.Is(err, payments.ErrConflict), errors.Is(err, payments.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, payments.ErrUnavailable), errors.Is(err, payments.ErrStoreRequired):
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
		strings.TrimSpace(q.Get("payerUserId")) != "" || strings.TrimSpace(q.Get("payer_user_id")) != "" ||
		strings.TrimSpace(q.Get("payeeUserId")) != "" || strings.TrimSpace(q.Get("payee_user_id")) != ""
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
