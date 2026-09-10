ALTER TABLE listings.listings
    DROP CONSTRAINT IF EXISTS listings_listings_moderation_state_check;

ALTER TABLE listings.listings
    DROP COLUMN IF EXISTS moderation_state;
