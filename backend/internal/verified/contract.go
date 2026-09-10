package verified

import (
	"context"
	"errors"

	verifiedcontracts "backend/internal/verified/contracts"
)

var _ verifiedcontracts.Interactions = (*interactionsAPI)(nil)

type interactionsAPI struct {
	store appointmentStore
}

// NewInteractions exposes completed verified interactions through the contract.
func NewInteractions(store appointmentStore) verifiedcontracts.Interactions {
	return interactionsAPI{store: store}
}

func (a interactionsAPI) GetInteraction(ctx context.Context, id verifiedcontracts.ID) (verifiedcontracts.InteractionRef, error) {
	if a.store == nil {
		return verifiedcontracts.InteractionRef{}, verifiedcontracts.ErrUnavailable
	}
	if id.IsZero() {
		return verifiedcontracts.InteractionRef{}, verifiedcontracts.ErrZeroID
	}
	row, err := a.store.GetInteraction(ctx, ID(id))
	if err != nil {
		return verifiedcontracts.InteractionRef{}, mapInteractionContractErr(err)
	}
	return verifiedcontracts.InteractionRef{
		ID:              verifiedcontracts.ID(row.ID),
		ListingID:       verifiedcontracts.ID(row.ListingID),
		RequesterUserID: verifiedcontracts.ID(row.RequesterUserID),
		ProviderUserID:  verifiedcontracts.ID(row.ProviderUserID),
		InteractionType: row.InteractionType,
		VerifiedAt:      row.VerifiedAt,
	}, nil
}

func mapInteractionContractErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) {
		return verifiedcontracts.ErrZeroID
	}
	if errors.Is(err, errNotFound) {
		return verifiedcontracts.ErrNotFound
	}
	if errors.Is(err, errUnavailable) || errors.Is(err, errStoreRequired) {
		return verifiedcontracts.ErrUnavailable
	}
	return err
}
