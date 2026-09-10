package eids

import (
	"context"
	"errors"

	eidscontracts "backend/internal/eids/contracts"
)

var _ eidscontracts.OwnerVerification = (*ownerAPI)(nil)
var _ eidscontracts.ListingPublishGate = (*publishGate)(nil)

type ownerAPI struct{ svc *Service }

func NewOwnerAPI(svc *Service) eidscontracts.OwnerVerification {
	return ownerAPI{svc: svc}
}

func (a ownerAPI) StartForOwner(ctx context.Context, ownerUserID, listingID eidscontracts.ID, clientType string) (eidscontracts.VerificationView, error) {
	if a.svc == nil {
		return eidscontracts.VerificationView{}, eidscontracts.ErrUnavailable
	}
	v, err := a.svc.StartForOwner(ctx, ID(ownerUserID), ID(listingID), clientType)
	if err != nil {
		return eidscontracts.VerificationView{}, mapContractErr(err)
	}
	return toView(v), nil
}

func (a ownerAPI) CurrentForOwner(ctx context.Context, ownerUserID, listingID eidscontracts.ID) (eidscontracts.VerificationView, error) {
	if a.svc == nil {
		return eidscontracts.VerificationView{}, eidscontracts.ErrUnavailable
	}
	v, err := a.svc.CurrentForOwner(ctx, ID(ownerUserID), ID(listingID))
	if err != nil {
		return eidscontracts.VerificationView{}, mapContractErr(err)
	}
	return toView(v), nil
}

type publishGate struct{ svc *Service }

func NewPublishGate(svc *Service) eidscontracts.ListingPublishGate {
	return publishGate{svc: svc}
}

func (g publishGate) IsVerified(ctx context.Context, listingID eidscontracts.ID, kind eidscontracts.VerificationType) (bool, error) {
	if g.svc == nil {
		return false, eidscontracts.ErrUnavailable
	}
	ok, err := g.svc.IsVerified(ctx, ID(listingID), VerificationType(kind))
	if err != nil {
		return false, mapContractErr(err)
	}
	return ok, nil
}

func toView(v Verification) eidscontracts.VerificationView {
	return eidscontracts.VerificationView{
		VerificationID:   eidscontracts.ID(v.ID),
		ListingID:        eidscontracts.ID(v.ListingID),
		VerificationType: eidscontracts.VerificationType(v.VerificationType),
		Status:           eidscontracts.Status(v.Status),
		FailureCode:      string(v.FailureCode),
		CreatedAt:        v.CreatedAt,
		UpdatedAt:        v.UpdatedAt,
		RequestedAt:      cloneTimePtr(v.RequestedAt),
		VerifiedAt:       cloneTimePtr(v.VerifiedAt),
		FailedAt:         cloneTimePtr(v.FailedAt),
	}
}

func mapContractErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errZeroID):
		return eidscontracts.ErrZeroID
	case errors.Is(err, errNotFound):
		return eidscontracts.ErrNotFound
	case errors.Is(err, errNotRequired):
		return eidscontracts.ErrNotRequired
	case errors.Is(err, errWrongType), errors.Is(err, errInvalidType):
		return eidscontracts.ErrWrongType
	case errors.Is(err, errInvalidVerification):
		return eidscontracts.ErrInvalidInput
	default:
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return eidscontracts.ErrUnavailable
	}
}
