package staffidp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/platform/config"
	"backend/internal/staffauth"
	"backend/internal/staffauth/contracts"
)

func TestDevProviderAuthenticatesServerAuthoritativePrincipal(t *testing.T) {
	id := mustDevStaffID(t)
	p, err := NewDev(config.StaffDevIDP{
		Enabled: true,
		Token:   "local-dev-staff-token",
		StaffID: id.String(),
		Roles:   []string{"moderator"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Verify(context.Background(), contracts.Credential{Token: "local-dev-staff-token"})
	if err != nil {
		t.Fatal(err)
	}
	if got.StaffID != id {
		t.Fatalf("staff id = %s want %s", got.StaffID, id)
	}
	if len(got.Roles) != 1 || got.Roles[0] != contracts.RoleModerator {
		t.Fatalf("roles = %v", got.Roles)
	}
}

func TestDevProviderRejectsInvalidCredential(t *testing.T) {
	id := mustDevStaffID(t)
	p, err := NewDev(config.StaffDevIDP{
		Enabled: true,
		Token:   "local-dev-staff-token",
		StaffID: id.String(),
		Roles:   []string{"moderator"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Verify(context.Background(), contracts.Credential{Token: "wrong"}); !errors.Is(err, contracts.ErrUnauthenticated) {
		t.Fatalf("err = %v", err)
	}
	if _, err := p.Verify(context.Background(), contracts.Credential{}); !errors.Is(err, contracts.ErrUnauthenticated) {
		t.Fatalf("empty token err = %v", err)
	}
}

func TestDevProviderDisabledDoesNotAuthenticate(t *testing.T) {
	id := mustDevStaffID(t)
	if _, err := NewDev(config.StaffDevIDP{
		Enabled: false,
		Token:   "local-dev-staff-token",
		StaffID: id.String(),
		Roles:   []string{"moderator"},
	}); !errors.Is(err, contracts.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestDevProviderRejectsUnknownRole(t *testing.T) {
	id := mustDevStaffID(t)
	if _, err := NewDev(config.StaffDevIDP{
		Enabled: true,
		Token:   "local-dev-staff-token",
		StaffID: id.String(),
		Roles:   []string{"superuser"},
	}); !errors.Is(err, contracts.ErrMisconfigured) {
		t.Fatalf("err = %v", err)
	}
}

func TestDevAuthorizerIgnoresClientRoleHeadersAndConsumerSession(t *testing.T) {
	id := mustDevStaffID(t)
	p, err := NewDev(config.StaffDevIDP{
		Enabled: true,
		Token:   "local-dev-staff-token",
		StaffID: id.String(),
		Roles:   []string{"moderator"},
	})
	if err != nil {
		t.Fatal(err)
	}
	authz, err := staffauth.NewAuthorizer(p, staffauth.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-konumlu_session", Value: "consumer-session"})
	req.Header.Set("X-Staff-Role", "admin")
	if _, err := authz.Authenticate(req); !errors.Is(err, contracts.ErrUnauthenticated) {
		t.Fatalf("consumer session err = %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports", nil)
	req.Header.Set("Authorization", "Bearer local-dev-staff-token")
	req.Header.Set("X-Staff-Role", "admin")
	req.Header.Set("X-Staff-Permission", "moderation.action.approve")
	principal, err := authz.Authenticate(req)
	if err != nil {
		t.Fatal(err)
	}
	if principal.StaffID != id {
		t.Fatalf("staff id = %s", principal.StaffID)
	}
	if authz.Allows(principal, contracts.PermModerationActionApprove) {
		t.Fatal("client header must not grant approve")
	}
	if !authz.Allows(principal, contracts.PermModerationReportRead) {
		t.Fatal("moderator should read reports from server roles")
	}
}

func TestResolveDevEnabled(t *testing.T) {
	id := mustDevStaffID(t)
	p, err := Resolve(config.Config{
		Environment: config.EnvDevelopment,
		StaffDevIDP: config.StaffDevIDP{
			Enabled: true,
			Token:   "local-dev-staff-token",
			StaffID: id.String(),
			Roles:   []string{"moderator"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*DevProvider); !ok {
		t.Fatalf("provider = %T", p)
	}
}

func TestResolveMissingDevDoesNotAuthenticate(t *testing.T) {
	p, err := Resolve(config.Config{Environment: config.EnvDevelopment})
	if err != nil || p != nil {
		t.Fatalf("p=%v err=%v", p, err)
	}
}

func TestResolveProductionRejectsDevAdapter(t *testing.T) {
	id := mustDevStaffID(t)
	dev := config.StaffDevIDP{
		Enabled: true,
		Token:   "local-dev-staff-token",
		StaffID: id.String(),
		Roles:   []string{"admin"},
	}
	for _, env := range []config.Environment{config.EnvProduction, config.EnvStaging} {
		_, err := Resolve(config.Config{Environment: env, StaffDevIDP: dev})
		if !errors.Is(err, ErrDevForbidden) {
			t.Fatalf("env %s err=%v", env, err)
		}
	}
}

func TestResolveProductionMissingAdapterFailClosed(t *testing.T) {
	p, err := Resolve(config.Config{Environment: config.EnvProduction})
	if err != nil || p != nil {
		t.Fatalf("empty production p=%v err=%v", p, err)
	}
	_, err = Resolve(config.Config{
		Environment: config.EnvProduction,
		StaffIDP: config.StaffIDP{
			Issuer:   "https://idp.example.test",
			Audience: "konumlu-staff",
			JWKSURL:  "https://idp.example.test/jwks",
		},
	})
	if !errors.Is(err, ErrAdapterRequired) {
		t.Fatalf("complete production without adapter err=%v", err)
	}
}

func TestResolvePartialProductionStaffIDPFailClosed(t *testing.T) {
	_, err := Resolve(config.Config{
		Environment: config.EnvProduction,
		StaffIDP:    config.StaffIDP{Issuer: "https://idp.example.test"},
	})
	if !errors.Is(err, ErrMisconfigured) {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveDevCannotCombineWithStaffIDP(t *testing.T) {
	id := mustDevStaffID(t)
	_, err := Resolve(config.Config{
		Environment: config.EnvDevelopment,
		StaffIDP: config.StaffIDP{
			Issuer:   "https://idp.example.test",
			Audience: "konumlu-staff",
			JWKSURL:  "https://idp.example.test/jwks",
		},
		StaffDevIDP: config.StaffDevIDP{
			Enabled: true,
			Token:   "local-dev-staff-token",
			StaffID: id.String(),
			Roles:   []string{"moderator"},
		},
	})
	if !errors.Is(err, ErrDevConflict) {
		t.Fatalf("err=%v", err)
	}
}

func mustDevStaffID(t *testing.T) contracts.ID {
	t.Helper()
	id, err := contracts.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
