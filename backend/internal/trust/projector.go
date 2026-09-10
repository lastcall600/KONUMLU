package trust

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/outbox"
	reviewscontracts "backend/internal/reviews/contracts"
	verifiedcontracts "backend/internal/verified/contracts"
)

// Projector applies verified-platform events onto derived trust projections.
type Projector struct {
	store  projectionStore
	policy LevelPolicy
	now    func() time.Time
}

func NewProjector(store projectionStore, policy LevelPolicy) (*Projector, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Projector{store: store, policy: policy, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (p *Projector) SetNow(now func() time.Time) {
	if p == nil || now == nil {
		return
	}
	p.now = now
}

func (p *Projector) Apply(ctx context.Context, event CompletedInteraction) error {
	if p == nil || p.store == nil {
		return errStoreRequired
	}
	if err := event.Validate(); err != nil {
		return err
	}
	switch event.InteractionType {
	case verifiedcontracts.InteractionTypeListingInspection,
		verifiedcontracts.InteractionTypeTransaction,
		verifiedcontracts.InteractionTypeDelivery:
	default:
		return errInvalidEvent
	}
	now := p.now()
	if err := p.store.ApplyCompleted(ctx, event, p.policy, now); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func (p *Projector) ApplyVerifiedReview(ctx context.Context, event VerifiedReview) error {
	if p == nil || p.store == nil {
		return errStoreRequired
	}
	if err := event.Validate(); err != nil {
		return err
	}
	now := p.now()
	if err := p.store.ApplyVerifiedReview(ctx, event, now); err != nil {
		return mapStoreErr(err)
	}
	return nil
}

func (p *Projector) Profile(ctx context.Context, userID ID) (UserProfile, error) {
	if p == nil || p.store == nil {
		return UserProfile{}, errStoreRequired
	}
	if userID.IsZero() {
		return UserProfile{}, errZeroID
	}
	got, err := p.store.GetProfile(ctx, userID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return ZeroProfile(userID, p.now()), nil
		}
		return UserProfile{}, mapStoreErr(err)
	}
	return got, nil
}

func completedFromOutbox(event outbox.Event) (CompletedInteraction, error) {
	if event.EventType != verifiedcontracts.EventTypeInteractionCompleted || event.EventVersion != verifiedcontracts.EventVersion {
		return CompletedInteraction{}, errInvalidEvent
	}
	if event.ID.IsZero() {
		return CompletedInteraction{}, errInvalidEvent
	}
	payload, err := verifiedcontracts.DecodeInteractionCompleted(event.Payload)
	if err != nil {
		return CompletedInteraction{}, errInvalidEvent
	}
	interactionID, err := ParseID(payload.InteractionID)
	if err != nil {
		return CompletedInteraction{}, errInvalidEvent
	}
	listingID, err := ParseID(payload.ListingID)
	if err != nil {
		return CompletedInteraction{}, errInvalidEvent
	}
	requester, err := ParseID(payload.RequesterUserID)
	if err != nil {
		return CompletedInteraction{}, errInvalidEvent
	}
	provider, err := ParseID(payload.ProviderUserID)
	if err != nil {
		return CompletedInteraction{}, errInvalidEvent
	}
	verifiedAt, err := parseVerifiedAt(payload.VerifiedAt, event.CreatedAt)
	if err != nil {
		return CompletedInteraction{}, err
	}
	var eventID ID
	copy(eventID[:], event.ID[:])
	return CompletedInteraction{
		EventID:            eventID,
		InteractionID:      interactionID,
		ListingID:          listingID,
		RequesterUserID:    requester,
		ProviderUserID:     provider,
		InteractionType:    payload.InteractionType,
		VerificationMethod: payload.VerificationMethod,
		VerifiedAt:         verifiedAt,
	}, nil
}

func verifiedReviewFromOutbox(event outbox.Event) (VerifiedReview, error) {
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
	createdAt, err := parseVerifiedAt(payload.CreatedAt, event.CreatedAt)
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

func parseVerifiedAt(raw string, fallback time.Time) (time.Time, error) {
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
		errors.Is(err, errInvalidEvent) || errors.Is(err, errInvalidPolicy) ||
		errors.Is(err, errInvalidProfile) || errors.Is(err, errStoreRequired) {
		return err
	}
	return errUnavailable
}
