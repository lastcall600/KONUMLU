package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identitycontracts "backend/internal/identity/contracts"
	staffauth "backend/internal/staffauth/contracts"
	"backend/internal/trust"
)

func TestStaffTrustGet(t *testing.T) {
	h := newTestHandler(t)
	staff, err := NewStaff(allowStaff{}, h.svc, staffProfiles{
		publicID: identityID(h.publicID),
		userID:   identityID(h.userID),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDo(t, staff, "/v1/staff/trust/profiles/"+h.publicID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body staffTrustDTO
	decode(t, rec, &body)
	if body.Level != trust.LevelNew || body.VerifiedInteractionCount != 0 {
		t.Fatalf("body = %+v", body)
	}
	if body.History == nil {
		t.Fatal("history omitted")
	}
	if strings.Contains(rec.Body.String(), h.userID.String()) {
		t.Fatalf("user id leaked: %s", rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "score") {
		t.Fatalf("score leaked: %s", rec.Body.String())
	}
}

func TestStaffTrustAuthZ(t *testing.T) {
	h := newTestHandler(t)
	unauth, err := NewStaff(stubStaff{err: staffauth.ErrUnauthenticated}, h.svc, staffProfiles{})
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDo(t, unauth, "/v1/staff/trust/profiles/"+h.publicID.String())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth = %d", rec.Code)
	}
	deny, err := NewStaff(stubStaff{deny: map[staffauth.Permission]bool{staffauth.PermTrustRead: true}}, h.svc, staffProfiles{
		publicID: identityID(h.publicID),
		userID:   identityID(h.userID),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDo(t, deny, "/v1/staff/trust/profiles/"+h.publicID.String())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("forbidden = %d", rec.Code)
	}
	staff, err := NewStaff(allowStaff{}, h.svc, staffProfiles{
		publicID: identityID(h.publicID),
		userID:   identityID(h.userID),
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDo(t, staff, "/v1/staff/trust/profiles/"+mustID(t).String())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown = %d", rec.Code)
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

type staffProfiles struct {
	publicID identitycontracts.ID
	userID   identitycontracts.ID
}

func (s staffProfiles) StaffByPublicID(_ context.Context, publicProfileID identitycontracts.ID) (identitycontracts.StaffProfile, error) {
	if publicProfileID != s.publicID || s.publicID.IsZero() {
		return identitycontracts.StaffProfile{}, identitycontracts.ErrNotFound
	}
	return identitycontracts.StaffProfile{PublicProfileID: publicProfileID, AccountEligible: true}, nil
}

func (s staffProfiles) StaffByUserID(context.Context, identitycontracts.ID) (identitycontracts.StaffProfile, error) {
	return identitycontracts.StaffProfile{}, identitycontracts.ErrUnavailable
}

func (s staffProfiles) StaffUserIDByPublicID(_ context.Context, publicProfileID identitycontracts.ID) (identitycontracts.ID, error) {
	if publicProfileID != s.publicID || s.publicID.IsZero() {
		return identitycontracts.ID{}, identitycontracts.ErrNotFound
	}
	return s.userID, nil
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
