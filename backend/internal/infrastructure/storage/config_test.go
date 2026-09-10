package storage

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoadDisabledByDefault(t *testing.T) {
	t.Setenv(envEnabled, "")
	t.Setenv(envSecretKey, "must-not-be-required")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("storage must default to disabled")
	}
	if cfg.SecretKey != "" {
		t.Fatal("disabled load must not keep secrets")
	}
}

func TestLoadEnabledRequiresFields(t *testing.T) {
	t.Setenv(envEnabled, "true")
	t.Setenv(envRegion, "")
	t.Setenv(envBucket, "")
	t.Setenv(envAccessKey, "")
	t.Setenv(envSecretKey, "")
	t.Setenv(envUploadTTL, "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("secret leaked: %v", err)
	}
}

func TestLoadEnabledSuccessMinIO(t *testing.T) {
	setEnabledEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.Bucket != "konumlu-media" || !cfg.UsePathStyle {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Endpoint != "http://127.0.0.1:9000" {
		t.Fatalf("endpoint = %q", cfg.Endpoint)
	}
	if cfg.UploadTTL != 15*time.Minute || cfg.MaxUploadBytes != defaultMaxUploadBytes {
		t.Fatalf("ttl/max = %s %d", cfg.UploadTTL, cfg.MaxUploadBytes)
	}
	if cfg.GetTTL != defaultGetTTL || cfg.PublicBaseURL != "" {
		t.Fatalf("get ttl/base = %s %q", cfg.GetTTL, cfg.PublicBaseURL)
	}
}

func TestLoadEnabledMalformedTTL(t *testing.T) {
	setEnabledEnv(t)
	t.Setenv(envUploadTTL, "soon")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), envUploadTTL) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadEnabledPathStyleOverride(t *testing.T) {
	setEnabledEnv(t)
	t.Setenv(envPathStyle, "false")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UsePathStyle {
		t.Fatal("path-style override ignored")
	}
}

func TestLoadEnabledRejectsSecretInError(t *testing.T) {
	secret := "super-secret-value-xyz"
	t.Setenv(envEnabled, "true")
	t.Setenv(envRegion, "us-east-1")
	t.Setenv(envBucket, "konumlu-media")
	t.Setenv(envAccessKey, "local-access")
	t.Setenv(envSecretKey, secret)
	t.Setenv(envUploadTTL, "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("secret leaked: %v", err)
	}
}

func TestValidateRejectsBucketTraversal(t *testing.T) {
	cfg := validCfg()
	cfg.Bucket = "../etc"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected bucket error")
	}
}

func TestLoadMalformedEnabledFlag(t *testing.T) {
	t.Setenv(envEnabled, "maybe")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
}

func setEnabledEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envEnabled, "true")
	t.Setenv(envEndpoint, "http://127.0.0.1:9000")
	t.Setenv(envRegion, "us-east-1")
	t.Setenv(envBucket, "konumlu-media")
	t.Setenv(envAccessKey, "local-access")
	t.Setenv(envSecretKey, "local-secret")
	t.Setenv(envUploadTTL, "15m")
	t.Setenv(envMaxUploadBytes, "")
	t.Setenv(envPathStyle, "")
}

func validCfg() Config {
	return Config{
		Enabled:        true,
		Endpoint:       "http://127.0.0.1:9000",
		Region:         "us-east-1",
		Bucket:         "konumlu-media",
		AccessKey:      "local-access",
		SecretKey:      "local-secret",
		UsePathStyle:   true,
		UploadTTL:      15 * time.Minute,
		GetTTL:         defaultGetTTL,
		MaxUploadBytes: defaultMaxUploadBytes,
	}
}

func TestLoadPublicBaseURLAndGetTTL(t *testing.T) {
	setEnabledEnv(t)
	t.Setenv(envGetTTL, "2m")
	t.Setenv(envPublicBaseURL, "https://media.example.test/public")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GetTTL != 2*time.Minute {
		t.Fatalf("get ttl = %s", cfg.GetTTL)
	}
	if cfg.PublicBaseURL != "https://media.example.test/public" {
		t.Fatalf("base = %q", cfg.PublicBaseURL)
	}
}

func TestLoadWorkloadIdentityForbidsStaticKeys(t *testing.T) {
	setEnabledEnv(t)
	t.Setenv(envCredentialSource, CredentialWorkload)
	t.Setenv(envAccessKey, "access")
	t.Setenv(envSecretKey, "secret")
	if _, err := Load(); err == nil {
		t.Fatal("expected workload+keys error")
	}
}

func TestLoadWorkloadIdentityWithoutKeys(t *testing.T) {
	setEnabledEnv(t)
	t.Setenv(envCredentialSource, CredentialWorkload)
	t.Setenv(envAccessKey, "")
	t.Setenv(envSecretKey, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CredentialSource != CredentialWorkload {
		t.Fatalf("source = %q", cfg.CredentialSource)
	}
	if _, err := New(cfg); !errors.Is(err, errWorkloadIdentityUnwired) {
		t.Fatalf("new = %v", err)
	}
	if strings.Contains(cfg.String(), "local-secret") || strings.Contains(cfg.String(), "local-access") {
		t.Fatalf("String leaked: %s", cfg.String())
	}
}

func TestLoadRejectsMalformedPublicBaseURL(t *testing.T) {
	setEnabledEnv(t)
	t.Setenv(envPublicBaseURL, "ftp://cdn.example.test")
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewDisabledFails(t *testing.T) {
	_, err := New(Config{Enabled: false})
	if !errors.Is(err, errStorageDisabled) {
		t.Fatalf("err = %v", err)
	}
}
