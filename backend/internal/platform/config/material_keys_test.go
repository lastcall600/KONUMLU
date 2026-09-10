package config

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func setRequiredLoadEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envDatabaseURL, "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv(envSessionIdle, "1h")
	t.Setenv(envSessionAbsolute, "24h")
	t.Setenv(envWebAuthnCeremonyTTL, "2m")
	setAuthRateLimitEnv(t)
	setVerificationSignupEnv(t)
	setOutboxEnv(t)
}

func TestLoadValidMaterialKeyring(t *testing.T) {
	setRequiredLoadEnv(t)
	v1 := bytes.Repeat([]byte{0x11}, materialAESKeySize)
	v2 := bytes.Repeat([]byte{0x22}, materialAESKeySize)
	t.Setenv(envMaterialActiveKeyID, "k2")
	t.Setenv(envMaterialKeys, "k1:"+base64.StdEncoding.EncodeToString(v1)+",k2:"+base64.StdEncoding.EncodeToString(v2))

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaterialKeys.ActiveID != "k2" {
		t.Fatalf("active = %q", cfg.MaterialKeys.ActiveID)
	}
	if !bytes.Equal(cfg.MaterialKeys.Keys["k1"], v1) || !bytes.Equal(cfg.MaterialKeys.Keys["k2"], v2) {
		t.Fatal("decoded keys mismatch")
	}
}

func TestLoadRejectsMalformedMaterialKeyBase64(t *testing.T) {
	setRequiredLoadEnv(t)
	secret := "not-valid-base64-leak-token"
	t.Setenv(envMaterialActiveKeyID, "k1")
	t.Setenv(envMaterialKeys, "k1:"+secret)
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked key material: %v", err)
	}
}

func TestLoadRejectsWrongMaterialKeyLength(t *testing.T) {
	setRequiredLoadEnv(t)
	short := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x01}, 16))
	t.Setenv(envMaterialActiveKeyID, "k1")
	t.Setenv(envMaterialKeys, "k1:"+short)
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), short) {
		t.Fatalf("error leaked encoding: %v", err)
	}
}

func TestLoadRejectsMissingActiveMaterialKey(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv(envMaterialActiveKeyID, "")
	t.Setenv(envMaterialKeys, "k1:"+base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, materialAESKeySize)))
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRejectsUnknownActiveMaterialKey(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv(envMaterialActiveKeyID, "missing")
	t.Setenv(envMaterialKeys, "k1:"+base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, materialAESKeySize)))
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRejectsDuplicateMaterialKeyIDs(t *testing.T) {
	setRequiredLoadEnv(t)
	enc := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, materialAESKeySize))
	t.Setenv(envMaterialActiveKeyID, "k1")
	t.Setenv(envMaterialKeys, "k1:"+enc+",k1:"+enc)
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRequiresMaterialKeyConfig(t *testing.T) {
	setRequiredLoadEnv(t)
	t.Setenv(envMaterialActiveKeyID, "")
	t.Setenv(envMaterialKeys, "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestMaterialKeysStringHidesSecrets(t *testing.T) {
	secret := bytes.Repeat([]byte{0x99}, materialAESKeySize)
	m := MaterialKeys{ActiveID: "k1", Keys: map[string][]byte{"k1": secret}}
	if strings.Contains(m.String(), string(secret)) || strings.Contains(m.GoString(), string(secret)) {
		t.Fatal("String leaked key material")
	}
}
