package reviews

import (
	"context"
	"errors"
	"strings"
	"time"

	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/platform/outbox"
	verifiedcontracts "backend/internal/verified/contracts"
)

type eventEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

type Service struct {
	store    reviewStore
	verified verifiedcontracts.Interactions
	listings listingcontracts.Ownership
	outbox   eventEnqueuer
	policy   Policy
	now      func() time.Time
}

func NewService(store reviewStore, verified verifiedcontracts.Interactions, listings listingcontracts.Ownership, enqueuer eventEnqueuer, policy Policy, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if verified == nil {
		return nil, errVerifiedReq
	}
	if listings == nil {
		return nil, errListingsReq
	}
	if enqueuer == nil {
		return nil, errOutboxRequired
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, verified: verified, listings: listings, outbox: enqueuer, policy: policy, now: now}, nil
}

func (s *Service) ListPublicForListing(ctx context.Context, q PublicListQuery) (PublicListingPage, error) {
	if s == nil || s.store == nil {
		return PublicListingPage{}, errStoreRequired
	}
	if q.ListingID.IsZero() {
		return PublicListingPage{}, errZeroID
	}
	limit, err := normalizePublicLimit(q.Limit)
	if err != nil {
		return PublicListingPage{}, err
	}
	var cursor *listingCursor
	if raw := strings.TrimSpace(q.Cursor); raw != "" {
		cur, err := decodePublicCursor(raw, q.ListingID)
		if err != nil {
			return PublicListingPage{}, err
		}
		cursor = &cur
	}
	if err := s.requirePublishedListing(ctx, q.ListingID); err != nil {
		return PublicListingPage{}, err
	}
	rows, err := s.store.ListForListing(ctx, q.ListingID, cursor, limit+1)
	if err != nil {
		return PublicListingPage{}, mapStoreErr(err)
	}
	page := PublicListingPage{Reviews: make([]PublicReview, 0, len(rows))}
	if len(rows) > limit {
		rows = rows[:limit]
		last := toPublicReview(rows[limit-1])
		next, err := encodePublicCursor(q.ListingID, last)
		if err != nil {
			return PublicListingPage{}, err
		}
		page.NextCursor = next
	}
	for _, row := range rows {
		page.Reviews = append(page.Reviews, toPublicReview(row))
	}
	return page, nil
}

func (s *Service) requirePublishedListing(ctx context.Context, listingID ID) error {
	if s.listings == nil {
		return errListingsReq
	}
	ref, err := s.listings.ResolveListingOwner(ctx, listingcontracts.ID(listingID))
	if err != nil {
		if errors.Is(err, listingcontracts.ErrNotFound) || errors.Is(err, listingcontracts.ErrForbidden) {
			return errNotFound
		}
		if errors.Is(err, listingcontracts.ErrZeroID) {
			return errZeroID
		}
		return errUnavailable
	}
	if ref.Status != listingcontracts.StatusPublished {
		return errNotFound
	}
	return nil
}

func (s *Service) Eligibility(ctx context.Context, reviewerUserID, verifiedInteractionID ID) (Eligibility, error) {
	if s == nil || s.store == nil {
		return Eligibility{}, errStoreRequired
	}
	if reviewerUserID.IsZero() || verifiedInteractionID.IsZero() {
		return Eligibility{}, errZeroID
	}
	ref, err := s.requireRequesterInteraction(ctx, reviewerUserID, verifiedInteractionID)
	if err != nil {
		return Eligibility{}, err
	}
	expires := s.policy.ExpiresAt(ref.VerifiedAt)
	already, err := s.alreadyReviewed(ctx, verifiedInteractionID)
	if err != nil {
		return Eligibility{}, err
	}
	eligible := !already && s.reviewable(ref) && s.policy.WithinWindow(ref.VerifiedAt, s.now())
	return Eligibility{
		Eligible:        eligible,
		ExpiresAt:       &expires,
		AlreadyReviewed: already,
	}, nil
}

func (s *Service) Create(ctx context.Context, reviewerUserID ID, in CreateInput) (Review, error) {
	if s == nil || s.store == nil {
		return Review{}, errStoreRequired
	}
	if reviewerUserID.IsZero() || in.VerifiedInteractionID.IsZero() {
		return Review{}, errZeroID
	}
	if err := ValidateRating(in.ListingAccuracy); err != nil {
		return Review{}, err
	}
	if err := ValidateRating(in.ProviderService); err != nil {
		return Review{}, err
	}
	body, err := NormalizeBody(in.Body)
	if err != nil {
		return Review{}, err
	}
	ref, err := s.requireRequesterInteraction(ctx, reviewerUserID, in.VerifiedInteractionID)
	if err != nil {
		return Review{}, err
	}
	if !s.reviewable(ref) || !s.policy.WithinWindow(ref.VerifiedAt, s.now()) {
		return Review{}, errNotEligible
	}
	already, err := s.alreadyReviewed(ctx, in.VerifiedInteractionID)
	if err != nil {
		return Review{}, err
	}
	if already {
		return Review{}, errConflict
	}
	id, err := NewID()
	if err != nil {
		return Review{}, errUnavailable
	}
	now := s.now().UTC()
	row := Review{
		ID:                    id,
		VerifiedInteractionID: ID(ref.ID),
		ListingID:             ID(ref.ListingID),
		ReviewerUserID:        reviewerUserID,
		ProviderUserID:        ID(ref.ProviderUserID),
		Body:                  body,
		ListingAccuracy:       in.ListingAccuracy,
		ProviderService:       in.ProviderService,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := row.Validate(); err != nil {
		return Review{}, err
	}
	ev, err := encodeVerifiedCreated(row)
	if err != nil {
		return Review{}, err
	}
	err = s.store.InsertReview(ctx, row, func(ctx context.Context, exec outbox.Execer) error {
		_, err := s.outbox.Enqueue(ctx, exec, ev)
		return err
	})
	if err != nil {
		return Review{}, mapStoreErr(err)
	}
	return row, nil
}

func (s *Service) ListMine(ctx context.Context, reviewerUserID ID) ([]Review, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if reviewerUserID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListForReviewer(ctx, reviewerUserID, MaxReviews)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return rows, nil
}

func (s *Service) requireRequesterInteraction(ctx context.Context, reviewerUserID, verifiedInteractionID ID) (verifiedcontracts.InteractionRef, error) {
	if s.verified == nil {
		return verifiedcontracts.InteractionRef{}, errVerifiedReq
	}
	ref, err := s.verified.GetInteraction(ctx, verifiedcontracts.ID(verifiedInteractionID))
	if err != nil {
		return verifiedcontracts.InteractionRef{}, mapVerifiedErr(err)
	}
	if ID(ref.RequesterUserID) != reviewerUserID {
		return verifiedcontracts.InteractionRef{}, errNotFound
	}
	return ref, nil
}

func (s *Service) alreadyReviewed(ctx context.Context, verifiedInteractionID ID) (bool, error) {
	_, err := s.store.GetByInteraction(ctx, verifiedInteractionID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, errNotFound) {
		return false, nil
	}
	return false, mapStoreErr(err)
}

func (s *Service) reviewable(ref verifiedcontracts.InteractionRef) bool {
	if ref.ID.IsZero() || ref.ListingID.IsZero() || ref.RequesterUserID.IsZero() || ref.ProviderUserID.IsZero() {
		return false
	}
	if ref.VerifiedAt.IsZero() {
		return false
	}
	return ref.InteractionType == verifiedcontracts.InteractionTypeListingInspection
}

func mapVerifiedErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, verifiedcontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, verifiedcontracts.ErrNotFound) {
		return errNotFound
	}
	if errors.Is(err, verifiedcontracts.ErrUnavailable) {
		return errUnavailable
	}
	return errUnavailable
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) || errors.Is(err, errInvalidBody) || errors.Is(err, errInvalidRating) ||
		errors.Is(err, errInvalidReview) || errors.Is(err, errSelfReview) || errors.Is(err, errNotFound) ||
		errors.Is(err, errConflict) || errors.Is(err, errNotEligible) || errors.Is(err, errStoreRequired) ||
		errors.Is(err, errUnavailable) || errors.Is(err, errListingsReq) || errors.Is(err, errInvalidQuery) {
		return err
	}
	return errUnavailable
}
