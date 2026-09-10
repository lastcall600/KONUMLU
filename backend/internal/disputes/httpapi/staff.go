package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"backend/internal/disputes"
	staffauth "backend/internal/staffauth/contracts"
)

var ErrNotStaff = errors.New("not staff")

type StaffActor struct {
	ID        disputes.ID
	principal staffauth.Principal
}

type staffAuthorizer interface {
	Authenticate(*http.Request) (staffauth.Principal, error)
	Allows(staffauth.Principal, staffauth.Permission) bool
}

type StaffHandler struct {
	staff staffAuthorizer
	svc   *disputes.Service
}

func NewStaff(staff staffAuthorizer, svc *disputes.Service) (*StaffHandler, error) {
	if staff == nil || svc == nil {
		return nil, disputes.ErrUnavailable
	}
	return &StaffHandler{staff: staff, svc: svc}, nil
}

func (h *StaffHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/staff/disputes/{disputeId}", h.get)
	mux.HandleFunc("POST /v1/staff/disputes/{disputeId}/review", h.review)
	mux.HandleFunc("POST /v1/staff/disputes/{disputeId}/resolve", h.resolve)
	mux.HandleFunc("POST /v1/staff/disputes/{disputeId}/close", h.close)
	mux.HandleFunc("GET /v1/staff/disputes/{disputeId}/evidence", h.listEvidence)
	mux.HandleFunc("POST /v1/staff/disputes/{disputeId}/evidence", h.addEvidence)
}

func (h *StaffHandler) requireStaff(w http.ResponseWriter, r *http.Request, perm staffauth.Permission) (StaffActor, bool) {
	principal, err := h.staff.Authenticate(r)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) || errors.Is(err, staffauth.ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return StaffActor{}, false
		}
		if errors.Is(err, ErrNotStaff) || errors.Is(err, staffauth.ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return StaffActor{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return StaffActor{}, false
	}
	if principal.StaffID.IsZero() {
		writeError(w, http.StatusForbidden, "forbidden")
		return StaffActor{}, false
	}
	if !h.staff.Allows(principal, perm) {
		writeError(w, http.StatusForbidden, "forbidden")
		return StaffActor{}, false
	}
	var id disputes.ID
	copy(id[:], principal.StaffID[:])
	return StaffActor{ID: id, principal: principal.Clone()}, true
}

func (h *StaffHandler) get(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermDisputesRead); !ok {
		return
	}
	id, ok := parsePathID(w, r, "disputeId")
	if !ok {
		return
	}
	d, err := h.svc.GetInternal(r.Context(), id)
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffDTO(d))
}

func (h *StaffHandler) review(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermDisputesReview); !ok {
		return
	}
	id, ok := parsePathID(w, r, "disputeId")
	if !ok {
		return
	}
	d, err := h.svc.StartReview(r.Context(), id)
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffDTO(d))
}

func (h *StaffHandler) resolve(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermDisputesReview); !ok {
		return
	}
	id, ok := parsePathID(w, r, "disputeId")
	if !ok {
		return
	}
	var req struct {
		ResolutionCode string `json:"resolutionCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	d, err := h.svc.Resolve(r.Context(), id, disputes.ResolutionCode(strings.TrimSpace(req.ResolutionCode)))
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffDTO(d))
}

func (h *StaffHandler) close(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermDisputesReview); !ok {
		return
	}
	id, ok := parsePathID(w, r, "disputeId")
	if !ok {
		return
	}
	d, err := h.svc.Close(r.Context(), id)
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toStaffDTO(d))
}

func (h *StaffHandler) listEvidence(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireStaff(w, r, staffauth.PermDisputesRead); !ok {
		return
	}
	id, ok := parsePathID(w, r, "disputeId")
	if !ok {
		return
	}
	list, err := h.svc.ListEvidenceInternal(r.Context(), id)
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

func (h *StaffHandler) addEvidence(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireStaff(w, r, staffauth.PermDisputesReview)
	if !ok {
		return
	}
	id, ok := parsePathID(w, r, "disputeId")
	if !ok {
		return
	}
	in, ok := decodeStaffEvidence(w, r)
	if !ok {
		return
	}
	staffID := actor.ID
	e, err := h.svc.AddInternalEvidence(r.Context(), id, in, &staffID)
	if err != nil {
		writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toEvidenceDTO(e))
}

func decodeStaffEvidence(w http.ResponseWriter, r *http.Request) (disputes.EvidenceInput, bool) {
	var req struct {
		Title          string `json:"title"`
		Description    string `json:"description"`
		ReferenceValue string `json:"referenceValue"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return disputes.EvidenceInput{}, false
	}
	in := disputes.EvidenceInput{
		EvidenceType: disputes.EvidenceInternalReference,
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

type staffDisputeDTO struct {
	disputeDTO
	OpenedByUserID  string `json:"openedByUserId"`
	RequesterUserID string `json:"requesterUserId"`
	ProviderUserID  string `json:"providerUserId"`
}

func toStaffDTO(d disputes.Dispute) staffDisputeDTO {
	return staffDisputeDTO{
		disputeDTO:      toDTO(d),
		OpenedByUserID:  d.OpenedByUserID.String(),
		RequesterUserID: d.RequesterUserID.String(),
		ProviderUserID:  d.ProviderUserID.String(),
	}
}
