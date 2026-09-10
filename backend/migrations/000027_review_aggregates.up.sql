CREATE SCHEMA review_aggregates;

-- Derived, rebuildable review projections. Authoritative review rows stay in Reviews.
-- Source event: reviews.verified.created v1. Listing accuracy and provider service
-- are kept as separate aggregates with no hidden weighting.

CREATE TABLE review_aggregates.processed_events (
    event_id UUID PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE review_aggregates.listing_accuracy (
    listing_id UUID PRIMARY KEY,
    review_count INTEGER NOT NULL,
    rating_sum INTEGER NOT NULL,
    average_rating NUMERIC NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT listing_accuracy_count_positive CHECK (review_count > 0),
    CONSTRAINT listing_accuracy_sum_bounds CHECK (
        rating_sum >= review_count
        AND rating_sum <= review_count * 5
    )
);

CREATE TABLE review_aggregates.provider_service (
    provider_user_id UUID PRIMARY KEY,
    review_count INTEGER NOT NULL,
    rating_sum INTEGER NOT NULL,
    average_rating NUMERIC NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT provider_service_count_positive CHECK (review_count > 0),
    CONSTRAINT provider_service_sum_bounds CHECK (
        rating_sum >= review_count
        AND rating_sum <= review_count * 5
    )
);

CREATE INDEX listing_accuracy_updated_idx
    ON review_aggregates.listing_accuracy (updated_at DESC, listing_id);

CREATE INDEX provider_service_updated_idx
    ON review_aggregates.provider_service (updated_at DESC, provider_user_id);
