# PROJECT-MAP.md

> The authoritative guide to repository layout. Every top-level directory and key file is listed here with its purpose.
> Update this file before merging any new top-level directory or documentation section.

---

## Root Files

| File | Purpose |
|---|---|
| `README.md` | Project overview, quick orientation, tech stack, links to key docs |
| `CURRENT-STATE.md` | Active phase, what is built, in progress, and deferred � the live status record |
| `DECISIONS.md` | Frozen technical decisions and open decisions with their gates |
| `ARCHITECTURE.md` | Authoritative architecture specification (frozen baseline) |
| `PROJECT-MAP.md` | This file � repository layout guide |
| `AGENTS.md` | AI agent operating norms and constraints (frozen baseline) |
| `Makefile` | Local commands (`arch-check`) |
| `docker-compose.yml` | Local PostgreSQL/PostGIS, Valkey, and MinIO |
| `docker-compose.restore-drill.yml` | Isolated local restore-target PostgreSQL/PostGIS (separate volume/port; never the source DB) |
| `docker-compose.pitr-lab.yml` | Isolated WAL/PITR capability lab (not the application database) |
| `.env.example` | Environment variable names (no values) |

---

## `/docs/` � Documentation Root

All documentation that is not a root orientation file lives under `/docs/`.

### `/docs/ADR/` � Architecture Decision Records

| File | Purpose |
|---|---|
| `INDEX.md` | Index of all ADRs by number, title, status, and date |
| `ADR-NNN-title.md` | Individual ADR files (NNN = zero-padded number) |

ADRs follow the format: context � decision � consequences � status.

### `/docs/architecture/` � Architecture Supplements

Detailed architecture diagrams, domain interaction sequence diagrams, data flow diagrams, and supplementary architecture documents that are too detailed for ARCHITECTURE.md.

| File | Purpose |
|---|---|
| `slice-a.md` | Slice A end-to-end flow detail (register — favorite) |
| `slice-b.md` | Slice B end-to-end flow detail (need — messaging) |
| `outbox-design.md` | Transactional outbox schema and relay design detail |
| `auth-flow.md` | Passkey and password authentication flow diagrams |
| `production-runtime.md` | Production process topology, config/fail-closed rules, health, backup/restore pointers, promotion gates (no cloud vendor) |
| `auth-abuse.md` | AUTH-A Identity abuse primitives: multi-dimensional Valkey rate limits, RiskDecision, HumanChallenge port (no production vendor) |
| `auth-session.md` | AUTH-B session lifecycle, idle Touch, security-center HTTP, passkey management, Step-Up, first-passkey bootstrap (Valkey, fail-closed) |
| `media-pipeline.md` | Listing-image upload, quarantine prefixes, WebP processing, worker/outbox, orphan cleanup, local MinIO, CDN attachment point |

*(Files above except `production-runtime.md` are planned; create them as Phase 0A/V1 tasks progress.)*

### `/docs/operations/` — Operator runbooks

| File | Purpose |
|---|---|
| `BACKUP-RESTORE.md` | PostgreSQL backup model, local restore drill, PITR production requirements (no cloud vendor in app code) |

### `/docs/security/` — Production security baseline

| File | Purpose |
|---|---|
| `HIGH-RISK-INVENTORY.md` | Implemented high-risk HTTP surfaces (no invented endpoints) |
| `AUTHORIZATION-MATRIX.md` | Endpoint × actor × role × ownership outcomes |
| `AI-DEVELOPMENT-POLICY.md` | AI/dependency rules; AI is not a security authority |
| `CI-SECURITY.md` | Scanner jobs, failure policy, supply-chain pins |

### `/docs/domains/` � Domain Specifications

One file per domain. Each file follows the domain spec template.

| File | Domain |
|---|---|
| `identity.md` | Identity / Auth |
| `users.md` | Users |
| `location.md` | Location / Geo |
| `trust.md` | Trust / Verification |
| `master-data.md` | Master Data / Categories |
| `listings.md` | Listings |
| `business-profiles.md` | Business Profiles |
| `services.md` | Services |
| `needs-matching.md` | Needs / Matching |
| `search-discovery.md` | Search / Discovery |
| `media.md` | Media |
| `favorites.md` | Favorites / Saved |
| `messaging.md` | Messaging |
| `notifications.md` | Notifications |
| `reviews.md` | Reviews |
| `moderation.md` | Moderation |
| `fraud.md` | Fraud |
| `case-engine.md` | Case Engine |
| `compliance-eids.md` | Compliance / E�DS |
| `billing.md` | Billing (deferred V2) |
| `transactions.md` | Transactions (deferred V2) |
| `payments.md` | Payments (deferred V2) |
| `delivery.md` | Delivery (deferred V2) |
| `disputes.md` | Disputes (deferred V2) |
| `analytics.md` | Analytics |
| `audit.md` | Audit |
| `feature-flags.md` | Feature Flags |
| `observability.md` | Observability |
| `integrations.md` | Integrations |

### `/docs/TASKS/` � Phase and Sprint Task Lists

| File | Purpose |
|---|---|
| `PHASE-0A.md` | Full Phase 0A task list with status tracking |
| `PHASE-V1.md` | V1 implementation task list (created at end of Phase 0A) |

### `/docs/MASTER-SPEC.md` � Master Specification

Aggregates product vision, domain tier structure, vertical slices, phases, architecture risks, open gates, and ADR list. See the file for full content.

---

## `/.kiro/` � Kiro Spec Files

| Path | Purpose |
|---|---|
| `.kiro/specs/{feature-name}/` | Kiro spec directory per feature |
| `.kiro/specs/{feature-name}/.config.kiro` | Spec configuration (type, workflow) |
| `.kiro/specs/{feature-name}/requirements.md` | Requirements document |
| `.kiro/specs/{feature-name}/design.md` | Design document (created in design phase) |
| `.kiro/specs/{feature-name}/tasks.md` | Implementation task list (created in tasks phase) |

---

## Application Directories

| Directory | Purpose | Status |
|---|---|---|
| `/backend/` | Go modular monolith — all backend source code | Initialized (Phase 0B: server, worker, archcheck, migrate, platform, identity). Production images: `backend/Dockerfile` (`COMMAND=server` or `worker`). |
| `/backend/cmd/` | Go binary entry points (server, worker, migrate) | `cmd/server`, `cmd/worker`, `cmd/archcheck`, `cmd/migrate` |
| `/backend/migrations/` | Numbered SQL migrations (golang-migrate; SQL files only) | `000001`–`000051` (PostGIS, identity core through public profiles, platform outbox, notifications deliveries, listings core, location listing geo, media assets/processing, master data core + category EİDS requirement, search listing projection, favorites listing saves, saved search filter snapshots, messaging conversations/messages, verified appointments/challenges/interactions, trust projection + verified-review counters, reviews core, review aggregates, moderation reports + staff queue/cases/actions/appeals, businesses profiles/services + match location/category, needs core, offers core, transactions core, payments core, deliveries, disputes, eids verifications) |
| `/backend/internal/` | Domain module packages (e.g. `internal/identity/`, `internal/listings/`) | includes `eids` (listing property/vehicle EİDS verification lifecycle; `eids.verifications`; owner HTTP start/get; Listings publish gate via contracts; no official provider, no person/e-Devlet identity, no admin bypass) plus `staffauth` (staff IAM realm separate from consumer Identity; RBAC + provider port; no vendor adapter yet) plus existing domains listed previously | (users/devices/sessions/passkeys/webauthn ceremonies, password fallback hashes, login identifiers, verification challenges; passkey/password HTTP session edge with Valkey auth rate limits; public profile identity fields with opaque `public_profile_id`, lazy-create, self `GET`/`PATCH /v1/profile/me`, public `GET /v1/public/profiles/{publicProfileId}`, `contracts.PublicProfileResolver` including server-side `ResolveUserIDByPublicID`; no avatars/bio); `notifications` (V1 intent contract, deliveries, outbox handler; no provider send); `listings` (listing lifecycle + controlled JSONB attributes; owner draft HTTP; public published detail HTTP including ready processed media URLs; outbox publish/update/archive for Search; EİDS publish eligibility via Master Data category policy + EİDS contract, not client `eligible`; public listing DTO optional `seller.publicProfileId` via Identity `PublicProfileResolver`, never owner `user_id`); `businesses` (business profile lifecycle draft/active/suspended/closed; one profile per owner user; owner HTTP create/mine/get/patch/activate/close; public active `GET /v1/public/businesses/{businessId}`; offered services draft/active/paused/closed; `contracts.Lookup` + `contracts.Catalog` + `contracts.CandidateDiscovery` + `contracts.OfferEligibility`; no booking/search/payments); `needs` (user-created need/request lifecycle draft/open/fulfilled/cancelled/expired; owner HTTP create/list/get/patch/open/fulfill/cancel; PostGIS point on `needs.needs`; optional published `categoryId` via Master Data `PublishedCategoryLookup`; requester from session; owner candidate list via Businesses contract; `contracts.Lookup` for Offers; no public discovery/payments); `offers` (Need-scoped provider offers submitted/withdrawn/accepted/rejected/expired; `offers.offers`; session provider create/mine/withdraw; requester list/accept/reject; eligibility via Businesses `OfferEligibility`; `contracts.Lookup` for Transactions; Need stays open on accept; no payment/notification/public Need marketplace); `transactions` (commercial agreement from accepted Offer; `transactions.transactions`; pending/active/completed/cancelled; requester-triggered idempotent create; participant HTTP; UUID refs via Offers/Needs contracts; `contracts.Lookup` for Payments; no escrow/delivery/disputes); `payments` (payment intent lifecycle pending/authorized/captured/failed/cancelled; `payments.payments`; one intent per Transaction; amount/currency from Transaction agreed price via `transactions/contracts.Lookup`; no PSP adapter, card data, escrow, refunds, or Transaction completion); `location` (listing geography Point 4326; catalog ID reference only; listing-changed outbox; no HTTP/search/H3); `media` (listing-image metadata + upload lifecycle; object-storage port; signed PUT + public GET delivery; processing; public listing-media contract); `masterdata` (categories, versioned schemas, attributes/options, TR/EN/RU/AR labels; form-definition resolve; public catalog HTTP; no write/admin/taxonomy seed); `search` (derived `listing_documents` projection + FTS/PostGIS; outbox rebuild; public Search HTTP; no OpenSearch/H3); `favorites` (session-scoped listing saves; UUID refs; public eligibility via Listings `SearchSource`); `savedsearch` (session-scoped saved Search filters; UUID refs; no Identity FK; no alerts); `messaging` (listing-scoped conversations + text messages + per-user read state; UUID refs; public listing eligibility via Listings `Ownership`; no realtime); `verified` (listing-scoped appointments + hash-only otp/qr challenges + listing_inspection interactions; appointment list/detail DTO includes nullable `verifiedInteractionId` from Verified storage; UUID refs; provider from Listings `Ownership`; `verified/contracts` interaction-completed DTO + interaction read surface; no PDF/NFC); `trust` (derived `user_profiles` + `user_verified_history`; outbox ingest of `verified.interaction.completed` v1 and `reviews.verified.created` v1; authenticated `GET /v1/trust/me` with interaction counts plus visible verified-review counters/provider-service average; public `GET /v1/public/profiles/{publicProfileId}/trust` via Identity public-profile resolve; trust level from interaction count only; no listing accuracy on user Trust; no `/v1/public/trust/{userId}` / scoring); `reviews` (one verified `listing_inspection` review per interaction; listing accuracy and provider service ratings kept separate; UUID refs; authenticated create/mine/eligibility HTTP; public published-listing review bodies via Listings `Ownership`; `reviews.verified.created` v1 outbox without body; no reviewer identity/media); `reviewaggregates` (derived `listing_accuracy` + `provider_service`; outbox ingest of `reviews.verified.created` v1 with processed-event idempotency; public listing summary + authenticated self provider summary; no public review bodies); `moderation` (user-submitted `reports` for listing/public_profile; session reporter; fixed V1 reason codes; authenticated create/mine HTTP; staff queue list/get/status transitions + optional staff note in domain/store; staff HTTP not production-wired until Staff IAM; no cases/Trust/punishment) |
| `/backend/internal/platform/` | Shared platform primitives (config, health, db, cache, outbox, httpx, observability) | `config` (including `APP_ENV` fail-closed production gates), `health`, `db`, `cache`, `outbox`, `httpx` (trusted proxy + request id), `observability` (JSON logs, no-op metrics/tracing) |
| `/backend/internal/infrastructure/` | Provider adapter implementations (storage, notifications, maps, payments, EİDS) | `notifications` email/SMS adapters (no vendor SDK; `disabled`/`external` modes); `storage` S3-compatible adapter (AWS SDK v2; signed PUT/GET; optional public base URL; custom endpoint/path-style; no production vendor/CDN); `eids` unconfigured gateway (unavailable, never verified; no official SDK) |
| `/web/apps/consumer/` | Public consumer Next.js app (konumlu.com) | Initialized (Phase 0B-32 scaffold + 0B-34 `/giris` + 0B-35 `/kayit` + 0B-36 `/sifre-sifirla` + 0B-45 `/ilan-ver` owner draft create + 0B-49 Master Data category/form + 0B-55 `/ara` public search/list + 0B-56 `/ilan/[listingId]` public detail + 0B-57 `/ara` MapLibre map/list sync + 0B-59 `/ilan-ver` owner publish + 0B-60 `/favoriler` listing favorites + 0B-61 `/kayitli-aramalar` saved searches + 0B-62 `/mesajlar` listing messaging + 0B-64 `/randevular` Konumlu Verified appointments + 0B-66 `/guven-pasaportum` session Trust passport + 0B-68 `/degerlendirme/[verifiedInteractionId]` + `/degerlendirmelerim` verified reviews + 0B-68A `/randevular` review entry from appointment `verifiedInteractionId` + 0B-70 listing accuracy + self provider-service review summaries + 0B-71 public verified review bodies on listing detail + 0B-73 `/guven-pasaportum` verified-review signals from `GET /v1/trust/me` only + 0B-75 `/profil/[publicProfileId]` public Identity + public Güven Pasaportu + 0B-76 `/ilan/[listingId]` Satıcı link to `/profil/{publicProfileId}`; listing EİDS HTTP exists on owner API, consumer publish UI is unchanged) |
| `/web/apps/admin/` | Management Center Next.js app (admin.konumlu.com; ADR-013) | Initialized (Phase 0B-33: App Router scaffold, shell + dashboard placeholder; no Staff IAM) |
| `/scripts/` | Operator scripts | `scripts/ops/backup-restore/` — local PostgreSQL dump/restore verification SQL (not application runtime) |

## Planned Application Directories (not yet initialized)

These directories are planned for V1. Architecture gates G-01 through G-09 are resolved (G-06 deferred to V1.5).

| Directory | Purpose |
|---|---|
| `/mobile/` | React Native mobile application |
| `/shared/` | Shared TypeScript types and utilities between web and mobile |
| `/infra/` | Infrastructure-as-code (Terraform, Pulumi — TBD) |

---

## Naming Conventions

| Context | Convention | Example |
|---|---|---|
| Go packages | lowercase, no hyphens | `internal/identity` |
| Go files | snake_case | `user_repository.go` |
| TypeScript files | PascalCase for components, camelCase for utilities | `ListingCard.tsx`, `formatDate.ts` |
| Documentation files | SCREAMING-KEBAB for root files, kebab-case for docs | `ARCHITECTURE.md`, `domain-spec.md` |
| ADR files | `ADR-NNN-short-title.md` | `ADR-001-go-monolith-structure.md` |
| Domain spec files | `{domain-name}.md` | `needs-matching.md` |
| Environment variables | SCREAMING_SNAKE_CASE | `DATABASE_URL`, `VALKEY_URL` |
