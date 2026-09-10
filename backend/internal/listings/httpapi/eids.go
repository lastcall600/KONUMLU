package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	eidscontracts "backend/internal/eids/contracts"
	"backend/internal/listings"
	mdcontracts "backend/internal/masterdata/contracts"
)

func (h *Handler) publishEligible(ctx context.Context, listing listings.Listing) (bool, error) {
	if h.policy == nil {
		return false, listings.ErrUnavailable
	}
	req, err := h.policy.Requirement(ctx, mdcontracts.ID(listing.CategoryID))
	if err != nil {
		return false, mapEIDSPolicyErr(err)
	}
	if !req.Required() {
		return true, nil
	}
	if h.eidsGate == nil {
		return false, nil
	}
	kind := eidscontracts.VerificationType(req)
	ok, err := h.eidsGate.IsVerified(ctx, eidscontracts.ID(listing.ID), kind)
	if err != nil {
		return false, mapEIDSErr(err)
	}
	return ok, nil
}

func (h *Handler) startEIDS(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	if _, ok := h.loadOwned(w, r, ownerID); !ok {
		return
	}
	if h.eidsOwner == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return
	}
	clientType, ok := readEIDSStartBody(w, r)
	if !ok {
		return
	}
	view, err := h.eidsOwner.StartForOwner(r.Context(), eidscontracts.ID(ownerID), eidscontracts.ID(listingID), clientType)
	if err != nil {
		writeEIDSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toEIDSDTO(view))
}

func (h *Handler) getEIDS(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	if _, ok := h.loadOwned(w, r, ownerID); !ok {
		return
	}
	if h.eidsOwner == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	listingID, ok := parseListingID(w, r)
	if !ok {
		return
	}
	view, err := h.eidsOwner.CurrentForOwner(r.Context(), eidscontracts.ID(ownerID), eidscontracts.ID(listingID))
	if err != nil {
		writeEIDSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toEIDSDTO(view))
}

type eidsStartRequest struct {
	VerificationType  string `json:"verificationType"`
	Status            string `json:"status"`
	ProviderReference string `json:"providerReference"`
	Eligible          *bool  `json:"eligible"`
}

func readEIDSStartBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Body == nil || r.ContentLength == 0 {
		return "", true
	}
	var req eidsStartRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return "", false
	}
	if strings.TrimSpace(req.Status) != "" || strings.TrimSpace(req.ProviderReference) != "" || req.Eligible != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return "", false
	}
	return strings.TrimSpace(req.VerificationType), true
}

type eidsDTO struct {
	VerificationID   string  `json:"verificationId"`
	ListingID        string  `json:"listingId"`
	VerificationType string  `json:"verificationType"`
	Status           string  `json:"status"`
	FailureCode      string  `json:"failureCode,omitempty"`
	CreatedAt        string  `json:"createdAt"`
	UpdatedAt        string  `json:"updatedAt"`
	RequestedAt      *string `json:"requestedAt,omitempty"`
	VerifiedAt       *string `json:"verifiedAt,omitempty"`
	FailedAt         *string `json:"failedAt,omitempty"`
}

func toEIDSDTO(v eidscontracts.VerificationView) eidsDTO {
	return eidsDTO{
		VerificationID:   formatEIDSID(v.VerificationID),
		ListingID:        formatEIDSID(v.ListingID),
		VerificationType: string(v.VerificationType),
		Status:           string(v.Status),
		FailureCode:      v.FailureCode,
		CreatedAt:        v.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        v.UpdatedAt.UTC().Format(time.RFC3339),
		RequestedAt:      formatTimePtr(v.RequestedAt),
		VerifiedAt:       formatTimePtr(v.VerifiedAt),
		FailedAt:         formatTimePtr(v.FailedAt),
	}
}

func formatEIDSID(id eidscontracts.ID) string {
	return listings.ID(id).String()
}

func writeEIDSError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, eidscontracts.ErrInvalidType), errors.Is(err, eidscontracts.ErrInvalidInput),
		errors.Is(err, eidscontracts.ErrZeroID):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, eidscontracts.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, eidscontracts.ErrWrongType), errors.Is(err, eidscontracts.ErrNotRequired):
		writeError(w, http.StatusConflict, "conflict")
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func mapEIDSPolicyErr(err error) error {
	if errors.Is(err, mdcontracts.ErrZeroID) {
		return listings.ErrZeroID
	}
	return listings.ErrUnavailable
}

func mapEIDSErr(err error) error {
	if errors.Is(err, eidscontracts.ErrZeroID) || errors.Is(err, eidscontracts.ErrInvalidType) {
		return listings.ErrZeroID
	}
	return listings.ErrUnavailable
}
