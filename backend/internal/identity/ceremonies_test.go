package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWebAuthnConfigValidation(t *testing.T) {
	valid := WebAuthnConfig{
		RPDisplayName: "KONUMLU local",
		RPID:          "localhost",
		RPOrigins:     []string{"http://localhost:8080", "http://127.0.0.1:8080"},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}

	cases := []WebAuthnConfig{
		{RPID: "localhost", RPOrigins: []string{"http://localhost:8080"}},
		{RPDisplayName: "KONUMLU local", RPOrigins: []string{"http://localhost:8080"}},
		{RPDisplayName: "KONUMLU local", RPID: "localhost"},
		{RPDisplayName: "KONUMLU local", RPID: "https://localhost", RPOrigins: []string{"http://localhost:8080"}},
		{RPDisplayName: "KONUMLU local", RPID: "localhost", RPOrigins: []string{"not-an-origin"}},
		{RPDisplayName: "KONUMLU local", RPID: "localhost", RPOrigins: []string{"ftp://localhost"}},
		{RPDisplayName: "KONUMLU local", RPID: "localhost", RPOrigins: []string{"http://localhost:8080", "http://localhost:8080"}},
		{RPDisplayName: "KONUMLU local", RPID: "localhost", RPOrigins: []string{"http://localhost:8080", ""}},
	}
	for _, cfg := range cases {
		if err := cfg.Validate(); !errors.Is(err, errInvalidWebAuthnConfig) {
			t.Fatalf("cfg %+v: err = %v, want %v", cfg, err, errInvalidWebAuthnConfig)
		}
	}
}

func TestNewWebAuthnRequiresConfig(t *testing.T) {
	if _, err := NewWebAuthn(WebAuthnConfig{}); !errors.Is(err, errInvalidWebAuthnConfig) {
		t.Fatalf("empty err = %v, want %v", err, errInvalidWebAuthnConfig)
	}
	wa, err := NewWebAuthn(WebAuthnConfig{
		RPDisplayName: "KONUMLU local",
		RPID:          "localhost",
		RPOrigins:     []string{"http://localhost:8080", "https://app.example.test"},
	})
	if err != nil {
		t.Fatalf("NewWebAuthn: %v", err)
	}
	if wa == nil || wa.Config.RPID != "localhost" {
		t.Fatal("webauthn instance missing rpid")
	}
	if got := strings.Join(wa.Config.RPOrigins, ","); !strings.Contains(got, "http://localhost:8080") {
		t.Fatalf("origins = %v", wa.Config.RPOrigins)
	}
}

func TestGenerateCeremonyToken(t *testing.T) {
	raw, hash, err := GenerateCeremonyToken()
	if err != nil {
		t.Fatalf("GenerateCeremonyToken: %v", err)
	}
	secret, err := decodeCeremonyToken(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want, err := HashSessionSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(hash, want) {
		t.Fatal("hash must be SHA-256 of raw secret bytes")
	}
	if bytes.Equal(hash, []byte(raw)) || strings.Contains(string(hash), raw) {
		t.Fatal("raw token must not equal or appear in the hash")
	}
	if _, err := decodeCeremonyToken(""); err != errInvalidCeremony {
		t.Fatalf("empty token err = %v, want %v", err, errInvalidCeremony)
	}
}

func TestCeremonyCreateAndConsumeOnce(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestCeremonies(t)
	user := mustID(t)
	session := []byte(`{"challenge":"server-session"}`)

	issued, err := svc.Create(ctx, CeremonyRegistration, &user, session)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if issued.RawToken == "" {
		t.Fatal("raw token must be returned once")
	}
	if bytes.Contains(issued.State.TokenHash, []byte(issued.RawToken)) {
		t.Fatal("raw token must not appear in stored hash")
	}
	if store.rawTokens[issued.State.ID] != "" {
		t.Fatal("store must never persist raw token")
	}
	if !bytes.Equal(store.must(t, issued.State.ID).SessionData, session) {
		t.Fatal("server session data must be stored")
	}
	if issued.State.ExpiresAt != now().Add(2*time.Minute) {
		t.Fatalf("ttl must come from injected policy, got %v", issued.State.ExpiresAt)
	}

	got, err := svc.Consume(ctx, issued.RawToken)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if got.ID != issued.State.ID || got.Kind != CeremonyRegistration || got.UserID == nil || *got.UserID != user {
		t.Fatalf("consumed mismatch: %+v", got)
	}
	if got.ConsumedAt == nil {
		t.Fatal("consumed_at must be set")
	}

	if _, err := svc.Consume(ctx, issued.RawToken); err != errCeremonyConsumed {
		t.Fatalf("second consume err = %v, want %v", err, errCeremonyConsumed)
	}
}

func TestCeremonyAuthenticationWithoutUser(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestCeremonies(t)
	issued, err := svc.Create(ctx, CeremonyAuthentication, nil, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Consume(ctx, issued.RawToken)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != CeremonyAuthentication || got.UserID != nil {
		t.Fatalf("discoverable auth ceremony mismatch: %+v", got)
	}
}

func TestCeremonyExpiration(t *testing.T) {
	ctx := context.Background()
	store := newMemCeremonyStore()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	current := now
	svc, err := NewCeremonies(store, CeremonyPolicy{TTL: time.Minute}, func() time.Time { return current })
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.Create(ctx, CeremonyRegistration, nil, []byte{0x02})
	if err != nil {
		t.Fatal(err)
	}
	current = now.Add(time.Minute)
	if _, err := svc.Consume(ctx, issued.RawToken); err != errCeremonyExpired {
		t.Fatalf("expired err = %v, want %v", err, errCeremonyExpired)
	}
	if store.must(t, issued.State.ID).ConsumedAt != nil {
		t.Fatal("expired ceremony must not be consumed")
	}
}

func TestCeremonyInvalidKindAndState(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestCeremonies(t)
	if _, err := svc.Create(ctx, CeremonyKind("login"), nil, []byte{0x01}); err != errInvalidCeremony {
		t.Fatalf("kind err = %v, want %v", err, errInvalidCeremony)
	}
	if _, err := svc.Create(ctx, CeremonyRegistration, nil, nil); err != errInvalidCeremony {
		t.Fatalf("empty session err = %v, want %v", err, errInvalidCeremony)
	}
	zero := ID{}
	if _, err := svc.Create(ctx, CeremonyRegistration, &zero, []byte{0x01}); err != errZeroID {
		t.Fatalf("zero user err = %v, want %v", err, errZeroID)
	}
	if _, err := svc.Consume(ctx, ""); err != errInvalidCeremony {
		t.Fatalf("empty token err = %v, want %v", err, errInvalidCeremony)
	}
	if _, err := svc.Consume(ctx, "not-a-token"); err != errInvalidCeremony {
		t.Fatalf("malformed token err = %v, want %v", err, errInvalidCeremony)
	}
}

func TestCeremonyStoreErrorsAreNotAuthDecisions(t *testing.T) {
	ctx := context.Background()
	store := newMemCeremonyStore()
	store.unavailable = true
	svc, err := NewCeremonies(store, CeremonyPolicy{TTL: time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, CeremonyRegistration, nil, []byte{0x01}); !errors.Is(err, errUnavailable) {
		t.Fatalf("create err = %v, want %v", err, errUnavailable)
	}
	if errors.Is(errUnavailable, errUnauthenticated) || errors.Is(errUnavailable, errInvalidCeremony) {
		t.Fatal("store unavailability must not be an authentication outcome")
	}
}

func TestCeremonySingleUseConcurrent(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newTestCeremonies(t)
	issued, err := svc.Create(ctx, CeremonyAuthentication, nil, []byte{0x03})
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
			_, err := svc.Consume(ctx, issued.RawToken)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, errCeremonyConsumed):
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

func TestInvalidCeremonyPolicy(t *testing.T) {
	if _, err := NewCeremonies(newMemCeremonyStore(), CeremonyPolicy{}, nil); err != errInvalidCeremonyPolicy {
		t.Fatalf("err = %v, want %v", err, errInvalidCeremonyPolicy)
	}
}

func TestPasskeyUserAdapter(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	c := validPasskey(t, now)
	c.SignCount = 9
	u := PasskeyUser{ID: c.UserID, Name: "handle", DisplayName: "Handle", Passkeys: []PasskeyCredential{c}}
	if got := u.WebAuthnID(); !bytes.Equal(got, c.UserID[:]) {
		t.Fatal("webauthn user handle must be the identity id")
	}
	creds := u.WebAuthnCredentials()
	if len(creds) != 1 || !bytes.Equal(creds[0].ID, c.CredentialID) || creds[0].Authenticator.SignCount != 9 {
		t.Fatalf("credential adapter mismatch: %+v", creds)
	}
}

func newTestCeremonies(t *testing.T) (*Ceremonies, *memCeremonyStore, func() time.Time) {
	t.Helper()
	store := newMemCeremonyStore()
	now := func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }
	svc, err := NewCeremonies(store, CeremonyPolicy{TTL: 2 * time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, now
}

type memCeremonyStore struct {
	mu          sync.Mutex
	rows        map[ID]CeremonyState
	byHash      map[string]ID
	rawTokens   map[ID]string
	unavailable bool
}

func newMemCeremonyStore() *memCeremonyStore {
	return &memCeremonyStore{
		rows:      make(map[ID]CeremonyState),
		byHash:    make(map[string]ID),
		rawTokens: make(map[ID]string),
	}
}

func (m *memCeremonyStore) must(t *testing.T, id ID) CeremonyState {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[id]
	if !ok {
		t.Fatalf("ceremony %v missing", id)
	}
	return cloneCeremony(s)
}

func (m *memCeremonyStore) InsertCeremony(ctx context.Context, state CeremonyState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return errUnavailable
	}
	key := string(state.TokenHash)
	if _, exists := m.byHash[key]; exists {
		return errUnavailable
	}
	m.rows[state.ID] = cloneCeremony(state)
	m.byHash[key] = state.ID
	return nil
}

func (m *memCeremonyStore) ConsumeCeremonyByTokenHash(ctx context.Context, tokenHash []byte, now time.Time) (CeremonyState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unavailable {
		return CeremonyState{}, errUnavailable
	}
	id, ok := m.byHash[string(tokenHash)]
	if !ok {
		return CeremonyState{}, errInvalidCeremony
	}
	s := m.rows[id]
	if s.ConsumedAt != nil {
		return CeremonyState{}, errCeremonyConsumed
	}
	if !now.Before(s.ExpiresAt) {
		return CeremonyState{}, errCeremonyExpired
	}
	consumed := now
	s.ConsumedAt = &consumed
	m.rows[id] = s
	return cloneCeremony(s), nil
}

func cloneCeremony(s CeremonyState) CeremonyState {
	s.TokenHash = cloneBytes(s.TokenHash)
	s.SessionData = cloneBytes(s.SessionData)
	if s.UserID != nil {
		id := *s.UserID
		s.UserID = &id
	}
	if s.ConsumedAt != nil {
		t := *s.ConsumedAt
		s.ConsumedAt = &t
	}
	return s
}
