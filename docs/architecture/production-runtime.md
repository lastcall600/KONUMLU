# Production runtime (V1)

Vendor-neutral process topology, configuration, health, and operations expectations for KONUMLU. Hosting provider, Kubernetes, Kafka, and OpenTofu/Terraform provider selection remain deferred.

## Topology (minimum production processes)

Do not split domains into separate deployable services. The Go backend remains a modular monolith.

| Process / component | Role |
|---|---|
| `cmd/server` | HTTP API |
| `cmd/worker` | Outbox relay / async handlers |
| PostgreSQL + PostGIS | Durable source of truth |
| Valkey | Cache, session hot cache, rate limits, presence — never durable truth |
| S3-compatible object storage | Media objects behind `internal/infrastructure/storage` |
| Consumer web (`web/apps/consumer`) | Public Next.js app |
| Admin web (`web/apps/admin`) | Management Center Next.js app |
| External providers | Staff IdP, EİDS, email/SMS, malware/moderation, PSP — only when configured |

`cmd/migrate` is an explicit operator CLI. Applications do not auto-run migrations on startup.

Local compose (`docker-compose.yml`) is development-only (PostgreSQL/PostGIS + Valkey + MinIO). It is not a production topology.

## Runtime modes (`APP_ENV`)

`development` | `test` | `staging` | `production`

Unset `APP_ENV` loads as `development`. Invalid values fail process start. Staging and production are fail-closed for security-critical settings. Development-only fallbacks (optional Valkey at load, optional WebAuthn at load, disabled object storage, default outbox worker concurrency) cannot activate in staging or production.

## Configuration classification

**Required (all modes):** `DATABASE_URL`, session TTLs, WebAuthn ceremony TTL, auth rate-limit policy, verification/signup policy, outbox claim/retry policy, verification material keyring.

**Required in staging/production:** `VALKEY_URL`, `TRUSTED_PROXIES` (empty value means do not trust `X-Forwarded-For`), WebAuthn RP display name / RP ID / origins, `OBJECT_STORAGE_ENABLED=true` plus endpoint, region, bucket, and static credentials or `OBJECT_STORAGE_CREDENTIAL_SOURCE=workload` with empty static keys, `OUTBOX_WORKER_CONCURRENCY`.

**Optional:** `HTTP_ADDR` (default `:8080`), `SHUTDOWN_TIMEOUT` (default `10s`), `DB_CONNECT_TIMEOUT` (default `5s`), `DB_MAX_CONNS` / `DB_MIN_CONNS` / `DB_MAX_CONN_LIFETIME` / `DB_MAX_CONN_IDLE_TIME`, `LOG_LEVEL` (default `info`), notification channel modes (default `disabled`), media scanner requirement flags, staff IdP triple (issuer/audience/JWKS).

**Provider-dependent:** `STAFF_IDP_*` (complete or empty; partial fails closed; adapter still required to register staff HTTP), `NOTIFICATIONS_EMAIL_MODE` / `NOTIFICATIONS_SMS_MODE`=`external` (adapter required at worker start), EİDS official adapter (unconfigured gateway returns unavailable, never verified), object-storage workload identity (config path exists; client wiring waits hosting).

**Development-only:** `ALLOW_INSECURE_COOKIES` (rejected in staging/production; cookies remain `__Host-` + Secure in handlers). Disabled object storage. Unset Valkey / WebAuthn RP at config load.

No secret defaults. Config `String` / slog representations omit URLs, keys, and cookies.

## Health

| Endpoint | Meaning |
|---|---|
| `GET /healthz` | Liveness. Process is up. No DB, Valkey, storage, or vendor checks. |
| `GET /readyz` | Readiness. PostgreSQL + PostGIS, and Valkey when `VALKEY_URL` is set. Body never includes check errors. |

External provider outage (EİDS, email/SMS, staff IdP, object storage) is not process death. Liveness stays OK. Readiness does not probe those vendors. Request handling already fails closed or degrades per domain (auth rate-limit/session Valkey semantics, media 503, EİDS unavailable, staff routes unregistered).

## Data stores

**PostgreSQL/PostGIS:** source of truth. Pool bounds are optional env (`DB_MAX_*`). Migrations: `go run ./cmd/migrate up` (or `down 1` / `version`) only. No destructive auto-migrate on boot.

**Valkey:** non-durable. No persistence assumption. When unavailable: auth abuse / issuance rate limits fail closed (`unavailable`); human challenge, when required, fails closed; session hot cache still rehydrates from PostgreSQL; never reconstruct business truth from Valkey. See `docs/architecture/auth-abuse.md`.

**Object storage:** S3-compatible adapter. Originals are private quarantine (`media/listing-images/{owner}/{asset}/{random}`); public listing media uses processed keys only (`.../p/{random}`). Production requires explicit bucket/endpoint/region and either static keys or workload-identity config (adapter for workload is unwired until hosting). `OBJECT_STORAGE_PUBLIC_BASE_URL` is processed-media delivery only. CDN, when chosen, attaches in front of processed objects — see `docs/architecture/media-pipeline.md`. Upload presign TTL is capped at 15 minutes. Worker reclaims expired pending/rejected quarantine objects; it never deletes `ready` media by age.

## Worker / outbox

PostgreSQL transactional outbox + `cmd/worker` poller. No Kafka or other broker. Settings: batch, lease, poll interval, retry base/multiplier/cap/jitter, worker concurrency (competing `SKIP LOCKED` loops). Unknown handlers reschedule with `unknown_handler` and are logged. Shutdown cancels the poll loop; in-flight handler cancellation leaves the lease so another worker can retry. `SHUTDOWN_TIMEOUT` bounds HTTP graceful shutdown on the API process.

## Observability and security

- JSON structured logs (`log/slog`), request id (`X-Request-Id`), no-op metrics/tracing hooks (no vendor SDK, no OpenTelemetry dependency until chosen).
- Access logs: method, path, status, duration, request id. No cookies, Authorization, or bodies.
- Trusted proxies: explicit CIDRs. Untrusted peers never honor `X-Forwarded-For`.
- Browser cookies remain `__Host-`, HttpOnly, Secure, SameSite=Lax.
- Allowed origins: WebAuthn/CORS origin list; required in staging/production.
- Staff auth fail-closed without a registered IdP adapter. EİDS fail-closed (unconfigured = unavailable, not verified). No debug/admin bypasses.

## Deployment images

`backend/Dockerfile` builds `COMMAND=server` or `COMMAND=worker`. Consumer and admin web are separate Next.js builds. Do not add Kubernetes manifests here. Do not select AWS/GCP/Azure/Fly. OpenTofu/Terraform remains deferred with hosting.

## Backup / restore (encode, do not implement cloud backups)

Operator runbook: `docs/operations/BACKUP-RESTORE.md`. Logical backup restore is proven locally; production PITR (WAL archive + base backup) remains a hosting implementation step.

- PostgreSQL: automated backups plus point-in-time recovery. Retention is a hosting decision.
- Object storage: provider durability plus versioning for media buckets. Retention is a hosting decision. PostgreSQL backup does **not** include S3/MinIO objects.
- Valkey is not a backup source and must not be restored as truth.
- Restore procedure (DB PITR + object restore + migrate version check + smoke `/healthz`/`/readyz`) must be tested before production launch.

## Production gates (no automatic production deploy)

Required before a production promotion:

1. Targeted platform/config/infrastructure tests and `cmd/server` + `cmd/worker` tests
2. `go run ./cmd/archcheck` (`make arch-check`)
3. Migration validation (`migrate version` / `up` in a controlled environment — not this process’s auto-start)
4. Frontend typecheck/build when web config or public API contracts change
5. Image build verification for server and worker Dockerfiles

GitHub Actions full pipeline is not introduced by this foundation. No automatic production deploy.

## Remaining hosting / provider decisions

Cloud vendor; Kubernetes (not required for V1); OpenTofu/Terraform provider; object-storage product and workload-identity wiring; Staff IdP vendor; official EİDS adapter; email/SMS vendors; CDN for processed media; backup retention numbers.
