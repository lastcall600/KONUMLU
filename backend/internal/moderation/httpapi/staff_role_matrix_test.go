package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/moderation"
	staffauthimpl "backend/internal/staffauth"
	staffauth "backend/internal/staffauth/contracts"
)

func TestStaffDefaultPolicyRoleMatrixHTTP(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.other)
	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, validListingReport(listing), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}

	support, err := NewStaff(mustRoleAuthorizer(t, staffauth.RoleSupport), h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDoBearer(t, support, http.MethodGet, "/v1/staff/moderation/reports", nil, "staff-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("support reports status = %d body=%s", rec.Code, rec.Body.String())
	}

	moderator, err := NewStaff(mustRoleAuthorizer(t, staffauth.RoleModerator), h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDoBearer(t, moderator, http.MethodGet, "/v1/staff/moderation/reports", nil, "staff-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("moderator reports status = %d body=%s", rec.Code, rec.Body.String())
	}

	subject := h.publishListing(t, h.other)
	detail, err := h.svc.CreateCase(context.Background(), moderation.CreateCaseInput{
		SubjectType: moderation.TargetListing, SubjectID: subject, Title: "matrix",
	})
	if err != nil {
		t.Fatal(err)
	}
	base := "/v1/staff/moderation/cases/" + detail.Case.ID.String() + "/actions"
	rec = staffDoBearer(t, moderator, http.MethodPost, base, map[string]any{
		"targetType": "listing",
		"targetId":   subject.String(),
		"actionType": "warning",
		"reasonCode": "policy_violation",
	}, "staff-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("moderator create action status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created staffActionDTO
	decode(t, rec, &created)
	rec = staffDoBearer(t, moderator, http.MethodPost, base+"/"+created.ActionID+"/status", map[string]any{"status": "approved"}, "staff-token")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("moderator approve status = %d body=%s", rec.Code, rec.Body.String())
	}

	senior, err := NewStaff(mustRoleAuthorizer(t, staffauth.RoleSeniorModerator), h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDoBearer(t, senior, http.MethodPost, base+"/"+created.ActionID+"/status", map[string]any{"status": "approved"}, "staff-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("senior approve status = %d body=%s", rec.Code, rec.Body.String())
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

func staffDoBearer(t *testing.T, h *StaffHandler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(rec, r)
	return rec
}
