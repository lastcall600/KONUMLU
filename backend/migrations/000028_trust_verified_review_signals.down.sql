ALTER TABLE trust.user_profiles
    DROP CONSTRAINT IF EXISTS user_profiles_provider_service_bounds;

ALTER TABLE trust.user_profiles
    DROP CONSTRAINT IF EXISTS user_profiles_review_counts_nonnegative;

ALTER TABLE trust.user_profiles
    DROP COLUMN IF EXISTS last_provider_service_review_at,
    DROP COLUMN IF EXISTS last_verified_review_at,
    DROP COLUMN IF EXISTS provider_service_average,
    DROP COLUMN IF EXISTS provider_service_rating_sum,
    DROP COLUMN IF EXISTS provider_service_review_count,
    DROP COLUMN IF EXISTS verified_review_count;
