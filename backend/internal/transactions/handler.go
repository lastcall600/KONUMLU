package transactions

import (
	"context"
	"errors"

	needcontracts "backend/internal/needs/contracts"
	"backend/internal/platform/outbox"
	txncontracts "backend/internal/transactions/contracts"
)

// CompletionHandler coordinates Need fulfillment after a completed Transaction
// outbox event. It never writes Needs tables and never rolls back the Transaction.
type CompletionHandler struct {
	store   store
	fulfill needcontracts.TransactionFulfillment
}

func NewCompletionHandler(store store, fulfill needcontracts.TransactionFulfillment) (*CompletionHandler, error) {
	if store == nil || fulfill == nil {
		return nil, errStoreRequired
	}
	return &CompletionHandler{store: store, fulfill: fulfill}, nil
}

func (h *CompletionHandler) Handle(ctx context.Context, event outbox.Event) error {
	if h == nil || h.store == nil || h.fulfill == nil {
		return errStoreRequired
	}
	if event.EventType != txncontracts.EventTypeCompleted || event.EventVersion != txncontracts.EventVersion {
		return errInvalidTxn
	}
	payload, err := txncontracts.DecodeCompleted(event.Payload)
	if err != nil {
		return err
	}
	txnID, err := ParseID(payload.TransactionID)
	if err != nil {
		return errInvalidTxn
	}
	txn, err := h.store.Get(ctx, txnID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil
		}
		return mapStoreErr(err)
	}
	if txn.Status != StatusCompleted {
		return nil
	}
	_, err = h.fulfill.FulfillFromCompletedTransaction(ctx, needcontracts.TransactionFulfillmentCommand{
		NeedID:          needcontracts.ID(txn.NeedID),
		RequesterUserID: needcontracts.ID(txn.RequesterUserID),
		TransactionID:   needcontracts.ID(txn.ID),
	})
	return mapNeedFulfillErr(err)
}

func mapNeedFulfillErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, needcontracts.ErrUnavailable) || errors.Is(err, needcontracts.ErrConflict) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, needcontracts.ErrZeroID) {
		return errInvalidTxn
	}
	return errUnavailable
}

var _ outbox.Handler = (*CompletionHandler)(nil)
