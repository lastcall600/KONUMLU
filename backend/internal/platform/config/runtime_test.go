package config

import (
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"
)

func setBaseLoadEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envDatabaseURL, "postgres://konumlu:super-secret-db@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)
}

func setProductionRequiredEnv(t *testing.T) {
	t.Helper()
	setBaseLoadEnv(t)
	t.Setenv(envAppEnv, "production")
	t.Setenv(envValkeyURL, "redis://127.0.0.1:6379/0")
	t.Setenv(envTrustedProxies, "10.0.0.0/8")
	t.Setenv(envWebAuthnRPDisplayName, "KONUMLU")
	t.Setenv(envWebAuthnRPID, "konumlu.example")
	t.Setenv(envWebAuthnRPOrigins, "https://konumlu.example")
	t.Setenv(envObjectStorageEnabled, "true")
	t.Setenv(envOutboxWorkerConcurrency, "2")
	t.Setenv("OBJECT_STORAGE_ENDPOINT", "https://objects.example.test")
	t.Setenv("OBJECT_STORAGE_REGION", "eu-central-1")
	t.Setenv("OBJECT_STORAGE_BUCKET", "konumlu-media")
	t.Setenv("OBJECT_STORAGE_ACCESS_KEY", "access-key")
	t.Setenv("OBJECT_STORAGE_SECRET_KEY", "object-secret-xyz")
	t.Setenv("OBJECT_STORAGE_UPLOAD_TTL", "15m")
}

func TestLoadRejectsUnknownEnvironment(t *testing.T) {
	setBaseLoadEnv(t)
	t.Setenv(envAppEnv, "prod")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid APP_ENV error")
	}
}

func TestProductionRejectsMissingSecurityConfig(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv(envValkeyURL, "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected missing VALKEY_URL")
	}
	if strings.Contains(err.Error(), "super-secret-db") {
		t.Fatalf("error leaked database secret: %v", err)
	}

	setProductionRequiredEnv(t)
	t.Setenv(envTrustedProxies, "")
	t.Setenv("TRUSTED_PROXIES", "")
	// empty TRUSTED_PROXIES is explicit none; missing is fail-closed. Keep it set via Setenv("").
	cfg, err := Load()
	if err != nil {
		t.Fatalf("empty TRUSTED_PROXIES must be allowed: %v", err)
	}
	if !cfg.TrustedProxiesSet || len(cfg.TrustedProxies) != 0 {
		t.Fatalf("empty trusted proxies: set=%v nets=%v", cfg.TrustedProxiesSet, cfg.TrustedProxies)
	}
}

func TestProductionRejectsUnsetTrustedProxies(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv(envTrustedProxies, "")
	// Unset: t.Setenv to empty still sets the variable. Use a child-style check via applyRuntimeGates.
	cfg := Config{Environment: EnvProduction, TrustedProxiesSet: false, ValkeyURL: "redis://127.0.0.1:6379/0", WebAuthnRPDisplayName: "x", WebAuthnRPID: "x", WebAuthnRPOrigins: []string{"https://x.example"}}
	t.Setenv(envObjectStorageEnabled, "true")
	t.Setenv(envOutboxWorkerConcurrency, "1")
	if err := applyRuntimeGates(&cfg); err == nil {
		t.Fatal("unset TRUSTED_PROXIES must fail in production")
	}
}

func TestProductionRejectsDisabledObjectStorage(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv(envObjectStorageEnabled, "")
	if _, err := Load(); err == nil {
		t.Fatal("expected object storage required")
	}
}

func TestProductionRejectsMissingWebAuthnOrigins(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv(envWebAuthnRPOrigins, "")
	if _, err := Load(); err == nil {
		t.Fatal("expected webauthn origins required")
	}
}

func TestDevelopmentOnlyInsecureCookiesCannotActivateInProduction(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv(envAllowInsecureCookies, "true")
	if _, err := Load(); err == nil {
		t.Fatal("expected ALLOW_INSECURE_COOKIES rejected in production")
	}
	setBaseLoadEnv(t)
	t.Setenv(envAppEnv, "development")
	t.Setenv(envAllowInsecureCookies, "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AllowInsecureCookies {
		t.Fatal("development may set ALLOW_INSECURE_COOKIES")
	}
}

func TestStagingFailClosedLikeProduction(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv(envAppEnv, "staging")
	t.Setenv(envValkeyURL, "")
	if _, err := Load(); err == nil {
		t.Fatal("staging must require VALKEY_URL")
	}
}

func TestTrustedProxyCIDRParse(t *testing.T) {
	setBaseLoadEnv(t)
	t.Setenv(envTrustedProxies, "10.0.0.0/8, 192.0.2.1")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("proxies = %#v", cfg.TrustedProxies)
	}
	ip := net.ParseIP("10.9.1.2")
	if ip == nil || !cfg.TrustedProxies[0].Contains(ip) {
		t.Fatal("10.0.0.0/8 must contain 10.9.1.2")
	}
}

func TestTrustedProxyMalformed(t *testing.T) {
	setBaseLoadEnv(t)
	t.Setenv(envTrustedProxies, "not-a-cidr")
	if _, err := Load(); err == nil {
		t.Fatal("expected malformed TRUSTED_PROXIES")
	}
}

func TestWorkerAndPoolConfigParsing(t *testing.T) {
	setBaseLoadEnv(t)
	t.Setenv(envOutboxWorkerConcurrency, "4")
	t.Setenv(envDBMaxConns, "20")
	t.Setenv(envDBMinConns, "2")
	t.Setenv(envDBMaxConnLifetime, "30m")
	t.Setenv(envDBMaxConnIdleTime, "5m")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OutboxWorkerConcurrency != 4 {
		t.Fatalf("workers = %d", cfg.OutboxWorkerConcurrency)
	}
	if cfg.DBPool.MaxConns != 20 || cfg.DBPool.MinConns != 2 || cfg.DBPool.MaxConnLifetime != 30*time.Minute {
		t.Fatalf("pool = %+v", cfg.DBPool)
	}
}

func TestProductionRequiresWorkerConcurrencySet(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv(envOutboxWorkerConcurrency, "")
	if _, err := Load(); err == nil {
		t.Fatal("expected OUTBOX_WORKER_CONCURRENCY required in production")
	}
}

func TestConfigLogValueOmitsSecrets(t *testing.T) {
	setProductionRequiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	logger := slog.New(slog.NewTextHandler(&b, nil))
	logger.Info("boot", "config", cfg)
	out := b.String()
	if strings.Contains(out, "super-secret-db") || strings.Contains(out, "postgres://") || strings.Contains(out, "object-secret-xyz") {
		t.Fatalf("log leaked secrets: %s", out)
	}
	if strings.Contains(cfg.String(), "super-secret-db") || strings.Contains(cfg.String(), "object-secret-xyz") {
		t.Fatalf("String leaked: %s", cfg.String())
	}
}

func TestProductionObjectStorageMissingKeys(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv("OBJECT_STORAGE_SECRET_KEY", "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected missing object storage secret")
	}
	if strings.Contains(err.Error(), "object-secret-xyz") {
		t.Fatalf("leaked: %v", err)
	}
}

func TestProductionWorkloadForbidsStaticKeys(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv("OBJECT_STORAGE_CREDENTIAL_SOURCE", "workload")
	if _, err := Load(); err == nil {
		t.Fatal("expected workload+keys error")
	}
}

func TestStaffIDPAbsenceIsNotAProductionConfigError(t *testing.T) {
	setProductionRequiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.StaffIDP.Empty() {
		t.Fatalf("staff idp = %+v", cfg.StaffIDP)
	}
	if !cfg.StaffDevIDP.Empty() {
		t.Fatalf("staff dev idp = %+v", cfg.StaffDevIDP)
	}
}

func TestHumanChallengeConfigGates(t *testing.T) {
	setBaseLoadEnv(t)
	t.Setenv(envHumanChallengeOperations, "password_login")
	t.Setenv(envHumanChallengeProvider, "none")
	if _, err := Load(); err == nil {
		t.Fatal("required operations with none must fail")
	}

	setBaseLoadEnv(t)
	t.Setenv(envHumanChallengeOperations, "password_login")
	t.Setenv(envHumanChallengeProvider, "fake")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("dev fake: %v", err)
	}
	if cfg.HumanChallenge.Provider != "fake" || cfg.HumanChallenge.ReplayTTL <= 0 {
		t.Fatalf("dev fake cfg = %+v", cfg.HumanChallenge)
	}

	setProductionRequiredEnv(t)
	t.Setenv(envHumanChallengeProvider, "fake")
	if _, err := Load(); err == nil {
		t.Fatal("production fake must fail")
	}

	setProductionRequiredEnv(t)
	t.Setenv(envAppEnv, "staging")
	t.Setenv(envHumanChallengeProvider, "fake")
	if _, err := Load(); err == nil {
		t.Fatal("staging fake must fail")
	}

	setProductionRequiredEnv(t)
	t.Setenv(envHumanChallengeOperations, "password_login")
	t.Setenv(envHumanChallengeProvider, "unconfigured")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("production unconfigured: %v", err)
	}
	if cfg.HumanChallenge.Provider != "unconfigured" {
		t.Fatalf("provider = %q", cfg.HumanChallenge.Provider)
	}
}

func TestAuthProviderLaunchBlockersAreExplicit(t *testing.T) {
	setProductionRequiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cfg.AuthProviderLaunchBlockers(), ",")
	for _, want := range []string{"human_challenge_vendor", "email_vendor", "sms_vendor"} {
		if !strings.Contains(got, want) {
			t.Fatalf("blockers = %q missing %s", got, want)
		}
	}
}

func TestProductionCannotDisableAbuseLimits(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv(envAuthIPMaxAttempts, "0")
	if _, err := Load(); err == nil {
		t.Fatal("zero IP max must fail")
	}
}
