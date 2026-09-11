package publicprofile

import (
	"errors"
	"net/http"
	"strings"
	"time"

	staffauth "backend/internal/staffauth/contracts"
)

type staffAuthorizer interface {
	Authenticate(*http.Request) (staffauth.Principal, error)
	Allows(staffauth.Principal, staffauth.Permission) bool
}

// StaffHandler is the read-only staff public-profile HTTP adapter.
type StaffHandler struct {
	staff staffAuthorizer
	svc   *Service
}

func NewStaff(staff staffAuthorizer, svc *Service) (*StaffHandler, error) {
	if staff == nil || svc == nil {
		return nil, ErrUnavailable
	}
	return &StaffHandler{staff: staff, svc: svc}, nil
}

func (h *StaffHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/staff/identity/profiles/{publicProfileId}", h.get)
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
	if !h.requireStaff(w, r, staffauth.PermIdentityProfileRead) {
		return
	}
	raw := strings.TrimSpace(r.PathValue("publicProfileId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	id, err := ParsePublicID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	view, err := h.svc.StaffByPublicID(r.Context(), id)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffDTO(view))
}

type staffProfileDTO struct {
	PublicProfileID string  `json:"publicProfileId"`
	DisplayName     *string `json:"displayName"`
	ModerationState string  `json:"moderationState"`
	MemberSince     string  `json:"memberSince"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	AccountEligible bool    `json:"accountEligible"`
	Disabled        bool    `json:"disabled"`
	Deleted         bool    `json:"deleted"`
}

func toStaffDTO(v StaffView) staffProfileDTO {
	return staffProfileDTO{
		PublicProfileID: v.PublicProfileID.String(),
		DisplayName:     cloneDisplay(v.DisplayName),
		ModerationState: string(v.ModerationState),
		MemberSince:     v.MemberSince.UTC().Format(time.RFC3339),
		CreatedAt:       v.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       v.UpdatedAt.UTC().Format(time.RFC3339),
		AccountEligible: v.AccountEligible,
		Disabled:        v.Disabled,
		Deleted:         v.Deleted,
	}
}
