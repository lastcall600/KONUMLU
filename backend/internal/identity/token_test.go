package identity

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateSessionToken(t *testing.T) {
	raw, hash, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken: %v", err)
	}
	if strings.Contains(raw, "=") {
		t.Fatal("raw token must be unpadded")
	}
	if strings.ContainsAny(raw, "+/") {
		t.Fatal("raw token must be URL-safe")
	}
	secret, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(secret) < MinSessionSecretBytes {
		t.Fatalf("entropy bytes = %d, want >= %d", len(secret), MinSessionSecretBytes)
	}
	if len(hash) != TokenHashSize {
		t.Fatalf("hash len = %d, want %d", len(hash), TokenHashSize)
	}
	want, err := HashSessionSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash, want) {
		t.Fatal("hash must be SHA-256 of raw secret bytes")
	}
	if bytes.Equal(hash, secret) {
		t.Fatal("hash must not equal raw secret")
	}

	raw2, hash2, err := GenerateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw == raw2 || bytes.Equal(hash, hash2) {
		t.Fatal("tokens must be unique")
	}
}

func TestGenerateCeremonyTokenMatchesSessionEntropy(t *testing.T) {
	raw, hash, err := GenerateCeremonyToken()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := decodeCeremonyToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) < MinSessionSecretBytes {
		t.Fatalf("entropy bytes = %d, want >= %d", len(secret), MinSessionSecretBytes)
	}
	if len(hash) != TokenHashSize {
		t.Fatalf("hash len = %d, want %d", len(hash), TokenHashSize)
	}
}

func TestDecodeSessionTokenRejectsMalformed(t *testing.T) {
	if _, err := decodeSessionToken(""); err != errUnauthenticated {
		t.Fatalf("empty: %v", err)
	}
	if _, err := decodeSessionToken("@@@"); err != errUnauthenticated {
		t.Fatalf("invalid encoding: %v", err)
	}
	padded := base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{1}, MinSessionSecretBytes))
	if _, err := decodeSessionToken(padded); err != errUnauthenticated {
		t.Fatalf("padded: %v", err)
	}
	short := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, MinSessionSecretBytes-1))
	if _, err := decodeSessionToken(short); err != errUnauthenticated {
		t.Fatalf("short: %v", err)
	}
}
