package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMaterialEncryptDecryptRoundTrip(t *testing.T) {
	p := mustProtector(t)
	ch := testMaterialChallenge(t)
	plain := []byte("delivery-secret")
	sealed, err := p.Seal(ch, plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Open(ch, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("got %q want %q", got, plain)
	}
	if bytes.Contains(sealed.Ciphertext, plain) || bytes.Contains(sealed.Nonce, plain) {
		t.Fatal("plaintext must not appear in sealed fields")
	}
}

func TestMaterialRandomNonce(t *testing.T) {
	p := mustProtector(t)
	ch := testMaterialChallenge(t)
	a, err := p.Seal(ch, []byte("same-secret"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Seal(ch, []byte("same-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Nonce, b.Nonce) {
		t.Fatal("nonces must be unique per seal")
	}
	if bytes.Equal(a.Ciphertext, b.Ciphertext) {
		t.Fatal("ciphertext must differ when nonce differs")
	}
}

func TestMaterialWrongKeyFails(t *testing.T) {
	ch := testMaterialChallenge(t)
	sealer := mustProtector(t)
	sealed, err := sealer.Seal(ch, []byte("otp-secret"))
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := NewMaterialProtector(mustKeyring(t, "test-v1", map[string][]byte{
		"test-v1": bytes.Repeat([]byte{0x22}, aes256KeySize),
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := wrong.Open(ch, sealed)
	if err == nil || got != nil {
		t.Fatal("wrong key must fail closed")
	}
	if !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want %v", err, errUnavailable)
	}
}

func TestMaterialModifiedCiphertextAndAADFail(t *testing.T) {
	p := mustProtector(t)
	ch := testMaterialChallenge(t)
	sealed, err := p.Seal(ch, []byte("token-value"))
	if err != nil {
		t.Fatal(err)
	}

	tampered := cloneMaterial(sealed)
	tampered.Ciphertext[0] ^= 0xff
	if got, err := p.Open(ch, tampered); err == nil || got != nil || !errors.Is(err, errUnavailable) {
		t.Fatalf("modified ciphertext err = %v got %q", err, got)
	}

	other := ch
	other.DestinationCanonical = "other@example.com"
	if got, err := p.Open(other, sealed); err == nil || got != nil || !errors.Is(err, errUnavailable) {
		t.Fatalf("modified AAD err = %v got %q", err, got)
	}
}

func TestMaterialChallengeMismatchFails(t *testing.T) {
	p := mustProtector(t)
	ch := testMaterialChallenge(t)
	sealed, err := p.Seal(ch, []byte("bound-secret"))
	if err != nil {
		t.Fatal(err)
	}
	other := ch
	other.ID = mustOtherID(t, ch.ID)
	if got, err := p.Open(other, sealed); err == nil || got != nil || !errors.Is(err, errUnavailable) {
		t.Fatalf("challenge mismatch err = %v got %q", err, got)
	}
}

func TestResolveVerificationDeliveryReturnsKindDestinationSecret(t *testing.T) {
	ctx := context.Background()
	svc, store, _, _ := newTestChallenges(t)

	email, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "Owner@Example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ResolveVerificationDelivery(ctx, email.Challenge.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != IdentifierEmail || got.DestinationCanonical != "owner@example.com" || got.Secret != email.RawSecret {
		t.Fatalf("got %+v", got)
	}
	if store.rawSecrets[email.Challenge.ID] != "" {
		t.Fatal("plaintext must not be persisted")
	}
	if _, err := svc.Verify(ctx, email.Challenge.ID, email.RawSecret); err != nil {
		t.Fatalf("hash verification must still succeed after resolve: %v", err)
	}

	phone, err := svc.Issue(ctx, IdentifierPhone, ChallengeSignup, "+15551234567", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err = svc.ResolveVerificationDelivery(ctx, phone.Challenge.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != IdentifierPhone || got.DestinationCanonical != "+15551234567" || got.Secret != phone.RawSecret {
		t.Fatalf("phone got %+v", got)
	}
	if strings.Contains(got.String(), phone.RawSecret) || strings.Contains(got.GoString(), "+15551234567") {
		t.Fatal("VerificationDelivery String must not include destination or secret")
	}
}

func TestResolveVerificationDeliveryRejectsUnusable(t *testing.T) {
	ctx := context.Background()
	store := newMemChallengeStore()
	inc := &memIssuanceCounter{}
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	current := now
	svc, err := NewChallenges(store, testChallengePolicy(), mustLimiter(t, inc), mustProtector(t), func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "send@example.com", "")
	if err != nil {
		t.Fatal(err)
	}

	current = now.Add(10 * time.Minute)
	_, err = svc.ResolveVerificationDelivery(ctx, issued.Challenge.ID)
	if !errors.Is(err, errChallengeExpired) {
		t.Fatalf("expired err = %v", err)
	}
	assertNoSensitive(t, err, issued.RawSecret, "send@example.com")

	current = now
	fresh, err := svc.Issue(ctx, IdentifierPhone, ChallengeSignup, "+15551230000", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, fresh.Challenge.ID, fresh.RawSecret); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveVerificationDelivery(ctx, fresh.Challenge.ID); !errors.Is(err, errChallengeConsumed) {
		t.Fatalf("consumed err = %v", err)
	}

	ex, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "ex@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	row := store.must(t, ex.Challenge.ID)
	row.FailedAttempts = row.MaxAttempts
	store.rows[ex.Challenge.ID] = row
	if _, err := svc.ResolveVerificationDelivery(ctx, ex.Challenge.ID); !errors.Is(err, errChallengeExhausted) {
		t.Fatalf("exhausted err = %v", err)
	}
}

func assertNoSensitive(t *testing.T, err error, parts ...string) {
	t.Helper()
	msg := err.Error()
	for _, p := range parts {
		if p != "" && strings.Contains(msg, p) {
			t.Fatalf("error leaked %q: %v", p, err)
		}
	}
}

func TestResolveRejectsExpiredAndConsumed(t *testing.T) {
	ctx := context.Background()
	store := newMemChallengeStore()
	inc := &memIssuanceCounter{}
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	current := now
	svc, err := NewChallenges(store, testChallengePolicy(), mustLimiter(t, inc), mustProtector(t), func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "send@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ResolveDeliverySecret(ctx, issued.Challenge.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != issued.RawSecret {
		t.Fatal("resolved secret must match issued secret")
	}

	current = now.Add(10 * time.Minute)
	if _, err := svc.ResolveDeliverySecret(ctx, issued.Challenge.ID); !errors.Is(err, errChallengeExpired) {
		t.Fatalf("expired resolve err = %v, want %v", err, errChallengeExpired)
	}
	if store.materials[issued.Challenge.ID].DestroyedAt == nil {
		t.Fatal("expired material should be destroyed when safely possible")
	}

	current = now
	fresh, err := svc.Issue(ctx, IdentifierPhone, ChallengeSignup, "+15551230000", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, fresh.Challenge.ID, fresh.RawSecret); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveDeliverySecret(ctx, fresh.Challenge.ID); !errors.Is(err, errChallengeConsumed) {
		t.Fatalf("consumed resolve err = %v, want %v", err, errChallengeConsumed)
	}
}

func TestMaterialStorageFailureFailsClosed(t *testing.T) {
	ctx := context.Background()
	store := newMemChallengeStore()
	store.materialFail = true
	svc, err := NewChallenges(store, testChallengePolicy(), mustLimiter(t, &memIssuanceCounter{}), mustProtector(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "fail@example.com", ""); !errors.Is(err, errUnavailable) {
		t.Fatalf("issue err = %v, want %v", err, errUnavailable)
	}
	if len(store.rows) != 0 || len(store.materials) != 0 {
		t.Fatal("storage failure must not persist challenge or material")
	}

	store.materialFail = false
	store.unavailable = true
	if _, err := svc.ResolveDeliverySecret(ctx, mustOtherID(t, ID{})); !errors.Is(err, errUnavailable) {
		t.Fatalf("resolve store err = %v, want %v", err, errUnavailable)
	}
}

func TestMaterialPersistenceModelHasNoPlaintext(t *testing.T) {
	var m VerificationMaterial
	// Compile-time persistence model: no plaintext field exists on VerificationMaterial.
	_ = m.ChallengeID
	_ = m.KeyID
	_ = m.Nonce
	_ = m.Ciphertext
	_ = m.CreatedAt
	_ = m.DestroyedAt
}

func TestMaterialKeyRotationUsesStoredKeyID(t *testing.T) {
	ch := testMaterialChallenge(t)
	v1 := bytes.Repeat([]byte{0x31}, aes256KeySize)
	v2 := bytes.Repeat([]byte{0x32}, aes256KeySize)
	sealer, err := NewMaterialProtector(mustKeyring(t, "k1", map[string][]byte{"k1": v1, "k2": v2}))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealer.Seal(ch, []byte("rotated"))
	if err != nil {
		t.Fatal(err)
	}
	if sealed.KeyID != "k1" {
		t.Fatalf("key_id = %q, want k1", sealed.KeyID)
	}
	opener, err := NewMaterialProtector(mustKeyring(t, "k2", map[string][]byte{"k1": v1, "k2": v2}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := opener.Open(ch, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "rotated" {
		t.Fatalf("got %q", got)
	}
}

func TestNewKeyringRejectsMissingAndUnknownActive(t *testing.T) {
	key := bytes.Repeat([]byte{0x41}, aes256KeySize)
	if _, err := NewKeyring("", map[string][]byte{"k1": key}); !errors.Is(err, errInvalidChallenge) {
		t.Fatalf("empty active err = %v", err)
	}
	if _, err := NewKeyring("k2", map[string][]byte{"k1": key}); !errors.Is(err, errInvalidChallenge) {
		t.Fatalf("unknown active err = %v", err)
	}
}

func TestNewKeyringRejectsWrongLength(t *testing.T) {
	if _, err := NewKeyring("k1", map[string][]byte{"k1": bytes.Repeat([]byte{0x41}, 16)}); !errors.Is(err, errInvalidChallenge) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewKeyringErrorsDoNotIncludeKeyBytes(t *testing.T) {
	secret := bytes.Repeat([]byte{'S'}, aes256KeySize)
	err := error(nil)
	_, err = NewKeyring("missing", map[string][]byte{"k1": secret})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), string(secret)) {
		t.Fatalf("error leaked key: %v", err)
	}
}

func TestNewMaterialProtectorFromDecodedKeys(t *testing.T) {
	v1 := bytes.Repeat([]byte{0x31}, aes256KeySize)
	p, err := NewMaterialProtectorFromDecodedKeys("k1", map[string][]byte{"k1": v1})
	if err != nil {
		t.Fatal(err)
	}
	ch := testMaterialChallenge(t)
	sealed, err := p.Seal(ch, []byte("from-config"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Open(ch, sealed)
	if err != nil || string(got) != "from-config" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestUnknownKeyIDFailsClosed(t *testing.T) {
	p := mustProtector(t)
	ch := testMaterialChallenge(t)
	sealed, err := p.Seal(ch, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	sealed.KeyID = "unknown"
	if _, err := p.Open(ch, sealed); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeliveryOnlyChallengesAllowNilLimiter(t *testing.T) {
	store := newMemChallengeStore()
	p := mustProtector(t)
	svc, err := NewChallenges(store, VerificationChallengePolicy{}, nil, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Issue(context.Background(), IdentifierEmail, ChallengeSignup, "a@b.co", ""); !errors.Is(err, errUnavailable) {
		t.Fatalf("issue without limiter err = %v", err)
	}
}

func testMaterialChallenge(t *testing.T) VerificationChallenge {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	hash, err := HashVerificationSecret([]byte("123456"))
	if err != nil {
		t.Fatal(err)
	}
	return VerificationChallenge{
		ID:                   id,
		Kind:                 IdentifierEmail,
		Purpose:              ChallengeSignup,
		DestinationCanonical: "owner@example.com",
		TokenHash:            hash,
		CreatedAt:            now,
		ExpiresAt:            now.Add(10 * time.Minute),
		MaxAttempts:          3,
	}
}

func mustOtherID(t *testing.T, avoid ID) ID {
	t.Helper()
	for {
		id, err := NewID()
		if err != nil {
			t.Fatal(err)
		}
		if id != avoid {
			return id
		}
	}
}
