package config

import (
	"strings"
	"testing"
)

func TestGermanyLoadRejectsTRPrivateKey(t *testing.T) {
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
	t.Setenv(envTRSigningPrivateKey, "super-secret-tr-private-key-material")
	_, err := Load()
	if err == nil {
		t.Fatal("expected private key rejected")
	}
	if strings.Contains(err.Error(), "super-secret-tr-private-key-material") {
		t.Fatalf("error leaked key: %v", err)
	}
}

func TestTRIngressEnabledRequiresTokenAndKeys(t *testing.T) {
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
	t.Setenv(envTRIngressEnabled, "true")
	if _, err := Load(); err == nil {
		t.Fatal("expected missing token/keys")
	}
}

func TestTRSignerStringHidesPrivateKey(t *testing.T) {
	s := TRSigner{Enabled: true, KeyID: "tr-v1", PrivateKeyRaw: "secret-seed-must-not-leak-value", IngressToken: "ingress-token-secret"}
	if strings.Contains(s.String(), "secret-seed-must-not-leak-value") || strings.Contains(s.GoString(), "secret-seed-must-not-leak-value") {
		t.Fatalf("private key leaked: %s", s.String())
	}
	if strings.Contains(s.String(), "ingress-token-secret") {
		t.Fatalf("token leaked: %s", s.String())
	}
}
