package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	needcontracts "backend/internal/needs/contracts"
	"backend/internal/offers"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (offers.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *offers.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *offers.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, offers.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, offers.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, offers.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/needs/{needId}/offer-context", h.providerNeedView)
	mux.HandleFunc("GET /v1/needs/{needId}/offers", h.listForRequester)
	mux.HandleFunc("POST /v1/needs/{needId}/offers", h.create)
	mux.HandleFunc("POST /v1/needs/{needId}/offers/{offerId}/accept", h.accept)
	mux.HandleFunc("POST /v1/needs/{needId}/offers/{offerId}/reject", h.reject)
	mux.HandleFunc("GET /v1/offers/mine", h.listMine)
	mux.HandleFunc("POST /v1/offers/{offerId}/withdraw", h.withdraw)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	needID, ok := parseNeedID(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if spoofedFields(req.Spoof) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	content, ok := req.content(needID)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	offer, err := h.svc.Create(r.Context(), userID, content)
	if err != nil {
		writeOfferError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.toProviderDTO(r.Context(), offer))
}

func (h *Handler) listForRequester(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	needID, ok := parseNeedID(w, r)
	if !ok {
		return
	}
	list, err := h.svc.ListForRequester(r.Context(), userID, needID)
	if err != nil {
		writeOfferError(w, err)
		return
	}
	out := make([]requesterOfferDTO, 0, len(list))
	for _, o := range list {
		out = append(out, h.toRequesterDTO(r.Context(), o))
	}
	writeJSON(w, http.StatusOK, requesterListDTO{Offers: out})
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
		writeOfferError(w, err)
		return
	}
	out := make([]providerOfferDTO, 0, len(list))
	for _, o := range list {
		out = append(out, h.toProviderDTO(r.Context(), o))
	}
	writeJSON(w, http.StatusOK, providerListDTO{Offers: out})
}

func (h *Handler) accept(w http.ResponseWriter, r *http.Request) {
	h.requesterLifecycle(w, r, h.svc.Accept)
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	h.requesterLifecycle(w, r, h.svc.Reject)
}

func (h *Handler) requesterLifecycle(w http.ResponseWriter, r *http.Request, fn func(context.Context, offers.ID, offers.ID, offers.ID) (offers.Offer, error)) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	needID, ok := parseNeedID(w, r)
	if !ok {
		return
	}
	offerID, ok := parseOfferID(w, r)
	if !ok {
		return
	}
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	offer, err := fn(r.Context(), userID, needID, offerID)
	if err != nil {
		writeOfferError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.toRequesterDTO(r.Context(), offer))
}

func (h *Handler) withdraw(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	offerID, ok := parseOfferID(w, r)
	if !ok {
		return
	}
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	offer, err := h.svc.Withdraw(r.Context(), userID, offerID)
	if err != nil {
		writeOfferError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.toProviderDTO(r.Context(), offer))
}

func (h *Handler) providerNeedView(w http.ResponseWriter, r *http.Request) {
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	needID, ok := parseNeedID(w, r)
	if !ok {
		return
	}
	businessID, ok := parseQueryID(w, r, "businessId")
	if !ok {
		return
	}
	serviceID, ok := parseQueryID(w, r, "serviceId")
	if !ok {
		return
	}
	need, err := h.svc.ProviderNeedView(r.Context(), userID, needID, businessID, serviceID)
	if err != nil {
		writeOfferError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toProviderNeedDTO(need))
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

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (offers.ID, bool) {
	if !h.requireOrigin(w, r) {
		return offers.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return offers.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return offers.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (offers.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return offers.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return offers.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return offers.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return offers.ID{}, false
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

func parseNeedID(w http.ResponseWriter, r *http.Request) (offers.ID, bool) {
	return parsePathID(w, r, "needId")
}

func parseOfferID(w http.ResponseWriter, r *http.Request) (offers.ID, bool) {
	return parsePathID(w, r, "offerId")
}

func parsePathID(w http.ResponseWriter, r *http.Request, name string) (offers.ID, bool) {
	raw := strings.TrimSpace(r.PathValue(name))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return offers.ID{}, false
	}
	id, err := offers.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return offers.ID{}, false
	}
	return id, true
}

func parseQueryID(w http.ResponseWriter, r *http.Request, name string) (offers.ID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return offers.ID{}, false
	}
	id, err := offers.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return offers.ID{}, false
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
	ID                   string `json:"id"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
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
		strings.TrimSpace(s.ID) != "" ||
		strings.TrimSpace(s.CreatedAt) != "" ||
		strings.TrimSpace(s.UpdatedAt) != ""
}

type priceBody struct {
	Amount   *string `json:"amount"`
	Currency *string `json:"currency"`
}

type createRequest struct {
	BusinessID string     `json:"businessId"`
	ServiceID  string     `json:"serviceId"`
	Message    *string    `json:"message"`
	Price      *priceBody `json:"price"`
	Spoof      spoofFields
}

func (c *createRequest) UnmarshalJSON(b []byte) error {
	type alias createRequest
	var raw alias
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var spoof spoofFields
	if err := json.Unmarshal(b, &spoof); err != nil {
		return err
	}
	*c = createRequest(raw)
	c.Spoof = spoof
	return nil
}

func (c createRequest) content(needID offers.ID) (offers.Content, bool) {
	biz, err := offers.ParseID(c.BusinessID)
	if err != nil {
		return offers.Content{}, false
	}
	svc, err := offers.ParseID(c.ServiceID)
	if err != nil {
		return offers.Content{}, false
	}
	content := offers.Content{
		NeedID:             needID,
		ProviderBusinessID: biz,
		ServiceID:          svc,
		Message:            deref(c.Message),
	}
	if c.Price != nil {
		content.Price = &offers.Price{
			Amount:   deref(c.Price.Amount),
			Currency: deref(c.Price.Currency),
		}
	}
	return content, true
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

type offerCoreDTO struct {
	OfferID    string    `json:"offerId"`
	NeedID     string    `json:"needId"`
	BusinessID string    `json:"businessId"`
	ServiceID  string    `json:"serviceId"`
	Message    *string   `json:"message,omitempty"`
	Price      *priceDTO `json:"price,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  string    `json:"createdAt"`
	UpdatedAt  string    `json:"updatedAt"`
}

type requesterOfferDTO struct {
	offerCoreDTO
	BusinessDisplayName string `json:"businessDisplayName,omitempty"`
	ServiceTitle        string `json:"serviceTitle,omitempty"`
}

type providerNeedDTO struct {
	NeedID      string       `json:"needId"`
	Title       string       `json:"title"`
	Description *string      `json:"description,omitempty"`
	CategoryID  *string      `json:"categoryId,omitempty"`
	Status      string       `json:"status"`
	Location    locationDTO  `json:"location"`
	RadiusKm    *float64     `json:"radiusKm,omitempty"`
	Budget      *budgetDTO   `json:"budget,omitempty"`
	ExpiresAt   *string      `json:"expiresAt,omitempty"`
}

type locationDTO struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type budgetDTO struct {
	MinAmount string `json:"minAmount"`
	MaxAmount string `json:"maxAmount"`
	Currency  string `json:"currency"`
}

type providerOfferDTO struct {
	offerCoreDTO
	Need *providerNeedDTO `json:"need,omitempty"`
}

type requesterListDTO struct {
	Offers []requesterOfferDTO `json:"offers"`
}

type providerListDTO struct {
	Offers []providerOfferDTO `json:"offers"`
}

func toCoreDTO(o offers.Offer) offerCoreDTO {
	dto := offerCoreDTO{
		OfferID:    o.ID.String(),
		NeedID:     o.NeedID.String(),
		BusinessID: o.ProviderBusinessID.String(),
		ServiceID:  o.ServiceID.String(),
		Status:     string(o.Status),
		CreatedAt:  o.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  o.UpdatedAt.UTC().Format(time.RFC3339),
	}
	dto.Message = optionalString(o.Message)
	if o.Price != nil {
		dto.Price = &priceDTO{Amount: o.Price.Amount, Currency: o.Price.Currency}
	}
	return dto
}

func (h *Handler) toRequesterDTO(ctx context.Context, o offers.Offer) requesterOfferDTO {
	dto := requesterOfferDTO{offerCoreDTO: toCoreDTO(o)}
	if biz, err := h.svc.PublicBusiness(ctx, o.ProviderBusinessID); err == nil {
		dto.BusinessDisplayName = biz.DisplayName
	}
	if svc, err := h.svc.PublicService(ctx, o.ServiceID); err == nil {
		dto.ServiceTitle = svc.Title
	}
	return dto
}

func (h *Handler) toProviderDTO(ctx context.Context, o offers.Offer) providerOfferDTO {
	dto := providerOfferDTO{offerCoreDTO: toCoreDTO(o)}
	if need, err := h.svc.GetNeed(ctx, o.NeedID); err == nil {
		view := toProviderNeedDTO(need)
		dto.Need = &view
	}
	return dto
}

func toProviderNeedDTO(need needcontracts.NeedRef) providerNeedDTO {
	dto := providerNeedDTO{
		NeedID: formatNeedID(need.ID),
		Title:  need.Title,
		Status: need.Status,
		Location: locationDTO{
			Latitude:  need.Latitude,
			Longitude: need.Longitude,
		},
		RadiusKm: need.RadiusKm,
	}
	dto.Description = optionalString(need.Description)
	if need.CategoryID != nil {
		s := formatNeedID(*need.CategoryID)
		dto.CategoryID = &s
	}
	if need.Budget != nil {
		dto.Budget = &budgetDTO{
			MinAmount: need.Budget.MinAmount,
			MaxAmount: need.Budget.MaxAmount,
			Currency:  need.Budget.Currency,
		}
	}
	if need.ExpiresAt != nil {
		s := need.ExpiresAt.UTC().Format(time.RFC3339)
		dto.ExpiresAt = &s
	}
	return dto
}

func formatNeedID(id needcontracts.ID) string {
	return offers.ID(id).String()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	out := s
	return &out
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeOfferError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, offers.ErrInvalidOffer), errors.Is(err, offers.ErrInvalidMessage),
		errors.Is(err, offers.ErrInvalidPrice), errors.Is(err, offers.ErrInvalidStatus),
		errors.Is(err, offers.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, offers.ErrNotFound), errors.Is(err, offers.ErrForbidden),
		errors.Is(err, offers.ErrNotEligible):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, offers.ErrConflict), errors.Is(err, offers.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "conflict")
	case errors.Is(err, offers.ErrUnavailable), errors.Is(err, offers.ErrStoreRequired):
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
