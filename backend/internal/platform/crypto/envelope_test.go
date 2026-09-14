package crypto

import (
	"bytes"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	k := bytes.Repeat([]byte{0x11}, AES256KeySize)
	return k
}

func TestSealOpenRoundTrip(t *testing.T) {
	kr, err := NewSingleKey(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := NewAEAD(kr)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"version":1,"endpoint":"https://push.example/x"}`)
	aad := []byte("konumlu.notifications.push_endpoint.v1")
	env, err := aead.Seal(plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(env.Ciphertext, plain) {
		t.Fatal("plaintext in ciphertext")
	}
	got, err := aead.Open(env, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q", got)
	}
}

func TestWrongKeyAndMalformedFailClosed(t *testing.T) {
	kr, err := NewSingleKey(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := NewAEAD(kr)
	if err != nil {
		t.Fatal(err)
	}
	env, err := aead.Seal([]byte("secret-token"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := NewAEAD(mustKeyring(t, bytes.Repeat([]byte{0x22}, AES256KeySize)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Open(env, []byte("aad")); err == nil {
		t.Fatal("wrong key opened")
	}
	bad := env
	bad.Ciphertext = append([]byte{0}, env.Ciphertext...)
	if _, err := aead.Open(bad, []byte("aad")); err == nil {
		t.Fatal("malformed opened")
	}
	if _, err := aead.Open(env, []byte("other-aad")); err == nil {
		t.Fatal("wrong aad opened")
	}
}

func TestEnvelopeStringersOmitMaterial(t *testing.T) {
	env := Envelope{KeyID: "v1", Nonce: []byte("n"), Ciphertext: []byte("ciphertext-must-not-print")}
	if strings.Contains(env.String(), "ciphertext") || strings.Contains(env.GoString(), "ciphertext-must-not-print") {
		t.Fatalf("leaked: %s %s", env.String(), env.GoString())
	}
	kr, err := NewSingleKey(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(kr.String(), string(testKey(t))) {
		t.Fatal("keyring leaked")
	}
}

func TestNewSingleKeyStableV1NoRotationRing(t *testing.T) {
	kr, err := NewSingleKey(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if kr.ActiveID() != "v1" {
		t.Fatalf("active id = %q, want v1", kr.ActiveID())
	}
	if _, ok := kr.keys["v1"]; !ok || len(kr.keys) != 1 {
		t.Fatalf("v1 push keyring must hold exactly one key, n=%d", len(kr.keys))
	}
}

func TestHMACKeyed(t *testing.T) {
	a, err := NewHMACKey(bytes.Repeat([]byte{0x33}, 32))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewHMACKey(bytes.Repeat([]byte{0x44}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ha, err := a.Sum([]byte("same-endpoint"))
	if err != nil {
		t.Fatal(err)
	}
	hb, err := b.Sum([]byte("same-endpoint"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ha, hb) {
		t.Fatal("hmac not keyed")
	}
	if _, err := NewHMACKey([]byte("short")); err == nil {
		t.Fatal("short hmac key")
	}
}

func mustKeyring(t *testing.T, key []byte) *Keyring {
	t.Helper()
	kr, err := NewSingleKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}
