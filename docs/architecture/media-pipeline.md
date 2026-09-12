# Media pipeline (upload, quarantine, processing)

Authoritative runtime behavior for listing images. Domain states remain those already defined on `media.assets`. This document does not redesign Media.

## Upload flow

1. Authenticated consumer `POST /v1/media/listing-images` (session cookie + CSRF + allowed Origin). Owner is the session user. Optional `listingId` is checked through Listings ownership; client `ownerUserId` / `objectKey` are ignored.
2. Media creates `pending_upload` with a **server-generated** object key: `media/listing-images/{ownerUserId}/{assetId}/{random}`.
3. API returns a short-lived **presigned PUT** (`uploadUrl`, `expiresAt` ≤ 15m, `maxBytes`, optional required headers). Storage credentials are never returned. File bytes do not pass through the Go API.
4. Client PUTs directly to private MinIO/S3.
5. `POST /v1/media/listing-images/{assetId}/confirm` stats the **server** key. Missing object → 400. Oversized → `rejected` + delete. Success → `uploaded` (untrusted) and a transactional outbox event `media.image.process` v1. Duplicate confirm while `uploaded`/`processing` is idempotent. Confirm never marks `ready`. Client Content-Type is not trusted.

## Object lifecycle and prefixes

| Role | Key shape | Public? |
|---|---|---|
| Quarantine original | `media/listing-images/{owner}/{asset}/{random}` | No. Adapter refuses `IssueGetTarget`. Bucket has no anonymous policy. |
| Processed / approved | `media/listing-images/{owner}/{asset}/p/{random}` | Delivery only: short-lived signed GET, or `OBJECT_STORAGE_PUBLIC_BASE_URL` for processed keys only. |

One bucket (`OBJECT_STORAGE_BUCKET`, local default `konumlu-media`). Prefixes separate quarantine from processed. Do not use a public anonymous bucket.

## States

Existing CHECK constraint only:

`pending_upload` → `uploaded` → `processing` → `ready` | `rejected` | `deleted`

Bytes stay untrusted until `ready`. Public listing media lists only ready processed objects.

## Image validation and processing

Worker (`cmd/worker`) handles `media.image.process`. HTTP confirm does not decode.

- Magic bytes + stdlib JPEG/PNG decode (not filename / Content-Type).
- Size ceiling (`OBJECT_STORAGE_MAX_UPLOAD_BYTES`, default 10 MiB).
- Dimension / pixel-count bomb limits (8192, 32e6 pixels).
- Re-encode to **lossless WebP** (`github.com/HugoSmits86/nativewebp`, MIT, pure Go — stdlib has no WebP encoder). EXIF/GPS is dropped by decode + encode (`UseExtendedFormat=false`).
- Downscale to a **detail** long-edge of 1600px (or tighter processing policy). Aspect ratio preserved. No upscaling.
- Metadata stored on `media.assets`: `content_type=image/webp`, `size_bytes`, `width`, `height`, `processed_object_key`. Binaries stay in object storage.

Thumbnail / listing-card extra variants are **not** persisted. The current row has a single processed key; additional variants would need a migration (`media.asset_variants` or equivalent). Do not add that without review.

## Video

V1 `kind` is `listing_image` only. There is no video upload, FFmpeg, or HLS contract. Treat video as the next media completion layer.

## Retry / idempotency

- Outbox idempotency key `media.image.process:{assetId}`.
- Process no-ops on `ready` / `rejected` / `deleted` / `pending_upload`.
- Processed PUT uses the deterministic processed key; retries overwrite the same object rather than minting a second variant.
- Scanner/moderation required-but-missing stays `processing` (retryable). Invalid bytes become `rejected` and are not approved on retry.

## Cleanup / orphans

Worker ticker (`RunOrphanSweeper`, every 5m):

- `pending_upload` older than 24h → delete object + `deleted`
- `rejected` older than 24h → delete object + `deleted`
- **Never** selects `ready`

Stuck `uploaded`/`processing` is the outbox worker’s job, not age-based deletion of approved media.

## Local MinIO

`docker-compose.yml` runs PostgreSQL/PostGIS, Valkey, MinIO (`:9000`, console `:9001`), and `minio-init` (private bucket `konumlu-media`).

Example API/worker env (development):

```
OBJECT_STORAGE_ENABLED=true
OBJECT_STORAGE_ENDPOINT=http://127.0.0.1:9000
OBJECT_STORAGE_REGION=us-east-1
OBJECT_STORAGE_BUCKET=konumlu-media
OBJECT_STORAGE_ACCESS_KEY=konumlu
OBJECT_STORAGE_SECRET_KEY=konumlu-dev
OBJECT_STORAGE_PATH_STYLE=true
OBJECT_STORAGE_UPLOAD_TTL=15m
OBJECT_STORAGE_GET_TTL=5m
OBJECT_STORAGE_MAX_UPLOAD_BYTES=10485760
```

Live proof (direct MinIO adapter): `LIVE_MEDIA_MINIO=1` plus the env above, `go test ./internal/infrastructure/storage -run TestLiveMinIOUploadProcessAndReject -count=1`.

Live proof (real confirm → outbox → `cmd/worker` → MinIO, does not call `ProcessAsset`): run `cmd/worker` against local PostgreSQL/MinIO, then `LIVE_MEDIA_WORKER=1` with the same env, `go test ./internal/media/httpapi -run TestLiveOutboxWorkerMinIOProcess -count=1`.

## Production S3 and CDN

Same adapter: any S3-compatible endpoint, path-style or virtual-host, static keys or (later) workload identity. Production `APP_ENV` requires object storage enabled.

CDN attaches **in front of processed objects only**: either signed GET from the origin, or `OBJECT_STORAGE_PUBLIC_BASE_URL` pointing at a CDN that can read the processed prefix. Quarantine originals must stay private. CDN vendor is not selected in this sprint.

## Security limits

- Session + CSRF + Origin on mutating media HTTP
- Non-owner confirm/get → privacy-safe 404
- Server-owned keys only; adapter rejects other keys
- Presign TTL capped at 15 minutes
- Logs: `media_id`, `media_status`, `processing_outcome`, `object_category`, `duration_ms`, `error_class`. No signed URLs, credentials, EXIF, or object keys.

## Known gaps

- Extra image variants (thumbnail/card) need a schema/migration
- Video ingest/transcode not in contract
- Malware/moderation vendor adapters (flags exist; missing required adapter is retryable)
- Workload identity S3 wiring
- CDN vendor
- Age-based cleanup of stuck `processing` (outbox retries instead)
- Profile/message media kinds not implemented
