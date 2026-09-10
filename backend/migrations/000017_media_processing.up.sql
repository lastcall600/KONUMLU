ALTER TABLE media.assets
    ADD COLUMN processed_object_key TEXT;

ALTER TABLE media.assets
    ADD CONSTRAINT media_assets_processed_object_key_unique UNIQUE (processed_object_key);

ALTER TABLE media.assets
    ADD CONSTRAINT media_assets_processed_key_ready CHECK (
        (status = 'ready' AND processed_object_key IS NOT NULL)
        OR (status <> 'ready' AND processed_object_key IS NULL)
    );
