package deliveries

import (
	"context"
	"errors"

	"backend/internal/deliveries/contracts"
)

var _ contracts.Lookup = (*Service)(nil)

func (s *Service) GetDelivery(ctx context.Context, deliveryID contracts.ID) (contracts.DeliveryRef, error) {
	d, err := s.get(ctx, ID(deliveryID))
	if err != nil {
		return contracts.DeliveryRef{}, mapDeliveryLookupErr(err)
	}
	return toDeliveryRef(d), nil
}

func (s *Service) GetDeliveryByTransaction(ctx context.Context, transactionID contracts.ID) (contracts.DeliveryRef, error) {
	if s == nil || s.store == nil {
		return contracts.DeliveryRef{}, contracts.ErrUnavailable
	}
	if transactionID.IsZero() {
		return contracts.DeliveryRef{}, contracts.ErrZeroID
	}
	d, err := s.store.GetByTransactionID(ctx, ID(transactionID))
	if err != nil {
		return contracts.DeliveryRef{}, mapDeliveryLookupErr(mapStoreErr(err))
	}
	return toDeliveryRef(d), nil
}

func toDeliveryRef(d Delivery) contracts.DeliveryRef {
	ref := contracts.DeliveryRef{
		ID:            contracts.ID(d.ID),
		TransactionID: contracts.ID(d.TransactionID),
		Eligibility:   d.Eligibility,
		Status:        string(d.Status),
	}
	if d.Method != nil {
		m := string(*d.Method)
		ref.Method = &m
	}
	return ref
}

func mapDeliveryLookupErr(err error) error {
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
