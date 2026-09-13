package disputes

import (
	"context"
	"errors"
	"time"

	delcontracts "backend/internal/deliveries/contracts"
	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
	txncontracts "backend/internal/transactions/contracts"
)

type notifyEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

type Service struct {
	store      store
	txns       txncontracts.Lookup
	deliveries delcontracts.Lookup
	policy     Policy
	now        func() time.Time
	tx         Transactor
	outbox     notifyEnqueuer
}

func NewService(store store, txns txncontracts.Lookup, deliveries delcontracts.Lookup, policy Policy, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if policy == (Policy{}) {
		policy = DefaultPolicy()
	}
	normalized, err := policy.normalized()
	if err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, txns: txns, deliveries: deliveries, policy: normalized, now: now}, nil
}

func (s *Service) SetOutbox(tx Transactor, enqueuer notifyEnqueuer) {
	if s == nil {
		return
	}
	s.tx = tx
	s.outbox = enqueuer
}

type CreateInput struct {
	ReasonCode ReasonCode
	Statement  *string
}

type EvidenceInput struct {
	EvidenceType   EvidenceType
	Title          string
	Description    *string
	ReferenceValue *string
}

func (s *Service) CreateForTransaction(ctx context.Context, actorUserID, transactionID ID, in CreateInput) (Dispute, error) {
	if s == nil || s.store == nil {
		return Dispute{}, errStoreRequired
	}
	if actorUserID.IsZero() || transactionID.IsZero() {
		return Dispute{}, errZeroID
	}
	ref, err := s.getTransaction(ctx, transactionID)
	if err != nil {
		return Dispute{}, err
	}
	if !ref.Participant(txncontracts.ID(actorUserID)) {
		return Dispute{}, errNotFound
	}
	if existing, err := s.store.GetActiveByTransactionID(ctx, transactionID); err == nil {
		return existing, nil
	} else if !errors.Is(err, errNotFound) {
		return Dispute{}, mapStoreErr(err)
	}
	concluded, err := s.store.ListConcludedByTransactionID(ctx, transactionID)
	if err != nil {
		return Dispute{}, mapStoreErr(err)
	}
	if len(concluded) > 0 {
		return Dispute{}, errConcluded
	}
	if !ref.MayOpenDispute() {
		return Dispute{}, errNotEligible
	}
	if err := s.assertWindow(ref); err != nil {
		return Dispute{}, err
	}
	s.observeDelivery(ctx, in.ReasonCode, transactionID)
	created, err := CreateOpen(CreateCommand{
		TransactionID:   ID(ref.ID),
		RequesterUserID: ID(ref.RequesterUserID),
		ProviderUserID:  ID(ref.ProviderUserID),
		OpenedByUserID:  actorUserID,
		ReasonCode:      in.ReasonCode,
		Statement:       in.Statement,
	}, s.now().UTC())
	if err != nil {
		return Dispute{}, err
	}
	commit := func(ctx context.Context) error {
		if err := s.store.Create(ctx, created); err != nil {
			return mapStoreErr(err)
		}
		return s.enqueueUpdated(ctx, created, actorUserID)
	}
	if err := s.runMaybeTx(ctx, commit); err != nil {
		if errors.Is(err, errConflict) {
			existing, findErr := s.store.GetActiveByTransactionID(ctx, transactionID)
			if findErr == nil {
				return existing, nil
			}
			return Dispute{}, errConflict
		}
		return Dispute{}, err
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, userID, disputeID ID) (Dispute, error) {
	d, err := s.authorizeParticipant(ctx, userID, disputeID)
	if err != nil {
		return Dispute{}, err
	}
	return d, nil
}

func (s *Service) GetInternal(ctx context.Context, disputeID ID) (Dispute, error) {
	return s.get(ctx, disputeID)
}

func (s *Service) ListMine(ctx context.Context, userID ID) ([]Dispute, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if userID.IsZero() {
		return nil, errZeroID
	}
	list, err := s.store.ListForParticipant(ctx, userID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	if list == nil {
		return []Dispute{}, nil
	}
	return list, nil
}

func (s *Service) Withdraw(ctx context.Context, actorUserID, disputeID ID) (Dispute, error) {
	return s.applyParticipant(ctx, actorUserID, disputeID, func(d Dispute, now time.Time) (Dispute, bool, error) {
		return d.Withdraw(now)
	})
}

func (s *Service) StartReview(ctx context.Context, disputeID ID) (Dispute, error) {
	current, err := s.get(ctx, disputeID)
	if err != nil {
		return Dispute{}, err
	}
	return s.apply(ctx, current, ID{}, func(d Dispute, now time.Time) (Dispute, bool, error) {
		return d.StartReview(now)
	})
}

func (s *Service) Resolve(ctx context.Context, disputeID ID, code ResolutionCode) (Dispute, error) {
	current, err := s.get(ctx, disputeID)
	if err != nil {
		return Dispute{}, err
	}
	return s.apply(ctx, current, ID{}, func(d Dispute, now time.Time) (Dispute, bool, error) {
		return d.Resolve(code, now)
	})
}

func (s *Service) Close(ctx context.Context, disputeID ID) (Dispute, error) {
	current, err := s.get(ctx, disputeID)
	if err != nil {
		return Dispute{}, err
	}
	return s.apply(ctx, current, ID{}, func(d Dispute, now time.Time) (Dispute, bool, error) {
		return d.Close(now)
	})
}

func (s *Service) AddEvidence(ctx context.Context, actorUserID, disputeID ID, in EvidenceInput) (Evidence, error) {
	d, err := s.authorizeParticipant(ctx, actorUserID, disputeID)
	if err != nil {
		return Evidence{}, err
	}
	if in.EvidenceType == EvidenceInternalReference {
		return Evidence{}, errInvalidEvidence
	}
	role := d.RoleOf(actorUserID)
	if role == "" {
		return Evidence{}, errNotFound
	}
	return s.appendEvidence(ctx, d, EvidenceCommand{
		DisputeID:      d.ID,
		EvidenceType:   in.EvidenceType,
		Title:          in.Title,
		Description:    in.Description,
		ReferenceValue: in.ReferenceValue,
		ActorUserID:    &actorUserID,
		ActorRole:      role,
	})
}

func (s *Service) AddInternalEvidence(ctx context.Context, disputeID ID, in EvidenceInput, actorStaffID *ID) (Evidence, error) {
	d, err := s.get(ctx, disputeID)
	if err != nil {
		return Evidence{}, err
	}
	if in.EvidenceType == EvidenceType("") {
		in.EvidenceType = EvidenceInternalReference
	}
	if in.EvidenceType != EvidenceInternalReference {
		return Evidence{}, errInvalidEvidence
	}
	return s.appendEvidence(ctx, d, EvidenceCommand{
		DisputeID:      d.ID,
		EvidenceType:   EvidenceInternalReference,
		Title:          in.Title,
		Description:    in.Description,
		ReferenceValue: in.ReferenceValue,
		ActorUserID:    cloneID(actorStaffID),
		ActorRole:      RoleInternal,
	})
}

func (s *Service) ListEvidence(ctx context.Context, userID, disputeID ID) ([]Evidence, error) {
	if _, err := s.authorizeParticipant(ctx, userID, disputeID); err != nil {
		return nil, err
	}
	return s.listEvidence(ctx, disputeID)
}

func (s *Service) ListEvidenceInternal(ctx context.Context, disputeID ID) ([]Evidence, error) {
	if _, err := s.get(ctx, disputeID); err != nil {
		return nil, err
	}
	return s.listEvidence(ctx, disputeID)
}

func (s *Service) appendEvidence(ctx context.Context, d Dispute, cmd EvidenceCommand) (Evidence, error) {
	if !d.AcceptsEvidence() {
		return Evidence{}, errInvalidTransition
	}
	created, err := CreateEvidence(cmd, s.now().UTC())
	if err != nil {
		return Evidence{}, err
	}
	if err := s.store.AppendEvidence(ctx, created); err != nil {
		return Evidence{}, mapStoreErr(err)
	}
	return created, nil
}

func (s *Service) listEvidence(ctx context.Context, disputeID ID) ([]Evidence, error) {
	list, err := s.store.ListEvidence(ctx, disputeID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	if list == nil {
		return []Evidence{}, nil
	}
	return list, nil
}

func (s *Service) applyParticipant(ctx context.Context, actorUserID, disputeID ID, fn func(Dispute, time.Time) (Dispute, bool, error)) (Dispute, error) {
	current, err := s.authorizeParticipant(ctx, actorUserID, disputeID)
	if err != nil {
		return Dispute{}, err
	}
	return s.apply(ctx, current, actorUserID, fn)
}

func (s *Service) authorizeParticipant(ctx context.Context, actorUserID, disputeID ID) (Dispute, error) {
	current, err := s.get(ctx, disputeID)
	if err != nil {
		return Dispute{}, err
	}
	if !current.Participant(actorUserID) {
		return Dispute{}, errNotFound
	}
	return current, nil
}

func (s *Service) apply(ctx context.Context, current Dispute, actorUserID ID, fn func(Dispute, time.Time) (Dispute, bool, error)) (Dispute, error) {
	next, changed, err := fn(current, s.now().UTC())
	if err != nil {
		return Dispute{}, err
	}
	if !changed {
		return current, nil
	}
	commit := func(ctx context.Context) error {
		if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
			return mapStoreErr(err)
		}
		return s.enqueueUpdated(ctx, next, actorUserID)
	}
	if err := s.runMaybeTx(ctx, commit); err != nil {
		if !errors.Is(err, errConflict) {
			return Dispute{}, err
		}
		latest, getErr := s.get(ctx, current.ID)
		if getErr != nil {
			return Dispute{}, getErr
		}
		_, againChanged, againErr := fn(latest, s.now().UTC())
		if againErr != nil {
			return Dispute{}, againErr
		}
		if !againChanged {
			return latest, nil
		}
		return Dispute{}, errConflict
	}
	return next, nil
}

func (s *Service) enqueueUpdated(ctx context.Context, d Dispute, actorUserID ID) error {
	if s == nil || s.outbox == nil {
		return nil
	}
	ev, err := encodeUpdated(d, actorUserID)
	if err != nil {
		return err
	}
	if _, err := s.outbox.Enqueue(ctx, nil, ev); err != nil {
		if errors.Is(err, outbox.ErrConflict) {
			return nil
		}
		return mapStoreErr(err)
	}
	return nil
}

func (s *Service) runMaybeTx(ctx context.Context, fn func(context.Context) error) error {
	if s.outbox != nil && s.tx != nil && db.TxFrom(ctx) == nil {
		tx, err := s.tx.Begin(ctx)
		if err != nil {
			return mapStoreErr(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := fn(tx.Context(ctx)); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return mapStoreErr(err)
		}
		return nil
	}
	return fn(ctx)
}

func (s *Service) get(ctx context.Context, id ID) (Dispute, error) {
	if s == nil || s.store == nil {
		return Dispute{}, errStoreRequired
	}
	if id.IsZero() {
		return Dispute{}, errZeroID
	}
	d, err := s.store.Get(ctx, id)
	if err != nil {
		return Dispute{}, mapStoreErr(err)
	}
	return d, nil
}

func (s *Service) getTransaction(ctx context.Context, transactionID ID) (txncontracts.TransactionRef, error) {
	if s == nil || s.txns == nil {
		return txncontracts.TransactionRef{}, errUnavailable
	}
	ref, err := s.txns.GetTransaction(ctx, txncontracts.ID(transactionID))
	if err != nil {
		return txncontracts.TransactionRef{}, mapTxnErr(err)
	}
	return ref, nil
}

func (s *Service) assertWindow(ref txncontracts.TransactionRef) error {
	anchor := ref.DisputeAnchor()
	if anchor.IsZero() {
		return errNotEligible
	}
	if s.now().UTC().Sub(anchor) > s.policy.Window {
		return errWindowClosed
	}
	return nil
}

// observeDelivery is a read-only, optional context check for non_delivery.
// Missing delivery does not reject the dispute. Nothing is mutated.
func (s *Service) observeDelivery(ctx context.Context, reason ReasonCode, transactionID ID) {
	if s == nil || s.deliveries == nil || reason != ReasonNonDelivery {
		return
	}
	_, _ = s.deliveries.GetDeliveryByTransaction(ctx, delcontracts.ID(transactionID))
}

func mapTxnErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, txncontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, txncontracts.ErrNotFound) {
		return errNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, errConflict) || errors.Is(err, db.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, db.ErrUnavailable) {
		return errUnavailable
	}
	if errors.Is(err, errStoreRequired) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidDispute) || errors.Is(err, errInvalidStatus) ||
		errors.Is(err, errInvalidTransition) || errors.Is(err, errInvalidReason) ||
		errors.Is(err, errInvalidStatement) || errors.Is(err, errInvalidResolution) ||
		errors.Is(err, errInvalidEvidence) || errors.Is(err, errInvalidPolicy) ||
		errors.Is(err, errForbidden) || errors.Is(err, errNotEligible) ||
		errors.Is(err, errConcluded) || errors.Is(err, errWindowClosed) {
		return err
	}
	if errors.Is(err, outbox.ErrUnavailable) || errors.Is(err, outbox.ErrInvalidEvent) ||
		errors.Is(err, outbox.ErrSensitivePayload) {
		return errUnavailable
	}
	return errUnavailable
}
