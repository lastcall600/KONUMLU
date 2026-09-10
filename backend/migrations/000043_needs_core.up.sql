CREATE SCHEMA needs;

CREATE TABLE needs.needs (
    id UUID PRIMARY KEY,
    requester_user_id UUID NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    category_id UUID,
    status TEXT NOT NULL,
    budget_min_amount NUMERIC,
    budget_max_amount NUMERIC,
    budget_currency TEXT,
    point geography(Point, 4326) NOT NULL,
    radius_km DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ,
    CONSTRAINT needs_needs_status_check CHECK (
        status IN ('draft', 'open', 'fulfilled', 'cancelled', 'expired')
    ),
    CONSTRAINT needs_needs_title_not_blank CHECK (
        char_length(btrim(title)) > 0
    ),
    CONSTRAINT needs_needs_budget_all_or_none CHECK (
        (budget_min_amount IS NULL AND budget_max_amount IS NULL AND budget_currency IS NULL)
        OR (budget_min_amount IS NOT NULL AND budget_max_amount IS NOT NULL AND budget_currency IS NOT NULL)
    ),
    CONSTRAINT needs_needs_budget_range CHECK (
        budget_min_amount IS NULL OR budget_min_amount <= budget_max_amount
    ),
    CONSTRAINT needs_needs_budget_currency_check CHECK (
        budget_currency IS NULL OR budget_currency ~ '^[A-Z]{3}$'
    ),
    CONSTRAINT needs_needs_radius_bounds CHECK (
        radius_km IS NULL OR (radius_km >= 0.1 AND radius_km <= 50)
    ),
    CONSTRAINT needs_needs_latitude_range CHECK (
        ST_Y(point::geometry) >= -90 AND ST_Y(point::geometry) <= 90
    ),
    CONSTRAINT needs_needs_longitude_range CHECK (
        ST_X(point::geometry) >= -180 AND ST_X(point::geometry) <= 180
    ),
    CONSTRAINT needs_needs_expires_after_created CHECK (
        expires_at IS NULL OR expires_at > created_at
    ),
    CONSTRAINT needs_needs_updated_not_before_created CHECK (updated_at >= created_at)
);

CREATE INDEX needs_needs_requester_created_id_idx
    ON needs.needs (requester_user_id, created_at DESC, id);

CREATE INDEX needs_needs_status_idx
    ON needs.needs (status);

CREATE INDEX needs_needs_created_at_idx
    ON needs.needs (created_at);
