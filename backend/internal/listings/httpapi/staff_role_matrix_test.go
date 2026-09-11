package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	staffauthimpl "backend/internal/staffauth"
	staffauth "backend/internal/staffauth/contracts"
)

func TestStaffListingDefaultPolicyRoleMatrix(t *testing.T) {
	env := newTestHandler(t)
	listing := createDraft(t, env)
	path := "/v1/staff/listings/" + listing.ID.String()

	support, err := NewStaff(mustRoleAuthorizer(t, staffauth.RoleSupport), env.svc, nil, nil, &staffProfiles{})
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDoBearer(t, support, http.MethodGet, path, "staff-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("support status = %d body=%s", rec.Code, rec.Body.String())
	}

	moderator, err := NewStaff(mustRoleAuthorizer(t, staffauth.RoleModerator), env.svc, nil, nil, &staffProfiles{})
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDoBearer(t, moderator, http.MethodGet, path, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer status = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "__Host-konumlu_session", Value: "consumer"})
	req.Header.Set("X-Staff-Role", "admin")
	rec = staffServeReq(t, moderator, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("consumer cookie status = %d", rec.Code)
	}

	rec = staffDoBearer(t, moderator, http.MethodGet, path, "staff-token")
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

func staffDoBearer(t *testing.T, h *StaffHandler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return staffServeReq(t, h, req)
}

func staffServeReq(t *testing.T, h *StaffHandler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
