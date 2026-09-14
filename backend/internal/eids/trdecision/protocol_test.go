package trdecision

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T, id string) PrivateKey {
	t.Helper()
	k, err := GeneratePrivateKey(id)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func validClaims(subject string) ClaimsV1 {
	issued := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	return ClaimsV1{
		SchemaVersion:    SchemaV1,
		VerificationType: TypeProperty,
		Status:           StatusApproved,
		DecisionID:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SubjectRef:       subject,
		IssuedAt:         issued,
		ValidUntil:       issued.Add(24 * time.Hour),
		Audience:         AudienceGermanyV1,
	}
}

func sampleSubject() string {
	return strings.Repeat("ab", 32)
}

func TestSignAndVerify(t *testing.T) {
	k := testKey(t, "tr-v1")
	pub, err := k.Public()
	if err != nil {
		t.Fatal(err)
	}
	ring, err := NewPublicRing([]PublicKey{pub})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 14, 12, 0, 30, 0, time.UTC)
	v, err := NewVerifier(ring, AudienceGermanyV1, DefaultTimePolicy(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	env, err := k.Sign(validClaims(sampleSubject()))
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Verify(env, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusApproved {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestKeyIDRoutingAndUnknownKey(t *testing.T) {
	a := testKey(t, "old")
	b := testKey(t, "new")
	pubA, _ := a.Public()
	pubB, _ := b.Public()
	ring, err := NewPublicRing([]PublicKey{pubA, pubB})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := NewVerifier(ring, AudienceGermanyV1, DefaultTimePolicy(), nil)
	now := time.Date(2026, 9, 14, 12, 0, 30, 0, time.UTC)
	env, err := b.Sign(validClaims(sampleSubject()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(env, now); err != nil {
		t.Fatal(err)
	}
	env.KeyID = "missing"
	if _, err := v.Verify(env, now); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("unknown key: %v", err)
	}
}

func TestTamperRejected(t *testing.T) {
	k := testKey(t, "tr-v1")
	pub, _ := k.Public()
	ring, _ := NewPublicRing([]PublicKey{pub})
	now := time.Date(2026, 9, 14, 12, 0, 30, 0, time.UTC)
	v, _ := NewVerifier(ring, AudienceGermanyV1, DefaultTimePolicy(), func() time.Time { return now })
	env, err := k.Sign(validClaims(sampleSubject()))
	if err != nil {
		t.Fatal(err)
	}
	env.Claims.Status = StatusRejected
	if _, err := v.Verify(env, now); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("status tamper: %v", err)
	}
	env, _ = k.Sign(validClaims(sampleSubject()))
	env.Claims.SubjectRef = strings.Repeat("cd", 32)
	if _, err := v.Verify(env, now); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("subject tamper: %v", err)
	}
	env, _ = k.Sign(validClaims(sampleSubject()))
	env.Claims.ValidUntil = env.Claims.ValidUntil.Add(time.Hour)
	if _, err := v.Verify(env, now); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("valid_until tamper: %v", err)
	}
	env, _ = k.Sign(validClaims(sampleSubject()))
	env.signatureB[0] ^= 0xff
	if _, err := v.Verify(env, now); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("bad sig: %v", err)
	}
}

func TestWrongAudienceAndUnsupported(t *testing.T) {
	k := testKey(t, "tr-v1")
	pub, _ := k.Public()
	ring, _ := NewPublicRing([]PublicKey{pub})
	now := time.Date(2026, 9, 14, 12, 0, 30, 0, time.UTC)
	v, _ := NewVerifier(ring, AudienceGermanyV1, DefaultTimePolicy(), nil)
	c := validClaims(sampleSubject())
	c.Audience = "other-service"
	env, err := k.Sign(c)
	if err == nil {
		if _, err := v.Verify(env, now); !errors.Is(err, ErrInvalidAudience) {
			t.Fatalf("audience: %v", err)
		}
	}
	c = validClaims(sampleSubject())
	c.SchemaVersion = "v2"
	if _, err := k.Sign(c); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("schema sign: %v", err)
	}
	c = validClaims(sampleSubject())
	c.VerificationType = "person"
	if _, err := k.Sign(c); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("type: %v", err)
	}
	c = validClaims(sampleSubject())
	c.Status = "pending"
	if _, err := k.Sign(c); !errors.Is(err, ErrUnsupportedStatus) {
		t.Fatalf("status: %v", err)
	}
}

func TestTimePolicy(t *testing.T) {
	k := testKey(t, "tr-v1")
	pub, _ := k.Public()
	ring, _ := NewPublicRing([]PublicKey{pub})
	policy := DefaultTimePolicy()
	issued := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	c := validClaims(sampleSubject())
	env, err := k.Sign(c)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := NewVerifier(ring, AudienceGermanyV1, policy, nil)
	if _, err := v.Verify(env, issued.Add(30*time.Second)); err != nil {
		t.Fatalf("fresh: %v", err)
	}
	if _, err := v.Verify(env, issued.Add(-2*time.Minute)); err != nil {
		t.Fatalf("skew edge accepted: %v", err)
	}
	if _, err := v.Verify(env, issued.Add(-2*time.Minute-time.Second)); !errors.Is(err, ErrFutureIssuedAt) {
		t.Fatalf("future issued: %v", err)
	}
	if _, err := v.Verify(env, issued.Add(10*time.Minute+2*time.Minute+time.Second)); !errors.Is(err, ErrStale) {
		t.Fatalf("stale: %v", err)
	}
	expired := c
	expired.ValidUntil = issued.Add(time.Minute)
	env, err = k.Sign(expired)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(env, issued.Add(2*time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired: %v", err)
	}
}

func TestEnvelopeRoundTripAndForbidden(t *testing.T) {
	k := testKey(t, "tr-v1")
	env, err := k.Sign(validClaims(sampleSubject()))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeEnvelope(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.Claims.DecisionID != env.Claims.DecisionID {
		t.Fatal("decision_id mismatch")
	}
	if _, err := DecodeEnvelope(bytes.NewReader([]byte(`{"key_id":"tr-v1","claims":{"schema_version":"tr-decision-v1","verification_type":"property","status":"approved","decision_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","subject_ref":"` + sampleSubject() + `","issued_at":"2026-09-14T12:00:00.000000000Z","valid_until":"2026-09-15T12:00:00.000000000Z","audience":"konumlu-germany-eids-v1","tckn":"10000000146"},"signature":"` + env.Signature + `"}`))); !errors.Is(err, ErrForbiddenField) && !errors.Is(err, ErrMalformedEnvelope) {
		t.Fatalf("tckn: %v", err)
	}
	if _, err := DecodeEnvelope(bytes.NewReader([]byte(`{"alg":"none","key_id":"tr-v1","claims":{},"signature":""}`))); err == nil {
		t.Fatal("alg=none must fail")
	}
}

func TestMalformedKeysAndBase64(t *testing.T) {
	if _, err := ParsePrivateKey("tr-v1", "not-a-key"); !errors.Is(err, ErrMalformedKey) {
		t.Fatalf("priv: %v", err)
	}
	if _, err := ParsePublicKey("tr-v1", "short"); !errors.Is(err, ErrMalformedKey) {
		t.Fatalf("pub: %v", err)
	}
	if _, err := ParsePrivateKey("bad id!", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, ed25519.SeedSize))); !errors.Is(err, ErrInvalidKeyID) {
		t.Fatalf("kid: %v", err)
	}
	env := Envelope{Signature: "%%%"}
	if _, err := DecodeEnvelope(bytes.NewReader([]byte(`{"key_id":"tr-v1","claims":{"schema_version":"tr-decision-v1","verification_type":"property","status":"approved","decision_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","subject_ref":"` + sampleSubject() + `","issued_at":"2026-09-14T12:00:00Z","valid_until":"2026-09-15T12:00:00Z","audience":"konumlu-germany-eids-v1"},"signature":"%%%"}`))); err == nil {
		t.Fatal("bad b64")
	}
	_ = env
}

func TestIssuerDoesNotAcceptIdentity(t *testing.T) {
	k := testKey(t, "tr-v1")
	iss, err := NewIssuer(k, func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	env, err := iss.Issue(ProviderResult{
		SubjectRef:       sampleSubject(),
		VerificationType: TypeVehicle,
		Status:           StatusRejected,
		ValidUntil:       time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if env.Claims.Status != StatusRejected || env.Claims.VerificationType != TypeVehicle {
		t.Fatalf("claims=%+v", env.Claims)
	}
	if _, err := iss.Issue(ProviderResult{SubjectRef: "tckn-not-allowed", VerificationType: TypeProperty, Status: StatusApproved, ValidUntil: time.Now().Add(time.Hour)}); !errors.Is(err, ErrProviderResult) && !errors.Is(err, ErrInvalidClaims) {
		t.Fatalf("tckn subject: %v", err)
	}
}

func TestPrivateKeyNotPrinted(t *testing.T) {
	k := testKey(t, "tr-v1")
	seed, err := EncodePrivateSeed(k)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(k.String(), seed) || strings.Contains(k.GoString(), seed) || strings.Contains(k.LogValue().String(), seed) {
		t.Fatal("private key leaked")
	}
}

func TestNoAlgorithmNegotiation(t *testing.T) {
	raw := []byte(`{"key_id":"tr-v1","alg":"Ed25519","claims":{"schema_version":"tr-decision-v1","verification_type":"property","status":"approved","decision_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","subject_ref":"` + sampleSubject() + `","issued_at":"2026-09-14T12:00:00Z","valid_until":"2026-09-15T12:00:00Z","audience":"konumlu-germany-eids-v1"},"signature":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`)
	if _, err := DecodeEnvelope(bytes.NewReader(raw)); err == nil {
		t.Fatal("alg field must be rejected")
	}
}
