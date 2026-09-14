package deliveries

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
	txncontracts "backend/internal/transactions/contracts"
)

type notifyEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

type Service struct {
	store  store
	txns   txncontracts.Lookup
	now    func() time.Time
	tx     Transactor
	outbox notifyEnqueuer
}

func NewService(store store, txns txncontracts.Lookup, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, txns: txns, now: now}, nil
}

func (s *Service) SetOutbox(tx Transactor, enqueuer notifyEnqueuer) {
	if s == nil {
		return
	}
	s.tx = tx
	s.outbox = enqueuer
}

type CreateInput struct {
	Eligibility string
	Method      *Method
	Note        *string
}

func (s *Service) CreateForTransaction(ctx context.Context, actorUserID, transactionID ID, in CreateInput) (Delivery, error) {
	if s == nil || s.store == nil {
		return Delivery{}, errStoreRequired
	}
	if actorUserID.IsZero() || transactionID.IsZero() {
		return Delivery{}, errZeroID
	}
	ref, err := s.getTransaction(ctx, transactionID)
	if err != nil {
		return Delivery{}, err
	}
	if !ref.Participant(txncontracts.ID(actorUserID)) {
		return Delivery{}, errNotFound
	}
	existing, err := s.store.GetByTransactionID(ctx, transactionID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, errNotFound) {
		return Delivery{}, mapStoreErr(err)
	}
	if !ref.MayCreateDelivery() {
		return Delivery{}, errNotEligible
	}
	created, err := CreatePending(CreateCommand{
		TransactionID:   ID(ref.ID),
		RequesterUserID: ID(ref.RequesterUserID),
		ProviderUserID:  ID(ref.ProviderUserID),
		Eligibility:     in.Eligibility,
		Method:          in.Method,
		Note:            in.Note,
	}, s.now().UTC())
	if err != nil {
		return Delivery{}, err
	}
	commit := func(ctx context.Context) error {
		if err := s.store.Create(ctx, created); err != nil {
			return mapStoreErr(err)
		}
		return s.enqueueStatus(ctx, created, actorUserID)
	}
	if err := s.runMaybeTx(ctx, commit); err != nil {
		if errors.Is(err, errConflict) {
			existing, findErr := s.store.GetByTransactionID(ctx, transactionID)
			if findErr == nil {
				return existing, nil
			}
			return Delivery{}, errConflict
		}
		return Delivery{}, err
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, userID, deliveryID ID) (Delivery, error) {
	d, err := s.get(ctx, deliveryID)
	if err != nil {
		return Delivery{}, err
	}
	if !d.Participant(userID) {
		return Delivery{}, errNotFound
	}
	return d, nil
}

func (s *Service) ListMine(ctx context.Context, userID ID) ([]Delivery, error) {
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
		return []Delivery{}, nil
	}
	return list, nil
}

func (s *Service) MarkReady(ctx context.Context, actorUserID, deliveryID ID) (Delivery, error) {
	return s.applyProvider(ctx, actorUserID, deliveryID, func(d Delivery, now time.Time) (Delivery, bool, error) {
		return d.MarkReady(now)
	})
}

func (s *Service) MarkInTransit(ctx context.Context, actorUserID, deliveryID ID) (Delivery, error) {
	return s.applyProvider(ctx, actorUserID, deliveryID, func(d Delivery, now time.Time) (Delivery, bool, error) {
		return d.MarkInTransit(now)
	})
}

func (s *Service) MarkDelivered(ctx context.Context, actorUserID, deliveryID ID) (Delivery, error) {
	return s.applyProvider(ctx, actorUserID, deliveryID, func(d Delivery, now time.Time) (Delivery, bool, error) {
		return d.MarkDelivered(now)
	})
}

func (s *Service) Cancel(ctx context.Context, actorUserID, deliveryID ID) (Delivery, error) {
	return s.applyParticipant(ctx, actorUserID, deliveryID, func(d Delivery, now time.Time) (Delivery, bool, error) {
		return d.Cancel(now)
	})
}

func (s *Service) applyProvider(ctx context.Context, actorUserID, deliveryID ID, fn func(Delivery, time.Time) (Delivery, bool, error)) (Delivery, error) {
	current, err := s.authorizeParticipant(ctx, actorUserID, deliveryID)
	if err != nil {
		return Delivery{}, err
	}
	if !current.Provider(actorUserID) {
		return Delivery{}, errNotFound
	}
	return s.apply(ctx, current, actorUserID, fn, true)
}

func (s *Service) applyParticipant(ctx context.Context, actorUserID, deliveryID ID, fn func(Delivery, time.Time) (Delivery, bool, error)) (Delivery, error) {
	current, err := s.authorizeParticipant(ctx, actorUserID, deliveryID)
	if err != nil {
		return Delivery{}, err
	}
	return s.apply(ctx, current, actorUserID, fn, false)
}

func (s *Service) authorizeParticipant(ctx context.Context, actorUserID, deliveryID ID) (Delivery, error) {
	current, err := s.get(ctx, deliveryID)
	if err != nil {
		return Delivery{}, err
	}
	if !current.Participant(actorUserID) {
		return Delivery{}, errNotFound
	}
	return current, nil
}

func (s *Service) apply(ctx context.Context, current Delivery, actorUserID ID, fn func(Delivery, time.Time) (Delivery, bool, error), refuseCancelledTxn bool) (Delivery, error) {
	next, changed, err := fn(current, s.now().UTC())
	if err != nil {
		return Delivery{}, err
	}
	if !changed {
		return current, nil
	}
	if refuseCancelledTxn {
		if err := s.refuseIfTransactionCancelled(ctx, current.TransactionID); err != nil {
			return Delivery{}, err
		}
	}
	commit := func(ctx context.Context) error {
		if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
			return mapStoreErr(err)
		}
		return s.enqueueStatus(ctx, next, actorUserID)
	}
	if err := s.runMaybeTx(ctx, commit); err != nil {
		if !errors.Is(err, errConflict) {
			return Delivery{}, err
		}
		latest, getErr := s.get(ctx, current.ID)
		if getErr != nil {
			return Delivery{}, getErr
		}
		_, againChanged, againErr := fn(latest, s.now().UTC())
		if againErr != nil {
			return Delivery{}, againErr
		}
		if !againChanged {
			return latest, nil
		}
		return Delivery{}, errConflict
	}
	return next, nil
}

func (s *Service) enqueueStatus(ctx context.Context, d Delivery, actorUserID ID) error {
	if s == nil || s.outbox == nil {
		return nil
	}
	ev, err := encodeStatusChanged(d, actorUserID)
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

func (s *Service) get(ctx context.Context, id ID) (Delivery, error) {
	if s == nil || s.store == nil {
		return Delivery{}, errStoreRequired
	}
	if id.IsZero() {
		return Delivery{}, errZeroID
	}
	d, err := s.store.Get(ctx, id)
	if err != nil {
		return Delivery{}, mapStoreErr(err)
	}
	return d, nil
}

func (s *Service) refuseIfTransactionCancelled(ctx context.Context, transactionID ID) error {
	ref, err := s.getTransaction(ctx, transactionID)
	if err != nil {
		return err
	}
	if ref.Status == "cancelled" {
		return errNotEligible
	}
	return nil
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
		errors.Is(err, errInvalidDelivery) || errors.Is(err, errInvalidStatus) ||
		errors.Is(err, errInvalidTransition) || errors.Is(err, errInvalidMethod) ||
		errors.Is(err, errInvalidNote) || errors.Is(err, errInvalidEligibility) ||
		errors.Is(err, errForbidden) || errors.Is(err, errNotEligible) {
		return err
	}
	if errors.Is(err, outbox.ErrUnavailable) || errors.Is(err, outbox.ErrInvalidEvent) ||
		errors.Is(err, outbox.ErrSensitivePayload) {
		return errUnavailable
	}
	return errUnavailable
}
