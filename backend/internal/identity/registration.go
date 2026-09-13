package identity

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// registrationRP is the Identity-internal WebAuthn registration surface.
// Tests substitute a fake; production wraps go-webauthn.
type registrationRP interface {
	BeginRegistration(user webauthn.User, opts ...webauthn.RegistrationOption) (*protocol.CredentialCreation, *webauthn.SessionData, error)
	ParseCreationResponse(raw []byte) (*protocol.ParsedCredentialCreationData, error)
	CreateCredential(user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialCreationData) (*webauthn.Credential, error)
}

type goWebAuthn struct {
	wa *webauthn.WebAuthn
}

func (g *goWebAuthn) BeginRegistration(user webauthn.User, opts ...webauthn.RegistrationOption) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	if g == nil || g.wa == nil {
		return nil, nil, errUnavailable
	}
	return g.wa.BeginRegistration(user, opts...)
}

func (g *goWebAuthn) ParseCreationResponse(raw []byte) (*protocol.ParsedCredentialCreationData, error) {
	if g == nil || g.wa == nil {
		return nil, errUnavailable
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(raw)
	if err != nil {
		return nil, errWebAuthnVerification
	}
	return parsed, nil
}

func (g *goWebAuthn) CreateCredential(user webauthn.User, session webauthn.SessionData, parsed *protocol.ParsedCredentialCreationData) (*webauthn.Credential, error) {
	if g == nil || g.wa == nil {
		return nil, errUnavailable
	}
	cred, err := g.wa.CreateCredential(user, session, parsed)
	if err != nil {
		return nil, errWebAuthnVerification
	}
	return cred, nil
}

// BeginRegistrationInput is trusted Relying Party input. Names are not Users-domain profile fields.
type BeginRegistrationInput struct {
	UserID      ID
	Name        string
	DisplayName string
}

// BeginRegistrationResult is returned once. RawToken must not be persisted or logged.
type BeginRegistrationResult struct {
	Creation *protocol.CredentialCreation
	RawToken string
}

// FinishRegistrationInput is the browser registration response plus the one-time ceremony token.
// UserID is the authenticated Identity user; it must match the ceremony binding.
type FinishRegistrationInput struct {
	UserID   ID
	RawToken string
	Response []byte
}

// Registration orchestrates WebAuthn passkey registration (no HTTP).
type Registration struct {
	rp         registrationRP
	passkeys   *Passkeys
	ceremonies *Ceremonies
	now        func() time.Time
	txns       transactor
	security   TxSecurityRecorder
}

func NewRegistration(wa *webauthn.WebAuthn, passkeys *Passkeys, ceremonies *Ceremonies, now func() time.Time) (*Registration, error) {
	if wa == nil {
		return nil, errInvalidWebAuthnConfig
	}
	return newRegistration(&goWebAuthn{wa: wa}, passkeys, ceremonies, now)
}

func newRegistration(rp registrationRP, passkeys *Passkeys, ceremonies *Ceremonies, now func() time.Time) (*Registration, error) {
	if rp == nil || passkeys == nil || ceremonies == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Registration{rp: rp, passkeys: passkeys, ceremonies: ceremonies, now: now}, nil
}

func (r *Registration) BeginRegistration(ctx context.Context, in BeginRegistrationInput) (BeginRegistrationResult, error) {
	if in.UserID.IsZero() {
		return BeginRegistrationResult{}, errZeroID
	}
	name := strings.TrimSpace(in.Name)
	display := strings.TrimSpace(in.DisplayName)
	if name == "" || display == "" {
		return BeginRegistrationResult{}, errInvalidRegistration
	}

	active, err := r.passkeys.ListActiveForUser(ctx, in.UserID)
	if err != nil {
		return BeginRegistrationResult{}, err
	}

	user := PasskeyUser{
		ID:          in.UserID,
		Name:        name,
		DisplayName: display,
		Passkeys:    active,
	}

	creation, session, err := r.rp.BeginRegistration(user, registrationOptions(active)...)
	if err != nil {
		return BeginRegistrationResult{}, mapRegistrationRPErr(err)
	}
	if creation == nil || session == nil {
		return BeginRegistrationResult{}, errUnavailable
	}

	sessionBytes, err := json.Marshal(session)
	if err != nil || len(sessionBytes) == 0 {
		return BeginRegistrationResult{}, errUnavailable
	}

	userID := in.UserID
	issued, err := r.ceremonies.Create(ctx, CeremonyRegistration, &userID, sessionBytes)
	if err != nil {
		return BeginRegistrationResult{}, err
	}
	return BeginRegistrationResult{Creation: creation, RawToken: issued.RawToken}, nil
}

func (r *Registration) FinishRegistration(ctx context.Context, in FinishRegistrationInput) (PasskeyCredential, error) {
	return r.FinishRegistrationWithSecurity(ctx, in, SecurityRecord{})
}

func (r *Registration) BindDurableSecurity(txns transactor, rec TxSecurityRecorder) {
	if r == nil {
		return
	}
	r.txns = txns
	r.security = rec
}

func (r *Registration) FinishRegistrationWithSecurity(ctx context.Context, in FinishRegistrationInput, rec SecurityRecord) (PasskeyCredential, error) {
	if in.UserID.IsZero() {
		return PasskeyCredential{}, errUnauthenticated
	}
	state, err := r.ceremonies.Consume(ctx, in.RawToken)
	if err != nil {
		return PasskeyCredential{}, err
	}
	if state.Kind != CeremonyRegistration {
		return PasskeyCredential{}, errCeremonyKind
	}
	if state.UserID == nil || state.UserID.IsZero() {
		return PasskeyCredential{}, errInvalidCeremony
	}
	if subtle.ConstantTimeCompare(state.UserID[:], in.UserID[:]) != 1 {
		return PasskeyCredential{}, errInvalidCeremony
	}

	var session webauthn.SessionData
	if err := json.Unmarshal(state.SessionData, &session); err != nil {
		return PasskeyCredential{}, errUnavailable
	}

	parsed, err := r.rp.ParseCreationResponse(in.Response)
	if err != nil {
		return PasskeyCredential{}, mapFinishRPErr(err)
	}

	active, err := r.passkeys.ListActiveForUser(ctx, *state.UserID)
	if err != nil {
		return PasskeyCredential{}, err
	}
	user := PasskeyUser{ID: *state.UserID, Passkeys: active}

	cred, err := r.rp.CreateCredential(user, session, parsed)
	if err != nil {
		return PasskeyCredential{}, mapFinishRPErr(err)
	}
	if cred == nil {
		return PasskeyCredential{}, errWebAuthnVerification
	}

	mapped, err := passkeyFromVerifiedCredential(*state.UserID, *cred, r.now())
	if err != nil {
		return PasskeyCredential{}, err
	}

	existing, err := r.passkeys.FindActiveByCredentialID(ctx, mapped.CredentialID)
	if err == nil && existing.Active() {
		return PasskeyCredential{}, errCredentialConflict
	}
	if err != nil && !errors.Is(err, errNotFound) {
		return PasskeyCredential{}, err
	}

	if err := r.persistPasskey(ctx, mapped, rec); err != nil {
		return PasskeyCredential{}, err
	}
	return mapped, nil
}

type passkeyMutationStore interface {
	transactor
	InsertPasskeyTx(ctx context.Context, tx transaction, credential PasskeyCredential) error
	RevokePasskeyTx(ctx context.Context, tx transaction, id ID, at time.Time) error
	RevokeSessionsForUserTx(ctx context.Context, tx transaction, userID ID, at time.Time) (int64, error)
}

func (r *Registration) persistPasskey(ctx context.Context, mapped PasskeyCredential, rec SecurityRecord) error {
	if store, ok := r.txns.(passkeyMutationStore); ok && r.security != nil && rec.Type != "" {
		tx, err := store.Begin(ctx)
		if err != nil {
			return mapStoreErr(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := store.InsertPasskeyTx(ctx, tx, mapped); err != nil {
			return mapPasskeyStoreErr(err)
		}
		if err := r.security.RecordOn(ctx, tx, rec); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return mapStoreErr(err)
		}
		return nil
	}
	if err := r.passkeys.Create(ctx, mapped); err != nil {
		return err
	}
	if rec.Type == "" || r.security == nil {
		return nil
	}
	return r.security.RecordOn(ctx, nil, rec)
}

func registrationOptions(active []PasskeyCredential) []webauthn.RegistrationOption {
	return []webauthn.RegistrationOption{
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(excludeDescriptors(active)),
	}
}

func excludeDescriptors(creds []PasskeyCredential) []protocol.CredentialDescriptor {
	wa := make(webauthn.Credentials, 0, len(creds))
	for _, c := range creds {
		wa = append(wa, c.WebAuthnCredential())
	}
	return wa.CredentialDescriptors()
}

func passkeyFromVerifiedCredential(userID ID, cred webauthn.Credential, now time.Time) (PasskeyCredential, error) {
	id, err := NewID()
	if err != nil {
		return PasskeyCredential{}, errUnavailable
	}
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		if t == "" {
			continue
		}
		transports = append(transports, string(t))
	}
	c := PasskeyCredential{
		ID:             id,
		UserID:         userID,
		CredentialID:   cloneBytes(cred.ID),
		PublicKey:      cloneBytes(cred.PublicKey),
		SignCount:      int64(cred.Authenticator.SignCount),
		BackupEligible: cred.Flags.BackupEligible,
		BackupState:    cred.Flags.BackupState,
		Transports:     transports,
		CreatedAt:      now,
	}
	if err := c.Validate(); err != nil {
		return PasskeyCredential{}, errWebAuthnVerification
	}
	return c, nil
}

func mapRegistrationRPErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, errInvalidWebAuthnConfig) {
		return err
	}
	return errUnavailable
}

func mapFinishRPErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errUnavailable) {
		return err
	}
	return errWebAuthnVerification
}
