package eids

import (
	"bytes"
	"context"
	"errors"

	"backend/internal/eids/trdecision"
	"backend/internal/platform/db"
)

type IngestResult struct {
	DecisionID   string
	Status       trdecision.Status
	Idempotent   bool
	Verification Verification
}

func (s *Service) SetSignedDecisionVerifier(v *trdecision.Verifier) {
	if s == nil {
		return
	}
	s.verifier = v
}

func (s *Service) IngestSignedDecision(ctx context.Context, env trdecision.Envelope) (IngestResult, error) {
	if s == nil || s.store == nil {
		return IngestResult{}, errStoreRequired
	}
	if s.verifier == nil {
		return IngestResult{}, errDecisionRejected
	}
	now := s.now().UTC()
	claims, err := s.verifier.Verify(env, now)
	if err != nil {
		return IngestResult{}, mapDecisionErr(err)
	}
	hash, err := claims.CanonicalHash(env.KeyID)
	if err != nil {
		return IngestResult{}, errDecisionRejected
	}
	var out IngestResult
	err = s.inTx(ctx, func(ctx context.Context) error {
		existing, getErr := s.store.GetReplay(ctx, claims.DecisionID)
		if getErr == nil {
			if !bytes.Equal(existing.ClaimsHash[:], hash[:]) {
				return errReplayConflict
			}
			binding, err := s.store.LookupSubject(ctx, claims.SubjectRef)
			if err != nil {
				return err
			}
			v, err := s.store.Get(ctx, binding.VerificationID)
			if err != nil {
				return err
			}
			out = IngestResult{DecisionID: claims.DecisionID, Status: claims.Status, Idempotent: true, Verification: v}
			return nil
		}
		if !errors.Is(getErr, errNotFound) {
			return getErr
		}
		binding, err := s.store.LookupSubject(ctx, claims.SubjectRef)
		if err != nil {
			if errors.Is(err, errNotFound) {
				return errUnmappedSubject
			}
			return err
		}
		if binding.VerificationType != VerificationType(claims.VerificationType) {
			return errDecisionRejected
		}
		current, err := s.store.Get(ctx, binding.VerificationID)
		if err != nil {
			return err
		}
		if current.ListingID != binding.ListingID || current.VerificationType != binding.VerificationType {
			return errDecisionRejected
		}
		rec := ReplayRecord{
			DecisionID:       claims.DecisionID,
			ClaimsHash:       hash,
			SubjectRef:       claims.SubjectRef,
			VerificationType: VerificationType(claims.VerificationType),
			Status:           string(claims.Status),
			IssuedAt:         claims.IssuedAt.UTC(),
			ValidUntil:       claims.ValidUntil.UTC(),
			KeyID:            env.KeyID,
			ReceivedAt:       now,
			AppliedAt:        now,
		}
		latestIssued, hasLatest, err := s.store.LatestReplayIssuedAt(ctx, claims.SubjectRef)
		if err != nil {
			return err
		}
		if hasLatest {
			switch rec.IssuedAt.Compare(latestIssued.UTC()) {
			case -1:
				if err := s.store.InsertReplay(ctx, rec); err != nil {
					return replayInsertErr(err, s.store, ctx, rec.DecisionID, hash, claims.Status, current, &out)
				}
				out = IngestResult{DecisionID: claims.DecisionID, Status: claims.Status, Verification: current}
				return nil
			case 0:
				return errReplayConflict
			}
		}
		until := claims.ValidUntil.UTC()
		next, err := current.ApplySignedDecision(claims.Status == trdecision.StatusApproved, &until, now)
		if err != nil {
			return err
		}
		if err := s.store.ApplyReplayAndUpdate(ctx, rec, next, current.UpdatedAt); err != nil {
			return replayInsertErr(err, s.store, ctx, rec.DecisionID, hash, claims.Status, current, &out)
		}
		out = IngestResult{DecisionID: claims.DecisionID, Status: claims.Status, Verification: next}
		return nil
	})
	if err != nil {
		return IngestResult{}, mapStoreErr(err)
	}
	return out, nil
}

func replayInsertErr(err error, st store, ctx context.Context, decisionID string, hash [32]byte, status trdecision.Status, current Verification, out *IngestResult) error {
	if !errors.Is(err, errConflict) {
		return err
	}
	again, getErr := st.GetReplay(ctx, decisionID)
	if getErr == nil && bytes.Equal(again.ClaimsHash[:], hash[:]) {
		*out = IngestResult{DecisionID: decisionID, Status: status, Idempotent: true, Verification: current}
		return nil
	}
	return errReplayConflict
}

func (s *Service) inTx(ctx context.Context, fn func(context.Context) error) error {
	pg, ok := s.store.(*PostgresStore)
	if !ok || pg == nil || pg.db == nil {
		return fn(ctx)
	}
	tx, err := pg.db.Begin(ctx)
	if err != nil {
		return errUnavailable
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := fn(db.WithTx(ctx, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return errUnavailable
	}
	return nil
}

func (s *Service) ensureSubjectRef(ctx context.Context, v Verification) error {
	if _, err := s.store.LookupSubjectByVerification(ctx, v.ID); err == nil {
		return nil
	} else if !errors.Is(err, errNotFound) {
		return err
	}
	ref, err := NewSubjectRef()
	if err != nil {
		return err
	}
	return s.store.BindSubject(ctx, SubjectBinding{
		SubjectRef:       ref,
		VerificationID:   v.ID,
		ListingID:        v.ListingID,
		VerificationType: v.VerificationType,
		CreatedAt:        s.now().UTC(),
	})
}

func mapDecisionErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, trdecision.ErrInvalidSignature), errors.Is(err, trdecision.ErrUnknownKey),
		errors.Is(err, trdecision.ErrInvalidAudience), errors.Is(err, trdecision.ErrInvalidKey):
		return errDecisionRejected
	case errors.Is(err, trdecision.ErrUnsupportedSchema), errors.Is(err, trdecision.ErrUnsupportedType),
		errors.Is(err, trdecision.ErrUnsupportedStatus), errors.Is(err, trdecision.ErrMalformedEnvelope),
		errors.Is(err, trdecision.ErrForbiddenField), errors.Is(err, trdecision.ErrInvalidClaims),
		errors.Is(err, trdecision.ErrInvalidTime), errors.Is(err, trdecision.ErrInvalidKeyID):
		return errInvalidVerification
	case errors.Is(err, trdecision.ErrExpired), errors.Is(err, trdecision.ErrStale),
		errors.Is(err, trdecision.ErrFutureIssuedAt), errors.Is(err, trdecision.ErrClockPolicy):
		return err
	default:
		return errDecisionRejected
	}
}
