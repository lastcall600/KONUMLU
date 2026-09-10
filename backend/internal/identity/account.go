package identity

import (
	"context"
	"errors"
	"strings"
	"time"
)

// CompleteSignupInput is trusted orchestration input. No HTTP.
type CompleteSignupInput struct {
	SignupProof string
	Password    []byte
}

// CompleteSignupResult is the durable new account. It is not a session.
type CompleteSignupResult struct {
	UserID ID
}

// AccountCreation creates an Identity user from a consumed signup proof.
type AccountCreation struct {
	proofs    *SignupProofs
	passwords *Passwords
	accounts  accountStore
	txns      transactor
	now       func() time.Time
}

func NewAccountCreation(proofs *SignupProofs, passwords *Passwords, accounts accountStore, txns transactor, now func() time.Time) (*AccountCreation, error) {
	if proofs == nil || passwords == nil || accounts == nil || txns == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = proofs.now
	}
	return &AccountCreation{
		proofs:    proofs,
		passwords: passwords,
		accounts:  accounts,
		txns:      txns,
		now:       now,
	}, nil
}

func (a *AccountCreation) CompleteSignup(ctx context.Context, in CompleteSignupInput) (CompleteSignupResult, error) {
	if a == nil || a.proofs == nil || a.accounts == nil || a.txns == nil {
		return CompleteSignupResult{}, errStoreRequired
	}
	raw := strings.TrimSpace(in.SignupProof)
	if raw == "" {
		return CompleteSignupResult{}, errInvalidSignupProof
	}

	var passwordHash string
	if len(in.Password) > 0 {
		if a.passwords == nil {
			return CompleteSignupResult{}, errUnavailable
		}
		encoded, err := a.passwords.hashForCreate(in.Password)
		if err != nil {
			return CompleteSignupResult{}, err
		}
		passwordHash = encoded
	}

	tx, err := a.txns.Begin(ctx)
	if err != nil {
		return CompleteSignupResult{}, mapAccountErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	proof, err := a.proofs.Consume(ctx, tx, raw)
	if err != nil {
		return CompleteSignupResult{}, mapAccountErr(err)
	}

	existing, err := a.accounts.GetActiveIdentifierTx(ctx, tx, proof.Kind, proof.DestinationCanonical)
	if err == nil && existing.Active() {
		return CompleteSignupResult{}, errIdentifierConflict
	}
	if err != nil && !errors.Is(err, errNotFound) {
		return CompleteSignupResult{}, mapAccountErr(err)
	}

	now := a.now()
	userID, err := NewID()
	if err != nil {
		return CompleteSignupResult{}, errUnavailable
	}
	user := User{ID: userID, CreatedAt: now, UpdatedAt: now}
	if err := a.accounts.InsertUserTx(ctx, tx, user); err != nil {
		return CompleteSignupResult{}, mapAccountErr(err)
	}

	identID, err := NewID()
	if err != nil {
		return CompleteSignupResult{}, errUnavailable
	}
	verifiedAt := now
	ident := UserIdentifier{
		ID:             identID,
		UserID:         userID,
		Kind:           proof.Kind,
		ValueCanonical: proof.DestinationCanonical,
		VerifiedAt:     &verifiedAt,
		CreatedAt:      now,
	}
	if err := ident.Validate(); err != nil || !ident.VerifiedActive() {
		return CompleteSignupResult{}, errUnavailable
	}
	if err := a.accounts.InsertIdentifierTx(ctx, tx, ident); err != nil {
		return CompleteSignupResult{}, mapAccountErr(err)
	}

	if passwordHash != "" {
		cred := PasswordCredential{
			UserID:       userID,
			PasswordHash: passwordHash,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := a.accounts.InsertPasswordCredentialTx(ctx, tx, cred); err != nil {
			return CompleteSignupResult{}, mapAccountErr(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return CompleteSignupResult{}, mapAccountErr(err)
	}
	return CompleteSignupResult{UserID: userID}, nil
}

func mapAccountErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errUnavailable) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidSignupProof) || errors.Is(err, errSignupProofExpired) ||
		errors.Is(err, errSignupProofConsumed) || errors.Is(err, errIdentifierConflict) ||
		errors.Is(err, errInvalidPassword) || errors.Is(err, errPasswordTooLong) ||
		errors.Is(err, errInvalidPasswordPolicy) || errors.Is(err, errStoreRequired) ||
		errors.Is(err, errUnauthenticated) {
		return err
	}
	return errUnavailable
}
