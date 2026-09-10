CREATE SCHEMA favorites;

-- UUID references only. No FK to identity or listings tables.
CREATE TABLE favorites.listing_favorites (
    user_id UUID NOT NULL,
    listing_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, listing_id)
);
