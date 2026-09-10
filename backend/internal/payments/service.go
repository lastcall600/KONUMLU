package payments

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/db"
	txncontracts "backend/internal/transactions/contracts"
)

type Service struct {
	store store
	txns  txncontracts.Lookup
	now   func() time.Time
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

func (s *Service) CreateForTransaction(ctx context.Context, payerUserID, transactionID ID) (Payment, error) {
	if s == nil || s.store == nil {
		return Payment{}, errStoreRequired
	}
	if payerUserID.IsZero() || transactionID.IsZero() {
		return Payment{}, errZeroID
	}
	ref, err := s.getTransaction(ctx, transactionID)
	if err != nil {
		return Payment{}, err
	}
	if ID(ref.RequesterUserID) != payerUserID {
		return Payment{}, errNotFound
	}
	if err := assertPayable(ref); err != nil {
		return Payment{}, err
	}
	existing, err := s.store.GetByTransactionID(ctx, transactionID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, errNotFound) {
		return Payment{}, mapStoreErr(err)
	}
	created, err := CreatePending(CreateCommand{
		TransactionID: ID(ref.ID),
		PayerUserID:   ID(ref.RequesterUserID),
		PayeeUserID:   ID(ref.ProviderUserID),
		Amount:        Money{Amount: ref.Price.Amount, Currency: ref.Price.Currency},
	}, s.now().UTC())
	if err != nil {
		return Payment{}, err
	}
	if err := s.store.Create(ctx, created); err != nil {
		if errors.Is(mapStoreErr(err), errConflict) {
			existing, findErr := s.store.GetByTransactionID(ctx, transactionID)
			if findErr == nil {
				return existing, nil
			}
			return Payment{}, errConflict
		}
		return Payment{}, mapStoreErr(err)
	}
	return created, nil
}

func (s *Service) Get(ctx context.Context, userID, paymentID ID) (Payment, error) {
	p, err := s.get(ctx, paymentID)
	if err != nil {
		return Payment{}, err
	}
	if !p.Participant(userID) {
		return Payment{}, errNotFound
	}
	return p, nil
}

func (s *Service) ListMine(ctx context.Context, userID ID) ([]Payment, error) {
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
		return []Payment{}, nil
	}
	return list, nil
}

func (s *Service) ApplyAuthorized(ctx context.Context, paymentID ID, signal ProviderSignal) (Payment, error) {
	return s.applyInternal(ctx, paymentID, func(p Payment, now time.Time) (Payment, bool, error) {
		return p.Authorize(now, signal)
	})
}

func (s *Service) ApplyCaptured(ctx context.Context, paymentID ID, signal ProviderSignal) (Payment, error) {
	return s.applyInternal(ctx, paymentID, func(p Payment, now time.Time) (Payment, bool, error) {
		return p.Capture(now, signal)
	})
}

func (s *Service) ApplyCancelled(ctx context.Context, paymentID ID, signal ProviderSignal) (Payment, error) {
	return s.applyInternal(ctx, paymentID, func(p Payment, now time.Time) (Payment, bool, error) {
		return p.Cancel(now, signal)
	})
}

func (s *Service) ApplyFailed(ctx context.Context, paymentID ID, signal ProviderSignal) (Payment, error) {
	return s.applyInternal(ctx, paymentID, func(p Payment, now time.Time) (Payment, bool, error) {
		return p.Fail(now, signal)
	})
}

func (s *Service) applyInternal(ctx context.Context, paymentID ID, fn func(Payment, time.Time) (Payment, bool, error)) (Payment, error) {
	current, err := s.get(ctx, paymentID)
	if err != nil {
		return Payment{}, err
	}
	next, changed, err := fn(current, s.now().UTC())
	if err != nil {
		return Payment{}, err
	}
	if !changed {
		return current, nil
	}
	if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
		if !errors.Is(mapStoreErr(err), errConflict) {
			return Payment{}, mapStoreErr(err)
		}
		latest, getErr := s.get(ctx, paymentID)
		if getErr != nil {
			return Payment{}, getErr
		}
		_, againChanged, againErr := fn(latest, s.now().UTC())
		if againErr != nil {
			return Payment{}, againErr
		}
		if !againChanged {
			return latest, nil
		}
		return Payment{}, errConflict
	}
	return next, nil
}

func (s *Service) get(ctx context.Context, id ID) (Payment, error) {
	if s == nil || s.store == nil {
		return Payment{}, errStoreRequired
	}
	if id.IsZero() {
		return Payment{}, errZeroID
	}
	p, err := s.store.Get(ctx, id)
	if err != nil {
		return Payment{}, mapStoreErr(err)
	}
	return p, nil
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

func assertPayable(ref txncontracts.TransactionRef) error {
	if ref.Price == nil || ref.Price.Amount == "" || ref.Price.Currency == "" {
		return errNotPriced
	}
	switch ref.Status {
	case "pending", "active":
		return nil
	default:
		return errNotPayable
	}
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
		errors.Is(err, errInvalidPayment) || errors.Is(err, errInvalidStatus) ||
		errors.Is(err, errInvalidTransition) || errors.Is(err, errInvalidAmount) ||
		errors.Is(err, errForbidden) || errors.Is(err, errNotPriced) ||
		errors.Is(err, errNotPayable) {
		return err
	}
	return errUnavailable
}
