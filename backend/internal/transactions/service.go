package transactions

import (
	"context"
	"errors"
	"time"

	needcontracts "backend/internal/needs/contracts"
	offercontracts "backend/internal/offers/contracts"
	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
	txncontracts "backend/internal/transactions/contracts"
)

type completionEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

type Service struct {
	store  store
	offers offercontracts.Lookup
	needs  needcontracts.Lookup
	now    func() time.Time
	tx     Transactor
	outbox completionEnqueuer
}

func NewService(store store, offers offercontracts.Lookup, needs needcontracts.Lookup, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, offers: offers, needs: needs, now: now}, nil
}

// SetOutbox enables transactional completion events. Nil enqueuer disables emit.
func (s *Service) SetOutbox(tx Transactor, enqueuer completionEnqueuer) {
	if s == nil {
		return
	}
	s.tx = tx
	s.outbox = enqueuer
}

func (s *Service) CreateFromAcceptedOffer(ctx context.Context, requesterUserID, offerID ID) (Transaction, error) {
	if s == nil || s.store == nil {
		return Transaction{}, errStoreRequired
	}
	if requesterUserID.IsZero() || offerID.IsZero() {
		return Transaction{}, errZeroID
	}
	offer, err := s.getOffer(ctx, offerID)
	if err != nil {
		return Transaction{}, err
	}
	need, err := s.getNeed(ctx, ID(offer.NeedID))
	if err != nil {
		return Transaction{}, err
	}
	if ID(need.RequesterUserID) != requesterUserID || ID(need.ID) != ID(offer.NeedID) {
		return Transaction{}, errNotFound
	}
	if offer.Status != "accepted" {
		return Transaction{}, errNotAccepted
	}
	existing, err := s.store.GetByOfferID(ctx, ID(offer.ID))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, errNotFound) {
		return Transaction{}, mapStoreErr(err)
	}
	if err := requireNeedOpen(need.Status); err != nil {
		return Transaction{}, err
	}
	src := AcceptedSource{
		OfferID:            ID(offer.ID),
		NeedID:             ID(offer.NeedID),
		RequesterUserID:    ID(need.RequesterUserID),
		ProviderUserID:     ID(offer.ProviderUserID),
		ProviderBusinessID: ID(offer.ProviderBusinessID),
		ServiceID:          ID(offer.ServiceID),
	}
	if offer.Price != nil {
		src.Price = &Price{Amount: offer.Price.Amount, Currency: offer.Price.Currency}
	}
	txn, err := CreatePending(src, s.now().UTC())
	if err != nil {
		return Transaction{}, err
	}
	commit := func(ctx context.Context) error {
		if err := s.store.Create(ctx, txn); err != nil {
			return mapStoreErr(err)
		}
		return s.enqueueLifecycle(ctx, txncontracts.EventTypeCreated, "created", txn, requesterUserID)
	}
	if err := s.runMaybeTx(ctx, commit); err != nil {
		if errors.Is(err, errConflict) {
			existing, findErr := s.store.GetByOfferID(ctx, ID(offer.ID))
			if findErr == nil {
				return existing, nil
			}
			return Transaction{}, errConflict
		}
		return Transaction{}, err
	}
	return txn, nil
}

func (s *Service) Get(ctx context.Context, userID, transactionID ID) (Transaction, error) {
	txn, err := s.get(ctx, transactionID)
	if err != nil {
		return Transaction{}, err
	}
	if !txn.Participant(userID) {
		return Transaction{}, errNotFound
	}
	return txn, nil
}

func (s *Service) ListMine(ctx context.Context, userID ID) ([]Transaction, error) {
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
		return []Transaction{}, nil
	}
	return list, nil
}

func (s *Service) Start(ctx context.Context, userID, transactionID ID) (Transaction, error) {
	return s.apply(ctx, userID, transactionID, func(txn Transaction, now time.Time) (Transaction, bool, error) {
		if txn.Status == StatusPending {
			if err := s.requireNeedOpenForTxn(ctx, txn); err != nil {
				return Transaction{}, false, err
			}
		}
		return txn.Start(now)
	}, "")
}

func (s *Service) Complete(ctx context.Context, userID, transactionID ID) (Transaction, error) {
	return s.apply(ctx, userID, transactionID, func(txn Transaction, now time.Time) (Transaction, bool, error) {
		if txn.Status == StatusActive {
			if err := s.requireNeedCompletableForTxn(ctx, txn); err != nil {
				return Transaction{}, false, err
			}
		}
		return txn.Complete(now)
	}, "completed")
}

func (s *Service) Cancel(ctx context.Context, userID, transactionID ID) (Transaction, error) {
	return s.apply(ctx, userID, transactionID, func(txn Transaction, now time.Time) (Transaction, bool, error) {
		return txn.Cancel(now)
	}, "cancelled")
}

func (s *Service) apply(ctx context.Context, userID, transactionID ID, fn func(Transaction, time.Time) (Transaction, bool, error), emit string) (Transaction, error) {
	current, err := s.Get(ctx, userID, transactionID)
	if err != nil {
		return Transaction{}, err
	}
	next, changed, err := fn(current, s.now().UTC())
	if err != nil {
		return Transaction{}, err
	}
	if !changed {
		return current, nil
	}
	commit := func(ctx context.Context) error {
		if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
			return mapStoreErr(err)
		}
		if emit == "" {
			return nil
		}
		switch emit {
		case "completed":
			return s.enqueueCompleted(ctx, next, userID)
		case "cancelled":
			return s.enqueueLifecycle(ctx, txncontracts.EventTypeCancelled, "cancelled", next, userID)
		default:
			return nil
		}
	}
	if err := s.runMaybeTx(ctx, commit); err != nil {
		if !errors.Is(err, errConflict) {
			return Transaction{}, err
		}
		latest, getErr := s.Get(ctx, userID, transactionID)
		if getErr != nil {
			return Transaction{}, getErr
		}
		_, againChanged, againErr := fn(latest, s.now().UTC())
		if againErr != nil {
			return Transaction{}, againErr
		}
		if !againChanged {
			return latest, nil
		}
		return Transaction{}, errConflict
	}
	return next, nil
}

func (s *Service) enqueueCompleted(ctx context.Context, txn Transaction, actorUserID ID) error {
	if s == nil || s.outbox == nil {
		return nil
	}
	ev, err := encodeCompletedEvent(txn, actorUserID)
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

func (s *Service) enqueueLifecycle(ctx context.Context, eventType, status string, txn Transaction, actorUserID ID) error {
	if s == nil || s.outbox == nil {
		return nil
	}
	ev, err := encodeLifecycleEvent(eventType, status, txn, actorUserID)
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

func (s *Service) get(ctx context.Context, id ID) (Transaction, error) {
	if s == nil || s.store == nil {
		return Transaction{}, errStoreRequired
	}
	if id.IsZero() {
		return Transaction{}, errZeroID
	}
	txn, err := s.store.Get(ctx, id)
	if err != nil {
		return Transaction{}, mapStoreErr(err)
	}
	return txn, nil
}

func (s *Service) getOffer(ctx context.Context, offerID ID) (offercontracts.OfferRef, error) {
	if s == nil || s.offers == nil {
		return offercontracts.OfferRef{}, errUnavailable
	}
	ref, err := s.offers.GetOffer(ctx, offercontracts.ID(offerID))
	if err != nil {
		return offercontracts.OfferRef{}, mapOfferErr(err)
	}
	return ref, nil
}

func (s *Service) requireNeedOpenForTxn(ctx context.Context, txn Transaction) error {
	need, err := s.sourceNeed(ctx, txn)
	if err != nil {
		return err
	}
	return requireNeedOpen(need.Status)
}

func (s *Service) requireNeedCompletableForTxn(ctx context.Context, txn Transaction) error {
	need, err := s.sourceNeed(ctx, txn)
	if err != nil {
		return err
	}
	return requireNeedCompletable(need.Status)
}

func (s *Service) sourceNeed(ctx context.Context, txn Transaction) (needcontracts.NeedRef, error) {
	need, err := s.getNeed(ctx, txn.NeedID)
	if err != nil {
		return needcontracts.NeedRef{}, err
	}
	if ID(need.ID) != txn.NeedID || ID(need.RequesterUserID) != txn.RequesterUserID {
		return needcontracts.NeedRef{}, errNotFound
	}
	return need, nil
}

func requireNeedOpen(status string) error {
	switch status {
	case "open":
		return nil
	case "draft":
		return errNeedDraft
	case "cancelled":
		return errNeedCancelled
	case "expired":
		return errNeedExpired
	case "fulfilled":
		return errNeedFulfilled
	default:
		return errInvalidTransition
	}
}

func requireNeedCompletable(status string) error {
	switch status {
	case "open", "fulfilled":
		return nil
	case "draft":
		return errNeedDraft
	case "cancelled":
		return errNeedCancelled
	case "expired":
		return errNeedExpired
	default:
		return errInvalidTransition
	}
}

func (s *Service) getNeed(ctx context.Context, needID ID) (needcontracts.NeedRef, error) {
	if s == nil || s.needs == nil {
		return needcontracts.NeedRef{}, errUnavailable
	}
	ref, err := s.needs.GetNeed(ctx, needcontracts.ID(needID))
	if err != nil {
		return needcontracts.NeedRef{}, mapNeedErr(err)
	}
	return ref, nil
}

func mapOfferErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, offercontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, offercontracts.ErrNotFound) {
		return errNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}

func mapNeedErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, needcontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, needcontracts.ErrNotFound) || errors.Is(err, needcontracts.ErrForbidden) {
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
		errors.Is(err, errInvalidTxn) || errors.Is(err, errInvalidStatus) ||
		errors.Is(err, errInvalidTransition) || errors.Is(err, errInvalidPrice) ||
		errors.Is(err, errForbidden) || errors.Is(err, errNotAccepted) ||
		errors.Is(err, errNeedDraft) || errors.Is(err, errNeedCancelled) ||
		errors.Is(err, errNeedExpired) || errors.Is(err, errNeedFulfilled) {
		return err
	}
	if errors.Is(err, outbox.ErrUnavailable) || errors.Is(err, outbox.ErrInvalidEvent) ||
		errors.Is(err, outbox.ErrSensitivePayload) {
		return errUnavailable
	}
	return errUnavailable
}
