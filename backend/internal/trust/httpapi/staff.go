package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	staffauth "backend/internal/staffauth/contracts"
	"backend/internal/trust"
)

type staffAuthorizer interface {
	Authenticate(*http.Request) (staffauth.Principal, error)
	Allows(staffauth.Principal, staffauth.Permission) bool
}

// StaffHandler is the read-only staff trust HTTP adapter.
type StaffHandler struct {
	staff    staffAuthorizer
	svc      *trust.Service
	profiles identitycontracts.StaffProfileReader
}

func NewStaff(staff staffAuthorizer, svc *trust.Service, profiles identitycontracts.StaffProfileReader) (*StaffHandler, error) {
	if staff == nil || svc == nil || profiles == nil {
		return nil, trust.ErrUnavailable
	}
	return &StaffHandler{staff: staff, svc: svc, profiles: profiles}, nil
}

func (h *StaffHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/staff/trust/profiles/{publicProfileId}", h.get)
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

func (h *StaffHandler) get(w http.ResponseWriter, r *http.Request) {
	if !h.requireStaff(w, r, staffauth.PermTrustRead) {
		return
	}
	if hasForeignUserID(r) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	raw := strings.TrimSpace(r.PathValue("publicProfileId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	publicID, err := parsePublicProfileID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if _, err := h.profiles.StaffByPublicID(r.Context(), publicID); err != nil {
		writePublicProfileError(w, err)
		return
	}
	userID, err := h.profiles.StaffUserIDByPublicID(r.Context(), publicID)
	if err != nil {
		writePublicProfileError(w, err)
		return
	}
	profile, err := h.svc.GetMe(r.Context(), trust.ID(userID))
	if err != nil {
		writeTrustError(w, err)
		return
	}
	history, err := h.svc.History(r.Context(), trust.ID(userID))
	if err != nil {
		writeTrustError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffTrustDTO(profile, history))
}

type staffTrustDTO struct {
	Level                             string            `json:"level"`
	VerifiedInteractionCount          int               `json:"verifiedInteractionCount"`
	RequesterVerifiedInteractionCount int               `json:"requesterVerifiedInteractionCount"`
	ProviderVerifiedInteractionCount  int               `json:"providerVerifiedInteractionCount"`
	LastVerifiedInteractionAt         *string           `json:"lastVerifiedInteractionAt"`
	VerifiedReviewCount               int               `json:"verifiedReviewCount"`
	ProviderServiceReviewCount        int               `json:"providerServiceReviewCount"`
	ProviderServiceAverage            *json.Number      `json:"providerServiceAverage"`
	LastVerifiedReviewAt              *string           `json:"lastVerifiedReviewAt"`
	History                           []staffHistoryDTO `json:"history"`
}

type staffHistoryDTO struct {
	InteractionID      string `json:"interactionId"`
	ListingID          string `json:"listingId"`
	Role               string `json:"role"`
	InteractionType    string `json:"interactionType"`
	VerificationMethod string `json:"verificationMethod"`
	VerifiedAt         string `json:"verifiedAt"`
}

func toStaffTrustDTO(p trust.UserProfile, history []trust.HistoryEntry) staffTrustDTO {
	out := staffTrustDTO{
		Level:                             p.TrustLevel,
		VerifiedInteractionCount:          p.VerifiedInteractionCount,
		RequesterVerifiedInteractionCount: p.RequesterVerifiedInteractionCount,
		ProviderVerifiedInteractionCount:  p.ProviderVerifiedInteractionCount,
		LastVerifiedInteractionAt:         formatTimePtr(p.LastVerifiedInteractionAt),
		VerifiedReviewCount:               p.VerifiedReviewCount,
		ProviderServiceReviewCount:        p.ProviderServiceReviewCount,
		ProviderServiceAverage:            p.ProviderServiceAverage(),
		LastVerifiedReviewAt:              formatTimePtr(p.LastVerifiedReviewAt),
		History:                           make([]staffHistoryDTO, 0, len(history)),
	}
	for _, row := range history {
		out.History = append(out.History, staffHistoryDTO{
			InteractionID:      row.InteractionID.String(),
			ListingID:          row.ListingID.String(),
			Role:               row.Role,
			InteractionType:    row.InteractionType,
			VerificationMethod: row.VerificationMethod,
			VerifiedAt:         row.VerifiedAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}
