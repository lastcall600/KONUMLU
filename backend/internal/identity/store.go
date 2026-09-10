package identity

import (
	"context"
	"time"

	"backend/internal/platform/outbox"
)

// sessionStore is the Identity persistence surface for durable sessions.
// It is not a generic repository.
type sessionStore interface {
	GetUser(ctx context.Context, id ID) (User, error)
	GetDevice(ctx context.Context, id ID) (Device, error)
	InsertSession(ctx context.Context, session Session) error
	GetSession(ctx context.Context, id ID) (Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error)
	UpdateSessionActivity(ctx context.Context, id ID, lastSeenAt, idleExpiresAt time.Time) error
	RevokeSession(ctx context.Context, id ID, at time.Time) error
	RevokeSessionsForUser(ctx context.Context, userID ID, at time.Time) (int64, error)
}

// passkeyStore is the Identity persistence surface for durable passkey credentials.
type passkeyStore interface {
	InsertPasskey(ctx context.Context, credential PasskeyCredential) error
	GetPasskey(ctx context.Context, id ID) (PasskeyCredential, error)
	GetPasskeyByCredentialID(ctx context.Context, credentialID []byte) (PasskeyCredential, error)
	GetActivePasskeyByCredentialID(ctx context.Context, credentialID []byte) (PasskeyCredential, error)
	ListActivePasskeysForUser(ctx context.Context, userID ID) ([]PasskeyCredential, error)
	UpdatePasskeySuccessfulUse(ctx context.Context, id ID, signCount int64, backupState bool, lastUsedAt time.Time) error
	RevokePasskey(ctx context.Context, id ID, at time.Time) error
}

// passwordStore is the Identity persistence surface for the single password fallback hash.
type passwordStore interface {
	GetUser(ctx context.Context, id ID) (User, error)
	GetPasswordCredential(ctx context.Context, userID ID) (PasswordCredential, error)
	UpsertPasswordCredential(ctx context.Context, credential PasswordCredential) error
	DisablePasswordCredential(ctx context.Context, userID ID, at time.Time) error
}

// ceremonyStore is the Identity persistence surface for single-use WebAuthn ceremony state.
type ceremonyStore interface {
	InsertCeremony(ctx context.Context, state CeremonyState) error
	ConsumeCeremonyByTokenHash(ctx context.Context, tokenHash []byte, now time.Time) (CeremonyState, error)
}

// identifierStore is the Identity persistence surface for email/phone login identifiers.
type identifierStore interface {
	GetUser(ctx context.Context, id ID) (User, error)
	InsertIdentifier(ctx context.Context, identifier UserIdentifier) error
	GetIdentifier(ctx context.Context, id ID) (UserIdentifier, error)
	GetActiveIdentifier(ctx context.Context, kind IdentifierKind, valueCanonical string) (UserIdentifier, error)
	ListActiveIdentifiersForUser(ctx context.Context, userID ID) ([]UserIdentifier, error)
	MarkIdentifierVerified(ctx context.Context, id ID, at time.Time) error
	RevokeIdentifier(ctx context.Context, id ID, at time.Time) error
}

// txExecer runs SQL on a connection or a caller-owned transaction.
type txExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
}

// transaction is the atomic PostgreSQL unit for challenge + material + outbox.
type transaction interface {
	txExecer
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// transactor starts the PostgreSQL transaction used as the atomic boundary.
type transactor interface {
	Begin(ctx context.Context) (transaction, error)
}

// intentEnqueuer writes notifications.intent into the platform outbox on the caller tx.
type intentEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

// challengeStore is the Identity persistence surface for email/phone verification challenges.
type challengeStore interface {
	InsertChallenge(ctx context.Context, challenge VerificationChallenge) error
	InsertChallengeAndMaterial(ctx context.Context, exec txExecer, challenge VerificationChallenge, material VerificationMaterial) error
	GetChallenge(ctx context.Context, id ID) (VerificationChallenge, error)
	GetActiveMaterial(ctx context.Context, challengeID ID) (VerificationMaterial, error)
	DestroyMaterial(ctx context.Context, challengeID ID, at time.Time) error
	ConsumeChallenge(ctx context.Context, id ID, now time.Time) (VerificationChallenge, error)
	IncrementFailedAttempt(ctx context.Context, id ID, now time.Time) (int, error)
}

// activeIdentifierLookup is the existence check used before signup challenge issuance.
type activeIdentifierLookup interface {
	GetActiveIdentifier(ctx context.Context, kind IdentifierKind, valueCanonical string) (UserIdentifier, error)
}

// signupProofStore is the Identity persistence surface for hash-only signup proofs.
type signupProofStore interface {
	InsertSignupProof(ctx context.Context, proof SignupProof) error
	GetSignupProof(ctx context.Context, id ID) (SignupProof, error)
	ConsumeSignupProofByTokenHash(ctx context.Context, tx transaction, tokenHash []byte, now time.Time) (SignupProof, error)
}

// resetProofStore is the Identity persistence surface for hash-only password-reset proofs.
type resetProofStore interface {
	InsertResetProof(ctx context.Context, proof PasswordResetProof) error
	GetResetProof(ctx context.Context, id ID) (PasswordResetProof, error)
	ConsumeResetProofByTokenHash(ctx context.Context, tx transaction, tokenHash []byte, now time.Time) (PasswordResetProof, error)
}

// verifiedAccountLookup resolves an eligible user from a verified active identifier.
type verifiedAccountLookup interface {
	ResolveVerified(ctx context.Context, kind IdentifierKind, raw string) (User, error)
}

// resetCompleteStore is the Identity persistence surface for password-reset completion.
type resetCompleteStore interface {
	ConsumeResetProofByTokenHash(ctx context.Context, tx transaction, tokenHash []byte, now time.Time) (PasswordResetProof, error)
	GetUserTx(ctx context.Context, tx transaction, id ID) (User, error)
	GetPasswordCredentialTx(ctx context.Context, tx transaction, userID ID) (PasswordCredential, error)
	UpsertPasswordCredentialTx(ctx context.Context, tx transaction, credential PasswordCredential) error
	RevokeSessionsForUserTx(ctx context.Context, tx transaction, userID ID, at time.Time) (int64, error)
}

// accountStore is the Identity persistence surface for signup account creation.
type accountStore interface {
	ConsumeSignupProofByTokenHash(ctx context.Context, tx transaction, tokenHash []byte, now time.Time) (SignupProof, error)
	GetActiveIdentifierTx(ctx context.Context, tx transaction, kind IdentifierKind, valueCanonical string) (UserIdentifier, error)
	InsertUserTx(ctx context.Context, tx transaction, user User) error
	InsertIdentifierTx(ctx context.Context, tx transaction, identifier UserIdentifier) error
	InsertPasswordCredentialTx(ctx context.Context, tx transaction, credential PasswordCredential) error
}
