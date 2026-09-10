package needs

import (
	"context"
	"errors"

	"backend/internal/needs/contracts"
)

var _ contracts.TransactionFulfillment = (*Service)(nil)

func (s *Service) FulfillFromCompletedTransaction(ctx context.Context, cmd contracts.TransactionFulfillmentCommand) (contracts.FulfillmentResult, error) {
	if s == nil || s.store == nil {
		return contracts.FulfillmentResult{}, contracts.ErrUnavailable
	}
	if cmd.NeedID.IsZero() || cmd.RequesterUserID.IsZero() || cmd.TransactionID.IsZero() {
		return contracts.FulfillmentResult{}, contracts.ErrZeroID
	}
	need, err := s.get(ctx, ID(cmd.NeedID))
	if err != nil {
		if errors.Is(err, errNotFound) {
			return contracts.FulfillmentResult{Outcome: contracts.FulfillmentNotFulfillable}, nil
		}
		return contracts.FulfillmentResult{}, mapNeedFulfillmentErr(err)
	}
	if need.RequesterUserID != ID(cmd.RequesterUserID) {
		return contracts.FulfillmentResult{
			Outcome: contracts.FulfillmentNotFulfillable,
			Status:  string(need.Status),
		}, nil
	}
	next, outcome, err := need.ApplyCompletedTransactionFulfillment(s.now().UTC())
	if err != nil {
		return contracts.FulfillmentResult{}, mapNeedFulfillmentErr(err)
	}
	if outcome != contracts.FulfillmentFulfilled {
		return contracts.FulfillmentResult{Outcome: outcome, Status: string(need.Status)}, nil
	}
	if err := s.store.Update(ctx, next, need.UpdatedAt); err != nil {
		return contracts.FulfillmentResult{}, mapNeedFulfillmentErr(err)
	}
	return contracts.FulfillmentResult{Outcome: outcome, Status: string(next.Status)}, nil
}

func mapNeedFulfillmentErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) {
		return contracts.ErrZeroID
	}
	if errors.Is(err, errNotFound) {
		return contracts.ErrNotFound
	}
	if errors.Is(err, errForbidden) {
		return contracts.ErrForbidden
	}
	if errors.Is(err, errConflict) {
		return contracts.ErrConflict
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return contracts.ErrUnavailable
}
