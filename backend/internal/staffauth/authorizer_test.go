package staffauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/staffauth/contracts"
)

func TestDefaultPolicyRolePermissions(t *testing.T) {
	policy := DefaultPolicy()
	id := mustStaffID(t)
	moderator := contracts.Principal{StaffID: id, Roles: []contracts.Role{contracts.RoleModerator}}
	senior := contracts.Principal{StaffID: id, Roles: []contracts.Role{contracts.RoleSeniorModerator}}
	support := contracts.Principal{StaffID: id, Roles: []contracts.Role{contracts.RoleSupport}}
	admin := contracts.Principal{StaffID: id, Roles: []contracts.Role{contracts.RoleAdmin}}

	if !policy.Allows(moderator, contracts.PermModerationCaseWrite) {
		t.Fatal("moderator should write cases")
	}
	if policy.Allows(moderator, contracts.PermModerationActionApprove) {
		t.Fatal("moderator must not approve")
	}
	if policy.Allows(moderator, contracts.PermModerationAppealReview) {
		t.Fatal("moderator must not review appeals")
	}
	if policy.Allows(moderator, contracts.PermDisputesReview) {
		t.Fatal("moderator must not review disputes")
	}
	if !policy.Allows(senior, contracts.PermModerationActionApprove) || !policy.Allows(senior, contracts.PermModerationAppealReview) {
		t.Fatal("senior moderator should approve and review appeals")
	}
	if policy.Allows(support, contracts.PermModerationCaseRead) {
		t.Fatal("support must not read moderation cases")
	}
	if !policy.Allows(support, contracts.PermDisputesReview) {
		t.Fatal("support should review disputes")
	}
	if !policy.Allows(admin, contracts.PermModerationActionApprove) || !policy.Allows(admin, contracts.PermDisputesRead) {
		t.Fatal("admin should have both moderation and dispute permissions")
	}
	if policy.Allows(contracts.Principal{Roles: []contracts.Role{contracts.RoleAdmin}}, contracts.PermDisputesRead) {
		t.Fatal("zero staffId must not grant permissions")
	}
}

func TestAuthorizerRequiresProvider(t *testing.T) {
	if _, err := NewAuthorizer(nil, DefaultPolicy()); !errors.Is(err, contracts.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthorizerUnauthenticatedAndIgnoresPrivilegeHeaders(t *testing.T) {
	id := mustStaffID(t)
	authz, err := NewAuthorizer(staticProvider{principal: contracts.Principal{
		StaffID: id,
		Roles:   []contracts.Role{contracts.RoleModerator},
	}}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-konumlu_session", Value: "consumer-session"})
	req.Header.Set("X-Staff-Role", "admin")
	req.Header.Set("X-Staff-Permission", "moderation.action.approve")
	if _, err := authz.Authenticate(req); !errors.Is(err, contracts.ErrUnauthenticated) {
		t.Fatalf("consumer cookie + privilege headers err = %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports", nil)
	req.Header.Set("Authorization", "Bearer staff-token")
	req.Header.Set("X-Staff-Role", "admin")
	p, err := authz.Authenticate(req)
	if err != nil {
		t.Fatal(err)
	}
	if authz.Allows(p, contracts.PermModerationActionApprove) {
		t.Fatal("client role header must not grant approve")
	}
	if !authz.Allows(p, contracts.PermModerationCaseWrite) {
		t.Fatal("moderator should still have case.write from provider roles")
	}
}

func TestAuthorizerProviderUnavailableFailClosed(t *testing.T) {
	authz, err := NewAuthorizer(staticProvider{err: errors.New("jwks down")}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports", nil)
	req.Header.Set("Authorization", "Bearer staff-token")
	if _, err := authz.Authenticate(req); !errors.Is(err, contracts.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthorizerAuthenticatedWithoutPermission(t *testing.T) {
	id := mustStaffID(t)
	authz, err := NewAuthorizer(staticProvider{principal: contracts.Principal{
		StaffID: id,
		Roles:   []contracts.Role{contracts.RoleSupport},
	}}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/staff/moderation/cases/x/actions/y/status", nil)
	req.Header.Set("Authorization", "Bearer staff-token")
	p, err := authz.Authenticate(req)
	if err != nil {
		t.Fatal(err)
	}
	if authz.Allows(p, contracts.PermModerationActionApprove) {
		t.Fatal("support must not approve")
	}
}

type staticProvider struct {
	principal contracts.Principal
	err       error
}

func (s staticProvider) Verify(_ context.Context, cred contracts.Credential) (contracts.Principal, error) {
	if s.err != nil {
		return contracts.Principal{}, s.err
	}
	if cred.Token != "staff-token" {
		return contracts.Principal{}, contracts.ErrUnauthenticated
	}
	return s.principal, nil
}

func mustStaffID(t *testing.T) contracts.ID {
	t.Helper()
	id, err := contracts.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
