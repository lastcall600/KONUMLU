package publicprofile

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	staffauthimpl "backend/internal/staffauth"
	staffauth "backend/internal/staffauth/contracts"
)

func TestStaffProfileDefaultPolicyRoleMatrix(t *testing.T) {
	h := newHTTPFixture(t)
	me := do(t, h, http.MethodGet, "/v1/profile/me", "", nil, authedCookies())
	id := decodeDTO(t, me).PublicProfileID
	path := "/v1/staff/identity/profiles/" + id

	support, err := NewStaff(mustRoleAuthorizer(t, staffauth.RoleSupport), h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDoBearer(t, support, path, "staff-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("support status = %d body=%s", rec.Code, rec.Body.String())
	}

	moderator, err := NewStaff(mustRoleAuthorizer(t, staffauth.RoleModerator), h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDoBearer(t, moderator, path, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer status = %d", rec.Code)
	}
	rec = staffDoBearer(t, moderator, path, "staff-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("moderator status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func mustRoleAuthorizer(t *testing.T, role staffauth.Role) *staffauthimpl.Authorizer {
	t.Helper()
	id, err := staffauth.NewID()
	if err != nil {
		t.Fatal(err)
	}
	authz, err := staffauthimpl.NewAuthorizer(matrixStaffProvider{principal: staffauth.Principal{
		StaffID: id,
		Roles:   []staffauth.Role{role},
	}}, staffauthimpl.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return authz
}

type matrixStaffProvider struct {
	principal staffauth.Principal
}

func (s matrixStaffProvider) Verify(_ context.Context, cred staffauth.Credential) (staffauth.Principal, error) {
	if cred.Token != "staff-token" {
		return staffauth.Principal{}, staffauth.ErrUnauthenticated
	}
	return s.principal, nil
}

func staffDoBearer(t *testing.T, h *StaffHandler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
