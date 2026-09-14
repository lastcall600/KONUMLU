package notifications

import (
	"bytes"
	"testing"

	"backend/internal/notifications/policy"
	"backend/internal/platform/crypto"
)

func TestWebPushIdentityHashIgnoresCredentialBundle(t *testing.T) {
	hmacKey := mustHMAC(t, bytes.Repeat([]byte{0x22}, 32))
	base := webReg("https://push.example.test/subscription/stable-endpoint")
	refreshed := base
	refreshed.Web = &WebPushMaterial{
		Endpoint: base.Web.Endpoint,
		P256dh:   "CNcRdreALRFGKhHy8pL7jHwGNBXne2j5ROqE5m8xN8wG1k2o3p4q5r6s7t8u9v0w2",
		Auth:     "uBHItJI5svbpez7KI4CCXh",
	}
	otherURL := webReg("https://push.example.test/subscription/other-endpoint")

	h1, err := hashPushMaterial(hmacKey, base)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := hashPushMaterial(hmacKey, refreshed)
	if err != nil {
		t.Fatal(err)
	}
	h3, err := hashPushMaterial(hmacKey, otherURL)
	if err != nil {
		t.Fatal(err)
	}
	if !hashesEqual(h1, h2) {
		t.Fatal("web identity must stay stable when p256dh/auth refresh")
	}
	if hashesEqual(h1, h3) {
		t.Fatal("different web endpoints must not collide")
	}
	raw, err := canonicalPushHashInput(base)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(base.Web.P256dh)) || bytes.Contains(raw, []byte(base.Web.Auth)) {
		t.Fatal("credential bundle must not enter identity hash")
	}
	if !bytes.Contains(raw, []byte(base.Web.Endpoint)) {
		t.Fatal("web identity must include endpoint URL")
	}
}

func TestMobilePushIdentityHashUsesOpaqueToken(t *testing.T) {
	hmacKey := mustHMAC(t, bytes.Repeat([]byte{0x22}, 32))
	a := androidReg("fcm-token-aaaaaaaaaaaaaaaa")
	same := androidReg("fcm-token-aaaaaaaaaaaaaaaa")
	b := androidReg("fcm-token-bbbbbbbbbbbbbbbb")
	ios := PushRegistration{
		Channel:  policy.ChannelMobilePush,
		Platform: PushPlatformIOS,
		Provider: PushProviderAPNs,
		Mobile:   &MobilePushMaterial{Token: "fcm-token-aaaaaaaaaaaaaaaa"},
	}

	ha, err := hashPushMaterial(hmacKey, a)
	if err != nil {
		t.Fatal(err)
	}
	hs, err := hashPushMaterial(hmacKey, same)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := hashPushMaterial(hmacKey, b)
	if err != nil {
		t.Fatal(err)
	}
	hios, err := hashPushMaterial(hmacKey, ios)
	if err != nil {
		t.Fatal(err)
	}
	if !hashesEqual(ha, hs) {
		t.Fatal("same mobile token must hash equal")
	}
	if hashesEqual(ha, hb) {
		t.Fatal("different mobile tokens must not collide")
	}
	if hashesEqual(ha, hios) {
		t.Fatal("platform/provider must remain in the HMAC input")
	}
}

func TestPushIdentityHashStaysKeyed(t *testing.T) {
	a := mustHMAC(t, bytes.Repeat([]byte{0x22}, 32))
	b := mustHMAC(t, bytes.Repeat([]byte{0x33}, 32))
	in := webReg("https://push.example.test/subscription/hmac-proof")
	ha, err := hashPushMaterial(a, in)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := hashPushMaterial(b, in)
	if err != nil {
		t.Fatal(err)
	}
	if hashesEqual(ha, hb) {
		t.Fatal("identity hash must remain HMAC-keyed")
	}
	if len(ha) != 32 {
		t.Fatalf("sha256 mac len=%d", len(ha))
	}
}

func TestPushEncryptionKeyIDIsStableV1(t *testing.T) {
	aead, _ := testPushKeys(t)
	env, err := aead.Seal([]byte(`{"version":1,"kind":"webpush"}`), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	if env.KeyID != "v1" {
		t.Fatalf("endpoint_key_id = %q, want v1 (single active key, no rotation ring)", env.KeyID)
	}
}

func mustHMAC(t *testing.T, key []byte) crypto.HMACKey {
	t.Helper()
	k, err := crypto.NewHMACKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
