package transactions

import (
	"context"
	"errors"

	"backend/internal/transactions/contracts"
)

var _ contracts.Lookup = (*Service)(nil)

func (s *Service) GetTransaction(ctx context.Context, transactionID contracts.ID) (contracts.TransactionRef, error) {
	txn, err := s.get(ctx, ID(transactionID))
	if err != nil {
		return contracts.TransactionRef{}, mapTxnLookupErr(err)
	}
	return toTransactionRef(txn), nil
}

func toTransactionRef(t Transaction) contracts.TransactionRef {
	ref := contracts.TransactionRef{
		ID:              contracts.ID(t.ID),
		RequesterUserID: contracts.ID(t.RequesterUserID),
		ProviderUserID:  contracts.ID(t.ProviderUserID),
		Status:          string(t.Status),
		CreatedAt:       t.CreatedAt.UTC(),
	}
	if t.CompletedAt != nil {
		stamp := t.CompletedAt.UTC()
		ref.CompletedAt = &stamp
	}
	if t.Price != nil {
		ref.Price = &contracts.Price{Amount: t.Price.Amount, Currency: t.Price.Currency}
	}
	return ref
}

func mapTxnLookupErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) {
		return contracts.ErrZeroID
	}
	if errors.Is(err, errNotFound) {
		return contracts.ErrNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return contracts.ErrUnavailable
}
