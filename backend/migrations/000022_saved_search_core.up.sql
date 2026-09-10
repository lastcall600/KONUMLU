CREATE SCHEMA saved_search;

-- UUID user_id only. No FK to identity or search tables.
CREATE TABLE saved_search.saved_searches (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    name TEXT NOT NULL,
    q TEXT,
    category_id UUID,
    min_price TEXT,
    max_price TEXT,
    currency TEXT,
    north DOUBLE PRECISION,
    south DOUBLE PRECISION,
    east DOUBLE PRECISION,
    west DOUBLE PRECISION,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX saved_searches_user_created_idx
    ON saved_search.saved_searches (user_id, created_at DESC, id ASC);
