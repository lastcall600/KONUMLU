package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"backend/internal/notifications/contracts"
	"backend/internal/platform/outbox"
)

var (
	_ transactor     = (*PostgresStore)(nil)
	_ intentEnqueuer = (*outbox.Outbox)(nil)
)

const (
	signupIntentIdempotencyPrefix = "notifications.intent:"
	signupAggregateType           = "verification_challenge"
)

// StartSignupVerificationInput is trusted orchestration input. No HTTP.
type StartSignupVerificationInput struct {
	Kind          IdentifierKind
	Destination   string
	Locale        string
	ClientIP      string
	CorrelationID string
}

// StartSignupVerificationResult is durable issuance outcome. It does not include the secret.
type StartSignupVerificationResult struct {
	ChallengeID ID
}

// FinishSignupVerificationInput is trusted orchestration input. No HTTP.
type FinishSignupVerificationInput struct {
	ChallengeID ID
	Code        string
}

// FinishSignupVerificationResult returns the raw signup proof once. It is not a session.
type FinishSignupVerificationResult struct {
	Verified    bool
	SignupProof string
}

// SignupVerification atomically issues a signup challenge, sealed material, and outbox intent.
type SignupVerification struct {
	challenges  *Challenges
	identifiers activeIdentifierLookup
	txns        transactor
	intents     intentEnqueuer
	proofs      *SignupProofs
}

func NewSignupVerification(challenges *Challenges, identifiers activeIdentifierLookup, txns transactor, intents intentEnqueuer, proofs *SignupProofs) (*SignupVerification, error) {
	if challenges == nil || identifiers == nil || txns == nil || intents == nil || proofs == nil {
		return nil, errStoreRequired
	}
	return &SignupVerification{
		challenges:  challenges,
		identifiers: identifiers,
		txns:        txns,
		intents:     intents,
		proofs:      proofs,
	}, nil
}

func (s *SignupVerification) StartSignupVerification(ctx context.Context, in StartSignupVerificationInput) (StartSignupVerificationResult, error) {
	if s == nil || s.challenges == nil || s.identifiers == nil || s.txns == nil || s.intents == nil {
		return StartSignupVerificationResult{}, errStoreRequired
	}
	if !contracts.ValidLocale(in.Locale) {
		return StartSignupVerificationResult{}, errInvalidChallenge
	}
	channel, err := signupChannel(in.Kind)
	if err != nil {
		return StartSignupVerificationResult{}, err
	}
	canonical, err := CanonicalizeIdentifier(in.Kind, in.Destination)
	if err != nil {
		return StartSignupVerificationResult{}, err
	}
	if s.challenges.limiter == nil {
		return StartSignupVerificationResult{}, errUnavailable
	}
	if err := s.challenges.limiter.Allow(ctx, in.Kind, ChallengeSignup, canonical, in.ClientIP); err != nil {
		return StartSignupVerificationResult{}, err
	}

	existing, err := s.identifiers.GetActiveIdentifier(ctx, in.Kind, canonical)
	if err == nil && existing.Active() {
		id, idErr := NewID()
		if idErr != nil {
			return StartSignupVerificationResult{}, errUnavailable
		}
		return StartSignupVerificationResult{ChallengeID: id}, nil
	}
	if err != nil && !errors.Is(err, errNotFound) {
		return StartSignupVerificationResult{}, mapSignupErr(err)
	}

	issued, sealed, err := s.challenges.prepareIssued(in.Kind, ChallengeSignup, canonical)
	if err != nil {
		return StartSignupVerificationResult{}, err
	}

	tx, err := s.txns.Begin(ctx)
	if err != nil {
		return StartSignupVerificationResult{}, mapSignupErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.challenges.store.InsertChallengeAndMaterial(ctx, tx, issued.Challenge, sealed); err != nil {
		return StartSignupVerificationResult{}, mapSignupErr(err)
	}

	intent, payload, err := signupIntent(issued.Challenge, channel, in.Locale, in.CorrelationID)
	if err != nil {
		return StartSignupVerificationResult{}, err
	}
	if _, err := s.intents.Enqueue(ctx, tx, outbox.NewEvent{
		EventType:      contracts.IntentEventType,
		EventVersion:   contracts.IntentEventVersion,
		AggregateType:  signupAggregateType,
		AggregateID:    issued.Challenge.ID.String(),
		Payload:        payload,
		IdempotencyKey: signupIntentIdempotencyPrefix + intent.IntentID,
		CorrelationID:  intent.CorrelationID,
	}); err != nil {
		return StartSignupVerificationResult{}, mapSignupErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return StartSignupVerificationResult{}, mapSignupErr(err)
	}
	return StartSignupVerificationResult{ChallengeID: issued.Challenge.ID}, nil
}

func (s *SignupVerification) FinishSignupVerification(ctx context.Context, in FinishSignupVerificationInput) (FinishSignupVerificationResult, error) {
	if s == nil || s.challenges == nil || s.proofs == nil {
		return FinishSignupVerificationResult{}, errStoreRequired
	}
	if in.ChallengeID.IsZero() || strings.TrimSpace(in.Code) == "" {
		return FinishSignupVerificationResult{}, errInvalidChallenge
	}
	ch, err := s.challenges.VerifyFor(ctx, in.ChallengeID, in.Code, ChallengeSignup)
	if err != nil {
		return FinishSignupVerificationResult{}, err
	}
	issued, err := s.proofs.Issue(ctx, ch)
	if err != nil {
		return FinishSignupVerificationResult{}, err
	}
	if issued.RawToken == "" {
		return FinishSignupVerificationResult{}, errUnavailable
	}
	return FinishSignupVerificationResult{Verified: true, SignupProof: issued.RawToken}, nil
}

func signupChannel(kind IdentifierKind) (contracts.Channel, error) {
	switch kind {
	case IdentifierEmail:
		return contracts.ChannelEmail, nil
	case IdentifierPhone:
		return contracts.ChannelSMS, nil
	default:
		return "", errInvalidChallenge
	}
}

func signupIntent(ch VerificationChallenge, channel contracts.Channel, locale, correlationID string) (contracts.Intent, json.RawMessage, error) {
	corr := strings.TrimSpace(correlationID)
	intent := contracts.Intent{
		IntentID:     ch.ID.String(),
		Version:      contracts.IntentEventVersion,
		Purpose:      contracts.PurposeSecurity,
		TemplateCode: contracts.TemplateIdentityVerificationSignup,
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

func mapSignupErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errChallengeThrottled) || errors.Is(err, errInvalidIdentifier) ||
		errors.Is(err, errInvalidChallenge) || errors.Is(err, errZeroID) ||
		errors.Is(err, errStoreRequired) {
		return err
	}
	return errUnavailable
}
