package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	staffauth "backend/internal/staffauth/contracts"
)

func TestStaffListingSummary(t *testing.T) {
	h := newTestHandler(t)
	staff, err := NewStaff(allowStaff{}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	listing := mustID(t)
	rec := staffDo(t, staff, "/v1/staff/review-aggregates/listings/"+listing.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body listingSummaryDTO
	decode(t, rec, &body)
	if body.ListingAccuracy.ReviewCount != 0 || body.ListingAccuracy.Average != nil {
		t.Fatalf("body = %+v", body)
	}
}

func TestStaffListingSummaryAuthZ(t *testing.T) {
	h := newTestHandler(t)
	unauth, err := NewStaff(stubStaff{err: staffauth.ErrUnauthenticated}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDo(t, unauth, "/v1/staff/review-aggregates/listings/"+mustID(t).String())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth = %d", rec.Code)
	}
	deny, err := NewStaff(stubStaff{deny: map[staffauth.Permission]bool{staffauth.PermListingsRead: true}}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDo(t, deny, "/v1/staff/review-aggregates/listings/"+mustID(t).String())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("forbidden = %d", rec.Code)
	}
}

type allowStaff struct{}

func (allowStaff) Authenticate(*http.Request) (staffauth.Principal, error) {
	var id staffauth.ID
	id[0] = 1
	return staffauth.Principal{StaffID: id, Roles: []staffauth.Role{staffauth.RoleModerator}}, nil
}

func (allowStaff) Allows(staffauth.Principal, staffauth.Permission) bool { return true }

type stubStaff struct {
	err  error
	deny map[staffauth.Permission]bool
}

func (s stubStaff) Authenticate(*http.Request) (staffauth.Principal, error) {
	if s.err != nil {
		return staffauth.Principal{}, s.err
	}
	var id staffauth.ID
	id[0] = 1
	return staffauth.Principal{StaffID: id}, nil
}

func (s stubStaff) Allows(_ staffauth.Principal, perm staffauth.Permission) bool {
	return !s.deny[perm]
}

func staffDo(t *testing.T, h *StaffHandler, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
