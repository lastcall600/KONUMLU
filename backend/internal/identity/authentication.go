package identity

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// authenticationRP is the Identity-internal WebAuthn assertion surface.
// Tests substitute a fake; production wraps go-webauthn.
type authenticationRP interface {
	BeginDiscoverableLogin(opts ...webauthn.LoginOption) (*protocol.CredentialAssertion, *webauthn.SessionData, error)
	ParseAssertionResponse(raw []byte) (*protocol.ParsedCredentialAssertionData, error)
	ValidateDiscoverableLogin(handler webauthn.DiscoverableUserHandler, session webauthn.SessionData, parsed *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, error)
}

type accountReader interface {
	GetUser(ctx context.Context, id ID) (User, error)
}

func (g *goWebAuthn) BeginDiscoverableLogin(opts ...webauthn.LoginOption) (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	if g == nil || g.wa == nil {
		return nil, nil, errUnavailable
	}
	return g.wa.BeginDiscoverableLogin(opts...)
}

func (g *goWebAuthn) ParseAssertionResponse(raw []byte) (*protocol.ParsedCredentialAssertionData, error) {
	if g == nil || g.wa == nil {
		return nil, errUnavailable
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(raw)
	if err != nil {
		return nil, errWebAuthnVerification
	}
	return parsed, nil
}

func (g *goWebAuthn) ValidateDiscoverableLogin(handler webauthn.DiscoverableUserHandler, session webauthn.SessionData, parsed *protocol.ParsedCredentialAssertionData) (*webauthn.Credential, error) {
	if g == nil || g.wa == nil {
		return nil, errUnavailable
	}
	cred, err := g.wa.ValidateDiscoverableLogin(handler, session, parsed)
	if err != nil {
		return nil, mapAuthVerifyErr(err)
	}
	return cred, nil
}

// BeginAuthenticationResult is returned once. RawToken must not be persisted or logged.
type BeginAuthenticationResult struct {
	Assertion *protocol.CredentialAssertion
	RawToken  string
}

// FinishAuthenticationInput is the browser assertion response plus the one-time ceremony token.
type FinishAuthenticationInput struct {
	RawToken string
	Response []byte
}

// Authentication orchestrates WebAuthn passkey authentication (no HTTP, no session issuance).
type Authentication struct {
	rp         authenticationRP
	passkeys   *Passkeys
	ceremonies *Ceremonies
	accounts   accountReader
	now        func() time.Time
}

func NewAuthentication(wa *webauthn.WebAuthn, passkeys *Passkeys, ceremonies *Ceremonies, accounts accountReader, now func() time.Time) (*Authentication, error) {
	if wa == nil {
		return nil, errInvalidWebAuthnConfig
	}
	return newAuthentication(&goWebAuthn{wa: wa}, passkeys, ceremonies, accounts, now)
}

func newAuthentication(rp authenticationRP, passkeys *Passkeys, ceremonies *Ceremonies, accounts accountReader, now func() time.Time) (*Authentication, error) {
	if rp == nil || passkeys == nil || ceremonies == nil || accounts == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Authentication{rp: rp, passkeys: passkeys, ceremonies: ceremonies, accounts: accounts, now: now}, nil
}

func (a *Authentication) BeginAuthentication(ctx context.Context) (BeginAuthenticationResult, error) {
	assertion, session, err := a.rp.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return BeginAuthenticationResult{}, mapAuthBeginErr(err)
	}
	if assertion == nil || session == nil {
		return BeginAuthenticationResult{}, errUnavailable
	}

	sessionBytes, err := json.Marshal(session)
	if err != nil || len(sessionBytes) == 0 {
		return BeginAuthenticationResult{}, errUnavailable
	}

	issued, err := a.ceremonies.Create(ctx, CeremonyAuthentication, nil, sessionBytes)
	if err != nil {
		return BeginAuthenticationResult{}, err
	}
	return BeginAuthenticationResult{Assertion: assertion, RawToken: issued.RawToken}, nil
}

func (a *Authentication) FinishAuthentication(ctx context.Context, in FinishAuthenticationInput) (ID, error) {
	state, err := a.ceremonies.Consume(ctx, in.RawToken)
	if err != nil {
		return ID{}, err
	}
	if state.Kind != CeremonyAuthentication {
		return ID{}, errCeremonyKind
	}

	var session webauthn.SessionData
	if err := json.Unmarshal(state.SessionData, &session); err != nil {
		return ID{}, errUnavailable
	}

	parsed, err := a.rp.ParseAssertionResponse(in.Response)
	if err != nil {
		return ID{}, mapAuthVerifyErr(err)
	}
	if parsed == nil {
		return ID{}, errWebAuthnVerification
	}

	cred, user, err := a.resolveAssertionSubject(ctx, parsed.RawID)
	if err != nil {
		return ID{}, err
	}

	verified, err := a.rp.ValidateDiscoverableLogin(a.discoverableUser(ctx), session, parsed)
	if err != nil {
		return ID{}, mapAuthVerifyErr(err)
	}
	if verified == nil {
		return ID{}, errWebAuthnVerification
	}
	if verified.Authenticator.CloneWarning {
		return ID{}, errCounterConflict
	}

	if err := a.passkeys.RecordSuccessfulUse(ctx, cred.ID, int64(verified.Authenticator.SignCount), verified.Flags.BackupState); err != nil {
		if errors.Is(err, errSignCountNotMonotonic) {
			return ID{}, errCounterConflict
		}
		return ID{}, err
	}
	return user.ID, nil
}

func (a *Authentication) discoverableUser(ctx context.Context) webauthn.DiscoverableUserHandler {
	return func(rawID, userHandle []byte) (webauthn.User, error) {
		cred, user, err := a.resolveAssertionSubject(ctx, rawID)
		if err != nil {
			return nil, err
		}
		return passkeyUserForAuthentication(user, cred), nil
	}
}

func (a *Authentication) resolveAssertionSubject(ctx context.Context, credentialID []byte) (PasskeyCredential, User, error) {
	if len(credentialID) == 0 {
		return PasskeyCredential{}, User{}, errUnknownCredential
	}
	cred, err := a.passkeys.GetByCredentialID(ctx, credentialID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return PasskeyCredential{}, User{}, errUnknownCredential
		}
		return PasskeyCredential{}, User{}, err
	}
	if cred.RevokedAt != nil {
		return PasskeyCredential{}, User{}, errCredentialRevoked
	}
	if !cred.Active() {
		return PasskeyCredential{}, User{}, errUnknownCredential
	}

	user, err := a.accounts.GetUser(ctx, cred.UserID)
	if err != nil {
		return PasskeyCredential{}, User{}, mapLookupErr(err, errAccountIneligible)
	}
	if !user.EligibleForSession() || user.ID != cred.UserID {
		return PasskeyCredential{}, User{}, errAccountIneligible
	}
	return cred, user, nil
}

func passkeyUserForAuthentication(user User, cred PasskeyCredential) PasskeyUser {
	placeholder := authenticationPlaceholderName(user.ID)
	return PasskeyUser{
		ID:          user.ID,
		Name:        placeholder,
		DisplayName: placeholder,
		Passkeys:    []PasskeyCredential{cred},
	}
}

func authenticationPlaceholderName(id ID) string {
	return "identity-" + hex.EncodeToString(id[:])
}

func mapAuthBeginErr(err error) error {
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

func mapAuthVerifyErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errUnavailable) ||
		errors.Is(err, errUnknownCredential) ||
		errors.Is(err, errCredentialRevoked) ||
		errors.Is(err, errAccountIneligible) ||
		errors.Is(err, errCounterConflict) {
		return err
	}
	return errWebAuthnVerification
}
