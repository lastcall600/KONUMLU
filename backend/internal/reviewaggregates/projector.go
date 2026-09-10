package reviewaggregates

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/outbox"
	reviewscontracts "backend/internal/reviews/contracts"
)

// Projector applies verified-review events onto derived aggregates.
type Projector struct {
	store projectionStore
	now   func() time.Time
}

func NewProjector(store projectionStore) (*Projector, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	return &Projector{store: store, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (p *Projector) SetNow(now func() time.Time) {
	if p == nil || now == nil {
		return
	}
	p.now = now
}

func (p *Projector) Apply(ctx context.Context, event VerifiedReview) error {
	if p == nil || p.store == nil {
		return errStoreRequired
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if err := p.store.ApplyVerified(ctx, event, p.now()); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func (p *Projector) ListingAccuracy(ctx context.Context, listingID ID) (RatingSummary, error) {
	if p == nil || p.store == nil {
		return RatingSummary{}, errStoreRequired
	}
	if listingID.IsZero() {
		return RatingSummary{}, errZeroID
	}
	got, err := p.store.GetListingAccuracy(ctx, listingID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return ZeroSummary(), nil
		}
		return RatingSummary{}, mapStoreErr(err)
	}
	return got, nil
}

func (p *Projector) ProviderService(ctx context.Context, providerUserID ID) (RatingSummary, error) {
	if p == nil || p.store == nil {
		return RatingSummary{}, errStoreRequired
	}
	if providerUserID.IsZero() {
		return RatingSummary{}, errZeroID
	}
	got, err := p.store.GetProviderService(ctx, providerUserID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return ZeroSummary(), nil
		}
		return RatingSummary{}, mapStoreErr(err)
	}
	return got, nil
}

func verifiedFromOutbox(event outbox.Event) (VerifiedReview, error) {
	if event.EventType != reviewscontracts.EventTypeVerifiedCreated || event.EventVersion != reviewscontracts.EventVersion {
		return VerifiedReview{}, errInvalidEvent
	}
	if event.ID.IsZero() {
		return VerifiedReview{}, errInvalidEvent
	}
	payload, err := reviewscontracts.DecodeVerifiedCreated(event.Payload)
	if err != nil {
		return VerifiedReview{}, errInvalidEvent
	}
	reviewID, err := ParseID(payload.ReviewID)
	if err != nil {
		return VerifiedReview{}, errInvalidEvent
	}
	interactionID, err := ParseID(payload.VerifiedInteractionID)
	if err != nil {
		return VerifiedReview{}, errInvalidEvent
	}
	listingID, err := ParseID(payload.ListingID)
	if err != nil {
		return VerifiedReview{}, errInvalidEvent
	}
	reviewer, err := ParseID(payload.ReviewerUserID)
	if err != nil {
		return VerifiedReview{}, errInvalidEvent
	}
	provider, err := ParseID(payload.ProviderUserID)
	if err != nil {
		return VerifiedReview{}, errInvalidEvent
	}
	createdAt, err := parseCreatedAt(payload.CreatedAt, event.CreatedAt)
	if err != nil {
		return VerifiedReview{}, err
	}
	var eventID ID
	copy(eventID[:], event.ID[:])
	return VerifiedReview{
		EventID:               eventID,
		ReviewID:              reviewID,
		VerifiedInteractionID: interactionID,
		ListingID:             listingID,
		ReviewerUserID:        reviewer,
		ProviderUserID:        provider,
		ListingAccuracy:       payload.ListingAccuracy,
		ProviderService:       payload.ProviderService,
		CreatedAt:             createdAt,
	}, nil
}

func parseCreatedAt(raw string, fallback time.Time) (time.Time, error) {
	if raw == "" {
		if fallback.IsZero() {
			return time.Time{}, errInvalidEvent
		}
		return fallback.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return time.Time{}, errInvalidEvent
		}
	}
	return t.UTC(), nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidEvent) || errors.Is(err, errInvalidRating) ||
		errors.Is(err, errInvalidSummary) || errors.Is(err, errStoreRequired) {
		return err
	}
	return errUnavailable
}
