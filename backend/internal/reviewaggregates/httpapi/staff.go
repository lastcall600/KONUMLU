package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"backend/internal/reviewaggregates"
	staffauth "backend/internal/staffauth/contracts"
)

type staffAuthorizer interface {
	Authenticate(*http.Request) (staffauth.Principal, error)
	Allows(staffauth.Principal, staffauth.Permission) bool
}

// StaffHandler is the read-only staff review-aggregate HTTP adapter.
type StaffHandler struct {
	staff staffAuthorizer
	svc   *reviewaggregates.Service
}

func NewStaff(staff staffAuthorizer, svc *reviewaggregates.Service) (*StaffHandler, error) {
	if staff == nil || svc == nil {
		return nil, reviewaggregates.ErrUnavailable
	}
	return &StaffHandler{staff: staff, svc: svc}, nil
}

func (h *StaffHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/staff/review-aggregates/listings/{listingId}", h.listing)
}

func (h *StaffHandler) requireStaff(w http.ResponseWriter, r *http.Request, perm staffauth.Permission) bool {
	principal, err := h.staff.Authenticate(r)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) || errors.Is(err, staffauth.ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return false
		}
		if errors.Is(err, staffauth.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return false
	}
	if principal.StaffID.IsZero() {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if !h.staff.Allows(principal, perm) {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func (h *StaffHandler) listing(w http.ResponseWriter, r *http.Request) {
	if !h.requireStaff(w, r, staffauth.PermListingsRead) {
		return
	}
	raw := strings.TrimSpace(r.PathValue("listingId"))
	listingID, err := reviewaggregates.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	got, err := h.svc.ListingAccuracy(r.Context(), listingID)
	if err != nil {
		writeSummaryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, listingSummaryDTO{
		ListingAccuracy: toRatingDTO(got),
	})
}
