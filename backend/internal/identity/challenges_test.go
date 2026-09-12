package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGenerateEmailVerificationToken(t *testing.T) {
	raw, hash, err := GenerateEmailVerificationToken()
	if err != nil {
		t.Fatalf("GenerateEmailVerificationToken: %v", err)
	}
	secret, err := decodeSessionToken(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(secret) < MinSessionSecretBytes {
		t.Fatalf("entropy bytes = %d, want >= %d", len(secret), MinSessionSecretBytes)
	}
	want, err := HashVerificationSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash, want) {
		t.Fatal("hash must be SHA-256 of raw secret bytes")
	}
	if bytes.Equal(hash, secret) || strings.Contains(string(hash), raw) {
		t.Fatal("raw token must not equal or appear in the hash")
	}
	raw2, hash2, err := GenerateEmailVerificationToken()
	if err != nil {
		t.Fatal(err)
	}
	if raw == raw2 || bytes.Equal(hash, hash2) {
		t.Fatal("tokens must be unique")
	}
}

func TestGeneratePhoneOTP(t *testing.T) {
	raw, hash, err := GeneratePhoneOTP(6)
	if err != nil {
		t.Fatalf("GeneratePhoneOTP: %v", err)
	}
	if len(raw) != 6 {
		t.Fatalf("digits = %d, want 6", len(raw))
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			t.Fatalf("non-digit OTP %q", raw)
		}
	}
	want, err := HashVerificationSecret([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash, want) {
		t.Fatal("hash must be SHA-256 of the numeric OTP")
	}
	if bytes.Equal(hash, []byte(raw)) {
		t.Fatal("hash must not equal plaintext OTP")
	}
	raw2, _, err := GeneratePhoneOTP(6)
	if err != nil {
		t.Fatal(err)
	}
	if raw == raw2 {
		t.Fatal("OTPs must not be a fixed value")
	}
}

func TestChallengeHashOnlyPersistence(t *testing.T) {
	ctx := context.Background()
	svc, store, inc, _ := newTestChallenges(t)

	email, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "Owner@Example.com", "203.0.113.10")
	if err != nil {
		t.Fatalf("email issue: %v", err)
	}
	if email.RawSecret == "" {
		t.Fatal("raw email token must be returned once")
	}
	stored := store.must(t, email.Challenge.ID)
	if bytes.Contains(stored.TokenHash, []byte(email.RawSecret)) {
		t.Fatal("raw email token must not appear in stored hash")
	}
	if store.rawSecrets[email.Challenge.ID] != "" {
		t.Fatal("store must never persist raw email token")
	}
	if _, err := base64.RawURLEncoding.DecodeString(email.RawSecret); err != nil {
		t.Fatal("email secret must be a high-entropy URL-safe token")
	}

	phone, err := svc.Issue(ctx, IdentifierPhone, ChallengeSignup, "+905551234567", "203.0.113.10")
	if err != nil {
		t.Fatalf("phone issue: %v", err)
	}
	if len(phone.RawSecret) != 6 {
		t.Fatalf("phone OTP width = %d, want injected 6", len(phone.RawSecret))
	}
	storedPhone := store.must(t, phone.Challenge.ID)
	if bytes.Contains(storedPhone.TokenHash, []byte(phone.RawSecret)) || storedPhone.DestinationCanonical != "+905551234567" {
		t.Fatal("plaintext OTP must not be stored")
	}
	if store.rawSecrets[phone.Challenge.ID] != "" {
		t.Fatal("store must never persist raw OTP")
	}
	for _, id := range []ID{email.Challenge.ID, phone.Challenge.ID} {
		mat, ok := store.materials[id]
		if !ok {
			t.Fatal("encrypted material must be stored at issue")
		}
		if bytes.Contains(mat.Ciphertext, []byte(email.RawSecret)) || bytes.Contains(mat.Nonce, []byte(email.RawSecret)) ||
			bytes.Contains(mat.Ciphertext, []byte(phone.RawSecret)) || bytes.Contains(mat.Nonce, []byte(phone.RawSecret)) {
			t.Fatal("plaintext must not appear in sealed material")
		}
		if mat.DestroyedAt != nil {
			t.Fatal("fresh material must be active")
		}
	}

	for _, key := range inc.keys {
		if strings.Contains(key, "Owner@Example.com") || strings.Contains(key, "owner@example.com") || strings.Contains(key, "+905551234567") {
			t.Fatalf("limiter key leaked destination PII: %q", key)
		}
	}
}

func TestChallengeValidVerify(t *testing.T) {
	ctx := context.Background()
	svc, _, _, now := newTestChallenges(t)
	issued, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "login@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Verify(ctx, issued.Challenge.ID, issued.RawSecret)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.ID != issued.Challenge.ID || got.ConsumedAt == nil || got.Kind != IdentifierEmail {
		t.Fatalf("consumed mismatch: %+v", got)
	}
	if got.ExpiresAt != now().Add(10*time.Minute) {
		t.Fatalf("ttl must come from injected policy, got %v", got.ExpiresAt)
	}
}

func TestChallengeWrongCodeIncrementsAttempts(t *testing.T) {
	ctx := context.Background()
	svc, store, _, _ := newTestChallenges(t)
	issued, err := svc.Issue(ctx, IdentifierPhone, ChallengeSignup, "+15551234567", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, issued.Challenge.ID, "000000"); !errors.Is(err, errInvalidChallenge) {
		t.Fatalf("wrong OTP err = %v, want %v", err, errInvalidChallenge)
	}
	row := store.must(t, issued.Challenge.ID)
	if row.FailedAttempts != 1 || row.ConsumedAt != nil {
		t.Fatalf("attempts = %d consumed=%v", row.FailedAttempts, row.ConsumedAt)
	}
}

func TestChallengeMaxAttemptExhaustion(t *testing.T) {
	ctx := context.Background()
	svc, store, _, _ := newTestChallenges(t)
	issued, err := svc.Issue(ctx, IdentifierPhone, ChallengeSignup, "+15551234568", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, issued.Challenge.ID, "111111"); !errors.Is(err, errInvalidChallenge) {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, issued.Challenge.ID, "222222"); !errors.Is(err, errChallengeExhausted) {
		t.Fatalf("second miss err = %v, want %v", err, errChallengeExhausted)
	}
	row := store.must(t, issued.Challenge.ID)
	if row.FailedAttempts != 2 {
		t.Fatalf("failed_attempts = %d, want 2", row.FailedAttempts)
	}
	if _, err := svc.Verify(ctx, issued.Challenge.ID, issued.RawSecret); !errors.Is(err, errChallengeExhausted) {
		t.Fatalf("correct after exhaustion err = %v, want %v", err, errChallengeExhausted)
	}
	if store.must(t, issued.Challenge.ID).FailedAttempts != 2 {
		t.Fatal("exhausted challenge must not increment further")
	}
	if store.must(t, issued.Challenge.ID).ConsumedAt != nil {
		t.Fatal("exhausted challenge must not be consumed")
	}
}

func TestChallengeExpiration(t *testing.T) {
	ctx := context.Background()
	store := newMemChallengeStore()
	inc := &memIssuanceCounter{}
	limiter := mustLimiter(t, inc)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	current := now
	svc, err := NewChallenges(store, testChallengePolicy(), limiter, mustProtector(t), func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "exp@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	current = now.Add(10 * time.Minute)
	if _, err := svc.Verify(ctx, issued.Challenge.ID, issued.RawSecret); !errors.Is(err, errChallengeExpired) {
		t.Fatalf("expired err = %v, want %v", err, errChallengeExpired)
	}
	if store.must(t, issued.Challenge.ID).ConsumedAt != nil {
		t.Fatal("expired challenge must not be consumed")
	}
}

func TestChallengeConsumeOnceReplay(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newTestChallenges(t)
	issued, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "once@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, issued.Challenge.ID, issued.RawSecret); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, issued.Challenge.ID, issued.RawSecret); !errors.Is(err, errChallengeConsumed) {
		t.Fatalf("replay err = %v, want %v", err, errChallengeConsumed)
	}
}

func TestChallengeConsumeOnceConcurrent(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newTestChallenges(t)
	issued, err := svc.Issue(ctx, IdentifierPhone, ChallengeSignup, "+15550001111", "")
	if err != nil {
		t.Fatal(err)
	}

	var successes atomic.Int32
	var consumed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Verify(ctx, issued.Challenge.ID, issued.RawSecret)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, errChallengeConsumed):
				consumed.Add(1)
			default:
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successes = %d, want 1", successes.Load())
	}
	if consumed.Load() != 7 {
		t.Fatalf("consumed rejections = %d, want 7", consumed.Load())
	}
}

func TestDestinationLimiterUsesHashNotRawPII(t *testing.T) {
	ctx := context.Background()
	svc, _, inc, _ := newTestChallenges(t)
	raw := "  Alice@Example.COM "
	if _, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, raw, "198.51.100.9"); err != nil {
		t.Fatal(err)
	}
	canonical := "alice@example.com"
	sum := sha256.Sum256([]byte(canonical))
	wantDest := destinationIssuanceKey(IdentifierEmail, ChallengeSignup, canonical)
	if !strings.Contains(wantDest, hex.EncodeToString(sum[:])) {
		t.Fatal("destination key must include SHA-256 of canonical destination")
	}
	if strings.Contains(wantDest, canonical) || strings.Contains(wantDest, raw) {
		t.Fatal("destination limiter key must not contain raw email")
	}

	wantIP := issuanceIPKeyPrefix + string(ChallengeSignup) + ":" + HashRateLimitSubject("198.51.100.9")
	foundDest, foundIP := false, false
	for _, key := range inc.keys {
		if strings.Contains(key, "Alice") || strings.Contains(key, "alice@example.com") || strings.Contains(key, "Example.COM") {
			t.Fatalf("raw email in limiter key %q", key)
		}
		if strings.Contains(key, "198.51.100.9") {
			t.Fatalf("raw IP in limiter key %q", key)
		}
		if key == wantDest {
			foundDest = true
		}
		if key == wantIP {
			foundIP = true
		}
	}
	if !foundDest {
		t.Fatalf("missing hashed dest key, got %v", inc.keys)
	}
	if !foundIP {
		t.Fatalf("missing IP bucket key, got %v", inc.keys)
	}
}

func TestIssuanceLimiterUnavailableFailsClosed(t *testing.T) {
	ctx := context.Background()
	store := newMemChallengeStore()
	inc := &memIssuanceCounter{fail: true}
	limiter := mustLimiter(t, inc)
	svc, err := NewChallenges(store, testChallengePolicy(), limiter, mustProtector(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "pump@example.com", "192.0.2.1"); !errors.Is(err, errUnavailable) {
		t.Fatalf("issue err = %v, want %v", err, errUnavailable)
	}
	if len(store.rows) != 0 {
		t.Fatal("fail-closed limiter must not persist a challenge")
	}
}

func TestChallengeStoreErrorsAreUnavailable(t *testing.T) {
	ctx := context.Background()
	store := newMemChallengeStore()
	store.unavailable = true
	svc, err := NewChallenges(store, testChallengePolicy(), mustLimiter(t, &memIssuanceCounter{}), mustProtector(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "db@example.com", ""); !errors.Is(err, errUnavailable) {
		t.Fatalf("issue err = %v, want %v", err, errUnavailable)
	}
	store.unavailable = false
	issued, err := svc.Issue(ctx, IdentifierEmail, ChallengeSignup, "db@example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	store.unavailable = true
	if _, err := svc.Verify(ctx, issued.Challenge.ID, issued.RawSecret); !errors.Is(err, errUnavailable) {
		t.Fatalf("verify err = %v, want %v", err, errUnavailable)
	}
	if errors.Is(errUnavailable, errInvalidChallenge) || errors.Is(errUnavailable, errChallengeThrottled) {
		t.Fatal("store unavailability must not be a verification outcome")
	}

	pg := NewPostgresStore(nil)
	if _, err := pg.GetChallenge(ctx, issued.Challenge.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("nil pool get err = %v, want %v", err, errUnavailable)
	}
	if err := pg.InsertChallenge(ctx, issued.Challenge); !errors.Is(err, errUnavailable) {
		t.Fatalf("nil pool insert err = %v, want %v", err, errUnavailable)
	}
	if err := pg.InsertChallengeAndMaterial(ctx, nil, issued.Challenge, VerificationMaterial{}); !errors.Is(err, errUnavailable) {
		t.Fatalf("nil pool insert+material err = %v, want %v", err, errUnavailable)
	}
	if _, err := pg.GetActiveMaterial(ctx, issued.Challenge.ID); !errors.Is(err, errUnavailable) {
		t.Fatalf("nil pool get material err = %v, want %v", err, errUnavailable)
	}
	if err := pg.DestroyMaterial(ctx, issued.Challenge.ID, time.Now()); !errors.Is(err, errUnavailable) {
		t.Fatalf("nil pool destroy material err = %v, want %v", err, errUnavailable)
	}
	if _, err := pg.ConsumeChallenge(ctx, issued.Challenge.ID, time.Now()); !errors.Is(err, errUnavailable) {
		t.Fatalf("nil pool consume err = %v, want %v", err, errUnavailable)
	}
	if _, err := pg.IncrementFailedAttempt(ctx, issued.Challenge.ID, time.Now()); !errors.Is(err, errUnavailable) {
		t.Fatalf("nil pool increment err = %v, want %v", err, errUnavailable)
	}
}

func TestInvalidChallengePolicy(t *testing.T) {
	if _, err := NewChallenges(newMemChallengeStore(), VerificationChallengePolicy{}, mustLimiter(t, &memIssuanceCounter{}), mustProtector(t), nil); !errors.Is(err, errInvalidChallengePolicy) {
		t.Fatalf("err = %v, want %v", err, errInvalidChallengePolicy)
	}
}

func testChallengePolicy() VerificationChallengePolicy {
	return VerificationChallengePolicy{TTL: 10 * time.Minute, MaxAttempts: 2, PhoneOTPDigits: 6}
}

func testIssuancePolicy() IssuanceLimitPolicy {
	return IssuanceLimitPolicy{
		DestinationMax:    5,
		DestinationWindow: time.Hour,
		IPMax:             10,
		IPWindow:          time.Hour,
	}
}

func mustProtector(t *testing.T) *MaterialProtector {
	t.Helper()
	p, err := NewMaterialProtector(mustKeyring(t, "test-v1", map[string][]byte{
		"test-v1": bytes.Repeat([]byte{0x11}, aes256KeySize),
	}))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustKeyring(t *testing.T, activeID string, keys map[string][]byte) *Keyring {
	t.Helper()
	k, err := NewKeyring(activeID, keys)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustLimiter(t *testing.T, inc issuanceCounter) *IssuanceLimiter {
	t.Helper()
	l, err := NewIssuanceLimiter(inc, testIssuancePolicy())
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func newTestChallenges(t *testing.T) (*Challenges, *memChallengeStore, *memIssuanceCounter, func() time.Time) {
	t.Helper()
	store := newMemChallengeStore()
	inc := &memIssuanceCounter{}
	now := func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }
	svc, err := NewChallenges(store, testChallengePolicy(), mustLimiter(t, inc), mustProtector(t), now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, inc, now
}

type memIssuanceCounter struct {
	mu   sync.Mutex
	keys []string
	n    map[string]int64
	fail bool
}

func (m *memIssuanceCounter) Increment(_ context.Context, key string, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, errUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return 0, errors.New("valkey down")
	}
	if m.n == nil {
		m.n = make(map[string]int64)
	}
	m.keys = append(m.keys, key)
	m.n[key]++
	return m.n[key], nil
}

type memChallengeStore struct {
	mu           sync.Mutex
	rows         map[ID]VerificationChallenge
	materials    map[ID]VerificationMaterial
	rawSecrets   map[ID]string
	unavailable  bool
	materialFail bool
}

func newMemChallengeStore() *memChallengeStore {
	return &memChallengeStore{
		rows:       make(map[ID]VerificationChallenge),
		materials:  make(map[ID]VerificationMaterial),
		rawSecrets: make(map[ID]string),
	}
}

func (m *memChallengeStore) must(t *testing.T, id ID) VerificationChallenge {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		t.Fatalf("challenge %v missing", id)
	}
	return cloneChallenge(c)
}

func (m *memChallengeStore) InsertChallenge(_ context.Context, challenge VerificationChallenge) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return errUnavailable
	}
	m.rows[challenge.ID] = cloneChallenge(challenge)
	return nil
}

func (m *memChallengeStore) InsertChallengeAndMaterial(ctx context.Context, exec txExecer, challenge VerificationChallenge, material VerificationMaterial) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable || m.materialFail {
		return errUnavailable
	}
	if err := material.Validate(); err != nil || !material.Active() || material.ChallengeID != challenge.ID {
		return errInvalidChallenge
	}
	if exec != nil {
		if _, err := exec.Exec(ctx, "identity.verification_challenge_material", challenge, material); err != nil {
			return errUnavailable
		}
		if tx, ok := exec.(*memTx); ok {
			tx.stageChallenge(challenge, material)
			return nil
		}
		if tx, ok := exec.(*memResetTx); ok {
			tx.stageChallenge(challenge, material)
			return nil
		}
	}
	m.rows[challenge.ID] = cloneChallenge(challenge)
	m.materials[challenge.ID] = cloneMaterial(material)
	return nil
}

func (m *memChallengeStore) GetActiveMaterial(_ context.Context, challengeID ID) (VerificationMaterial, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return VerificationMaterial{}, errUnavailable
	}
	mat, ok := m.materials[challengeID]
	if !ok || mat.DestroyedAt != nil {
		return VerificationMaterial{}, errNotFound
	}
	return cloneMaterial(mat), nil
}

func (m *memChallengeStore) DestroyMaterial(_ context.Context, challengeID ID, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return errUnavailable
	}
	mat, ok := m.materials[challengeID]
	if !ok || mat.DestroyedAt != nil {
		return errNotFound
	}
	destroyed := at
	mat.Nonce = []byte{}
	mat.Ciphertext = []byte{}
	mat.DestroyedAt = &destroyed
	m.materials[challengeID] = mat
	return nil
}

func (m *memChallengeStore) GetChallenge(_ context.Context, id ID) (VerificationChallenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return VerificationChallenge{}, errUnavailable
	}
	c, ok := m.rows[id]
	if !ok {
		return VerificationChallenge{}, errNotFound
	}
	return cloneChallenge(c), nil
}

func (m *memChallengeStore) ConsumeChallenge(_ context.Context, id ID, now time.Time) (VerificationChallenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return VerificationChallenge{}, errUnavailable
	}
	c, ok := m.rows[id]
	if !ok {
		return VerificationChallenge{}, errInvalidChallenge
	}
	if err := c.rejectUnusable(now); err != nil {
		return VerificationChallenge{}, err
	}
	consumed := now
	c.ConsumedAt = &consumed
	m.rows[id] = c
	if mat, ok := m.materials[id]; ok && mat.DestroyedAt == nil {
		mat.Nonce = []byte{}
		mat.Ciphertext = []byte{}
		mat.DestroyedAt = &consumed
		m.materials[id] = mat
	}
	return cloneChallenge(c), nil
}

func (m *memChallengeStore) IncrementFailedAttempt(_ context.Context, id ID, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return 0, errUnavailable
	}
	c, ok := m.rows[id]
	if !ok {
		return 0, errInvalidChallenge
	}
	if err := c.rejectUnusable(now); err != nil {
		return 0, err
	}
	c.FailedAttempts++
	m.rows[id] = c
	return c.FailedAttempts, nil
}

func cloneChallenge(c VerificationChallenge) VerificationChallenge {
	c.TokenHash = cloneBytes(c.TokenHash)
	if c.ConsumedAt != nil {
		t := *c.ConsumedAt
		c.ConsumedAt = &t
	}
	return c
}

func cloneMaterial(m VerificationMaterial) VerificationMaterial {
	m.Nonce = cloneBytes(m.Nonce)
	m.Ciphertext = cloneBytes(m.Ciphertext)
	if m.DestroyedAt != nil {
		t := *m.DestroyedAt
		m.DestroyedAt = &t
	}
	return m
}
