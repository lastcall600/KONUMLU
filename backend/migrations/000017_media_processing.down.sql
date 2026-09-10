ALTER TABLE media.assets DROP CONSTRAINT IF EXISTS media_assets_processed_key_ready;
ALTER TABLE media.assets DROP CONSTRAINT IF EXISTS media_assets_processed_object_key_unique;
ALTER TABLE media.assets DROP COLUMN IF EXISTS processed_object_key;
