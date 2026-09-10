CREATE SCHEMA media;

CREATE TABLE media.assets (
    id UUID PRIMARY KEY,
    owner_user_id UUID NOT NULL,
    listing_id UUID,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    object_key TEXT NOT NULL,
    original_filename TEXT,
    content_type TEXT,
    size_bytes BIGINT,
    width INTEGER,
    height INTEGER,
    sort_order INTEGER,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    ready_at TIMESTAMPTZ,
    rejected_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    CONSTRAINT media_assets_kind_check CHECK (kind IN ('listing_image')),
    CONSTRAINT media_assets_status_check CHECK (
        status IN ('pending_upload', 'uploaded', 'processing', 'ready', 'rejected', 'deleted')
    ),
    CONSTRAINT media_assets_object_key_unique UNIQUE (object_key),
    CONSTRAINT media_assets_updated_not_before_created CHECK (updated_at >= created_at),
    CONSTRAINT media_assets_size_positive CHECK (size_bytes IS NULL OR size_bytes > 0),
    CONSTRAINT media_assets_width_positive CHECK (width IS NULL OR width > 0),
    CONSTRAINT media_assets_height_positive CHECK (height IS NULL OR height > 0),
    CONSTRAINT media_assets_sort_order_non_negative CHECK (sort_order IS NULL OR sort_order >= 0)
);

CREATE INDEX media_assets_owner_user_id_idx ON media.assets (owner_user_id);
CREATE INDEX media_assets_listing_id_idx ON media.assets (listing_id);
