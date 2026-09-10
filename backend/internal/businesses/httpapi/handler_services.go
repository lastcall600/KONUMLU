package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"backend/internal/businesses"
)

func (h *Handler) createService(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	var req serviceWriteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if spoofedFields(req.Spoof) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	content, ok := req.content()
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	svc, err := h.svc.CreateOfferedService(r.Context(), ownerID, businessID, content)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerServiceDTO(svc))
}

func (h *Handler) listOwnedServices(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	list, err := h.svc.ListOwnedOfferedServices(r.Context(), ownerID, businessID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, serviceListDTO{Items: toOwnerServiceDTOs(list)})
}

func (h *Handler) getOwnedService(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	serviceID, ok := parseServiceID(w, r)
	if !ok {
		return
	}
	svc, err := h.svc.GetOwnedOfferedService(r.Context(), ownerID, businessID, serviceID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerServiceDTO(svc))
}

func (h *Handler) patchService(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	serviceID, ok := parseServiceID(w, r)
	if !ok {
		return
	}
	var req servicePatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if spoofedFields(req.Spoof) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	current, err := h.svc.GetOwnedOfferedService(r.Context(), ownerID, businessID, serviceID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	content := businesses.ServiceContent{
		Title:       current.Title,
		Description: current.Description,
		Price:       current.Price,
		CategoryID:  current.CategoryID,
	}
	if req.Title != nil {
		content.Title = *req.Title
	}
	if req.Description != nil {
		content.Description = *req.Description
	}
	if req.hasPrice {
		content.Price = req.price()
	}
	if req.hasCategory {
		cat, ok := parseOptionalCategory(req.CategoryID)
		if !ok {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		content.CategoryID = cat
	}
	svc, err := h.svc.UpdateOfferedService(r.Context(), ownerID, businessID, serviceID, content)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerServiceDTO(svc))
}

func (h *Handler) activateService(w http.ResponseWriter, r *http.Request) {
	h.mutateService(w, r, (*businesses.Service).ActivateOfferedService)
}

func (h *Handler) pauseService(w http.ResponseWriter, r *http.Request) {
	h.mutateService(w, r, (*businesses.Service).PauseOfferedService)
}

func (h *Handler) closeService(w http.ResponseWriter, r *http.Request) {
	h.mutateService(w, r, (*businesses.Service).CloseOfferedService)
}

type serviceMutator func(*businesses.Service, context.Context, businesses.ID, businesses.ID, businesses.ID) (businesses.OfferedService, error)

func (h *Handler) mutateService(w http.ResponseWriter, r *http.Request, fn serviceMutator) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	serviceID, ok := parseServiceID(w, r)
	if !ok {
		return
	}
	if !h.rejectMutationSpoof(w, r) {
		return
	}
	svc, err := fn(h.svc, r.Context(), ownerID, businessID, serviceID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOwnerServiceDTO(svc))
}

func (h *Handler) listPublicServices(w http.ResponseWriter, r *http.Request) {
	businessID, ok := parseBusinessID(w, r)
	if !ok {
		return
	}
	list, err := h.svc.ListPublicOfferedServices(r.Context(), businessID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", publicCacheControl)
	writeJSON(w, http.StatusOK, publicServiceListDTO{Items: toPublicServiceDTOs(list)})
}

func (h *Handler) getPublicService(w http.ResponseWriter, r *http.Request) {
	serviceID, ok := parseServiceID(w, r)
	if !ok {
		return
	}
	svc, err := h.svc.GetPublicOfferedService(r.Context(), serviceID)
	if err != nil {
		writeBusinessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", publicCacheControl)
	writeJSON(w, http.StatusOK, toPublicServiceDTO(svc))
}

func parseServiceID(w http.ResponseWriter, r *http.Request) (businesses.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("serviceId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return businesses.ID{}, false
	}
	id, err := businesses.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return businesses.ID{}, false
	}
	return id, true
}

type serviceWriteRequest struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	PriceModel  *string `json:"priceModel"`
	Amount      *string `json:"amount"`
	Currency    *string `json:"currency"`
	CategoryID  *string `json:"categoryId"`
	Spoof       spoofFields
}

func (c *serviceWriteRequest) UnmarshalJSON(b []byte) error {
	type alias serviceWriteRequest
	var raw alias
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var spoof spoofFields
	if err := json.Unmarshal(b, &spoof); err != nil {
		return err
	}
	*c = serviceWriteRequest(raw)
	c.Spoof = spoof
	return nil
}

func (c serviceWriteRequest) content() (businesses.ServiceContent, bool) {
	cat, ok := parseOptionalCategory(c.CategoryID)
	if !ok {
		return businesses.ServiceContent{}, false
	}
	return businesses.ServiceContent{
		Title:       c.Title,
		Description: deref(c.Description),
		Price: businesses.ServicePrice{
			Model:    businesses.PriceModel(deref(c.PriceModel)),
			Amount:   c.Amount,
			Currency: c.Currency,
		},
		CategoryID: cat,
	}, true
}

type servicePatchRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	PriceModel  *string `json:"priceModel"`
	Amount      *string `json:"amount"`
	Currency    *string `json:"currency"`
	CategoryID  *string `json:"categoryId"`
	hasPrice    bool
	hasCategory bool
	Spoof       spoofFields
}

func (c *servicePatchRequest) UnmarshalJSON(b []byte) error {
	type alias servicePatchRequest
	var raw alias
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var spoof spoofFields
	if err := json.Unmarshal(b, &spoof); err != nil {
		return err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		return err
	}
	*c = servicePatchRequest(raw)
	c.Spoof = spoof
	_, hasModel := keys["priceModel"]
	_, hasAmount := keys["amount"]
	_, hasCurrency := keys["currency"]
	c.hasPrice = hasModel || hasAmount || hasCurrency
	_, c.hasCategory = keys["categoryId"]
	return nil
}

func (c servicePatchRequest) price() businesses.ServicePrice {
	return businesses.ServicePrice{
		Model:    businesses.PriceModel(deref(c.PriceModel)),
		Amount:   c.Amount,
		Currency: c.Currency,
	}
}

type ownerServiceDTO struct {
	ServiceID   string  `json:"serviceId"`
	BusinessID  string  `json:"businessId"`
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	Status      string  `json:"status"`
	PriceModel  *string `json:"priceModel,omitempty"`
	Amount      *string `json:"amount,omitempty"`
	Currency    *string `json:"currency,omitempty"`
	CategoryID  *string `json:"categoryId,omitempty"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

type publicServiceDTO struct {
	ServiceID   string  `json:"serviceId"`
	BusinessID  string  `json:"businessId"`
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	PriceModel  *string `json:"priceModel,omitempty"`
	Amount      *string `json:"amount,omitempty"`
	Currency    *string `json:"currency,omitempty"`
	CategoryID  *string `json:"categoryId,omitempty"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

type serviceListDTO struct {
	Items []ownerServiceDTO `json:"items"`
}

type publicServiceListDTO struct {
	Items []publicServiceDTO `json:"items"`
}

func toOwnerServiceDTO(s businesses.OfferedService) ownerServiceDTO {
	return ownerServiceDTO{
		ServiceID:   s.ID.String(),
		BusinessID:  s.BusinessID.String(),
		Title:       s.Title,
		Description: optionalString(s.Description),
		Status:      string(s.Status),
		PriceModel:  optionalString(string(s.Price.Model)),
		Amount:      s.Price.Amount,
		Currency:    s.Price.Currency,
		CategoryID:  idString(s.CategoryID),
		CreatedAt:   s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toPublicServiceDTO(s businesses.OfferedService) publicServiceDTO {
	return publicServiceDTO{
		ServiceID:   s.ID.String(),
		BusinessID:  s.BusinessID.String(),
		Title:       s.Title,
		Description: optionalString(s.Description),
		PriceModel:  optionalString(string(s.Price.Model)),
		Amount:      s.Price.Amount,
		Currency:    s.Price.Currency,
		CategoryID:  idString(s.CategoryID),
		CreatedAt:   s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   s.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func idString(id *businesses.ID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func parseOptionalCategory(raw *string) (*businesses.ID, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, true
	}
	id, err := businesses.ParseID(*raw)
	if err != nil {
		return nil, false
	}
	return &id, true
}

func toOwnerServiceDTOs(list []businesses.OfferedService) []ownerServiceDTO {
	out := make([]ownerServiceDTO, 0, len(list))
	for _, svc := range list {
		out = append(out, toOwnerServiceDTO(svc))
	}
	return out
}

func toPublicServiceDTOs(list []businesses.OfferedService) []publicServiceDTO {
	out := make([]publicServiceDTO, 0, len(list))
	for _, svc := range list {
		out = append(out, toPublicServiceDTO(svc))
	}
	return out
}
