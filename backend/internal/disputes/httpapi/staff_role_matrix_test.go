package httpapi

import (
	"context"
	"net/http"
	"testing"

	staffauthimpl "backend/internal/staffauth"
	staffauth "backend/internal/staffauth/contracts"
)

func TestStaffDisputeDefaultPolicyRoleMatrix(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.requester
	rec := do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/dispute", allowedOrigin, map[string]any{"reasonCode": "other"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var created disputeDTO
	decode(t, rec, &created)
	path := "/v1/staff/disputes/" + created.DisputeID

	moderator, err := NewStaff(mustRoleAuthorizer(t, staffauth.RoleModerator), h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, moderator, http.MethodGet, path, allowedOrigin, nil, nil, withBearer("staff-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("moderator get status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, moderator, http.MethodPost, path+"/review", allowedOrigin, map[string]any{}, nil, withBearer("staff-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("moderator review status = %d body=%s", rec.Code, rec.Body.String())
	}

	support, err := NewStaff(mustRoleAuthorizer(t, staffauth.RoleSupport), h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, support, http.MethodGet, path, allowedOrigin, nil, nil, withBearer("staff-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("support get status = %d body=%s", rec.Code, rec.Body.String())
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

func withBearer(token string) headerOption {
	return func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
	}
}
