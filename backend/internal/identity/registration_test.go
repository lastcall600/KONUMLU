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

func TestBeginRegistrationStoresCeremonyAndRequiresUV(t *testing.T) {
	ctx := context.Background()
	reg, rp, pstore, cstore, now := newTestRegistration(t)
	userID := mustID(t)
	existing := validPasskey(t, now())
	existing.UserID = userID
	existing.CredentialID = []byte{0x0a, 0x0b}
	if err := pstore.InsertPasskey(ctx, existing); err != nil {
		t.Fatal(err)
	}

	out, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		UserID:      userID,
		Name:        "handle",
		DisplayName: "Display",
	})
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if out.RawToken == "" || out.Creation == nil {
		t.Fatal("options and raw token must be returned")
	}
	if out.Creation.Response.AuthenticatorSelection.UserVerification != protocol.VerificationRequired {
		t.Fatalf("uv = %q, want required", out.Creation.Response.AuthenticatorSelection.UserVerification)
	}
	if out.Creation.Response.Attestation != "" {
		t.Fatal("attestation policy must remain unset")
	}
	if out.Creation.Response.AuthenticatorSelection.ResidentKey != "" ||
		out.Creation.Response.AuthenticatorSelection.AuthenticatorAttachment != "" {
		t.Fatal("resident-key and attachment must remain unset")
	}
	if len(out.Creation.Response.CredentialExcludeList) != 1 ||
		!bytes.Equal(out.Creation.Response.CredentialExcludeList[0].CredentialID, existing.CredentialID) {
		t.Fatalf("excluded = %+v", out.Creation.Response.CredentialExcludeList)
	}

	passkeyUser, ok := rp.user.(PasskeyUser)
	if !ok || passkeyUser.ID != userID || passkeyUser.Name != "handle" || passkeyUser.DisplayName != "Display" {
		t.Fatalf("adapter mismatch: %+v", rp.user)
	}
	if len(passkeyUser.Passkeys) != 1 || !bytes.Equal(passkeyUser.Passkeys[0].CredentialID, existing.CredentialID) {
		t.Fatal("adapter must expose active credentials")
	}

	if len(cstore.rows) != 1 {
		t.Fatalf("stored ceremonies = %d, want 1", len(cstore.rows))
	}
	var stored CeremonyState
	for _, s := range cstore.rows {
		stored = s
	}
	if stored.Kind != CeremonyRegistration || stored.UserID == nil || *stored.UserID != userID {
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
}

func TestFinishRegistrationSuccessPersistsMappedCredentialOnce(t *testing.T) {
	ctx := context.Background()
	reg, rp, pstore, _, now := newTestRegistration(t)
	userID := mustID(t)
	begun, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		UserID: userID, Name: "handle", DisplayName: "Display",
	})
	if err != nil {
		t.Fatal(err)
	}

	rp.cred = &webauthn.Credential{
		ID:        []byte{0x11, 0x22, 0x33},
		PublicKey: []byte{0x44, 0x55, 0x66, 0x77},
		Transport: []protocol.AuthenticatorTransport{protocol.Internal, protocol.USB},
		Flags: webauthn.CredentialFlags{
			BackupEligible: true,
			BackupState:    true,
		},
		Authenticator: webauthn.Authenticator{SignCount: 3},
		Attestation: webauthn.CredentialAttestation{
			Object: []byte("attestation-blob-must-not-persist"),
		},
	}

	got, err := reg.FinishRegistration(ctx, FinishRegistrationInput{
		UserID:   userID,
		RawToken: begun.RawToken,
		Response: []byte(`{"type":"public-key"}`),
	})
	if err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
	if got.UserID != userID || !bytes.Equal(got.CredentialID, rp.cred.ID) || !bytes.Equal(got.PublicKey, rp.cred.PublicKey) {
		t.Fatalf("mapped mismatch: %+v", got)
	}
	if got.SignCount != 3 || !got.BackupEligible || !got.BackupState {
		t.Fatalf("flags/count mismatch: %+v", got)
	}
	if len(got.Transports) != 2 || got.Transports[0] != "internal" || got.Transports[1] != "usb" {
		t.Fatalf("transports = %v", got.Transports)
	}
	stored := pstore.must(t, got.ID)
	if stored.RevokedAt != nil || stored.LastUsedAt != nil {
		t.Fatal("new credential must not be revoked or last-used")
	}
	if bytes.Contains(stored.PublicKey, []byte("attestation")) || bytes.Contains(stored.CredentialID, []byte("attestation")) {
		t.Fatal("attestation blob must not be persisted")
	}
	if stored.CreatedAt != now() {
		t.Fatalf("created_at = %v", stored.CreatedAt)
	}

	if _, err := reg.FinishRegistration(ctx, FinishRegistrationInput{
		UserID:   userID,
		RawToken: begun.RawToken,
		Response: []byte(`{"type":"public-key"}`),
	}); err != errCeremonyConsumed {
		t.Fatalf("replay err = %v, want %v", err, errCeremonyConsumed)
	}
	list, err := pstore.ListActivePasskeysForUser(ctx, userID)
	if err != nil || len(list) != 1 {
		t.Fatalf("persist count = %d err = %v", len(list), err)
	}
}

func TestFinishRegistrationConsumesOnceAndRejectsWrongKind(t *testing.T) {
	ctx := context.Background()
	reg, _, pstore, _, _ := newTestRegistration(t)
	ceremonies, _, _ := newTestCeremonies(t)
	reg.ceremonies = ceremonies

	auth, err := ceremonies.Create(ctx, CeremonyAuthentication, nil, []byte(`{"challenge":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.FinishRegistration(ctx, FinishRegistrationInput{
		UserID:   mustID(t),
		RawToken: auth.RawToken,
		Response: []byte(`{}`),
	}); err != errCeremonyKind {
		t.Fatalf("kind err = %v, want %v", err, errCeremonyKind)
	}
	if _, err := reg.FinishRegistration(ctx, FinishRegistrationInput{
		UserID:   mustID(t),
		RawToken: auth.RawToken,
		Response: []byte(`{}`),
	}); err != errCeremonyConsumed {
		t.Fatalf("second finish err = %v, want %v", err, errCeremonyConsumed)
	}
	if len(pstore.creds) != 0 {
		t.Fatal("wrong kind must not persist a credential")
	}
}

func TestFinishRegistrationFailedVerificationDoesNotPersist(t *testing.T) {
	ctx := context.Background()
	reg, rp, pstore, _, _ := newTestRegistration(t)
	userID := mustID(t)
	begun, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		UserID: userID, Name: "handle", DisplayName: "Display",
	})
	if err != nil {
		t.Fatal(err)
	}
	rp.createErr = errors.New("library verification detail")

	_, err = reg.FinishRegistration(ctx, FinishRegistrationInput{
		UserID:   userID,
		RawToken: begun.RawToken,
		Response: []byte(`{"attestationObject":"secret"}`),
	})
	if !errors.Is(err, errWebAuthnVerification) {
		t.Fatalf("err = %v, want %v", err, errWebAuthnVerification)
	}
	if strings.Contains(err.Error(), "library verification detail") ||
		strings.Contains(err.Error(), begun.RawToken) ||
		strings.Contains(err.Error(), "attestationObject") {
		t.Fatalf("error leaked sensitive detail: %v", err)
	}
	if len(pstore.creds) != 0 {
		t.Fatal("failed verification must not persist")
	}
	if _, err := reg.FinishRegistration(ctx, FinishRegistrationInput{
		UserID:   userID,
		RawToken: begun.RawToken,
		Response: []byte(`{}`),
	}); err != errCeremonyConsumed {
		t.Fatalf("consumed after failed verify err = %v, want %v", err, errCeremonyConsumed)
	}
}

func TestRegistrationStorageFailureFailsClosed(t *testing.T) {
	ctx := context.Background()
	reg, rp, pstore, cstore, _ := newTestRegistration(t)
	userID := mustID(t)

	cstore.unavailable = true
	if _, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		UserID: userID, Name: "handle", DisplayName: "Display",
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("begin err = %v, want %v", err, errUnavailable)
	}
	cstore.unavailable = false

	pstore.unavailable = true
	if _, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		UserID: userID, Name: "handle", DisplayName: "Display",
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("list err = %v, want %v", err, errUnavailable)
	}
	pstore.unavailable = false

	begun, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		UserID: userID, Name: "handle", DisplayName: "Display",
	})
	if err != nil {
		t.Fatal(err)
	}
	pstore.insertFail = true
	if _, err := reg.FinishRegistration(ctx, FinishRegistrationInput{
		UserID:   userID,
		RawToken: begun.RawToken,
		Response: []byte(`{}`),
	}); !errors.Is(err, errUnavailable) {
		t.Fatalf("finish store err = %v, want %v", err, errUnavailable)
	}
	if len(pstore.creds) != 0 {
		t.Fatal("storage failure must not persist a credential")
	}
	if rp.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1 (verify then fail closed on persist)", rp.createCalls)
	}
}

func TestBeginRegistrationRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	reg, _, _, _, _ := newTestRegistration(t)
	if _, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		Name: "handle", DisplayName: "Display",
	}); err != errZeroID {
		t.Fatalf("zero user err = %v, want %v", err, errZeroID)
	}
	if _, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		UserID: mustID(t), DisplayName: "Display",
	}); err != errInvalidRegistration {
		t.Fatalf("empty name err = %v, want %v", err, errInvalidRegistration)
	}
}

func TestFinishRegistrationRejectsMismatchedAuthenticatedUser(t *testing.T) {
	ctx := context.Background()
	reg, rp, pstore, _, _ := newTestRegistration(t)
	owner := mustID(t)
	other := mustID(t)
	begun, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		UserID: owner, Name: "handle", DisplayName: "Display",
	})
	if err != nil {
		t.Fatal(err)
	}
	rp.cred = &webauthn.Credential{
		ID:        []byte{0x11, 0x22, 0x33},
		PublicKey: []byte{0x44, 0x55, 0x66, 0x77},
	}
	if _, err := reg.FinishRegistration(ctx, FinishRegistrationInput{
		UserID:   other,
		RawToken: begun.RawToken,
		Response: []byte(`{"type":"public-key"}`),
	}); err != errInvalidCeremony {
		t.Fatalf("mismatch err = %v, want %v", err, errInvalidCeremony)
	}
	if rp.createCalls != 0 {
		t.Fatal("mismatched user must not create a credential")
	}
	if len(pstore.creds) != 0 {
		t.Fatal("mismatched user must not persist a passkey")
	}
	if _, err := reg.FinishRegistration(ctx, FinishRegistrationInput{
		UserID:   owner,
		RawToken: begun.RawToken,
		Response: []byte(`{"type":"public-key"}`),
	}); err != errCeremonyConsumed {
		t.Fatalf("stolen ceremony must remain consumed: %v", err)
	}
}

func TestFinishRegistrationRequiresAuthenticatedUserID(t *testing.T) {
	ctx := context.Background()
	reg, _, _, cstore, _ := newTestRegistration(t)
	owner := mustID(t)
	begun, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		UserID: owner, Name: "handle", DisplayName: "Display",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.FinishRegistration(ctx, FinishRegistrationInput{
		RawToken: begun.RawToken,
		Response: []byte(`{"type":"public-key"}`),
	}); err != errUnauthenticated {
		t.Fatalf("zero user err = %v, want %v", err, errUnauthenticated)
	}
	if len(cstore.rows) != 1 {
		t.Fatal("missing authenticated user must not consume the ceremony")
	}
}

func TestFinishRegistrationCredentialConflict(t *testing.T) {
	ctx := context.Background()
	reg, rp, pstore, now := newTestRegistrationConflict(t)
	userID := mustID(t)
	begun, err := reg.BeginRegistration(ctx, BeginRegistrationInput{
		UserID: userID, Name: "handle", DisplayName: "Display",
	})
	if err != nil {
		t.Fatal(err)
	}
	dup := validPasskey(t, now())
	dup.UserID = userID
	dup.CredentialID = []byte{0x11, 0x22, 0x33}
	if err := pstore.InsertPasskey(ctx, dup); err != nil {
		t.Fatal(err)
	}
	rp.cred = &webauthn.Credential{
		ID:        dup.CredentialID,
		PublicKey: []byte{0x01, 0x02, 0x03, 0x04},
	}
	if _, err := reg.FinishRegistration(ctx, FinishRegistrationInput{
		UserID:   userID,
		RawToken: begun.RawToken,
		Response: []byte(`{}`),
	}); err != errCredentialConflict {
		t.Fatalf("conflict err = %v, want %v", err, errCredentialConflict)
	}
}

func newTestRegistrationConflict(t *testing.T) (*Registration, *fakeRP, *memPasskeyStore, func() time.Time) {
	t.Helper()
	reg, rp, pstore, _, now := newTestRegistration(t)
	return reg, rp, pstore, now
}

func newTestRegistration(t *testing.T) (*Registration, *fakeRP, *memPasskeyStore, *memCeremonyStore, func() time.Time) {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC) }
	pstore := newMemPasskeyStore()
	cstore := newMemCeremonyStore()
	passkeys, err := NewPasskeys(pstore, now)
	if err != nil {
		t.Fatal(err)
	}
	ceremonies, err := NewCeremonies(cstore, CeremonyPolicy{TTL: 2 * time.Minute}, now)
	if err != nil {
		t.Fatal(err)
	}
	rp := &fakeRP{}
	reg, err := newRegistration(rp, passkeys, ceremonies, now)
	if err != nil {
		t.Fatal(err)
	}
	return reg, rp, pstore, cstore, now
}

type fakeRP struct {
	user        webauthn.User
	createErr   error
	parseErr    error
	beginErr    error
	cred        *webauthn.Credential
	createCalls int
}

func (f *fakeRP) BeginRegistration(user webauthn.User, opts ...webauthn.RegistrationOption) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	f.user = user
	if f.beginErr != nil {
		return nil, nil, f.beginErr
	}
	creation := &protocol.CredentialCreation{}
	for _, opt := range opts {
		opt(&creation.Response)
	}
	session := &webauthn.SessionData{
		Challenge:        "server-session",
		UserID:           user.WebAuthnID(),
		UserVerification: creation.Response.AuthenticatorSelection.UserVerification,
	}
	return creation, session, nil
}

func (f *fakeRP) ParseCreationResponse(raw []byte) (*protocol.ParsedCredentialCreationData, error) {
	if f.parseErr != nil {
		return nil, f.parseErr
	}
	if len(raw) == 0 {
		return nil, errWebAuthnVerification
	}
	return &protocol.ParsedCredentialCreationData{}, nil
}

func (f *fakeRP) CreateCredential(user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialCreationData) (*webauthn.Credential, error) {
	f.createCalls++
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.cred != nil {
		return f.cred, nil
	}
	return &webauthn.Credential{
		ID:        []byte{0x99},
		PublicKey: []byte{0xaa, 0xbb, 0xcc},
	}, nil
}
