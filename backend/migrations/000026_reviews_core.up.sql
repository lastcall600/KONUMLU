CREATE SCHEMA reviews;

-- UUID references only. No FK to identity, listings, or verified tables.
CREATE TABLE reviews.reviews (
    id UUID PRIMARY KEY,
    verified_interaction_id UUID NOT NULL UNIQUE,
    listing_id UUID NOT NULL,
    reviewer_user_id UUID NOT NULL,
    provider_user_id UUID NOT NULL,
    body TEXT,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT reviews_participants_distinct CHECK (reviewer_user_id <> provider_user_id)
);

-- Listing accuracy is a separate dimension from provider service. Do not combine them.
CREATE TABLE reviews.listing_accuracy_ratings (
    review_id UUID PRIMARY KEY REFERENCES reviews.reviews (id),
    overall INTEGER NOT NULL,
    CONSTRAINT listing_accuracy_overall_range CHECK (overall BETWEEN 1 AND 5)
);

CREATE TABLE reviews.provider_service_ratings (
    review_id UUID PRIMARY KEY REFERENCES reviews.reviews (id),
    overall INTEGER NOT NULL,
    CONSTRAINT provider_service_overall_range CHECK (overall BETWEEN 1 AND 5)
);

CREATE INDEX reviews_reviewer_created_idx
    ON reviews.reviews (reviewer_user_id, created_at DESC, id DESC);
