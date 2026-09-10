package offers

import (
	"context"
	"errors"

	"backend/internal/offers/contracts"
)

var _ contracts.Lookup = (*Service)(nil)

func (s *Service) GetOffer(ctx context.Context, offerID contracts.ID) (contracts.OfferRef, error) {
	offer, err := s.get(ctx, ID(offerID))
	if err != nil {
		return contracts.OfferRef{}, mapOfferLookupErr(err)
	}
	return toOfferRef(offer), nil
}

func toOfferRef(o Offer) contracts.OfferRef {
	ref := contracts.OfferRef{
		ID:                 contracts.ID(o.ID),
		NeedID:             contracts.ID(o.NeedID),
		ProviderBusinessID: contracts.ID(o.ProviderBusinessID),
		ServiceID:          contracts.ID(o.ServiceID),
		ProviderUserID:     contracts.ID(o.ProviderUserID),
		Status:             string(o.Status),
	}
	if o.Price != nil {
		ref.Price = &contracts.Price{Amount: o.Price.Amount, Currency: o.Price.Currency}
	}
	return ref
}

func mapOfferLookupErr(err error) error {
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
