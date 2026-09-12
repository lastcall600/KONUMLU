package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"

	"backend/internal/identity"
	"backend/internal/notifications/contracts"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
	csrfSecretBytes   = 32
)

type authenticator interface {
	BeginAuthentication(ctx context.Context) (identity.BeginAuthenticationResult, error)
	FinishAuthentication(ctx context.Context, in identity.FinishAuthenticationInput) (identity.ID, error)
	BeginStepUp(ctx context.Context, userID identity.ID) (identity.BeginAuthenticationResult, error)
	FinishStepUp(ctx context.Context, userID identity.ID, in identity.FinishAuthenticationInput) error
}

type sessionManager interface {
	Create(ctx context.Context, userID identity.ID, deviceID *identity.ID) (identity.IssuedSession, error)
	Resolve(ctx context.Context, rawToken string) (identity.Session, error)
	Revoke(ctx context.Context, sessionID identity.ID) error
	ListActiveForUser(ctx context.Context, userID identity.ID) ([]identity.Session, error)
	RevokeOthers(ctx context.Context, userID, keepSessionID identity.ID) error
	GetOwned(ctx context.Context, actorUserID, sessionID identity.ID) (identity.Session, error)
}

type stepUpManager interface {
	Grant(ctx context.Context, session identity.Session) error
	Require(ctx context.Context, session identity.Session, op identity.SensitiveOperation) error
	Clear(ctx context.Context, sessionID identity.ID)
}

type credentialGuard interface {
	ListPasskeys(ctx context.Context, userID identity.ID) ([]identity.PasskeyCredential, error)
	RemovePasskey(ctx context.Context, actorUserID, credentialID identity.ID) error
}

type passkeyBootstrap interface {
	GrantSignup(ctx context.Context, session identity.Session) error
	GrantPassword(ctx context.Context, session identity.Session, password []byte) error
	Allow(ctx context.Context, session identity.Session, op identity.SensitiveOperation) error
	Consume(ctx context.Context, sessionID identity.ID)
	Clear(ctx context.Context, sessionID identity.ID)
}

type identifierResolver interface {
	ResolveVerified(ctx context.Context, kind identity.IdentifierKind, raw string) (identity.User, error)
}

type passwordVerifier interface {
	Verify(ctx context.Context, userID identity.ID, password []byte) (identity.PasswordVerifyResult, error)
	DummyVerify(password []byte)
}

type signupVerification interface {
	StartSignupVerification(ctx context.Context, in identity.StartSignupVerificationInput) (identity.StartSignupVerificationResult, error)
	FinishSignupVerification(ctx context.Context, in identity.FinishSignupVerificationInput) (identity.FinishSignupVerificationResult, error)
}

type signupCompletion interface {
	CompleteSignup(ctx context.Context, in identity.CompleteSignupInput) (identity.CompleteSignupResult, error)
}

type passwordReset interface {
	StartPasswordReset(ctx context.Context, in identity.StartPasswordResetInput) (identity.StartPasswordResetResult, error)
	VerifyPasswordReset(ctx context.Context, in identity.VerifyPasswordResetInput) (identity.VerifyPasswordResetResult, error)
	CompletePasswordReset(ctx context.Context, in identity.CompletePasswordResetInput) (identity.CompletePasswordResetResult, error)
}

type passkeyRegistrar interface {
	BeginRegistration(ctx context.Context, in identity.BeginRegistrationInput) (identity.BeginRegistrationResult, error)
	FinishRegistration(ctx context.Context, in identity.FinishRegistrationInput) (identity.PasskeyCredential, error)
}

// Handler is the browser session HTTP adapter. It does not own Identity rules.
type Handler struct {
	auth        authenticator
	sessions    sessionManager
	identifiers identifierResolver
	passwords   passwordVerifier
	signup      signupVerification
	accounts    signupCompletion
	reset       passwordReset
	register    passkeyRegistrar
	stepUp      stepUpManager
	credentials credentialGuard
	bootstrap   passkeyBootstrap
	guard       *AbuseGuard
	origins     map[string]struct{}
}

func New(auth authenticator, sessions sessionManager, identifiers identifierResolver, passwords passwordVerifier, signup signupVerification, accounts signupCompletion, register passkeyRegistrar, reset passwordReset, stepUp stepUpManager, credentials credentialGuard, bootstrap passkeyBootstrap, allowedOrigins []string, guard *AbuseGuard) (*Handler, error) {
	if auth == nil || sessions == nil || identifiers == nil || passwords == nil || signup == nil || accounts == nil || register == nil || reset == nil || stepUp == nil || credentials == nil || bootstrap == nil || guard == nil {
		return nil, identity.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, identity.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, identity.ErrUnavailable
	}
	return &Handler{
		auth:        auth,
		sessions:    sessions,
		identifiers: identifiers,
		passwords:   passwords,
		signup:      signup,
		accounts:    accounts,
		reset:       reset,
		register:    register,
		stepUp:      stepUp,
		credentials: credentials,
		bootstrap:   bootstrap,
		guard:       guard,
		origins:     origins,
	}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/passkey/register/begin", h.beginPasskeyRegister)
	mux.HandleFunc("POST /v1/auth/passkey/register/finish", h.finishPasskeyRegister)
	mux.HandleFunc("POST /v1/auth/passkey/register/password-reauth", h.passwordReauthForPasskeyAdd)
	mux.HandleFunc("GET /v1/auth/passkeys", h.listPasskeys)
	mux.HandleFunc("POST /v1/auth/passkeys/{passkeyId}/remove", h.removePasskey)
	mux.HandleFunc("POST /v1/auth/passkey/login/begin", h.beginPasskeyLogin)
	mux.HandleFunc("POST /v1/auth/passkey/login/finish", h.finishPasskeyLogin)
	mux.HandleFunc("POST /v1/auth/password/login", h.passwordLogin)
	mux.HandleFunc("POST /v1/auth/signup/verification/start", h.startSignupVerification)
	mux.HandleFunc("POST /v1/auth/signup/verification/finish", h.finishSignupVerification)
	mux.HandleFunc("POST /v1/auth/signup/complete", h.completeSignup)
	mux.HandleFunc("POST /v1/auth/password/reset/start", h.startPasswordReset)
	mux.HandleFunc("POST /v1/auth/password/reset/verify", h.verifyPasswordReset)
	mux.HandleFunc("POST /v1/auth/password/reset/complete", h.completePasswordReset)
	mux.HandleFunc("POST /v1/auth/step-up/passkey/begin", h.beginStepUp)
	mux.HandleFunc("POST /v1/auth/step-up/passkey/finish", h.finishStepUp)
	mux.HandleFunc("GET /v1/auth/session", h.getSession)
	mux.HandleFunc("GET /v1/auth/sessions", h.listSessions)
	mux.HandleFunc("POST /v1/auth/sessions/revoke-others", h.revokeOtherSessions)
	mux.HandleFunc("POST /v1/auth/sessions/{sessionId}/revoke", h.revokeSession)
	mux.HandleFunc("POST /v1/auth/logout", h.logout)
}

func (h *Handler) beginPasskeyRegister(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	var req registerBeginRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpPasskeyRegisterBegin,
		AccountID:      session.UserID,
		SessionID:      session.ID,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseAll) {
		return
	}
	if !h.requirePasskeyAdd(w, r, session) {
		return
	}
	name, display := registrationLabels(session.UserID, req.Name, req.DisplayName)
	out, err := h.register.BeginRegistration(r.Context(), identity.BeginRegistrationInput{
		UserID:      session.UserID,
		Name:        name,
		DisplayName: display,
	})
	if err != nil {
		writePasskeyRegisterError(w, err)
		return
	}
	if out.Creation == nil || out.RawToken == "" {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, registerBeginResponse{
		CeremonyToken: out.RawToken,
		PublicKey:     out.Creation.Response,
	})
}

func (h *Handler) finishPasskeyRegister(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	var req finishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.CeremonyToken) == "" || len(req.Credential) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpPasskeyRegisterFinish,
		AccountID:      session.UserID,
		SessionID:      session.ID,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseAll) {
		return
	}
	if !h.requirePasskeyAdd(w, r, session) {
		return
	}
	if _, err := h.register.FinishRegistration(r.Context(), identity.FinishRegistrationInput{
		UserID:   session.UserID,
		RawToken: req.CeremonyToken,
		Response: req.Credential,
	}); err != nil {
		writePasskeyRegisterError(w, err)
		return
	}
	h.bootstrap.Consume(r.Context(), session.ID)
	writeJSON(w, http.StatusOK, registerFinishResponse{OK: true})
}

func (h *Handler) passwordReauthForPasskeyAdd(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	var req passwordReauthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if req.Password == "" || len(req.Password) > identity.MaxPasswordBytes {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpPasswordReauth,
		AccountID:      session.UserID,
		SessionID:      session.ID,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseAll) {
		return
	}
	password := []byte(req.Password)
	defer clearBytes(password)
	if err := h.bootstrap.GrantPassword(r.Context(), session, password); err != nil {
		writePasswordReauthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, logoutResponse{OK: true})
}

func registrationLabels(userID identity.ID, name, displayName string) (string, string) {
	stable := userID.String()
	name = strings.TrimSpace(name)
	displayName = strings.TrimSpace(displayName)
	if name == "" {
		name = stable
	}
	if displayName == "" {
		displayName = stable
	}
	return name, displayName
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (identity.Session, bool) {
	if !h.requireOrigin(w, r) {
		return identity.Session{}, false
	}
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return identity.Session{}, false
	}
	session, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		writeIdentityError(w, err)
		return identity.Session{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return identity.Session{}, false
	}
	return session, true
}

func (h *Handler) beginPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrigin(w, r) {
		return
	}
	var req challengeTokenRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpPasskeyLoginBegin,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseIP|identity.PhaseChallenge) {
		return
	}
	out, err := h.auth.BeginAuthentication(r.Context())
	if err != nil {
		writeIdentityError(w, err)
		return
	}
	if out.Assertion == nil || out.RawToken == "" {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, beginResponse{
		CeremonyToken: out.RawToken,
		PublicKey:     out.Assertion.Response,
	})
}

func (h *Handler) finishPasskeyLogin(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrigin(w, r) {
		return
	}
	var req finishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.CeremonyToken) == "" || len(req.Credential) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpPasskeyLoginFinish,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseIP|identity.PhaseChallenge) {
		return
	}

	userID, err := h.auth.FinishAuthentication(r.Context(), identity.FinishAuthenticationInput{
		RawToken: req.CeremonyToken,
		Response: req.Credential,
	})
	if err != nil {
		writeIdentityError(w, err)
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation: identity.AuthOpPasskeyLoginFinish,
		AccountID: userID,
	}, identity.PhaseAccount) {
		return
	}
	h.issueBrowserSession(w, r, userID, true)
}

func (h *Handler) passwordLogin(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrigin(w, r) {
		return
	}
	var req passwordLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	kind := identity.IdentifierKind(strings.TrimSpace(req.Kind))
	if !kindIsSupported(kind) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.Identifier) == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	canonical, err := identity.CanonicalizeIdentifier(kind, req.Identifier)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpPasswordLogin,
		Target:         identity.PasswordLoginTarget(kind, canonical),
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseIP|identity.PhaseTarget|identity.PhaseChallenge) {
		return
	}

	password := []byte(req.Password)
	user, err := h.identifiers.ResolveVerified(r.Context(), kind, req.Identifier)
	if err != nil {
		switch identity.Classify(err) {
		case identity.FailureUnavailable:
			writeError(w, http.StatusServiceUnavailable, "unavailable")
		case identity.FailureUnauthenticated:
			h.passwords.DummyVerify(password)
			writeError(w, http.StatusUnauthorized, "unauthenticated")
		default:
			writeIdentityError(w, err)
		}
		return
	}

	if !h.protect(w, r, identity.AbuseSubject{
		Operation: identity.AuthOpPasswordLogin,
		AccountID: user.ID,
	}, identity.PhaseAccount) {
		return
	}

	if _, err := h.passwords.Verify(r.Context(), user.ID, password); err != nil {
		writeIdentityError(w, err)
		return
	}
	h.issueBrowserSession(w, r, user.ID, false)
}

func kindIsSupported(kind identity.IdentifierKind) bool {
	return kind == identity.IdentifierEmail || kind == identity.IdentifierPhone
}

func (h *Handler) startSignupVerification(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrigin(w, r) {
		return
	}
	var req signupStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	kind := identity.IdentifierKind(strings.TrimSpace(req.Kind))
	if !kindIsSupported(kind) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.Identifier) == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if _, err := identity.CanonicalizeIdentifier(kind, req.Identifier); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !contracts.ValidLocale(req.Locale) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpSignupStart,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseIP|identity.PhaseChallenge) {
		return
	}
	got, err := h.signup.StartSignupVerification(r.Context(), identity.StartSignupVerificationInput{
		Kind:        kind,
		Destination: req.Identifier,
		Locale:      req.Locale,
		ClientIP:    h.guard.clientIP(r),
	})
	if err != nil {
		writeSignupStartError(w, err)
		return
	}
	if got.ChallengeID.IsZero() {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, signupStartResponse{ChallengeID: got.ChallengeID.String()})
}

func (h *Handler) finishSignupVerification(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrigin(w, r) {
		return
	}
	var req signupFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	challengeID, err := identity.ParseID(req.ChallengeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpSignupFinish,
		Target:         challengeID.String(),
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseIP|identity.PhaseTarget|identity.PhaseChallenge) {
		return
	}
	got, err := h.signup.FinishSignupVerification(r.Context(), identity.FinishSignupVerificationInput{
		ChallengeID: challengeID,
		Code:        req.Code,
	})
	if err != nil {
		writeSignupFinishError(w, err)
		return
	}
	if !got.Verified || got.SignupProof == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	writeJSON(w, http.StatusOK, signupFinishResponse{
		Verified:    true,
		SignupProof: got.SignupProof,
	})
}

func (h *Handler) completeSignup(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrigin(w, r) {
		return
	}
	var req signupCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.SignupProof) == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpSignupComplete,
		Proof:          req.SignupProof,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseIP|identity.PhaseProof|identity.PhaseChallenge) {
		return
	}
	var password []byte
	if req.Password != "" {
		if len(req.Password) > identity.MaxPasswordBytes {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
		password = []byte(req.Password)
		defer clearBytes(password)
	}
	got, err := h.accounts.CompleteSignup(r.Context(), identity.CompleteSignupInput{
		SignupProof: req.SignupProof,
		Password:    password,
	})
	if err != nil {
		writeSignupCompleteError(w, err)
		return
	}
	if got.UserID.IsZero() {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	issued, ok := h.createFreshSession(w, r, got.UserID)
	if !ok {
		return
	}
	if len(password) == 0 {
		if err := h.bootstrap.GrantSignup(r.Context(), issued.Session); err != nil {
			h.clearElevations(r.Context(), issued.Session.ID)
			_ = h.sessions.Revoke(r.Context(), issued.Session.ID)
			writeError(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
	}
	h.writeIssuedSession(w, issued, got.UserID)
}

func (h *Handler) startPasswordReset(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrigin(w, r) {
		return
	}
	var req signupStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	kind := identity.IdentifierKind(strings.TrimSpace(req.Kind))
	if !kindIsSupported(kind) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.Identifier) == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if _, err := identity.CanonicalizeIdentifier(kind, req.Identifier); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !contracts.ValidLocale(req.Locale) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpResetStart,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseIP|identity.PhaseChallenge) {
		return
	}
	got, err := h.reset.StartPasswordReset(r.Context(), identity.StartPasswordResetInput{
		Kind:        kind,
		Destination: req.Identifier,
		Locale:      req.Locale,
		ClientIP:    h.guard.clientIP(r),
	})
	if err != nil {
		writeSignupStartError(w, err)
		return
	}
	if got.ChallengeID.IsZero() {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, signupStartResponse{ChallengeID: got.ChallengeID.String()})
}

func (h *Handler) verifyPasswordReset(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrigin(w, r) {
		return
	}
	var req signupFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	challengeID, err := identity.ParseID(req.ChallengeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpResetVerify,
		Target:         challengeID.String(),
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseIP|identity.PhaseTarget|identity.PhaseChallenge) {
		return
	}
	got, err := h.reset.VerifyPasswordReset(r.Context(), identity.VerifyPasswordResetInput{
		ChallengeID: challengeID,
		Code:        req.Code,
	})
	if err != nil {
		writeSignupFinishError(w, err)
		return
	}
	if got.ResetProof == "" {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	writeJSON(w, http.StatusOK, resetVerifyResponse{ResetProof: got.ResetProof})
}

func (h *Handler) completePasswordReset(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrigin(w, r) {
		return
	}
	var req resetCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.ResetProof) == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if len(req.NewPassword) > identity.MaxPasswordBytes {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpResetComplete,
		Proof:          req.ResetProof,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseIP|identity.PhaseProof|identity.PhaseChallenge) {
		return
	}
	password := []byte(req.NewPassword)
	defer clearBytes(password)
	got, err := h.reset.CompletePasswordReset(r.Context(), identity.CompletePasswordResetInput{
		ResetProof:  req.ResetProof,
		NewPassword: password,
	})
	if err != nil {
		writeResetCompleteError(w, err)
		return
	}
	if got.UserID.IsZero() {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, resetCompleteResponse{OK: true})
}

func (h *Handler) issueBrowserSession(w http.ResponseWriter, r *http.Request, userID identity.ID, strong bool) {
	issued, ok := h.createFreshSession(w, r, userID)
	if !ok {
		return
	}
	if strong {
		_ = h.stepUp.Grant(r.Context(), issued.Session)
	}
	h.writeIssuedSession(w, issued, userID)
}

func (h *Handler) createFreshSession(w http.ResponseWriter, r *http.Request, userID identity.ID) (identity.IssuedSession, bool) {
	if !h.rotatePredecessor(w, r) {
		return identity.IssuedSession{}, false
	}
	issued, err := h.sessions.Create(r.Context(), userID, nil)
	if err != nil {
		writeIdentityError(w, err)
		return identity.IssuedSession{}, false
	}
	if issued.RawToken == "" {
		writeError(w, http.StatusInternalServerError, "internal")
		return identity.IssuedSession{}, false
	}
	return issued, true
}

func (h *Handler) writeIssuedSession(w http.ResponseWriter, issued identity.IssuedSession, userID identity.ID) {
	csrf, err := newCSRFToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	setSessionCookie(w, issued.RawToken)
	setCSRFCookie(w, csrf)
	writeJSON(w, http.StatusOK, authResponse{
		Authenticated: true,
		UserID:        userID.String(),
	})
}

func (h *Handler) rotatePredecessor(w http.ResponseWriter, r *http.Request) bool {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		return true
	}
	session, err := h.sessions.Resolve(r.Context(), raw)
	switch identity.Classify(err) {
	case identity.FailureNone:
		h.clearElevations(r.Context(), session.ID)
		if rerr := h.sessions.Revoke(r.Context(), session.ID); rerr != nil {
			if identity.Classify(rerr) == identity.FailureUnavailable {
				writeIdentityError(w, rerr)
				return false
			}
		}
		return true
	case identity.FailureUnavailable, identity.FailureInternal:
		writeIdentityError(w, err)
		return false
	default:
		return true
	}
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, authResponse{
		Authenticated: true,
		UserID:        session.UserID.String(),
	})
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	list, err := h.sessions.ListActiveForUser(r.Context(), session.UserID)
	if err != nil {
		writeIdentityError(w, err)
		return
	}
	out := make([]sessionListItem, 0, len(list))
	for _, s := range list {
		out = append(out, sessionListItem{
			ID:         s.ID.String(),
			Current:    s.ID == session.ID,
			CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
			LastSeenAt: s.LastSeenAt.UTC().Format(time.RFC3339),
			ExpiresAt:  s.AbsoluteExpiresAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, sessionListResponse{Sessions: out})
}

func (h *Handler) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation: identity.AuthOpSessionRevoke,
		AccountID: session.UserID,
		SessionID: session.ID,
	}, identity.PhaseAll) {
		return
	}
	others, err := h.sessions.ListActiveForUser(r.Context(), session.UserID)
	if err != nil {
		writeIdentityError(w, err)
		return
	}
	if err := h.sessions.RevokeOthers(r.Context(), session.UserID, session.ID); err != nil {
		writeIdentityError(w, err)
		return
	}
	for _, s := range others {
		if s.ID != session.ID {
			h.clearElevations(r.Context(), s.ID)
		}
	}
	writeJSON(w, http.StatusOK, logoutResponse{OK: true})
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	targetID, err := identity.ParseID(r.PathValue("sessionId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation: identity.AuthOpSessionRevoke,
		AccountID: session.UserID,
		SessionID: session.ID,
	}, identity.PhaseAll) {
		return
	}
	owned, err := h.sessions.GetOwned(r.Context(), session.UserID, targetID)
	if err != nil {
		writeSecurityError(w, err)
		return
	}
	if err := h.sessions.Revoke(r.Context(), owned.ID); err != nil {
		writeIdentityError(w, err)
		return
	}
	h.clearElevations(r.Context(), owned.ID)
	if owned.ID == session.ID {
		clearSessionCookie(w)
		clearCSRFCookie(w)
	}
	writeJSON(w, http.StatusOK, logoutResponse{OK: true})
}

func (h *Handler) listPasskeys(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	list, err := h.credentials.ListPasskeys(r.Context(), session.UserID)
	if err != nil {
		writeIdentityError(w, err)
		return
	}
	out := make([]passkeyListItem, 0, len(list))
	for _, c := range list {
		item := passkeyListItem{
			ID:         c.ID.String(),
			CreatedAt:  c.CreatedAt.UTC().Format(time.RFC3339),
			Transports: append([]string(nil), c.Transports...),
		}
		if c.LastUsedAt != nil {
			v := c.LastUsedAt.UTC().Format(time.RFC3339)
			item.LastUsedAt = &v
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, passkeyListResponse{Passkeys: out})
}

func (h *Handler) removePasskey(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	credID, err := identity.ParseID(r.PathValue("passkeyId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation: identity.AuthOpPasskeyRemove,
		AccountID: session.UserID,
		SessionID: session.ID,
	}, identity.PhaseAll) {
		return
	}
	if !h.requireRecentStrong(w, r, session, identity.SensitivePasskeyRemove) {
		return
	}
	if err := h.credentials.RemovePasskey(r.Context(), session.UserID, credID); err != nil {
		writeSecurityError(w, err)
		return
	}
	h.clearElevations(r.Context(), session.ID)
	clearSessionCookie(w)
	clearCSRFCookie(w)
	writeJSON(w, http.StatusOK, logoutResponse{OK: true})
}

func (h *Handler) beginStepUp(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	var req challengeTokenRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpStepUpBegin,
		AccountID:      session.UserID,
		SessionID:      session.ID,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseAll) {
		return
	}
	out, err := h.auth.BeginStepUp(r.Context(), session.UserID)
	if err != nil {
		writeIdentityError(w, err)
		return
	}
	if out.Assertion == nil || out.RawToken == "" {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, beginResponse{
		CeremonyToken: out.RawToken,
		PublicKey:     out.Assertion.Response,
	})
}

func (h *Handler) finishStepUp(w http.ResponseWriter, r *http.Request) {
	session, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	var req finishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if strings.TrimSpace(req.CeremonyToken) == "" || len(req.Credential) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !h.protect(w, r, identity.AbuseSubject{
		Operation:      identity.AuthOpStepUpFinish,
		AccountID:      session.UserID,
		SessionID:      session.ID,
		ChallengeToken: req.ChallengeToken,
	}, identity.PhaseAll) {
		return
	}
	if err := h.auth.FinishStepUp(r.Context(), session.UserID, identity.FinishAuthenticationInput{
		RawToken: req.CeremonyToken,
		Response: req.Credential,
	}); err != nil {
		writeIdentityError(w, err)
		return
	}
	issued, err := h.sessions.Create(r.Context(), session.UserID, nil)
	if err != nil {
		writeIdentityError(w, err)
		return
	}
	if err := h.stepUp.Grant(r.Context(), issued.Session); err != nil {
		_ = h.sessions.Revoke(r.Context(), issued.Session.ID)
		writeIdentityError(w, err)
		return
	}
	h.clearElevations(r.Context(), session.ID)
	if err := h.sessions.Revoke(r.Context(), session.ID); err != nil {
		h.clearElevations(r.Context(), issued.Session.ID)
		_ = h.sessions.Revoke(r.Context(), issued.Session.ID)
		writeIdentityError(w, err)
		return
	}
	csrf, err := newCSRFToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	setSessionCookie(w, issued.RawToken)
	setCSRFCookie(w, csrf)
	writeJSON(w, http.StatusOK, logoutResponse{OK: true})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if !h.requireOrigin(w, r) {
		return
	}

	raw, hasSessionCookie := readCookie(r, sessionCookieName)
	if hasSessionCookie {
		session, err := h.sessions.Resolve(r.Context(), raw)
		switch identity.Classify(err) {
		case identity.FailureNone:
			if !csrfOK(r) {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
			h.clearElevations(r.Context(), session.ID)
			if err := h.sessions.Revoke(r.Context(), session.ID); err != nil {
				writeIdentityError(w, err)
				return
			}
		case identity.FailureUnavailable:
			writeIdentityError(w, err)
			return
		case identity.FailureInternal:
			writeIdentityError(w, err)
			return
		}
	}

	clearSessionCookie(w)
	clearCSRFCookie(w)
	writeJSON(w, http.StatusOK, logoutResponse{OK: true})
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (identity.Session, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return identity.Session{}, false
	}
	session, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		writeIdentityError(w, err)
		return identity.Session{}, false
	}
	return session, true
}

func (h *Handler) requireRecentStrong(w http.ResponseWriter, r *http.Request, session identity.Session, op identity.SensitiveOperation) bool {
	if err := h.stepUp.Require(r.Context(), session, op); err != nil {
		writeSecurityError(w, err)
		return false
	}
	return true
}

func (h *Handler) requirePasskeyAdd(w http.ResponseWriter, r *http.Request, session identity.Session) bool {
	err := h.stepUp.Require(r.Context(), session, identity.SensitivePasskeyAdd)
	if err == nil {
		return true
	}
	if !errors.Is(err, identity.ErrStepUpRequired) {
		writeSecurityError(w, err)
		return false
	}
	if err := h.bootstrap.Allow(r.Context(), session, identity.SensitivePasskeyAdd); err != nil {
		writeSecurityError(w, err)
		return false
	}
	return true
}

func (h *Handler) clearElevations(ctx context.Context, sessionID identity.ID) {
	h.stepUp.Clear(ctx, sessionID)
	h.bootstrap.Clear(ctx, sessionID)
}

func (h *Handler) requireOrigin(w http.ResponseWriter, r *http.Request) bool {
	if !h.originAllowed(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

func (h *Handler) protect(w http.ResponseWriter, r *http.Request, sub identity.AbuseSubject, phase identity.AbusePhase) bool {
	if h.guard == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return false
	}
	sub.IP = h.guard.clientIP(r)
	sub.Hostname = requestHostname(r)
	out := h.guard.evaluate(r.Context(), sub, phase)
	logAuthRisk(r.Context(), out)
	return writeRisk(w, out)
}

func writeRisk(w http.ResponseWriter, out identity.RiskOutcome) bool {
	if out.Allow() {
		return true
	}
	switch out.Reason {
	case identity.ReasonVelocityIP, identity.ReasonVelocityAccount, identity.ReasonVelocityTarget:
		if out.RetryAfter > 0 {
			sec := int(out.RetryAfter.Seconds())
			if sec < 1 {
				sec = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(sec))
		}
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return false
	case identity.ReasonStorageUnavailable, identity.ReasonProviderUnavailable:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return false
	default:
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
}

func (h *Handler) originAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || origin == "null" {
		return false
	}
	_, ok := h.origins[origin]
	return ok
}

type registerBeginRequest struct {
	Name           string `json:"name"`
	DisplayName    string `json:"displayName"`
	ChallengeToken string `json:"challengeToken"`
}

type passwordReauthRequest struct {
	Password       string `json:"password"`
	ChallengeToken string `json:"challengeToken"`
}

type registerBeginResponse struct {
	CeremonyToken string                                      `json:"ceremonyToken"`
	PublicKey     protocol.PublicKeyCredentialCreationOptions `json:"publicKey"`
}

type registerFinishResponse struct {
	OK bool `json:"ok"`
}

type beginResponse struct {
	CeremonyToken string                                     `json:"ceremonyToken"`
	PublicKey     protocol.PublicKeyCredentialRequestOptions `json:"publicKey"`
}

type finishRequest struct {
	CeremonyToken  string          `json:"ceremonyToken"`
	Credential     json.RawMessage `json:"credential"`
	ChallengeToken string          `json:"challengeToken"`
}

type challengeTokenRequest struct {
	ChallengeToken string `json:"challengeToken"`
}

type passwordLoginRequest struct {
	Kind           string `json:"kind"`
	Identifier     string `json:"identifier"`
	Password       string `json:"password"`
	ChallengeToken string `json:"challengeToken"`
}

type signupStartRequest struct {
	Kind           string `json:"kind"`
	Identifier     string `json:"identifier"`
	Locale         string `json:"locale"`
	ChallengeToken string `json:"challengeToken"`
}

type signupStartResponse struct {
	ChallengeID string `json:"challengeId"`
}

type signupFinishRequest struct {
	ChallengeID    string `json:"challengeId"`
	Code           string `json:"code"`
	ChallengeToken string `json:"challengeToken"`
}

type signupFinishResponse struct {
	Verified    bool   `json:"verified"`
	SignupProof string `json:"signupProof"`
}

type signupCompleteRequest struct {
	SignupProof    string `json:"signupProof"`
	Password       string `json:"password"`
	ChallengeToken string `json:"challengeToken"`
}

type resetVerifyResponse struct {
	ResetProof string `json:"resetProof"`
}

type resetCompleteRequest struct {
	ResetProof     string `json:"resetProof"`
	NewPassword    string `json:"newPassword"`
	ChallengeToken string `json:"challengeToken"`
}

type resetCompleteResponse struct {
	OK bool `json:"ok"`
}

type authResponse struct {
	Authenticated bool   `json:"authenticated"`
	UserID        string `json:"userId"`
}

type logoutResponse struct {
	OK bool `json:"ok"`
}

type sessionListItem struct {
	ID         string `json:"id"`
	Current    bool   `json:"current"`
	CreatedAt  string `json:"createdAt"`
	LastSeenAt string `json:"lastSeenAt"`
	ExpiresAt  string `json:"expiresAt"`
}

type sessionListResponse struct {
	Sessions []sessionListItem `json:"sessions"`
}

type passkeyListItem struct {
	ID         string   `json:"id"`
	CreatedAt  string   `json:"createdAt"`
	LastUsedAt *string  `json:"lastUsedAt,omitempty"`
	Transports []string `json:"transports,omitempty"`
}

type passkeyListResponse struct {
	Passkeys []passkeyListItem `json:"passkeys"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writePasswordReauthError(w http.ResponseWriter, err error) {
	if errors.Is(err, identity.ErrStepUpRequired) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	writeIdentityError(w, err)
}

func writeSecurityError(w http.ResponseWriter, err error) {
	if errors.Is(err, identity.ErrStepUpRequired) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if errors.Is(err, identity.ErrLastCredential) {
		writeError(w, http.StatusConflict, "conflict")
		return
	}
	if errors.Is(err, identity.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	writeIdentityError(w, err)
}

func writePasskeyRegisterError(w http.ResponseWriter, err error) {
	if errors.Is(err, identity.ErrCredentialConflict) {
		writeError(w, http.StatusConflict, "conflict")
		return
	}
	switch identity.Classify(err) {
	case identity.FailureUnavailable:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	case identity.FailureUnauthenticated:
		writeError(w, http.StatusBadRequest, "bad_request")
	default:
		writeError(w, http.StatusInternalServerError, "internal")
	}
}

func writeIdentityError(w http.ResponseWriter, err error) {
	switch identity.Classify(err) {
	case identity.FailureUnauthenticated:
		writeError(w, http.StatusUnauthorized, "unauthenticated")
	case identity.FailureUnavailable:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "internal")
	}
}

func writeSignupStartError(w http.ResponseWriter, err error) {
	if errors.Is(err, identity.ErrChallengeThrottled) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if errors.Is(err, identity.ErrInvalidChallenge) || errors.Is(err, identity.ErrInvalidIdentifier) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	writeIdentityError(w, err)
}

func writeSignupFinishError(w http.ResponseWriter, err error) {
	switch identity.Classify(err) {
	case identity.FailureUnauthenticated:
		writeError(w, http.StatusUnauthorized, "unauthenticated")
	case identity.FailureUnavailable:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	default:
		writeError(w, http.StatusUnauthorized, "unauthenticated")
	}
}

func writeSignupCompleteError(w http.ResponseWriter, err error) {
	if errors.Is(err, identity.ErrIdentifierConflict) {
		writeError(w, http.StatusConflict, "conflict")
		return
	}
	if errors.Is(err, identity.ErrInvalidPassword) || errors.Is(err, identity.ErrPasswordTooLong) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	switch identity.Classify(err) {
	case identity.FailureUnauthenticated:
		writeError(w, http.StatusUnauthorized, "unauthenticated")
	case identity.FailureUnavailable:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	default:
		writeError(w, http.StatusUnauthorized, "unauthenticated")
	}
}

func writeResetCompleteError(w http.ResponseWriter, err error) {
	if errors.Is(err, identity.ErrInvalidPassword) || errors.Is(err, identity.ErrPasswordTooLong) {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	switch identity.Classify(err) {
	case identity.FailureUnauthenticated:
		writeError(w, http.StatusUnauthorized, "unauthenticated")
	case identity.FailureUnavailable:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	default:
		writeError(w, http.StatusUnauthorized, "unauthenticated")
	}
}

func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorResponse{Error: code})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func readCookie(r *http.Request, name string) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil || c == nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

func setSessionCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, hostCookie(sessionCookieName, value, true, 0))
}

func setCSRFCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, hostCookie(csrfCookieName, value, false, 0))
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, hostCookie(sessionCookieName, "", true, -1))
}

func clearCSRFCookie(w http.ResponseWriter) {
	http.SetCookie(w, hostCookie(csrfCookieName, "", false, -1))
}

func hostCookie(name, value string, httpOnly bool, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   true,
		HttpOnly: httpOnly,
		SameSite: http.SameSiteLaxMode,
	}
}

func newCSRFToken() (string, error) {
	secret := make([]byte, csrfSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(secret), nil
}

func csrfOK(r *http.Request) bool {
	cookie, ok := readCookie(r, csrfCookieName)
	if !ok {
		return false
	}
	header := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	if header == "" || len(header) != len(cookie) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(cookie)) == 1
}
