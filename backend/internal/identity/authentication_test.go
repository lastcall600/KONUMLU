package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

func TestBeginAuthenticationStoresCeremonyAndRequiresUV(t *testing.T) {
	ctx := context.Background()
	auth, rp, _, cstore, _ := newTestAuthentication(t)

	out, err := auth.BeginAuthentication(ctx)
	if err != nil {
		t.Fatalf("BeginAuthentication: %v", err)
	}
	if out.RawToken == "" || out.Assertion == nil {
		t.Fatal("options and raw token must be returned")
	}
	if out.Assertion.Response.UserVerification != protocol.VerificationRequired {
		t.Fatalf("uv = %q, want required", out.Assertion.Response.UserVerification)
	}
	if len(rp.beginOpts) != 1 {
		t.Fatal("begin must request discoverable login options")
	}

	if len(cstore.rows) != 1 {
		t.Fatalf("stored ceremonies = %d, want 1", len(cstore.rows))
	}
	var stored CeremonyState
	for _, s := range cstore.rows {
		stored = s
	}
	if stored.Kind != CeremonyAuthentication || stored.UserID != nil {
		t.Fatalf("ceremony mismatch: %+v", stored)
	}
	if cstore.rawTokens[stored.ID] != "" {
		t.Fatal("raw token must not be persisted")
	}
	if bytes.Contains(stored.SessionData, []byte(out.RawToken)) {
		t.Fatal("raw token must not appear in session data")
	}

	var session webauthn.SessionData
	if err := json.Unmarshal(stored.SessionData, &session); err != nil {
		t.Fatalf("session data: %v", err)
	}
	if session.UserVerification != protocol.VerificationRequired {
		t.Fatalf("stored uv = %q, want required", session.UserVerification)
	}
	if len(session.UserID) != 0 {
		t.Fatal("discoverable session must not bind a user id")
	}
}

func TestFinishAuthenticationSuccessUpdatesCredentialAndReturnsUserID(t *testing.T) {
	ctx := context.Background()
	auth, rp, pstore, accounts, now := newTestAuthenticationReady(t)
	user, cred := seedActivePasskeyUser(t, pstore, accounts, now, 4)

	begun, err := auth.BeginAuthentication(ctx)
	if err != nil {
		t.Fatal(err)
	}

	rp.parsed = assertionParsed(cred.CredentialID, user.ID)
	rp.cred = &webauthn.Credential{
		ID: cloneBytes(cred.CredentialID),
		Flags: webauthn.CredentialFlags{
			BackupState: true,
		},
		Authenticator: webauthn.Authenticator{SignCount: 7},
	}

	got, err := auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: begun.RawToken,
		Response: []byte(`{"type":"public-key"}`),
	})
	if err != nil {
		t.Fatalf("FinishAuthentication: %v", err)
	}
	if got != user.ID {
		t.Fatalf("user id = %v, want %v", got, user.ID)
	}

	stored := pstore.must(t, cred.ID)
	if stored.SignCount != 7 || !stored.BackupState {
		t.Fatalf("credential state = %+v", stored)
	}
	if stored.LastUsedAt == nil || *stored.LastUsedAt != now() {
		t.Fatalf("last_used_at = %v", stored.LastUsedAt)
	}
	if rp.handlerUser == nil || rp.handlerUser.ID != user.ID {
		t.Fatal("discoverable handler must resolve the passkey user")
	}
	if !bytes.Equal(rp.handlerUser.WebAuthnID(), user.ID[:]) {
		t.Fatal("user handle must be the stable identity uuid")
	}
	if rp.handlerUser.Name == "" || strings.Contains(strings.ToLower(rp.handlerUser.Name), "email") {
		t.Fatal("placeholder name must be non-empty and non-profile")
	}

	if _, err := auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: begun.RawToken,
		Response: []byte(`{"type":"public-key"}`),
	}); err != errCeremonyConsumed {
		t.Fatalf("replay err = %v, want %v", err, errCeremonyConsumed)
	}
}

func TestFinishAuthenticationConsumesOnceAndRejectsWrongKind(t *testing.T) {
	ctx := context.Background()
	auth, _, pstore, _, now := newTestAuthenticationReady(t)
	_, cred := seedActivePasskeyUser(t, pstore, auth.accounts.(*memAccounts), now, 1)

	ceremonies, _, _ := newTestCeremonies(t)
	auth.ceremonies = ceremonies

	reg, err := ceremonies.Create(ctx, CeremonyRegistration, &cred.UserID, []byte(`{"challenge":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	before := pstore.must(t, cred.ID)

	if _, err := auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: reg.RawToken,
		Response: []byte(`{}`),
	}); err != errCeremonyKind {
		t.Fatalf("kind err = %v, want %v", err, errCeremonyKind)
	}
	if _, err := auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: reg.RawToken,
		Response: []byte(`{}`),
	}); err != errCeremonyConsumed {
		t.Fatalf("second finish err = %v, want %v", err, errCeremonyConsumed)
	}
	after := pstore.must(t, cred.ID)
	if after.SignCount != before.SignCount || after.LastUsedAt != nil {
		t.Fatal("wrong kind must not update credential")
	}
}

func TestFinishAuthenticationUnknownCredential(t *testing.T) {
	ctx := context.Background()
	auth, rp, pstore, _, now := newTestAuthenticationReady(t)
	seedActivePasskeyUser(t, pstore, auth.accounts.(*memAccounts), now, 1)

	begun, err := auth.BeginAuthentication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rp.parsed = assertionParsed([]byte{0xde, 0xad}, mustID(t))

	_, err = auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: begun.RawToken,
		Response: []byte(`{}`),
	})
	if err != errUnknownCredential {
		t.Fatalf("err = %v, want %v", err, errUnknownCredential)
	}
	if rp.validateCalls != 0 {
		t.Fatal("unknown credential must fail before verification")
	}
}

func TestFinishAuthenticationRevokedCredential(t *testing.T) {
	ctx := context.Background()
	auth, rp, pstore, accounts, now := newTestAuthenticationReady(t)
	_, cred := seedActivePasskeyUser(t, pstore, accounts, now, 2)
	revoked := now()
	c := pstore.must(t, cred.ID)
	c.RevokedAt = &revoked
	pstore.creds[c.ID] = c

	begun, err := auth.BeginAuthentication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rp.parsed = assertionParsed(cred.CredentialID, cred.UserID)

	_, err = auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: begun.RawToken,
		Response: []byte(`{}`),
	})
	if err != errCredentialRevoked {
		t.Fatalf("err = %v, want %v", err, errCredentialRevoked)
	}
	if rp.validateCalls != 0 {
		t.Fatal("revoked credential must fail before verification")
	}
}

func TestFinishAuthenticationDisabledAndDeletedUser(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	disabledAt := now

	t.Run("disabled", func(t *testing.T) {
		auth, rp, pstore, accounts, clock := newTestAuthenticationReady(t)
		user, cred := seedActivePasskeyUser(t, pstore, accounts, clock, 1)
		u := accounts.users[user.ID]
		u.DisabledAt = &disabledAt
		accounts.users[user.ID] = u

		begun, err := auth.BeginAuthentication(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rp.parsed = assertionParsed(cred.CredentialID, user.ID)
		_, err = auth.FinishAuthentication(ctx, FinishAuthenticationInput{
			RawToken: begun.RawToken, Response: []byte(`{}`),
		})
		if err != errAccountIneligible {
			t.Fatalf("err = %v, want %v", err, errAccountIneligible)
		}
		if rp.validateCalls != 0 {
			t.Fatal("disabled user must fail before verification")
		}
	})

	t.Run("deleted", func(t *testing.T) {
		auth, rp, pstore, accounts, clock := newTestAuthenticationReady(t)
		user, cred := seedActivePasskeyUser(t, pstore, accounts, clock, 1)
		u := accounts.users[user.ID]
		u.DeletedAt = &disabledAt
		accounts.users[user.ID] = u

		begun, err := auth.BeginAuthentication(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rp.parsed = assertionParsed(cred.CredentialID, user.ID)
		_, err = auth.FinishAuthentication(ctx, FinishAuthenticationInput{
			RawToken: begun.RawToken, Response: []byte(`{}`),
		})
		if err != errAccountIneligible {
			t.Fatalf("err = %v, want %v", err, errAccountIneligible)
		}
	})
}

func TestFinishAuthenticationFailedVerificationDoesNotUpdate(t *testing.T) {
	ctx := context.Background()
	auth, rp, pstore, accounts, now := newTestAuthenticationReady(t)
	_, cred := seedActivePasskeyUser(t, pstore, accounts, now, 6)

	begun, err := auth.BeginAuthentication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rp.parsed = assertionParsed(cred.CredentialID, cred.UserID)
	rp.validateErr = errors.New("library verification detail")

	_, err = auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: begun.RawToken,
		Response: []byte(`{"authenticatorData":"secret"}`),
	})
	if !errors.Is(err, errWebAuthnVerification) {
		t.Fatalf("err = %v, want %v", err, errWebAuthnVerification)
	}
	if strings.Contains(err.Error(), "library verification detail") ||
		strings.Contains(err.Error(), begun.RawToken) ||
		strings.Contains(err.Error(), "authenticatorData") {
		t.Fatalf("error leaked sensitive detail: %v", err)
	}
	stored := pstore.must(t, cred.ID)
	if stored.SignCount != 6 || stored.LastUsedAt != nil {
		t.Fatal("failed verification must not update credential")
	}
	if _, err := auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: begun.RawToken,
		Response: []byte(`{}`),
	}); err != errCeremonyConsumed {
		t.Fatalf("consumed after failed verify err = %v, want %v", err, errCeremonyConsumed)
	}
}

func TestFinishAuthenticationCounterConflictFailsClosed(t *testing.T) {
	ctx := context.Background()

	t.Run("clone warning", func(t *testing.T) {
		auth, rp, pstore, accounts, now := newTestAuthenticationReady(t)
		_, cred := seedActivePasskeyUser(t, pstore, accounts, now, 9)
		begun, err := auth.BeginAuthentication(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rp.parsed = assertionParsed(cred.CredentialID, cred.UserID)
		rp.cred = &webauthn.Credential{
			Authenticator: webauthn.Authenticator{SignCount: 9, CloneWarning: true},
			Flags:         webauthn.CredentialFlags{BackupState: true},
		}
		_, err = auth.FinishAuthentication(ctx, FinishAuthenticationInput{
			RawToken: begun.RawToken, Response: []byte(`{}`),
		})
		if err != errCounterConflict {
			t.Fatalf("err = %v, want %v", err, errCounterConflict)
		}
		stored := pstore.must(t, cred.ID)
		if stored.SignCount != 9 || stored.LastUsedAt != nil || stored.BackupState {
			t.Fatalf("clone warning must not update: %+v", stored)
		}
	})

	t.Run("monotonic decrease", func(t *testing.T) {
		auth, rp, pstore, accounts, now := newTestAuthenticationReady(t)
		_, cred := seedActivePasskeyUser(t, pstore, accounts, now, 9)
		begun, err := auth.BeginAuthentication(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rp.parsed = assertionParsed(cred.CredentialID, cred.UserID)
		rp.cred = &webauthn.Credential{
			Authenticator: webauthn.Authenticator{SignCount: 3},
		}
		_, err = auth.FinishAuthentication(ctx, FinishAuthenticationInput{
			RawToken: begun.RawToken, Response: []byte(`{}`),
		})
		if err != errCounterConflict {
			t.Fatalf("err = %v, want %v", err, errCounterConflict)
		}
		stored := pstore.must(t, cred.ID)
		if stored.SignCount != 9 || stored.LastUsedAt != nil {
			t.Fatal("decreased sign_count must not be stored")
		}
	})
}

func TestAuthenticationStorageFailureFailsClosed(t *testing.T) {
	ctx := context.Background()
	auth, rp, pstore, cstore, accounts, now := newTestAuthenticationFull(t)
	_, cred := seedActivePasskeyUser(t, pstore, accounts, now, 2)

	cstore.unavailable = true
	if _, err := auth.BeginAuthentication(ctx); !errors.Is(err, errUnavailable) {
		t.Fatalf("begin err = %v, want %v", err, errUnavailable)
	}
	cstore.unavailable = false

	begun, err := auth.BeginAuthentication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rp.parsed = assertionParsed(cred.CredentialID, cred.UserID)
	pstore.unavailable = true
	if _, err := auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: begun.RawToken, Response: []byte(`{}`),
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("lookup err = %v, want %v", err, errUnavailable)
	}
	pstore.unavailable = false

	begun, err = auth.BeginAuthentication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rp.parsed = assertionParsed(cred.CredentialID, cred.UserID)
	rp.cred = &webauthn.Credential{Authenticator: webauthn.Authenticator{SignCount: 4}}
	pstore.updateFail = true
	if _, err := auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: begun.RawToken, Response: []byte(`{}`),
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("update err = %v, want %v", err, errUnavailable)
	}
	stored := pstore.must(t, cred.ID)
	if stored.SignCount != 2 || stored.LastUsedAt != nil {
		t.Fatal("storage failure must not update credential")
	}
	if rp.validateCalls != 1 {
		t.Fatalf("validate calls = %d, want 1", rp.validateCalls)
	}

	accounts.unavailable = true
	begun, err = auth.BeginAuthentication(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pstore.updateFail = false
	rp.parsed = assertionParsed(cred.CredentialID, cred.UserID)
	if _, err := auth.FinishAuthentication(ctx, FinishAuthenticationInput{
		RawToken: begun.RawToken, Response: []byte(`{}`),
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("account store err = %v, want %v", err, errUnavailable)
	}
}

func newTestAuthentication(t *testing.T) (*Authentication, *fakeAuthRP, *memPasskeyStore, *memCeremonyStore, func() time.Time) {
	t.Helper()
	auth, rp, pstore, cstore, _, now := newTestAuthenticationFull(t)
	return auth, rp, pstore, cstore, now
}

func newTestAuthenticationReady(t *testing.T) (*Authentication, *fakeAuthRP, *memPasskeyStore, *memAccounts, func() time.Time) {
	t.Helper()
	auth, rp, pstore, _, accounts, now := newTestAuthenticationFull(t)
	return auth, rp, pstore, accounts, now
}

func newTestAuthenticationFull(t *testing.T) (*Authentication, *fakeAuthRP, *memPasskeyStore, *memCeremonyStore, *memAccounts, func() time.Time) {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }
	pstore := newMemPasskeyStore()
	cstore := newMemCeremonyStore()
	accounts := newMemAccounts()
	passkeys, err := NewPasskeys(pstore, now)
	if err != nil {
		t.Fatal(err)
	}
	ceremonies, err := NewCeremonies(cstore, CeremonyPolicy{TTL: 2 * time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	rp := &fakeAuthRP{}
	auth, err := newAuthentication(rp, passkeys, ceremonies, accounts, now)
	if err != nil {
		t.Fatal(err)
	}
	return auth, rp, pstore, cstore, accounts, now
}

func seedActivePasskeyUser(t *testing.T, pstore *memPasskeyStore, accounts *memAccounts, now func() time.Time, signCount int64) (User, PasskeyCredential) {
	t.Helper()
	user := accounts.put(t, User{})
	cred := validPasskey(t, now())
	cred.UserID = user.ID
	cred.SignCount = signCount
	if err := pstore.InsertPasskey(context.Background(), cred); err != nil {
		t.Fatal(err)
	}
	return user, cred
}

func assertionParsed(credentialID []byte, userID ID) *protocol.ParsedCredentialAssertionData {
	return &protocol.ParsedCredentialAssertionData{
		ParsedPublicKeyCredential: protocol.ParsedPublicKeyCredential{RawID: cloneBytes(credentialID)},
		Response: protocol.ParsedAssertionResponse{
			UserHandle: cloneBytes(userID[:]),
		},
	}
}

type fakeAuthRP struct {
	beginErr      error
	parseErr      error
	validateErr   error
	parsed        *protocol.ParsedCredentialAssertionData
	cred          *webauthn.Credential
	beginOpts     []webauthn.LoginOption
	validateCalls int
	handlerUser   *PasskeyUser
}

func (f *fakeAuthRP) BeginDiscoverableLogin(opts ...webauthn.LoginOption) (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	f.beginOpts = opts
	if f.beginErr != nil {
		return nil, nil, f.beginErr
	}
	assertion := &protocol.CredentialAssertion{}
	for _, opt := range opts {
		opt(&assertion.Response)
	}
	session := &webauthn.SessionData{
		Challenge:        "server-session",
		UserVerification: assertion.Response.UserVerification,
	}
	return assertion, session, nil
}

func (f *fakeAuthRP) ParseAssertionResponse(raw []byte) (*protocol.ParsedCredentialAssertionData, error) {
	if f.parseErr != nil {
		return nil, f.parseErr
	}
	if len(raw) == 0 {
		return nil, errWebAuthnVerification
	}
	if f.parsed != nil {
		return f.parsed, nil
	}
	return &protocol.ParsedCredentialAssertionData{}, nil
}

func (f *fakeAuthRP) ValidateDiscoverableLogin(handler webauthn.DiscoverableUserHandler, session webauthn.SessionData, parsed *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, error) {
	f.validateCalls++
	if f.validateErr != nil {
		return nil, f.validateErr
	}
	if handler != nil && parsed != nil {
		user, err := handler(parsed.RawID, parsed.Response.UserHandle)
		if err != nil {
			return nil, err
		}
		if pu, ok := user.(PasskeyUser); ok {
			cp := pu
			f.handlerUser = &cp
		}
	}
	if f.cred != nil {
		return f.cred, nil
	}
	return &webauthn.Credential{
		Authenticator: webauthn.Authenticator{SignCount: 1},
	}, nil
}

type memAccounts struct {
	users       map[ID]User
	unavailable bool
}

func newMemAccounts() *memAccounts {
	return &memAccounts{users: make(map[ID]User)}
}

func (m *memAccounts) put(t *testing.T, u User) User {
	t.Helper()
	if u.ID.IsZero() {
		u.ID = mustID(t)
	}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
		u.UpdatedAt = now
	}
	m.users[u.ID] = u
	return u
}

func (m *memAccounts) GetUser(ctx context.Context, id ID) (User, error) {
	if m.unavailable {
		return User{}, errUnavailable
	}
	u, ok := m.users[id]
	if !ok {
		return User{}, errNotFound
	}
	return u, nil
}
