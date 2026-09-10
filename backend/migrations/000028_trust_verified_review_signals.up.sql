-- Derived verified-review counters on Trust user profiles.
-- Trust level remains based only on verified interaction count.
-- Listing accuracy is not stored on user Trust profiles.

ALTER TABLE trust.user_profiles
    ADD COLUMN verified_review_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN provider_service_review_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN provider_service_rating_sum INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN provider_service_average NUMERIC,
    ADD COLUMN last_verified_review_at TIMESTAMPTZ,
    ADD COLUMN last_provider_service_review_at TIMESTAMPTZ;

ALTER TABLE trust.user_profiles
    ADD CONSTRAINT user_profiles_review_counts_nonnegative CHECK (
        verified_review_count >= 0
        AND provider_service_review_count >= 0
        AND provider_service_rating_sum >= 0
    );

ALTER TABLE trust.user_profiles
    ADD CONSTRAINT user_profiles_provider_service_bounds CHECK (
        (
            provider_service_review_count = 0
            AND provider_service_rating_sum = 0
            AND provider_service_average IS NULL
        )
        OR (
            provider_service_review_count > 0
            AND provider_service_rating_sum >= provider_service_review_count
            AND provider_service_rating_sum <= provider_service_review_count * 5
            AND provider_service_average IS NOT NULL
        )
    );
