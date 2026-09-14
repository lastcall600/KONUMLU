package config

import (
	"strings"
	"testing"
	"time"
)

func setAuthRateLimitEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envAuthIPMaxAttempts, "20")
	t.Setenv(envAuthIPWindow, "15m")
	t.Setenv(envAuthPasswordUserMaxAttempts, "10")
	t.Setenv(envAuthPasswordUserWindow, "15m")
}

func setVerificationSignupEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envChallengeTTL, "10m")
	t.Setenv(envChallengeMaxAttempts, "5")
	t.Setenv(envChallengePhoneOTPDigits, "6")
	t.Setenv(envIssueDestMax, "5")
	t.Setenv(envIssueDestWindow, "1h")
	t.Setenv(envIssueIPMax, "10")
	t.Setenv(envIssueIPWindow, "1h")
	t.Setenv(envSignupProofTTL, "15m")
}

func setOutboxEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envOutboxBatchSize, "10")
	t.Setenv(envOutboxLease, "30s")
	t.Setenv(envOutboxPollInterval, "1s")
	t.Setenv(envOutboxRetryBase, "1m")
	t.Setenv(envOutboxRetryMultiplier, "2")
	t.Setenv(envOutboxRetryCap, "10m")
	t.Setenv(envOutboxRetryJitter, "0s")
}

func setMaterialKeyEnv(t *testing.T) {
	t.Helper()
	enc := "ERERERERERERERERERERERERERERERERERERERERERE="
	t.Setenv(envMaterialActiveKeyID, "test-v1")
	t.Setenv(envMaterialKeys, "test-v1:"+enc)
}

func setPushEndpointKeyEnv(t *testing.T) {
	t.Helper()
	enc := "ERERERERERERERERERERERERERERERERERERERERERE="
	hash := "IiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiIiI="
	t.Setenv(envPushEndpointEncryptionKey, enc)
	t.Setenv(envPushEndpointHashKey, hash)
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv(envHTTPAddr, "")
	t.Setenv(envShutdownTimeout, "")
	t.Setenv(envDBConnectTimeout, "")
	t.Setenv(envValkeyURL, "")
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Fatalf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("ShutdownTimeout = %s, want %s", cfg.ShutdownTimeout, defaultShutdownTimeout)
	}
	if cfg.DBConnectTimeout != defaultDBConnectTimeout {
		t.Fatalf("DBConnectTimeout = %s, want %s", cfg.DBConnectTimeout, defaultDBConnectTimeout)
	}
	if cfg.DatabaseURL == "" {
		t.Fatal("DatabaseURL is empty")
	}
	if cfg.SessionIdle != time.Hour || cfg.SessionAbsolute != 24*time.Hour {
		t.Fatalf("session policy = %s / %s", cfg.SessionIdle, cfg.SessionAbsolute)
	}
	if cfg.StepUpTTL != 5*time.Minute {
		t.Fatalf("StepUpTTL = %s", cfg.StepUpTTL)
	}
	if cfg.WebAuthnCeremonyTTL != 2*time.Minute {
		t.Fatalf("WebAuthnCeremonyTTL = %s", cfg.WebAuthnCeremonyTTL)
	}
	if cfg.ValkeyURL != "" {
		t.Fatalf("ValkeyURL = %q, want empty when unset", cfg.ValkeyURL)
	}
	if cfg.Environment != EnvDevelopment {
		t.Fatalf("Environment = %q, want development when APP_ENV unset", cfg.Environment)
	}
	if cfg.OutboxWorkerConcurrency != 1 {
		t.Fatalf("OutboxWorkerConcurrency = %d", cfg.OutboxWorkerConcurrency)
	}
	if cfg.AllowInsecureCookies {
		t.Fatal("AllowInsecureCookies must default false")
	}
	if cfg.TrustedProxiesSet {
		t.Fatal("TRUSTED_PROXIES unset must not count as set")
	}
	if strings.Contains(cfg.String(), "postgres://") || strings.Contains(cfg.String(), "konumlu") {
		t.Fatalf("String leaked secrets: %s", cfg.String())
	}
	if cfg.AuthIPMaxAttempts != 20 || cfg.AuthIPWindow != 15*time.Minute {
		t.Fatalf("auth IP limit = %d / %s", cfg.AuthIPMaxAttempts, cfg.AuthIPWindow)
	}
	if cfg.AuthPasswordUserMaxAttempts != 10 || cfg.AuthPasswordUserWindow != 15*time.Minute {
		t.Fatalf("password user limit = %d / %s", cfg.AuthPasswordUserMaxAttempts, cfg.AuthPasswordUserWindow)
	}
	if cfg.AuthTargetMaxAttempts != 10 || cfg.AuthCompleteMaxAttempts != 20 || cfg.AuthSensitiveMaxAttempts != 10 {
		t.Fatalf("inherited abuse limits target=%d complete=%d sensitive=%d", cfg.AuthTargetMaxAttempts, cfg.AuthCompleteMaxAttempts, cfg.AuthSensitiveMaxAttempts)
	}
	if cfg.HumanChallenge.Provider != "none" || len(cfg.HumanChallenge.Operations) != 0 {
		t.Fatalf("human challenge = %+v", cfg.HumanChallenge)
	}
	if cfg.ChallengeTTL != 10*time.Minute || cfg.ChallengeMaxAttempts != 5 || cfg.ChallengePhoneOTPDigits != 6 {
		t.Fatalf("challenge policy = %s / %d / %d", cfg.ChallengeTTL, cfg.ChallengeMaxAttempts, cfg.ChallengePhoneOTPDigits)
	}
	if cfg.IssueDestMax != 5 || cfg.IssueDestWindow != time.Hour || cfg.IssueIPMax != 10 || cfg.IssueIPWindow != time.Hour {
		t.Fatalf("issuance policy = %d / %s / %d / %s", cfg.IssueDestMax, cfg.IssueDestWindow, cfg.IssueIPMax, cfg.IssueIPWindow)
	}
	if cfg.SignupProofTTL != 15*time.Minute {
		t.Fatalf("SignupProofTTL = %s", cfg.SignupProofTTL)
	}
	if cfg.OutboxBatchSize != 10 || cfg.OutboxLease != 30*time.Second || cfg.OutboxPollInterval != time.Second {
		t.Fatalf("outbox claim = %d / %s / %s", cfg.OutboxBatchSize, cfg.OutboxLease, cfg.OutboxPollInterval)
	}
	if cfg.OutboxRetryBase != time.Minute || cfg.OutboxRetryMultiplier != 2 || cfg.OutboxRetryCap != 10*time.Minute || cfg.OutboxRetryJitter != 0 {
		t.Fatalf("outbox retry = %s / %g / %s / %s", cfg.OutboxRetryBase, cfg.OutboxRetryMultiplier, cfg.OutboxRetryCap, cfg.OutboxRetryJitter)
	}
	if cfg.MaterialKeys.ActiveID != "test-v1" || len(cfg.MaterialKeys.Keys) != 1 || len(cfg.MaterialKeys.Keys["test-v1"]) != 32 {
		t.Fatalf("material keys = %+v", cfg.MaterialKeys)
	}
	if cfg.NotificationsEmailMode != NotificationChannelDisabled || cfg.NotificationsSMSMode != NotificationChannelDisabled {
		t.Fatalf("notification modes = %q / %q", cfg.NotificationsEmailMode, cfg.NotificationsSMSMode)
	}
	if cfg.PushEndpoints.Enabled {
		t.Fatal("push endpoints must stay disabled without keys")
	}
	if !cfg.StaffIDP.Empty() {
		t.Fatalf("StaffIDP = %+v, want empty", cfg.StaffIDP)
	}
	if !cfg.StaffDevIDP.Empty() {
		t.Fatalf("StaffDevIDP = %+v, want empty", cfg.StaffDevIDP)
	}
}

func TestLoadFromEnvironment(t *testing.T) {
	t.Setenv(envHTTPAddr, "127.0.0.1:9090")
	t.Setenv(envShutdownTimeout, "3s")
	t.Setenv(envDBConnectTimeout, "2s")
	t.Setenv(envDatabaseURL, "postgres://app:secret@db:5432/app?sslmode=disable")
	t.Setenv(envSessionIdle, "15m")
	t.Setenv(envSessionAbsolute, "8h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "90s")
	t.Setenv(envValkeyURL, "redis://127.0.0.1:6379/0")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9090" {
		t.Fatalf("HTTPAddr = %q, want %q", cfg.HTTPAddr, "127.0.0.1:9090")
	}
	if cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want 3s", cfg.ShutdownTimeout)
	}
	if cfg.DBConnectTimeout != 2*time.Second {
		t.Fatalf("DBConnectTimeout = %s, want 2s", cfg.DBConnectTimeout)
	}
	if cfg.DatabaseURL != "postgres://app:secret@db:5432/app?sslmode=disable" {
		t.Fatalf("DatabaseURL mismatch")
	}
	if cfg.SessionIdle != 15*time.Minute || cfg.SessionAbsolute != 8*time.Hour {
		t.Fatalf("session policy = %s / %s", cfg.SessionIdle, cfg.SessionAbsolute)
	}
	if cfg.WebAuthnCeremonyTTL != 90*time.Second {
		t.Fatalf("WebAuthnCeremonyTTL = %s", cfg.WebAuthnCeremonyTTL)
	}
	if cfg.ValkeyURL != "redis://127.0.0.1:6379/0" {
		t.Fatalf("ValkeyURL = %q", cfg.ValkeyURL)
	}
	if cfg.AuthIPMaxAttempts != 20 || cfg.AuthPasswordUserMaxAttempts != 10 {
		t.Fatalf("auth limits = %d / %d", cfg.AuthIPMaxAttempts, cfg.AuthPasswordUserMaxAttempts)
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv(envDatabaseURL, "")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error for empty DATABASE_URL")
	}
}

func TestLoadInvalidShutdownTimeout(t *testing.T) {
	t.Setenv(envShutdownTimeout, "not-a-duration")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error for invalid SHUTDOWN_TIMEOUT")
	}
}

func TestLoadRequiresSessionPolicy(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	t.Setenv(envSessionIdle, "")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty IDENTITY_SESSION_IDLE")
	}

	t.Setenv(envSessionIdle, "2h")
	t.Setenv(envSessionAbsolute, "1h")
	t.Setenv(envStepUpTTL, "5m")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when absolute < idle")
	}

	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty IDENTITY_STEP_UP_TTL")
	}
	t.Setenv(envStepUpTTL, "30m")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when step-up TTL exceeds 15m")
	}
	t.Setenv(envSessionIdle, "2m")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when step-up TTL exceeds idle")
	}
}

func TestLoadWebAuthnOriginsFromEnvironment(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	t.Setenv(envWebAuthnRPDisplayName, "KONUMLU local")
	t.Setenv(envWebAuthnRPID, "localhost")
	t.Setenv(envWebAuthnRPOrigins, "http://localhost:8080, https://app.example.test")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.WebAuthnRPDisplayName != "KONUMLU local" {
		t.Fatalf("WebAuthnRPDisplayName = %q", cfg.WebAuthnRPDisplayName)
	}
	if cfg.WebAuthnRPID != "localhost" {
		t.Fatalf("WebAuthnRPID = %q", cfg.WebAuthnRPID)
	}
	if len(cfg.WebAuthnRPOrigins) != 2 || cfg.WebAuthnRPOrigins[0] != "http://localhost:8080" || cfg.WebAuthnRPOrigins[1] != "https://app.example.test" {
		t.Fatalf("WebAuthnRPOrigins = %#v", cfg.WebAuthnRPOrigins)
	}
}

func TestLoadWebAuthnOptional(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	t.Setenv(envWebAuthnRPDisplayName, "")
	t.Setenv(envWebAuthnRPID, "")
	t.Setenv(envWebAuthnRPOrigins, "")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if cfg.WebAuthnRPDisplayName != "" || cfg.WebAuthnRPID != "" || len(cfg.WebAuthnRPOrigins) != 0 {
		t.Fatalf("missing WebAuthn env must stay empty at load: %+v", cfg)
	}
}

func TestLoadRequiresAuthRateLimit(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	t.Setenv(envAuthIPMaxAttempts, "")
	t.Setenv(envAuthIPWindow, "15m")
	t.Setenv(envAuthPasswordUserMaxAttempts, "10")
	t.Setenv(envAuthPasswordUserWindow, "15m")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty IDENTITY_AUTH_IP_MAX_ATTEMPTS")
	}

	t.Setenv(envAuthIPMaxAttempts, "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for zero IDENTITY_AUTH_IP_MAX_ATTEMPTS")
	}

	t.Setenv(envAuthIPMaxAttempts, "20")
	t.Setenv(envAuthIPWindow, "0s")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for zero IDENTITY_AUTH_IP_WINDOW")
	}
}

func TestLoadRequiresOutboxPolicy(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)

	t.Setenv(envOutboxBatchSize, "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for empty OUTBOX_BATCH_SIZE")
	}

	t.Setenv(envOutboxBatchSize, "10")
	t.Setenv(envOutboxRetryMultiplier, "0.5")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for OUTBOX_RETRY_MULTIPLIER < 1")
	}

	t.Setenv(envOutboxRetryMultiplier, "2")
	t.Setenv(envOutboxRetryBase, "10m")
	t.Setenv(envOutboxRetryCap, "1m")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when retry cap < base")
	}

	t.Setenv(envOutboxRetryBase, "1m")
	t.Setenv(envOutboxRetryCap, "10m")
	t.Setenv(envOutboxRetryJitter, "-1s")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for negative OUTBOX_RETRY_JITTER")
	}
}

func TestLoadNotificationChannelModes(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)

	t.Setenv(envNotificationsEmailMode, "external")
	t.Setenv(envEmailProvider, "ses")
	t.Setenv(envEmailSESRegion, "eu-central-1")
	t.Setenv(envEmailSESFrom, "noreply@example.test")
	t.Setenv(envNotificationsSMSMode, "DISABLED")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NotificationsEmailMode != NotificationChannelExternal || cfg.NotificationsSMSMode != NotificationChannelDisabled {
		t.Fatalf("modes = %q / %q", cfg.NotificationsEmailMode, cfg.NotificationsSMSMode)
	}
	if !cfg.Email.SESWired() || cfg.Email.Provider != EmailProviderSES {
		t.Fatalf("email = %+v", cfg.Email)
	}
	if strings.Contains(cfg.Email.String(), "noreply@example.test") {
		t.Fatal("email stringer must not dump From address")
	}

	t.Setenv(envNotificationsEmailMode, "noop")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for noop email mode")
	}
}

func TestLoadSMSNetgsmRequiresCredentials(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)

	t.Setenv(envNotificationsSMSMode, "external")
	if _, err := Load(); err == nil {
		t.Fatal("external SMS requires SMS_PROVIDER=netgsm")
	}

	t.Setenv(envSMSProvider, "netgsm")
	t.Setenv(envNetgsmUsername, "")
	t.Setenv(envNetgsmPassword, "netgsm-secret-must-not-leak")
	t.Setenv(envNetgsmMsgHeader, "KONUMLUTEST")
	if _, err := Load(); err == nil {
		t.Fatal("netgsm requires username")
	}

	t.Setenv(envNetgsmUsername, "netgsm-user")
	t.Setenv(envNetgsmPassword, "")
	if _, err := Load(); err == nil {
		t.Fatal("netgsm requires password")
	}

	t.Setenv(envNetgsmPassword, "netgsm-secret-must-not-leak")
	t.Setenv(envNetgsmMsgHeader, "")
	if _, err := Load(); err == nil {
		t.Fatal("netgsm requires msgheader")
	}

	t.Setenv(envNetgsmMsgHeader, "KONUMLUTEST")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SMS.NetgsmWired() || cfg.SMS.Provider != SMSProviderNetgsm {
		t.Fatalf("sms = %+v", cfg.SMS)
	}
	if strings.Contains(cfg.String(), "netgsm-secret-must-not-leak") || strings.Contains(cfg.SMS.String(), "netgsm-secret-must-not-leak") {
		t.Fatal("sms stringer must not dump password")
	}
}

func TestLoadStaffIDPPartialFailsClosed(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)
	t.Setenv(envStaffIDPIssuer, "https://idp.example.test")
	t.Setenv(envStaffIDPAudience, "")
	t.Setenv(envStaffIDPJWKSURL, "")
	if _, err := Load(); err == nil {
		t.Fatal("expected misconfigured staff IdP error")
	}
}

func TestLoadStaffIDPComplete(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)
	t.Setenv(envStaffIDPIssuer, "https://idp.example.test")
	t.Setenv(envStaffIDPAudience, "konumlu-staff")
	t.Setenv(envStaffIDPJWKSURL, "https://idp.example.test/jwks")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.StaffIDP.Complete() || cfg.StaffIDP.Issuer != "https://idp.example.test" {
		t.Fatalf("StaffIDP = %+v", cfg.StaffIDP)
	}
}

func TestLoadStaffDevIDPCompleteInDevelopment(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)
	t.Setenv(envStaffDevIDPEnabled, "true")
	t.Setenv(envStaffDevIDPToken, "local-dev-staff-token")
	t.Setenv(envStaffDevIDPStaffID, "11111111-1111-4111-8111-111111111111")
	t.Setenv(envStaffDevIDPRoles, "moderator")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.StaffDevIDP.Complete() || cfg.StaffDevIDP.StaffID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("StaffDevIDP = %+v", cfg.StaffDevIDP)
	}
	if strings.Contains(cfg.String(), "local-dev-staff-token") {
		t.Fatalf("String leaked token: %s", cfg.String())
	}
}

func TestLoadStaffDevIDPPartialFailsClosed(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)
	t.Setenv(envStaffDevIDPEnabled, "true")
	t.Setenv(envStaffDevIDPToken, "")
	t.Setenv(envStaffDevIDPStaffID, "")
	t.Setenv(envStaffDevIDPRoles, "")
	if _, err := Load(); err == nil {
		t.Fatal("expected incomplete STAFF_DEV_IDP error")
	}
}

func TestLoadStaffDevIDPRejectedInProduction(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv(envStaffDevIDPEnabled, "true")
	t.Setenv(envStaffDevIDPToken, "local-dev-staff-token")
	t.Setenv(envStaffDevIDPStaffID, "11111111-1111-4111-8111-111111111111")
	t.Setenv(envStaffDevIDPRoles, "admin")
	if _, err := Load(); err == nil {
		t.Fatal("expected production STAFF_DEV_IDP rejection")
	}
}

func TestLoadStaffDevIDPCannotCombineWithStaffIDP(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)
	t.Setenv(envStaffIDPIssuer, "https://idp.example.test")
	t.Setenv(envStaffIDPAudience, "konumlu-staff")
	t.Setenv(envStaffIDPJWKSURL, "https://idp.example.test/jwks")
	t.Setenv(envStaffDevIDPEnabled, "true")
	t.Setenv(envStaffDevIDPToken, "local-dev-staff-token")
	t.Setenv(envStaffDevIDPStaffID, "11111111-1111-4111-8111-111111111111")
	t.Setenv(envStaffDevIDPRoles, "moderator")
	if _, err := Load(); err == nil {
		t.Fatal("expected combined staff IdP error")
	}
}

func TestLoadEmailSESRequiresNonSecretSettings(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)

	t.Setenv(envNotificationsEmailMode, "external")
	if _, err := Load(); err == nil {
		t.Fatal("external email requires EMAIL_PROVIDER=ses")
	}

	t.Setenv(envEmailProvider, "ses")
	t.Setenv(envEmailSESRegion, "")
	t.Setenv(envEmailSESFrom, "noreply@example.test")
	if _, err := Load(); err == nil {
		t.Fatal("ses requires region")
	}

	t.Setenv(envEmailSESRegion, "eu-central-1")
	t.Setenv(envEmailSESFrom, "")
	if _, err := Load(); err == nil {
		t.Fatal("ses requires from")
	}

	setProductionRequiredEnv(t)
	t.Setenv(envNotificationsEmailMode, "external")
	t.Setenv(envEmailProvider, "ses")
	t.Setenv(envEmailSESRegion, "")
	t.Setenv(envEmailSESFrom, "noreply@example.test")
	if _, err := Load(); err == nil {
		t.Fatal("production ses requires region")
	}
}

func TestLoadPushEndpointKeys(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)
	setPushEndpointKeyEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PushEndpoints.Enabled || len(cfg.PushEndpoints.EncryptionKey) != 32 || len(cfg.PushEndpoints.HashKey) != 32 {
		t.Fatalf("push endpoints = %+v", cfg.PushEndpoints.String())
	}
	if strings.Contains(cfg.String(), "ERE") || strings.Contains(cfg.GoString(), "IiI") {
		t.Fatalf("string leaked keys: %s", cfg.String())
	}
	t.Setenv(envPushEndpointHashKey, "")
	if _, err := Load(); err == nil {
		t.Fatal("partial push keys")
	}
}

func TestProductionRequiresPushEndpointKeys(t *testing.T) {
	setProductionRequiredEnv(t)
	t.Setenv(envPushEndpointEncryptionKey, "")
	t.Setenv(envPushEndpointHashKey, "")
	if _, err := Load(); err == nil {
		t.Fatal("production requires push endpoint keys")
	}
}

func TestLoadPushTransportsOptionalAndSecrets(t *testing.T) {
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envStepUpTTL, "5m")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
	setMaterialKeyEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebPush.Wired() || cfg.FCM.Wired() || cfg.APNs.Wired() {
		t.Fatal("push transports must stay optional")
	}

	t.Setenv(envWebPushVAPIDPublicKey, "vapid-public-example")
	if _, err := Load(); err == nil {
		t.Fatal("partial VAPID")
	}
	t.Setenv(envWebPushVAPIDPrivateKey, "vapid-private-must-not-leak")
	t.Setenv(envWebPushVAPIDSubject, "mailto:ops@example.test")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.WebPush.Wired() {
		t.Fatal("webpush")
	}
	if strings.Contains(cfg.String(), "vapid-private-must-not-leak") || strings.Contains(cfg.WebPush.String(), "vapid-private-must-not-leak") {
		t.Fatal("vapid private leaked")
	}

	t.Setenv(envFCMCredentialsFile, "/tmp/fcm.json")
	if _, err := Load(); err == nil {
		t.Fatal("FCM file without project")
	}
	t.Setenv(envFCMProjectID, "konumlu-demo")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.FCM.Wired() {
		t.Fatal("fcm")
	}

	t.Setenv(envAPNSTeamID, "TEAMID01")
	if _, err := Load(); err == nil {
		t.Fatal("partial APNs")
	}
	t.Setenv(envAPNSKeyID, "KEYID001")
	t.Setenv(envAPNSTopic, "tr.konumlu.app")
	t.Setenv(envAPNSPrivateKey, "-----BEGIN PRIVATE KEY-----\nsecret-apns-key\n-----END PRIVATE KEY-----")
	t.Setenv(envAPNSEnvironment, "sandbox")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.APNs.Wired() || cfg.APNs.Environment != "sandbox" {
		t.Fatalf("apns=%s", cfg.APNs.String())
	}
	if strings.Contains(cfg.String(), "secret-apns-key") || strings.Contains(cfg.APNs.String(), "secret-apns-key") {
		t.Fatal("apns key leaked")
	}
}
