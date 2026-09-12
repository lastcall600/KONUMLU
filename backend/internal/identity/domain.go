package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Session secret hash is SHA-256 (ADR-002). Never persist the raw cookie value.
const TokenHashSize = sha256.Size

// Minimum entropy for the opaque session secret (ADR-002: ≥ 256 bits).
const MinSessionSecretBytes = 32

var (
	errZeroID                 = errors.New("identity id must not be zero")
	errWeakSessionSecret      = errors.New("session secret too short")
	errInvalidTokenHash       = errors.New("invalid session token hash")
	errSessionRevoked         = errors.New("session revoked")
	errSessionIdleExpired     = errors.New("session idle timeout exceeded")
	errSessionAbsExpired      = errors.New("session absolute timeout exceeded")
	errAccountIneligible      = errors.New("account is not eligible")
	errDeviceRevoked          = errors.New("device revoked")
	errCredentialRevoked      = errors.New("passkey credential revoked")
	errInvalidPasskey         = errors.New("invalid passkey credential")
	errSignCountNotMonotonic  = errors.New("passkey sign count must not decrease")
	errInvalidWebAuthnConfig  = errors.New("invalid webauthn config")
	errInvalidCeremonyPolicy  = errors.New("invalid ceremony policy")
	errInvalidCeremony        = errors.New("invalid webauthn ceremony")
	errCeremonyExpired        = errors.New("webauthn ceremony expired")
	errCeremonyConsumed       = errors.New("webauthn ceremony already consumed")
	errCeremonyKind           = errors.New("webauthn ceremony kind mismatch")
	errWebAuthnVerification   = errors.New("webauthn verification failed")
	errCredentialConflict     = errors.New("passkey credential conflict")
	errInvalidRegistration    = errors.New("invalid passkey registration")
	errUnknownCredential      = errors.New("unknown passkey credential")
	errCounterConflict        = errors.New("passkey counter conflict")
	errInvalidPasswordPolicy  = errors.New("invalid password policy")
	errInvalidPassword        = errors.New("invalid password")
	errPasswordTooLong        = errors.New("password exceeds maximum length")
	errMalformedPasswordHash  = errors.New("malformed password hash")
	errInvalidIdentifier      = errors.New("invalid identifier")
	errIdentifierConflict     = errors.New("identifier conflict")
	errInvalidChallengePolicy = errors.New("invalid verification challenge policy")
	errInvalidIssuancePolicy  = errors.New("invalid verification issuance policy")
	errInvalidChallenge       = errors.New("invalid verification challenge")
	errChallengeExpired       = errors.New("verification challenge expired")
	errChallengeConsumed      = errors.New("verification challenge already consumed")
	errChallengeExhausted     = errors.New("verification challenge attempts exhausted")
	errChallengeThrottled     = errors.New("verification challenge issuance throttled")
	errInvalidAbusePolicy     = errors.New("invalid auth abuse policy")
	errRateLimited            = errors.New("rate limited")
	errChallengeRequired      = errors.New("human challenge required")
	errChallengeFailed        = errors.New("human challenge failed")
	errInvalidSignupProof     = errors.New("invalid signup proof")
	errSignupProofExpired     = errors.New("signup proof expired")
	errSignupProofConsumed    = errors.New("signup proof already consumed")
	errInvalidResetProof      = errors.New("invalid password reset proof")
	errResetProofExpired      = errors.New("password reset proof expired")
	errResetProofConsumed     = errors.New("password reset proof already consumed")
)

// CeremonyKind is the WebAuthn ceremony type stored with server-side session state.
type CeremonyKind string

const (
	CeremonyRegistration   CeremonyKind = "registration"
	CeremonyAuthentication CeremonyKind = "authentication"
)

func (k CeremonyKind) valid() bool {
	return k == CeremonyRegistration || k == CeremonyAuthentication
}

// CeremonyState is durable WebAuthn ceremony state. TokenHash is the only client secret stored.
type CeremonyState struct {
	ID          ID
	Kind        CeremonyKind
	UserID      *ID
	TokenHash   []byte
	SessionData []byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
}

func (s CeremonyState) Validate() error {
	if s.ID.IsZero() {
		return errZeroID
	}
	if !s.Kind.valid() {
		return errInvalidCeremony
	}
	if s.UserID != nil && s.UserID.IsZero() {
		return errZeroID
	}
	if len(s.TokenHash) != TokenHashSize {
		return errInvalidCeremony
	}
	if len(s.SessionData) == 0 || s.CreatedAt.IsZero() || !s.ExpiresAt.After(s.CreatedAt) {
		return errInvalidCeremony
	}
	return nil
}

// ID is an application-generated UUID (16 bytes). The database does not mint IDs.
type ID [16]byte

func NewID() (ID, error) {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		return ID{}, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id, nil
}

func (id ID) IsZero() bool {
	return id == ID{}
}

func (id ID) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:])
}

// ParseID decodes a canonical UUID string. It does not log the input.
func ParseID(s string) (ID, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(s) != 32 {
		return ID{}, errInvalidChallenge
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errInvalidChallenge
	}
	var id ID
	copy(id[:], b)
	if id.IsZero() {
		return ID{}, errZeroID
	}
	return id, nil
}

// HashSessionSecret returns the one-way hash stored in PostgreSQL.
// The raw secret must never be written to the database, cache, or logs.
func HashSessionSecret(raw []byte) ([]byte, error) {
	if len(raw) < MinSessionSecretBytes {
		return nil, errWeakSessionSecret
	}
	sum := sha256.Sum256(raw)
	out := make([]byte, TokenHashSize)
	copy(out, sum[:])
	return out, nil
}

// User is the Identity account record (not a public profile).
type User struct {
	ID           ID
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DisabledAt   *time.Time
	DeletedAt    *time.Time
	SessionEpoch int64
}

func (u User) EligibleForSession() bool {
	return !u.ID.IsZero() && u.DeletedAt == nil && u.DisabledAt == nil
}

// Device is durable browser/device inventory for the security center (ADR-002).
type Device struct {
	ID         ID
	UserID     ID
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

func (d Device) Active() bool {
	return !d.ID.IsZero() && !d.UserID.IsZero() && d.RevokedAt == nil
}

// Session is the durable session row. TokenHash is the only credential material stored.
type Session struct {
	ID                ID
	UserID            ID
	DeviceID          *ID
	TokenHash         []byte
	CreatedAt         time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
}

// DurableValid reports whether the session row itself may authorize at now.
// Account standing is checked separately (ADR-002 §3 step 4).
func (s Session) DurableValid(now time.Time) error {
	if s.ID.IsZero() || s.UserID.IsZero() {
		return errZeroID
	}
	if len(s.TokenHash) != TokenHashSize {
		return errInvalidTokenHash
	}
	if s.RevokedAt != nil {
		return errSessionRevoked
	}
	if !now.Before(s.IdleExpiresAt) {
		return errSessionIdleExpired
	}
	if !now.Before(s.AbsoluteExpiresAt) {
		return errSessionAbsExpired
	}
	return nil
}

// AuthorizeSession fails closed unless the durable session, account, and device (if bound) are valid.
func AuthorizeSession(now time.Time, user User, device *Device, session Session) error {
	if !user.EligibleForSession() {
		return errAccountIneligible
	}
	if user.ID != session.UserID {
		return errAccountIneligible
	}
	if err := session.DurableValid(now); err != nil {
		return err
	}
	if session.DeviceID == nil {
		return nil
	}
	if device == nil || !device.Active() || device.ID != *session.DeviceID || device.UserID != user.ID {
		return errDeviceRevoked
	}
	return nil
}

// PasskeyCredential is durable WebAuthn public-key material (ADR-004).
// Challenges and attestation blobs are not stored.
type PasskeyCredential struct {
	ID             ID
	UserID         ID
	CredentialID   []byte
	PublicKey      []byte
	SignCount      int64
	BackupEligible bool
	BackupState    bool
	Transports     []string
	CreatedAt      time.Time
	LastUsedAt     *time.Time
	RevokedAt      *time.Time
}

func (c PasskeyCredential) Validate() error {
	if c.ID.IsZero() || c.UserID.IsZero() {
		return errZeroID
	}
	if len(c.CredentialID) == 0 || len(c.PublicKey) == 0 {
		return errInvalidPasskey
	}
	if c.SignCount < 0 || c.CreatedAt.IsZero() {
		return errInvalidPasskey
	}
	return nil
}

// Active reports whether the credential may be used in a later authentication ceremony.
func (c PasskeyCredential) Active() bool {
	return c.Validate() == nil && c.RevokedAt == nil
}

// ApplySuccessfulUse returns a copy with last-used state applied.
// A stored non-zero sign count is never decreased. Clone/replay policy is not decided here.
func (c PasskeyCredential) ApplySuccessfulUse(signCount int64, backupState bool, usedAt time.Time) (PasskeyCredential, error) {
	if !c.Active() {
		return PasskeyCredential{}, errCredentialRevoked
	}
	if signCount < 0 || usedAt.IsZero() {
		return PasskeyCredential{}, errInvalidPasskey
	}
	if c.SignCount > 0 && signCount < c.SignCount {
		return PasskeyCredential{}, errSignCountNotMonotonic
	}
	c.SignCount = signCount
	c.BackupState = backupState
	at := usedAt
	c.LastUsedAt = &at
	return c, nil
}

// PasswordCredential is the durable Argon2id fallback hash (ADR-004).
// Plaintext is never stored. At most one row per user.
type PasswordCredential struct {
	UserID       ID
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DisabledAt   *time.Time
}

func (c PasswordCredential) Active() bool {
	return !c.UserID.IsZero() && c.PasswordHash != "" && c.DisabledAt == nil
}

// IdentifierKind is a login identifier type. Primary/default identifier is not modeled.
type IdentifierKind string

const (
	IdentifierEmail IdentifierKind = "email"
	IdentifierPhone IdentifierKind = "phone"
)

func (k IdentifierKind) valid() bool {
	return k == IdentifierEmail || k == IdentifierPhone
}

// UserIdentifier is an Identity-owned email or phone login identifier.
type UserIdentifier struct {
	ID             ID
	UserID         ID
	Kind           IdentifierKind
	ValueCanonical string
	VerifiedAt     *time.Time
	CreatedAt      time.Time
	RevokedAt      *time.Time
}

func (i UserIdentifier) Validate() error {
	if i.ID.IsZero() || i.UserID.IsZero() {
		return errZeroID
	}
	if !i.Kind.valid() || i.ValueCanonical == "" || i.CreatedAt.IsZero() {
		return errInvalidIdentifier
	}
	return nil
}

func (i UserIdentifier) Active() bool {
	return i.Validate() == nil && i.RevokedAt == nil
}

func (i UserIdentifier) VerifiedActive() bool {
	return i.Active() && i.VerifiedAt != nil
}

// ChallengePurpose is why a verification challenge was issued.
type ChallengePurpose string

const (
	ChallengeSignup        ChallengePurpose = "signup"
	ChallengePasswordReset ChallengePurpose = "password_reset"
)

func (p ChallengePurpose) valid() bool {
	return p == ChallengeSignup || p == ChallengePasswordReset
}

// VerificationChallenge is a single-use email/phone proof. TokenHash is the verification source.
type VerificationChallenge struct {
	ID                   ID
	Kind                 IdentifierKind
	Purpose              ChallengePurpose
	DestinationCanonical string
	TokenHash            []byte
	CreatedAt            time.Time
	ExpiresAt            time.Time
	ConsumedAt           *time.Time
	FailedAttempts       int
	MaxAttempts          int
}

func (c VerificationChallenge) Validate() error {
	if c.ID.IsZero() {
		return errZeroID
	}
	if !c.Kind.valid() || !c.Purpose.valid() || c.DestinationCanonical == "" {
		return errInvalidChallenge
	}
	if len(c.TokenHash) != TokenHashSize {
		return errInvalidChallenge
	}
	if c.CreatedAt.IsZero() || !c.ExpiresAt.After(c.CreatedAt) {
		return errInvalidChallenge
	}
	if c.MaxAttempts <= 0 || c.FailedAttempts < 0 || c.FailedAttempts > c.MaxAttempts {
		return errInvalidChallenge
	}
	return nil
}

func (c VerificationChallenge) rejectUnusable(now time.Time) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.ConsumedAt != nil {
		return errChallengeConsumed
	}
	if !now.Before(c.ExpiresAt) {
		return errChallengeExpired
	}
	if c.FailedAttempts >= c.MaxAttempts {
		return errChallengeExhausted
	}
	return nil
}

// HashVerificationSecret returns the SHA-256 hash persisted for a challenge secret.
// The raw token/OTP must never be written to the database, cache, or logs.
func HashVerificationSecret(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, errInvalidChallenge
	}
	sum := sha256.Sum256(raw)
	out := make([]byte, TokenHashSize)
	copy(out, sum[:])
	return out, nil
}

// SignupProofPurpose is why a short-lived signup proof was issued. Signup only.
type SignupProofPurpose string

const SignupProofSignup SignupProofPurpose = "signup"

func (p SignupProofPurpose) valid() bool {
	return p == SignupProofSignup
}

// SignupProof is a single-use, hash-only token proving a signup challenge was verified.
// It cannot authorize login or password reset. The raw token is never stored.
type SignupProof struct {
	ID                   ID
	ChallengeID          ID
	Kind                 IdentifierKind
	DestinationCanonical string
	Purpose              SignupProofPurpose
	TokenHash            []byte
	CreatedAt            time.Time
	ExpiresAt            time.Time
	ConsumedAt           *time.Time
}

func (p SignupProof) Validate() error {
	if p.ID.IsZero() || p.ChallengeID.IsZero() {
		return errZeroID
	}
	if !p.Kind.valid() || !p.Purpose.valid() || p.DestinationCanonical == "" {
		return errInvalidSignupProof
	}
	if len(p.TokenHash) != TokenHashSize {
		return errInvalidSignupProof
	}
	if p.CreatedAt.IsZero() || !p.ExpiresAt.After(p.CreatedAt) {
		return errInvalidSignupProof
	}
	return nil
}

func (p SignupProof) String() string {
	return "identity.SignupProof"
}

func (p SignupProof) GoString() string {
	return "identity.SignupProof{}"
}

func (p SignupProof) rejectUnusable(now time.Time) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.ConsumedAt != nil {
		return errSignupProofConsumed
	}
	if !now.Before(p.ExpiresAt) {
		return errSignupProofExpired
	}
	return nil
}

// PasswordResetProofPurpose is why a short-lived reset proof was issued.
type PasswordResetProofPurpose string

const PasswordResetProofPasswordReset PasswordResetProofPurpose = "password_reset"

func (p PasswordResetProofPurpose) valid() bool {
	return p == PasswordResetProofPasswordReset
}

// PasswordResetProof is a single-use, hash-only token proving a reset challenge was verified.
// It cannot authorize signup or login. The raw token is never stored.
type PasswordResetProof struct {
	ID          ID
	UserID      ID
	ChallengeID ID
	Purpose     PasswordResetProofPurpose
	TokenHash   []byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
}

func (p PasswordResetProof) Validate() error {
	if p.ID.IsZero() || p.UserID.IsZero() || p.ChallengeID.IsZero() {
		return errZeroID
	}
	if !p.Purpose.valid() {
		return errInvalidResetProof
	}
	if len(p.TokenHash) != TokenHashSize {
		return errInvalidResetProof
	}
	if p.CreatedAt.IsZero() || !p.ExpiresAt.After(p.CreatedAt) {
		return errInvalidResetProof
	}
	return nil
}

func (p PasswordResetProof) String() string {
	return "identity.PasswordResetProof"
}

func (p PasswordResetProof) GoString() string {
	return "identity.PasswordResetProof{}"
}

func (p PasswordResetProof) rejectUnusable(now time.Time) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.ConsumedAt != nil {
		return errResetProofConsumed
	}
	if !now.Before(p.ExpiresAt) {
		return errResetProofExpired
	}
	return nil
}
