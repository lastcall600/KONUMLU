package identity

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"backend/internal/platform/config"
)

func setIdentityConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://konumlu:konumlu@127.0.0.1:5432/konumlu?sslmode=disable")
	t.Setenv("IDENTITY_SESSION_IDLE", "1h")
	t.Setenv("IDENTITY_SESSION_ABSOLUTE", "24h")
	t.Setenv("IDENTITY_WEBAUTHN_CEREMONY_TTL", "2m")
	t.Setenv("IDENTITY_AUTH_IP_MAX_ATTEMPTS", "20")
	t.Setenv("IDENTITY_AUTH_IP_WINDOW", "15m")
	t.Setenv("IDENTITY_AUTH_PASSWORD_USER_MAX_ATTEMPTS", "10")
	t.Setenv("IDENTITY_AUTH_PASSWORD_USER_WINDOW", "15m")
	t.Setenv("IDENTITY_VERIFICATION_CHALLENGE_TTL", "10m")
	t.Setenv("IDENTITY_VERIFICATION_CHALLENGE_MAX_ATTEMPTS", "5")
	t.Setenv("IDENTITY_VERIFICATION_PHONE_OTP_DIGITS", "6")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_DEST_MAX", "5")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_DEST_WINDOW", "1h")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_IP_MAX", "10")
	t.Setenv("IDENTITY_VERIFICATION_ISSUE_IP_WINDOW", "1h")
	t.Setenv("IDENTITY_SIGNUP_PROOF_TTL", "15m")
	t.Setenv("OUTBOX_BATCH_SIZE", "10")
	t.Setenv("OUTBOX_LEASE", "30s")
	t.Setenv("OUTBOX_POLL_INTERVAL", "1s")
	t.Setenv("OUTBOX_RETRY_BASE", "1m")
	t.Setenv("OUTBOX_RETRY_MULTIPLIER", "2")
	t.Setenv("OUTBOX_RETRY_CAP", "10m")
	t.Setenv("OUTBOX_RETRY_JITTER", "0s")
}

func TestProcessKeyringConfigValid(t *testing.T) {
	setIdentityConfigEnv(t)
	v1 := bytes.Repeat([]byte{0x11}, aes256KeySize)
	v2 := bytes.Repeat([]byte{0x22}, aes256KeySize)
	t.Setenv("IDENTITY_VERIFICATION_MATERIAL_ACTIVE_KEY_ID", "k2")
	t.Setenv("IDENTITY_VERIFICATION_MATERIAL_KEYS", "k1:"+base64.StdEncoding.EncodeToString(v1)+",k2:"+base64.StdEncoding.EncodeToString(v2))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewMaterialProtectorFromDecodedKeys(cfg.MaterialKeys.ActiveID, cfg.MaterialKeys.Keys)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("protector required")
	}
}

func TestProcessKeyringConfigRejectedWithoutLeaking(t *testing.T) {
	setIdentityConfigEnv(t)
	cases := []struct {
		name   string
		active string
		keys   string
		leak   string
	}{
		{name: "malformed base64", active: "k1", keys: "k1:not-valid-base64-leak-token", leak: "not-valid-base64-leak-token"},
		{name: "wrong length", active: "k1", keys: "k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x01}, 16)), leak: ""},
		{name: "missing active", active: "", keys: "k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, aes256KeySize)), leak: ""},
		{name: "unknown active", active: "missing", keys: "k1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, aes256KeySize)), leak: ""},
		{name: "missing config", active: "", keys: "", leak: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("IDENTITY_VERIFICATION_MATERIAL_ACTIVE_KEY_ID", tc.active)
			t.Setenv("IDENTITY_VERIFICATION_MATERIAL_KEYS", tc.keys)
			_, err := config.Load()
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.leak != "" && strings.Contains(err.Error(), tc.leak) {
				t.Fatalf("error leaked secret: %v", err)
			}
			if strings.Contains(err.Error(), string(bytes.Repeat([]byte{0x11}, aes256KeySize))) {
				t.Fatalf("error leaked key bytes: %v", err)
			}
		})
	}
}
