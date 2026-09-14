package offers

import (
	"context"
	"errors"
	"time"

	bizcontracts "backend/internal/businesses/contracts"
	needcontracts "backend/internal/needs/contracts"
	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
	offercontracts "backend/internal/offers/contracts"
)

type notifyEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

// V1 need lifecycle: accepting an offer does not mark the Need fulfilled.
// The Need stays open until a later transaction/fulfillment step.
const NeedRemainsOpenOnAccept = true

type Service struct {
	store       store
	needs       needcontracts.Lookup
	eligibility bizcontracts.OfferEligibility
	businesses  bizcontracts.Lookup
	catalog     bizcontracts.Catalog
	now         func() time.Time
	tx          Transactor
	outbox      notifyEnqueuer
}

func NewService(
	store store,
	needs needcontracts.Lookup,
	eligibility bizcontracts.OfferEligibility,
	businesses bizcontracts.Lookup,
	catalog bizcontracts.Catalog,
	now func() time.Time,
) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store:       store,
		needs:       needs,
		eligibility: eligibility,
		businesses:  businesses,
		catalog:     catalog,
		now:         now,
	}, nil
}

func (s *Service) SetOutbox(tx Transactor, enqueuer notifyEnqueuer) {
	if s == nil {
		return
	}
	s.tx = tx
	s.outbox = enqueuer
}

func (s *Service) Create(ctx context.Context, providerUserID ID, content Content) (Offer, error) {
	if s == nil || s.store == nil {
		return Offer{}, errStoreRequired
	}
	if providerUserID.IsZero() {
		return Offer{}, errZeroID
	}
	content, err := content.normalized()
	if err != nil {
		return Offer{}, err
	}
	need, err := s.requireOpenNeed(ctx, content.NeedID)
	if err != nil {
		return Offer{}, err
	}
	if err := s.assertBusinessOwner(ctx, content.ProviderBusinessID, providerUserID); err != nil {
		return Offer{}, err
	}
	if err := s.assertEligible(ctx, need, content.ProviderBusinessID, content.ServiceID); err != nil {
		return Offer{}, err
	}
	existing, err := s.store.FindSubmitted(ctx, content.NeedID, content.ServiceID)
	if err == nil {
		if existing.ProviderUserID != providerUserID || existing.ProviderBusinessID != content.ProviderBusinessID {
			return Offer{}, errConflict
		}
		return existing, nil
	}
	if !errors.Is(err, errNotFound) {
		return Offer{}, mapStoreErr(err)
	}
	offer, err := Submit(providerUserID, content, s.now().UTC())
	if err != nil {
		return Offer{}, err
	}
	requester := ID(need.RequesterUserID)
	commit := func(ctx context.Context) error {
		if err := s.store.Create(ctx, offer); err != nil {
			return mapStoreErr(err)
		}
		return s.enqueueTransition(ctx, offercontracts.EventTypeSubmitted, "submitted", offer, requester, providerUserID)
	}
	if err := s.runMaybeTx(ctx, commit); err != nil {
		if errors.Is(err, errConflict) {
			existing, findErr := s.store.FindSubmitted(ctx, content.NeedID, content.ServiceID)
			if findErr == nil && existing.ProviderUserID == providerUserID {
				return existing, nil
			}
			return Offer{}, errConflict
		}
		return Offer{}, err
	}
	return offer, nil
}

func (s *Service) ListForRequester(ctx context.Context, requesterUserID, needID ID) ([]Offer, error) {
	if err := s.assertNeedOwner(ctx, needID, requesterUserID); err != nil {
		return nil, err
	}
	return s.listByNeed(ctx, needID)
}

func (s *Service) ListMine(ctx context.Context, providerUserID ID) ([]Offer, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if providerUserID.IsZero() {
		return nil, errZeroID
	}
	list, err := s.store.ListByProvider(ctx, providerUserID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	if list == nil {
		return []Offer{}, nil
	}
	return list, nil
}

func (s *Service) Withdraw(ctx context.Context, providerUserID, offerID ID) (Offer, error) {
	current, err := s.get(ctx, offerID)
	if err != nil {
		return Offer{}, err
	}
	if current.ProviderUserID != providerUserID {
		return Offer{}, errNotFound
	}
	if current.Status == StatusWithdrawn {
		return current, nil
	}
	next, err := current.Withdraw(s.now().UTC())
	if err != nil {
		return Offer{}, err
	}
	if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
		return Offer{}, mapStoreErr(err)
	}
	return next, nil
}

func (s *Service) Accept(ctx context.Context, requesterUserID, needID, offerID ID) (Offer, error) {
	if err := s.assertNeedOwner(ctx, needID, requesterUserID); err != nil {
		return Offer{}, err
	}
	if _, err := s.requireOpenNeed(ctx, needID); err != nil {
		return Offer{}, err
	}
	current, err := s.get(ctx, offerID)
	if err != nil {
		return Offer{}, err
	}
	if current.NeedID != needID {
		return Offer{}, errNotFound
	}
	if current.Status == StatusAccepted {
		return current, nil
	}
	submittedSiblings := []Offer{}
	all, err := s.listByNeed(ctx, needID)
	if err != nil {
		return Offer{}, err
	}
	for _, o := range all {
		if o.ID != offerID && o.Status == StatusSubmitted {
			submittedSiblings = append(submittedSiblings, o)
		}
	}
	var got Offer
	commit := func(ctx context.Context) error {
		accepted, err := s.store.AcceptExclusive(ctx, offerID, needID, s.now().UTC())
		if err != nil {
			return mapStoreErr(err)
		}
		got = accepted
		if accepted.Status != StatusAccepted {
			return nil
		}
		if err := s.enqueueTransition(ctx, offercontracts.EventTypeAccepted, "accepted", accepted, requesterUserID, requesterUserID); err != nil {
			return err
		}
		for _, sibling := range submittedSiblings {
			if err := s.enqueueTransition(ctx, offercontracts.EventTypeRejected, "rejected", sibling, requesterUserID, requesterUserID); err != nil {
				return err
			}
		}
		return nil
	}
	if err := s.runMaybeTx(ctx, commit); err != nil {
		return Offer{}, err
	}
	return got, nil
}

func (s *Service) Reject(ctx context.Context, requesterUserID, needID, offerID ID) (Offer, error) {
	if err := s.assertNeedOwner(ctx, needID, requesterUserID); err != nil {
		return Offer{}, err
	}
	current, err := s.get(ctx, offerID)
	if err != nil {
		return Offer{}, err
	}
	if current.NeedID != needID {
		return Offer{}, errNotFound
	}
	if current.Status == StatusRejected {
		return current, nil
	}
	next, err := current.Reject(s.now().UTC())
	if err != nil {
		return Offer{}, err
	}
	commit := func(ctx context.Context) error {
		if err := s.store.Update(ctx, next, current.UpdatedAt); err != nil {
			return mapStoreErr(err)
		}
		return s.enqueueTransition(ctx, offercontracts.EventTypeRejected, "rejected", next, requesterUserID, requesterUserID)
	}
	if err := s.runMaybeTx(ctx, commit); err != nil {
		return Offer{}, err
	}
	return next, nil
}

func (s *Service) enqueueTransition(ctx context.Context, eventType, transition string, offer Offer, requesterUserID, actorUserID ID) error {
	if s == nil || s.outbox == nil {
		return nil
	}
	ev, err := encodeOfferTransition(eventType, transition, offer, requesterUserID, actorUserID)
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

func (s *Service) ProviderNeedView(ctx context.Context, providerUserID, needID, businessID, serviceID ID) (needcontracts.NeedRef, error) {
	if providerUserID.IsZero() || needID.IsZero() || businessID.IsZero() || serviceID.IsZero() {
		return needcontracts.NeedRef{}, errZeroID
	}
	need, err := s.requireOpenNeed(ctx, needID)
	if err != nil {
		return needcontracts.NeedRef{}, err
	}
	if err := s.assertBusinessOwner(ctx, businessID, providerUserID); err != nil {
		return needcontracts.NeedRef{}, err
	}
	if err := s.assertEligible(ctx, need, businessID, serviceID); err != nil {
		return needcontracts.NeedRef{}, err
	}
	return need, nil
}

func (s *Service) PublicBusiness(ctx context.Context, businessID ID) (bizcontracts.ProfileRef, error) {
	if s == nil || s.businesses == nil {
		return bizcontracts.ProfileRef{}, errUnavailable
	}
	ref, err := s.businesses.GetBusiness(ctx, bizcontracts.ID(businessID))
	if err != nil {
		return bizcontracts.ProfileRef{}, mapBizErr(err)
	}
	return ref, nil
}

func (s *Service) PublicService(ctx context.Context, serviceID ID) (bizcontracts.OfferedServiceRef, error) {
	if s == nil || s.catalog == nil {
		return bizcontracts.OfferedServiceRef{}, errUnavailable
	}
	ref, err := s.catalog.GetService(ctx, bizcontracts.ID(serviceID))
	if err != nil {
		return bizcontracts.OfferedServiceRef{}, mapBizErr(err)
	}
	return ref, nil
}

func (s *Service) GetNeed(ctx context.Context, needID ID) (needcontracts.NeedRef, error) {
	return s.getNeed(ctx, needID)
}

func (s *Service) listByNeed(ctx context.Context, needID ID) ([]Offer, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	list, err := s.store.ListByNeed(ctx, needID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	if list == nil {
		return []Offer{}, nil
	}
	return list, nil
}

func (s *Service) get(ctx context.Context, id ID) (Offer, error) {
	if s == nil || s.store == nil {
		return Offer{}, errStoreRequired
	}
	if id.IsZero() {
		return Offer{}, errZeroID
	}
	offer, err := s.store.Get(ctx, id)
	if err != nil {
		return Offer{}, mapStoreErr(err)
	}
	return offer, nil
}

func (s *Service) getNeed(ctx context.Context, needID ID) (needcontracts.NeedRef, error) {
	if s == nil || s.needs == nil {
		return needcontracts.NeedRef{}, errUnavailable
	}
	if needID.IsZero() {
		return needcontracts.NeedRef{}, errZeroID
	}
	need, err := s.needs.GetNeed(ctx, needcontracts.ID(needID))
	if err != nil {
		return needcontracts.NeedRef{}, mapNeedErr(err)
	}
	return need, nil
}

func (s *Service) requireOpenNeed(ctx context.Context, needID ID) (needcontracts.NeedRef, error) {
	need, err := s.getNeed(ctx, needID)
	if err != nil {
		return needcontracts.NeedRef{}, err
	}
	if need.Status != "open" {
		return needcontracts.NeedRef{}, errNotFound
	}
	return need, nil
}

func (s *Service) assertNeedOwner(ctx context.Context, needID, userID ID) error {
	if s == nil || s.needs == nil {
		return errUnavailable
	}
	if needID.IsZero() || userID.IsZero() {
		return errZeroID
	}
	if err := s.needs.AssertOwnedBy(ctx, needcontracts.ID(needID), needcontracts.ID(userID)); err != nil {
		return mapNeedErr(err)
	}
	return nil
}

func (s *Service) assertBusinessOwner(ctx context.Context, businessID, userID ID) error {
	if s == nil || s.businesses == nil {
		return errUnavailable
	}
	if err := s.businesses.AssertOwnedBy(ctx, bizcontracts.ID(businessID), bizcontracts.ID(userID)); err != nil {
		if errors.Is(err, bizcontracts.ErrForbidden) {
			return errNotFound
		}
		return mapBizErr(err)
	}
	return nil
}

func (s *Service) assertEligible(ctx context.Context, need needcontracts.NeedRef, businessID, serviceID ID) error {
	if s == nil || s.eligibility == nil {
		return errUnavailable
	}
	radius := needcontracts.DefaultMatchRadiusKm
	if need.RadiusKm != nil {
		radius = *need.RadiusKm
	}
	check := bizcontracts.EligibilityCheck{
		BusinessID: bizcontracts.ID(businessID),
		ServiceID:  bizcontracts.ID(serviceID),
		Latitude:   need.Latitude,
		Longitude:  need.Longitude,
		RadiusKm:   radius,
	}
	if need.CategoryID != nil {
		id := bizcontracts.ID(*need.CategoryID)
		check.CategoryID = &id
	}
	if _, err := s.eligibility.CheckServiceCandidate(ctx, check); err != nil {
		if errors.Is(err, bizcontracts.ErrNotFound) {
			return errNotEligible
		}
		return mapBizErr(err)
	}
	return nil
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

func mapBizErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, bizcontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, bizcontracts.ErrNotFound) || errors.Is(err, bizcontracts.ErrForbidden) {
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
		errors.Is(err, errInvalidOffer) || errors.Is(err, errInvalidStatus) ||
		errors.Is(err, errInvalidTransition) || errors.Is(err, errInvalidMessage) ||
		errors.Is(err, errInvalidPrice) || errors.Is(err, errForbidden) ||
		errors.Is(err, errNotEligible) {
		return err
	}
	if errors.Is(err, outbox.ErrUnavailable) || errors.Is(err, outbox.ErrInvalidEvent) ||
		errors.Is(err, outbox.ErrSensitivePayload) {
		return errUnavailable
	}
	return errUnavailable
}
