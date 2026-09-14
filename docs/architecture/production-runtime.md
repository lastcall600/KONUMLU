# Production runtime (V1)

Vendor-neutral process topology, configuration, health, and operations expectations for KONUMLU. Hosting provider, Kubernetes, Kafka, and OpenTofu/Terraform provider selection remain deferred.

## Topology (minimum production processes)

Do not split marketplace domains into separate deployable services. The Go backend remains a modular monolith **in Germany**. The Türkiye Compliance Gateway (ADR-015) is a **residency/compliance** process, not a listings/search/messaging microservice.

| Process / component | Role |
|---|---|
| `cmd/server` | HTTP API |
| `cmd/worker` | Outbox relay / async handlers **and** notification `channel_deliveries` dispatcher (sibling poll loop; unconfigured providers do not claim) |
| PostgreSQL + PostGIS | Durable source of truth |
| Valkey | Cache, session hot cache, rate limits, presence — never durable truth |
| S3-compatible object storage | Media objects behind `internal/infrastructure/storage` |
| Consumer web (`web/apps/consumer`) | Public Next.js app |
| Admin web (`web/apps/admin`) | Management Center Next.js app |
| External providers | Staff IdP, email/SMS, malware/moderation, PSP — only when configured |
| Türkiye Compliance Gateway | Separate Türkiye-hosted process + PostgreSQL for official EİDS/e-Devlet I/O, sensitive evidence, and **signed verification decisions**. Not a general app server. Source of truth: [ADR-015](../ADR/ADR-015-tr-compliance-gateway-data-residency.md) (🔒 FROZEN). |

`cmd/migrate` is an explicit operator CLI. Applications do not auto-run migrations on startup.

Local compose (`docker-compose.yml`) is development-only (PostgreSQL/PostGIS + Valkey + MinIO). It is not a production topology.

## Runtime modes (`APP_ENV`)

`development` | `test` | `staging` | `production`

Unset `APP_ENV` loads as `development`. Invalid values fail process start. Staging and production are fail-closed for security-critical settings. Development-only fallbacks (optional Valkey at load, optional WebAuthn at load, disabled object storage, default outbox worker concurrency) cannot activate in staging or production.

## Configuration classification

**Required (all modes):** `DATABASE_URL`, session TTLs, `IDENTITY_STEP_UP_TTL`, WebAuthn ceremony TTL, auth rate-limit policy, verification/signup policy, outbox claim/retry policy, verification material keyring.

**Required in staging/production:** `VALKEY_URL`, `TRUSTED_PROXIES` (empty value means do not trust `X-Forwarded-For`), WebAuthn RP display name / RP ID / origins, `OBJECT_STORAGE_ENABLED=true` plus endpoint, region, bucket, and static credentials or `OBJECT_STORAGE_CREDENTIAL_SOURCE=workload` with empty static keys, `OUTBOX_WORKER_CONCURRENCY`, `PUSH_ENDPOINT_ENCRYPTION_KEY` and `PUSH_ENDPOINT_HASH_KEY` (AES-256 + HMAC; never logged; required because push registration HTTP is part of the consumer API).

**Optional:** `HTTP_ADDR` (default `:8080`), `SHUTDOWN_TIMEOUT` (default `10s`), `DB_CONNECT_TIMEOUT` (default `5s`), `DB_MAX_CONNS` / `DB_MIN_CONNS` / `DB_MAX_CONN_LIFETIME` / `DB_MAX_CONN_IDLE_TIME`, `LOG_LEVEL` (default `info`), notification channel modes (default `disabled`), `EMAIL_PROVIDER=ses` with `EMAIL_SES_REGION` / `EMAIL_SES_FROM` / `EMAIL_SES_TIMEOUT` (default `5s`) when email mode is `external`, `SMS_PROVIDER=netgsm` with `NETGSM_USERNAME` / `NETGSM_PASSWORD` / `NETGSM_MSGHEADER` / `NETGSM_TIMEOUT` (default `5s`) when SMS mode is `external`, `PUSH_ENDPOINT_ENCRYPTION_KEY` / `PUSH_ENDPOINT_HASH_KEY` in development/test (when set, push registration HTTP is enabled), Web Push VAPID (`WEBPUSH_VAPID_*`, optional `WEBPUSH_TIMEOUT`) when Web Push send is enabled, FCM HTTP v1 (`FCM_PROJECT_ID`, optional `FCM_CREDENTIALS_FILE` / `FCM_TIMEOUT`; ADC otherwise), APNs token auth (`APNS_TEAM_ID` / `APNS_KEY_ID` / `APNS_TOPIC` / `APNS_ENVIRONMENT` plus `APNS_PRIVATE_KEY` or `APNS_PRIVATE_KEY_FILE`, optional `APNS_TIMEOUT`). Any combination of push providers may be enabled; missing credentials for an enabled provider fail process start. Disabled providers stay unconfigured (never fake accepted). media scanner requirement flags, staff IdP triple (issuer/audience/JWKS).

**Provider-dependent / AUTH launch blockers:** HumanChallenge production provider is Cloudflare Turnstile (`IDENTITY_HUMAN_CHALLENGE_PROVIDER=turnstile` plus secret, exact hostname allowlist, and timeout; `fake` rejected in staging/production; `unconfigured` fail-closed when operations are required). Production widget credentials are not committed. Transactional email provider is Amazon SES (`NOTIFICATIONS_EMAIL_MODE=external`, `EMAIL_PROVIDER=ses`, `EMAIL_SES_REGION`, `EMAIL_SES_FROM`; AWS default credential chain; SES API v2 `SendEmail`; accept ≠ delivered; sandbox is region-specific; sender identity must be verified; production access out of sandbox is an AWS account operator step, not application truth). Transactional/OTP SMS provider is Netgsm (`NOTIFICATIONS_SMS_MODE=external`, `SMS_PROVIDER=netgsm`, `NETGSM_USERNAME`, `NETGSM_PASSWORD`, `NETGSM_MSGHEADER`; REST v2 `POST /sms/rest/v2/send` for notification SMS and `POST /sms/rest/v2/otp` for Identity OTP; HTTP Basic Auth; stdlib `net/http`; server-owned sender header; `jobid` is an opaque string on internal `ProviderRef`; accept ≠ handset delivery; no adapter retry loop; dispatcher/outbox own retry; OTP package required on the Netgsm account for `/otp`; no marketing/`iysfilter` policy in this package). `disabled` never marks delivery sent. `STAFF_IDP_*` (complete or empty; partial fails closed; adapter still required to register staff HTTP), EİDS official adapter (unconfigured = unavailable, never verified; production official I/O and raw payloads belong on the TR Compliance Gateway per ADR-015), object-storage workload identity (config path exists; client wiring waits hosting).

Process start logs `auth_provider_launch_blocked` with `human_challenge_vendor`, `email_vendor`, and/or `sms_vendor` when those adapters are not production-wired. Wired Turnstile omits `human_challenge_vendor`. Wired SES (`EMAIL_PROVIDER=ses` plus region/from) omits `email_vendor`. Wired Netgsm (`SMS_PROVIDER=netgsm` plus username/password/msgheader with SMS mode `external`) omits `sms_vendor`. See `docs/operations/AUTH-SECURITY.md`.

**Development-only:** `ALLOW_INSECURE_COOKIES` (rejected in staging/production; cookies remain `__Host-` + Secure in handlers). Disabled object storage. Unset Valkey / WebAuthn RP at config load.

No secret defaults. Config `String` / slog representations omit URLs, keys, and cookies.

## Health

| Endpoint | Meaning |
|---|---|
| `GET /healthz` | Liveness. Process is up. No DB, Valkey, storage, or vendor checks. |
| `GET /readyz` | Readiness. PostgreSQL + PostGIS, and Valkey when `VALKEY_URL` is set. Body never includes check errors. |

External provider outage (EİDS / TR Compliance Gateway, email/SMS, staff IdP, object storage) is not Germany process death. Liveness stays OK. Readiness does not probe those vendors. Request handling already fails closed or degrades per domain (auth rate-limit/session Valkey semantics, media 503, EİDS unavailable **not verified**, staff routes unregistered). If the TR gateway is down: main KONUMLU should keep serving non-verification traffic; **new** government/EİDS verification is unavailable; **no fail-open** (ADR-015).

## Data stores

**PostgreSQL/PostGIS (Germany):** source of truth for the main platform. Pool bounds are optional env (`DB_MAX_*`). Migrations: `go run ./cmd/migrate up` (or `down 1` / `version`) only. No destructive auto-migrate on boot.

**PostgreSQL (Türkiye Compliance Gateway):** separate database in Türkiye. Not a replica of Germany. No cross-country DB connection. Stores sensitive verification/provider state. Germany stores only the minimum signed decision (ADR-015).

**Valkey:** non-durable. No persistence assumption. When unavailable: auth abuse / issuance rate limits fail closed (`unavailable`); human challenge, when required, fails closed; **step-up elevation fails closed** (require recent-strong again; never grant); **first-passkey bootstrap fails closed** (never grant enrollment authority); session hot cache still rehydrates from PostgreSQL; never reconstruct business truth from Valkey. See `docs/architecture/auth-abuse.md`, `docs/architecture/auth-session.md`, and `docs/operations/AUTH-SECURITY.md`.

Auth security audit uses `platform.outbox_events` (`identity.auth.security` v1). No Kafka. Worker runs Identity allowlisted logging then Notifications materialization as one sequential handler. Transient notification failure retries the outbox row. Selected events create `notifications.intents`; audit-only events do not.

**Object storage:** S3-compatible adapter. Originals are private quarantine (`media/listing-images/{owner}/{asset}/{random}`); public listing media uses processed keys only (`.../p/{random}`). Production requires explicit bucket/endpoint/region and either static keys or workload-identity config (adapter for workload is unwired until hosting). `OBJECT_STORAGE_PUBLIC_BASE_URL` is processed-media delivery only. CDN, when chosen, attaches in front of processed objects — see `docs/architecture/media-pipeline.md`. Upload presign TTL is capped at 15 minutes. Worker reclaims expired pending/rejected quarantine objects; it never deletes `ready` media by age.

## Worker / outbox

PostgreSQL transactional outbox + `cmd/worker` poller. A second bounded poll loop claims `notifications.channel_deliveries` without starving outbox `RunWorkers`. No Kafka or other broker. Settings: batch, lease (also used as dispatcher processing-hold for stranded `processing` rows), poll interval, retry base/multiplier/cap/jitter, worker concurrency (competing `SKIP LOCKED` loops). Unknown handlers reschedule with `unknown_handler` and are logged. Shutdown cancels both poll loops. `SHUTDOWN_TIMEOUT` bounds HTTP graceful shutdown on the API process.

## Observability and security

- JSON structured logs (`log/slog`), request id (`X-Request-Id`), no-op metrics/tracing hooks (no vendor SDK, no OpenTelemetry dependency until chosen).
- Access logs: method, path, status, duration, request id. No cookies, Authorization, or bodies. EİDS/TR logs: `request_id` / `decision_id` / type / result **class** / latency / HTTP status only — never TCKN, tokens, birth date, address, or raw provider bodies (ADR-015).
- Trusted proxies: explicit CIDRs. Untrusted peers never honor `X-Forwarded-For`.
- Browser cookies remain `__Host-`, HttpOnly, Secure, SameSite=Lax.
- Allowed origins: WebAuthn/CORS origin list; required in staging/production.
- Staff auth fail-closed without a registered IdP adapter. EİDS fail-closed (unconfigured = unavailable, not verified; TR outage = unavailable, not verified). No debug/admin bypasses.

## Deployment images

`backend/Dockerfile` builds `COMMAND=server` or `COMMAND=worker`. Consumer and admin web are separate Next.js builds. Do not add Kubernetes manifests here. Do not select AWS/GCP/Azure/Fly. OpenTofu/Terraform remains deferred with hosting.

## Backup / restore (encode, do not implement cloud backups)

Operator runbook: `docs/operations/BACKUP-RESTORE.md`. Logical backup restore is proven locally; production PITR (WAL archive + base backup) remains a hosting implementation step.

- PostgreSQL (Germany): automated backups plus point-in-time recovery. Retention is a hosting decision. Do **not** automatically back up the TR Compliance Gateway database to Germany.
- PostgreSQL (TR Compliance Gateway): encrypted backups **inside Türkiye**, keys not stored beside objects. Retention duration is a legal/product decision. See ADR-015.
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

Cloud vendor; Kubernetes (not required for V1); OpenTofu/Terraform provider; object-storage product and workload-identity wiring; Staff IdP vendor; official EİDS adapter onboarding (TR gateway implementation is ADR-015 freeze, not code); production Amazon SES and Netgsm credentials; production Cloudflare Turnstile widget credentials (adapter exists); frontend HumanChallenge widget; push/notification delivery vendors; CDN for processed media (must not sit in front of raw government-provider traffic); backup retention numbers (including TR legal retention).

Notification **policy, planning, marketplace producers, and provider-neutral dispatch** live in `internal/notifications` plus domain outbox events. Email transport is Amazon SES. SMS transport is Netgsm. Push vendor is not selected. Unconfigured adapters never mark `accepted`. Push endpoints are not stored. See `docs/architecture/notifications-policy.md` and `docs/operations/NOTIFICATIONS.md`.
