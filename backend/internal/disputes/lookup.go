package disputes

import (
	"context"
	"errors"

	"backend/internal/disputes/contracts"
)

var _ contracts.Lookup = (*Service)(nil)

func (s *Service) GetDispute(ctx context.Context, disputeID contracts.ID) (contracts.DisputeRef, error) {
	d, err := s.get(ctx, ID(disputeID))
	if err != nil {
		return contracts.DisputeRef{}, mapDisputeLookupErr(err)
	}
	return toDisputeRef(d), nil
}

func toDisputeRef(d Dispute) contracts.DisputeRef {
	ref := contracts.DisputeRef{
		ID:            contracts.ID(d.ID),
		TransactionID: contracts.ID(d.TransactionID),
		ReasonCode:    string(d.ReasonCode),
		Status:        string(d.Status),
	}
	if d.ResolutionCode != nil {
		code := string(*d.ResolutionCode)
		ref.ResolutionCode = &code
	}
	return ref
}

func mapDisputeLookupErr(err error) error {
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
