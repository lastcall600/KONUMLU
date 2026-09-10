ALTER TABLE listings.listings
    ADD COLUMN moderation_state TEXT NOT NULL DEFAULT 'none';

ALTER TABLE listings.listings
    ADD CONSTRAINT listings_listings_moderation_state_check CHECK (
        moderation_state IN ('none', 'restricted', 'removed')
    );
