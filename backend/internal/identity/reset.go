package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"backend/internal/notifications/contracts"
	"backend/internal/platform/outbox"
)

const (
	resetIntentIdempotencyPrefix = "notifications.intent:"
	resetAggregateType           = "verification_challenge"
)

// StartPasswordResetInput is trusted orchestration input. No HTTP.
type StartPasswordResetInput struct {
	Kind          IdentifierKind
	Destination   string
	Locale        string
	ClientIP      string
	CorrelationID string
}

// StartPasswordResetResult is a generic accepted outcome. It does not include the secret.
type StartPasswordResetResult struct {
	ChallengeID ID
}

// VerifyPasswordResetInput is trusted orchestration input. No HTTP.
type VerifyPasswordResetInput struct {
	ChallengeID ID
	Code        string
}

// VerifyPasswordResetResult returns the raw reset proof once. It is not a session.
type VerifyPasswordResetResult struct {
	ResetProof string
}

// CompletePasswordResetInput is trusted orchestration input. No HTTP.
type CompletePasswordResetInput struct {
	ResetProof  string
	NewPassword []byte
}

// CompletePasswordResetResult is durable completion. It is not a session.
type CompletePasswordResetResult struct {
	UserID       ID
	SessionEpoch int64
}

// PasswordReset issues reset challenges, proofs, and completes password replacement.
type PasswordReset struct {
	challenges  *Challenges
	identifiers verifiedAccountLookup
	txns        transactor
	intents     intentEnqueuer
	proofs      *ResetProofs
	passwords   *Passwords
	complete    resetCompleteStore
	sessions    *Sessions
}

func NewPasswordReset(challenges *Challenges, identifiers verifiedAccountLookup, txns transactor, intents intentEnqueuer, proofs *ResetProofs, passwords *Passwords, complete resetCompleteStore, sessions *Sessions) (*PasswordReset, error) {
	if challenges == nil || identifiers == nil || txns == nil || intents == nil || proofs == nil || passwords == nil || complete == nil {
		return nil, errStoreRequired
	}
	return &PasswordReset{
		challenges:  challenges,
		identifiers: identifiers,
		txns:        txns,
		intents:     intents,
		proofs:      proofs,
		passwords:   passwords,
		complete:    complete,
		sessions:    sessions,
	}, nil
}

func (r *PasswordReset) StartPasswordReset(ctx context.Context, in StartPasswordResetInput) (StartPasswordResetResult, error) {
	if r == nil || r.challenges == nil || r.identifiers == nil || r.txns == nil || r.intents == nil {
		return StartPasswordResetResult{}, errStoreRequired
	}
	if !contracts.ValidLocale(in.Locale) {
		return StartPasswordResetResult{}, errInvalidChallenge
	}
	channel, err := signupChannel(in.Kind)
	if err != nil {
		return StartPasswordResetResult{}, err
	}
	canonical, err := CanonicalizeIdentifier(in.Kind, in.Destination)
	if err != nil {
		return StartPasswordResetResult{}, err
	}
	if r.challenges.limiter == nil {
		return StartPasswordResetResult{}, errUnavailable
	}
	if err := r.challenges.limiter.Allow(ctx, in.Kind, ChallengePasswordReset, canonical, in.ClientIP); err != nil {
		return StartPasswordResetResult{}, err
	}

	user, err := r.identifiers.ResolveVerified(ctx, in.Kind, canonical)
	if err != nil {
		if coverResetStart(err) {
			return acceptedResetWithoutAuthority()
		}
		return StartPasswordResetResult{}, mapResetErr(err)
	}
	if !user.EligibleForSession() {
		return acceptedResetWithoutAuthority()
	}

	issued, sealed, err := r.challenges.prepareIssued(in.Kind, ChallengePasswordReset, canonical)
	if err != nil {
		return StartPasswordResetResult{}, err
	}

	tx, err := r.txns.Begin(ctx)
	if err != nil {
		return StartPasswordResetResult{}, mapResetErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.challenges.store.InsertChallengeAndMaterial(ctx, tx, issued.Challenge, sealed); err != nil {
		return StartPasswordResetResult{}, mapResetErr(err)
	}

	intent, payload, err := resetIntent(issued.Challenge, channel, in.Locale, in.CorrelationID)
	if err != nil {
		return StartPasswordResetResult{}, err
	}
	if _, err := r.intents.Enqueue(ctx, tx, outbox.NewEvent{
		EventType:      contracts.IntentEventType,
		EventVersion:   contracts.IntentEventVersion,
		AggregateType:  resetAggregateType,
		AggregateID:    issued.Challenge.ID.String(),
		Payload:        payload,
		IdempotencyKey: resetIntentIdempotencyPrefix + intent.IntentID,
		CorrelationID:  intent.CorrelationID,
	}); err != nil {
		return StartPasswordResetResult{}, mapResetErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return StartPasswordResetResult{}, mapResetErr(err)
	}
	return StartPasswordResetResult{ChallengeID: issued.Challenge.ID}, nil
}

func (r *PasswordReset) VerifyPasswordReset(ctx context.Context, in VerifyPasswordResetInput) (VerifyPasswordResetResult, error) {
	if r == nil || r.challenges == nil || r.proofs == nil || r.identifiers == nil {
		return VerifyPasswordResetResult{}, errStoreRequired
	}
	if in.ChallengeID.IsZero() || strings.TrimSpace(in.Code) == "" {
		return VerifyPasswordResetResult{}, errInvalidChallenge
	}
	ch, err := r.challenges.VerifyFor(ctx, in.ChallengeID, in.Code, ChallengePasswordReset)
	if err != nil {
		return VerifyPasswordResetResult{}, err
	}
	user, err := r.identifiers.ResolveVerified(ctx, ch.Kind, ch.DestinationCanonical)
	if err != nil {
		return VerifyPasswordResetResult{}, errInvalidChallenge
	}
	issued, err := r.proofs.Issue(ctx, ch, user.ID)
	if err != nil {
		return VerifyPasswordResetResult{}, err
	}
	if issued.RawToken == "" {
		return VerifyPasswordResetResult{}, errUnavailable
	}
	return VerifyPasswordResetResult{ResetProof: issued.RawToken}, nil
}

func (r *PasswordReset) CompletePasswordReset(ctx context.Context, in CompletePasswordResetInput) (CompletePasswordResetResult, error) {
	if r == nil || r.proofs == nil || r.passwords == nil || r.complete == nil || r.txns == nil {
		return CompletePasswordResetResult{}, errStoreRequired
	}
	raw := strings.TrimSpace(in.ResetProof)
	if raw == "" {
		return CompletePasswordResetResult{}, errInvalidResetProof
	}
	encoded, err := r.passwords.hashForCreate(in.NewPassword)
	if err != nil {
		return CompletePasswordResetResult{}, err
	}

	tx, err := r.txns.Begin(ctx)
	if err != nil {
		return CompletePasswordResetResult{}, mapResetErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	proof, err := r.proofs.Consume(ctx, tx, raw)
	if err != nil {
		return CompletePasswordResetResult{}, mapResetErr(err)
	}

	user, err := r.complete.GetUserTx(ctx, tx, proof.UserID)
	if err != nil {
		return CompletePasswordResetResult{}, mapResetErr(mapLookupErr(err, errAccountIneligible))
	}
	if !user.EligibleForSession() || user.ID != proof.UserID {
		return CompletePasswordResetResult{}, errAccountIneligible
	}

	now := r.proofs.now()
	cred := PasswordCredential{
		UserID:       proof.UserID,
		PasswordHash: encoded,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	existing, err := r.complete.GetPasswordCredentialTx(ctx, tx, proof.UserID)
	if err == nil {
		cred.CreatedAt = existing.CreatedAt
	} else if !errors.Is(err, errNotFound) {
		return CompletePasswordResetResult{}, mapResetErr(err)
	}
	if err := r.complete.UpsertPasswordCredentialTx(ctx, tx, cred); err != nil {
		return CompletePasswordResetResult{}, mapResetErr(err)
	}

	epoch, err := r.complete.RevokeSessionsForUserTx(ctx, tx, proof.UserID, now)
	if err != nil {
		return CompletePasswordResetResult{}, mapResetErr(err)
	}

	if err := enqueueAuthSecurity(ctx, r.intents, tx, SecurityRecord{
		Type:      AuthEventPasswordResetCompleted,
		UserID:    proof.UserID,
		Operation: AuthOpResetComplete,
		Result:    AuthResultSuccess,
	}, now); err != nil {
		return CompletePasswordResetResult{}, mapResetErr(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return CompletePasswordResetResult{}, mapResetErr(err)
	}
	if r.sessions != nil {
		r.sessions.ApplyUserEpoch(ctx, proof.UserID, epoch)
	}
	return CompletePasswordResetResult{UserID: proof.UserID, SessionEpoch: epoch}, nil
}

func acceptedResetWithoutAuthority() (StartPasswordResetResult, error) {
	id, err := NewID()
	if err != nil {
		return StartPasswordResetResult{}, errUnavailable
	}
	return StartPasswordResetResult{ChallengeID: id}, nil
}

func coverResetStart(err error) bool {
	return errors.Is(err, errUnauthenticated) || errors.Is(err, errAccountIneligible) || errors.Is(err, errNotFound)
}

func resetIntent(ch VerificationChallenge, channel contracts.Channel, locale, correlationID string) (contracts.Intent, json.RawMessage, error) {
	corr := strings.TrimSpace(correlationID)
	intent := contracts.Intent{
		IntentID:     ch.ID.String(),
		Version:      contracts.IntentEventVersion,
		Purpose:      contracts.PurposeSecurity,
		TemplateCode: contracts.TemplateIdentityPasswordReset,
		Channel:      channel,
		Locale:       locale,
		Recipient: contracts.RecipientRef{
			Kind: contracts.RecipientVerificationChallenge,
			ID:   ch.ID.String(),
		},
		CorrelationID: corr,
		CreatedAt:     ch.CreatedAt.UTC(),
	}
	if err := intent.Validate(); err != nil {
		return contracts.Intent{}, nil, errUnavailable
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return contracts.Intent{}, nil, errUnavailable
	}
	if _, err := contracts.DecodeIntent(raw); err != nil {
		return contracts.Intent{}, nil, errUnavailable
	}
	return intent, raw, nil
}

func mapResetErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errChallengeThrottled) || errors.Is(err, errInvalidIdentifier) ||
		errors.Is(err, errInvalidChallenge) || errors.Is(err, errZeroID) ||
		errors.Is(err, errStoreRequired) || errors.Is(err, errInvalidResetProof) ||
		errors.Is(err, errResetProofExpired) || errors.Is(err, errResetProofConsumed) ||
		errors.Is(err, errInvalidPassword) || errors.Is(err, errPasswordTooLong) ||
		errors.Is(err, errInvalidPasswordPolicy) || errors.Is(err, errAccountIneligible) ||
		errors.Is(err, errUnauthenticated) {
		return err
	}
	return errUnavailable
}
