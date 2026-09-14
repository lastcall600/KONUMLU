# CURRENT-STATE.md

> This file is the live status record of KONUMLU. It must be updated at the start and end of every phase. Do not let it drift.

---

## Active Phase

**Phase 0B — Backend Scaffold**

**Status: IN PROGRESS**

**Goal:** Create a runnable Go backend skeleton (stdlib HTTP, health check, env config) before domain modules.

**Constraint:** No third-party web framework. Local PostgreSQL/PostGIS and Valkey via Docker Compose are in scope. Password HTTP login is in place as Argon2id fallback; OTP send is not started. Remember Me is not a current requirement. Password reset HTTP is in place (challenge + hash-only reset proof + session revoke/`session_epoch`; no auto-login). Browser session resolve uses a disposable Valkey hot cache keyed by token hash, with PostgreSQL remaining authoritative. Authenticated passkey registration HTTP binds ceremonies to the session user. Signup verification start/finish HTTP issues a short-lived hash-only signup proof; `POST /v1/auth/signup/complete` creates the Identity user from that proof. Public and authenticated browser auth POSTs use Identity multi-dimensional Valkey abuse limits (fail-closed); session cache still fails open to PostgreSQL. Verification challenges persist hash-only secrets; worker/server load an env-injected AES-256 material keyring. Platform transactional outbox (`platform.outbox_events`) and `cmd/worker` relay are in place; Notifications intent handling upserts deliveries and invokes DeliveryService when a sender is wired. Email/SMS channel modes are `disabled` (default) or `external`; external fails worker startup unless a vendor adapter is registered (vendors not selected; missing/disabled channel is retryable, never a no-op send).

**Progress:** 0B-01 through 0B-05, 0B-07, 0B-08, 0B-09, 0B-10, 0B-12, 0B-13, 0B-14, 0B-15, 0B-16, 0B-17, 0B-18, 0B-19, 0B-20, 0B-24, 0B-25, 0B-26, 0B-27, 0B-28, 0B-29, 0B-30, 0B-31, 0B-32, 0B-33, 0B-34, 0B-35, 0B-36, 0B-37, 0B-38, 0B-39, 0B-40, 0B-41, 0B-42, 0B-43, 0B-44, 0B-45, 0B-46, 0B-48, 0B-49, 0B-51, 0B-53, 0B-54, 0B-55, 0B-56, 0B-57, 0B-59, 0B-60, 0B-61, 0B-62, 0B-63, 0B-64, 0B-65, 0B-66, 0B-67, 0B-68, 0B-68A, 0B-69, 0B-70, 0B-71, 0B-72, 0B-73, 0B-74, 0B-75, 0B-76, 0B-77, 0B-78, 0B-79, 0B-80, 0B-81, 0B-82, 0B-83, 0B-84, 0B-85, 0B-86, 0B-87, 0B-88, 0B-89, 0B-90, 0B-91, 0B-92, 0B-93, 0B-94, 0B-95, 0B-96, 0B-97, 0B-98, 0B-99, 0B-100, 0B-101, 0B-102, 0B-103, 0B-104, 0B-105, 0B-106, and 0B-108 complete (scaffold through Need status gate on Transaction lifecycle). NOTIFY-A (0C-NOTIFY-A) remains the frozen catalog/000052 foundation. NOTIFY-B local (0C-NOTIFY-B) marketplace producers, provider-neutral `channel_deliveries` dispatcher, and Identity JIT contact resolve are **READY_FOR_REVIEW** (no 000053, no vendors). Phase 0A architecture gate (0A-32): PASS. V1 blocking gates: none.

---

## What Has Been Done

### Phase 0A

**Group 1 — Root orientation files** ✅ Complete
- [x] `/README.md` — repository orientation
- [x] `/CURRENT-STATE.md` — this file
- [x] `/DECISIONS.md` — 20 frozen decisions (D-001–D-020); O-001–O-005, O-007–O-009 resolved (architecture); O-006 OPEN/DEFERRED V1.5
- [x] `/ARCHITECTURE.md` — authoritative architecture specification (all 20 sections + locale/RTL subsection)
- [x] `/PROJECT-MAP.md` — repository layout guide
- [x] `/AGENTS.md` — AI agent operating norms

**Group 2 — Documentation infrastructure** ✅ Complete
- [x] `/docs/MASTER-SPEC.md` — full product scope, slices, phases
- [x] `/docs/ADR/INDEX.md` — ADR index (accepted / pending / superseded synchronized)
- [x] `/docs/TASKS/PHASE-0A.md` — phase task list
- [x] `/docs/TASKS/`, `/docs/architecture/`, `/docs/domains/` — directory structure
- [x] `.kiro/specs/konumlu-architecture-foundation/requirements.md` — spec requirements document

**Group 3 — Architecture Decision Records (partial)**
- [x] ADR-001 / 0A-10 — Accepted; G-01 / O-001 resolved
- [x] ADR-002 / 0A-11 — Accepted; G-02 / O-002 resolved
- [x] ADR-003 / 0A-12 — Accepted; G-03 / O-003 resolved
- [x] ADR-004 / 0A-13 — Accepted
- [x] ADR-007 / 0A-14 — Accepted
- [x] ADR-009 / 0A-15 — Accepted; G-08 architecture resolved
- [x] ADR-005 / 0A-17 — Accepted; G-04 / O-004 resolved
- [x] ADR-011 / 0A-16 — Accepted; G-05 resolved
- [x] ADR-012 / 0A-21 — Accepted; G-07 / O-007 resolved
- [x] ADR-013 / 0A-33 — Accepted; G-09 / O-009 resolved (separate Next.js app, `web/apps/admin`)
- [x] ADR-014 / 0A-34 — SUPERSEDED / NOT NEEDED (duplicates ADR-009)
- [x] 0A-32 — Architecture gate review **PASS** (V1 blocking gates: none; G-06 deferred V1.5)

### Phase 0B

- [x] 0B-01 — Minimal Go backend scaffold (`backend/go.mod`, `cmd/server`, `internal/platform/config`, `internal/platform/health`, GET `/healthz`)
- [x] 0B-02 — Go architecture boundary check (`backend/cmd/archcheck`, `make arch-check`)
- [x] 0B-03 — PostgreSQL/PostGIS local foundation (`docker-compose.yml`) + `internal/platform/db` (pgx pool) + GET `/readyz`
- [x] 0B-04 — SQL migration foundation (`golang-migrate/migrate` v4, `cmd/migrate`, `backend/migrations/`); ADR-001 OI-002 resolved
- [x] 0B-05 — Identity domain foundation (`internal/identity`, schema `identity`, users/devices/sessions; no credentials/auth endpoints)
- [x] 0B-07 — Identity passkey credential foundation (`identity.passkey_credentials`; no WebAuthn ceremonies/HTTP)
- [x] 0B-08 — WebAuthn ceremony foundation (`go-webauthn`, `identity.webauthn_ceremonies`; no HTTP auth)
- [x] 0B-09 — Passkey registration ceremony (Begin/Finish registration; UV required; no HTTP auth)
- [x] 0B-10 — Passkey authentication ceremony (Begin/Finish discoverable login; UV required; no session issuance/HTTP)
- [x] 0B-12 — Browser session HTTP edge (passkey login begin/finish, session cookie, CSRF, GET session, logout)
- [x] 0B-13 — Identity login identifier foundation (`identity.user_identifiers`; email/phone canonical lookup; no signup/login HTTP)
- [x] 0B-14 — Browser password fallback login HTTP (`POST /v1/auth/password/login`; generic 401; same session cookies as passkey login)
- [x] 0B-15 — Valkey platform foundation (`docker-compose` Valkey, `internal/platform/cache`, `/readyz` ping when `VALKEY_URL` is set; no session cache or rate limiting)
- [x] 0B-16 — Auth HTTP rate limiting (Valkey Increment; per-IP on passkey/password login POSTs; per-user UUID on password login after identifier resolve)
- [x] AUTH-A local (0C-AUTH-A) — Identity abuse primitives: multi-dimensional Valkey rate limits on public auth (including previously uncovered signup complete, reset verify/complete, passkey enrollment), hashed subjects, fail-closed abuse checks, internal `RiskDecision`, HumanChallenge port with fake/unconfigured only (no production vendor). Trust untouched. See `docs/architecture/auth-abuse.md`.
- [x] AUTH-B local (0C-AUTH-B) — Session rotation on login/signup/step-up (predecessor only), idle Touch on Resolve, session list/revoke/revoke-others HTTP, passkey list/remove with last-factor 409, passkey add/remove gated by server-side Step-Up (passkey assertion; Valkey TTL; fail-closed). First-passkey bootstrap: password re-auth (`passkey_add` only) or passwordless signup-session grant; consumed after first enroll. Device binding deferred.
- [x] AUTH-C local (0C-AUTH-C) — Durable `identity.auth.security` v1 outbox events (safe metadata; unknown-account failures omit identifiers); HumanChallenge production boundary remains unwired (`unconfigured` fail-closed; no vendor selected); email/SMS remain unwired (`disabled` never marked sent; `external` requires adapter). Load/abuse/provider-failure proofs in Identity tests. Classification: **PRODUCTION_CODE_READY_PROVIDER_BLOCKED**. Runbook: `docs/operations/AUTH-SECURITY.md`. Not full 0C-AUTH production-ready until vendors are onboarded.
- [x] NOTIFY-A local (0C-NOTIFY-A) — Provider-neutral catalog + per-channel preference/consent resolver; approved additive schema `000052_notifications_policy_core` (unchanged after approval); PostgreSQL stores; consumer preference/consent/inbox HTTP (session CSRF/Origin); AUTH-C selected events materialize to `notifications.intents` / `channel_deliveries` / `inbox_items`; legacy verification `notifications.intent` v1 and moderation warning path unchanged. Classification: **NOTIFY_A_APPLICATION_READY_FOR_REVIEW** (not vendor-ready). See `docs/architecture/notifications-policy.md`. No vendor selected. AUTH/Trust/Staff IAM unchanged.
- [x] NOTIFY-B local (0C-NOTIFY-B) — Marketplace producers (messaging/offers/transactions/deliveries/disputes) via transactional domain outbox → worker `MaterializeFanout`; provider-neutral `Dispatcher` for `channel_deliveries` (SKIP LOCKED + `updated_at` processing recovery; no fake success; unconfigured providers do not claim); Identity JIT contact resolve (in-memory, redacted); push registration deferred (no 000053); saved-search match deferred; moderation warning left legacy. Classification: **NOTIFY_B_APPLICATION_READY_FOR_REVIEW**. Runbook: `docs/operations/NOTIFICATIONS.md`.
- [x] 0B-17 — Identity verification challenge foundation (`identity.verification_challenges`; email token / phone OTP hash-only; signup purpose; Valkey issuance throttle by dest SHA-256 + IP; no HTTP/send/signup)
- [x] 0B-18 — Transactional outbox foundation (`platform.outbox_events`, `internal/platform/outbox`; SKIP LOCKED claim + lease; no worker/providers)
- [x] 0B-19 — Outbox worker relay (`cmd/worker`, handler registry + claim/dispatch loop; empty registry; no Notifications/DLQ)
- [x] 0B-20 — Notifications domain foundation (`internal/notifications` + `contracts`, `notifications.deliveries`, intent outbox handler in `cmd/worker`; no email/SMS send)
- [x] 0B-24 — Notifications delivery worker wiring (intent handler invokes DeliveryService; optional email/SMS senders; no vendor; no fake production providers)
- [x] 0B-25 — Verification material keyring wiring (env-injected AES-256 keys; worker Identity resolver composed; no email/SMS vendor)
- [x] 0B-26 — Signup verification HTTP (`POST /v1/auth/signup/verification/start|finish`; generic existence response; hash-only `identity.signup_proofs`; no user create)
- [x] 0B-27 — Account creation from signup proof (`POST /v1/auth/signup/complete`; one transaction: consume proof + user + verified identifier + optional Argon2id password; then Identity session cookies)
- [x] 0B-28 — Authenticated passkey registration HTTP (`POST /v1/auth/passkey/register/begin|finish`; session + origin + CSRF; ceremony bound to session user)
- [x] 0B-29 — Identity session hot cache (Valkey keyed by session token SHA-256; PostgreSQL SoT; miss rehydrate; fail-open to PostgreSQL when Valkey is down)
- [x] 0B-31 — Password reset / recovery foundation (`POST /v1/auth/password/reset/start|verify|complete`; hash-only `identity.password_reset_proofs`; session revoke + `session_epoch`; no auto-login)
- [x] 0B-32 — Consumer web scaffold (`web/apps/consumer`; Next.js App Router, TypeScript strict; placeholder home only; no auth UI / listings / admin)
- [x] 0B-33 — Admin web scaffold (`web/apps/admin`; Next.js App Router, TypeScript strict; Management Center shell + dashboard placeholder; no Staff IAM / 360 / domain APIs)
- [x] 0B-34 — Consumer login UI (`/giris`; password + passkey against existing Identity HTTP; cookie session + CSRF logout; no signup/reset/profile)
- [x] 0B-35 — Consumer signup UI (`/kayit`; identifier → verification → complete against existing Identity HTTP; in-memory challenge/proof only; optional password fallback; cookie session; no passkey registration during signup)
- [x] 0B-36 — Consumer password reset UI (`/sifre-sifirla`; identifier → verification → new password against existing Identity HTTP; in-memory challenge/resetProof only; no auto-login)
- [x] 0B-37 — Listings domain foundation (`internal/listings`; `listings.listings`; draft/ready/archive/publish with caller-provided eligibility; no HTTP/media/geo/EİDS)
- [x] 0B-38 — Location domain foundation (`internal/location`; `location.listing_locations` PostGIS geography Point 4326; one point per listing; catalog ID reference only; no HTTP/search/H3)
- [x] 0B-39 — Media domain foundation (`internal/media`; `media.assets` listing-image metadata + upload lifecycle; object-storage port only; no S3/MinIO/HTTP/processing)
- [x] 0B-40 — S3-compatible storage adapter (`internal/infrastructure/storage`; AWS SDK v2 S3 client; signed PUT; MinIO/custom endpoint; no HTTP/processing/vendor lock-in)
- [x] 0B-41 — Media listing-image HTTP upload initiate/confirm (`internal/media/httpapi`; session owner; signed PUT target; confirm is uploaded/untrusted only; object storage disabled → 503)
- [x] 0B-43 — Listing ownership contracts + draft orchestration (`listings/contracts`, Location write scope, Media attach bind, shared PostgreSQL Tx via context; no HTTP/search/public URLs)
- [x] 0B-44 — Listings HTTP owner draft management (`internal/listings/httpapi`; create/get/patch/location/media/ready/archive; session owner; no public detail/publish/EİDS)
- [x] 0B-45 — Consumer listing create UI (`/ilan-ver`; session-gated draft wizard + media signed PUT; no map/search/publish/public URLs)
- [x] 0B-46 — Master Data core schema (`internal/masterdata`; `master_data` categories/schemas/attributes/options + labels; publish lifecycle; form-definition resolve; no HTTP/taxonomy seed/Search)
- [x] 0B-48 — Master Data public read HTTP (`internal/masterdata/httpapi`; published categories + localized form by current/exact schema version; no write/admin/seed/Search)
- [x] 0B-49 — Consumer dynamic listing form (`/ilan-ver`; published category tree + localized form fields; attributes JSON from form state; no admin editor/seed/search/map/public detail)
- [x] 0B-51 — Search projection foundation (`internal/search`; `search.listing_documents` PostgreSQL/PostGIS + FTS; outbox ingest; no Search HTTP/OpenSearch/H3)
- [x] 0B-53 — Public listing detail HTTP (`GET /v1/public/listings/{listingId}`; published-only; Location contract read; no media URLs)
- [x] 0B-54 — Public processed media delivery (`media/contracts.PublicListingMedia`; signed GET or `OBJECT_STORAGE_PUBLIC_BASE_URL`; `GET /v1/public/listings/{listingId}` `media[]`; storage disabled → empty `media`; store/delivery failure → 503)
- [x] 0B-55 — Consumer public search/list UI (`/ara`; `GET /v1/search/listings` + published categories; query-string filters; opaque `nextCursor` load-more; no MapLibre/detail/favorites)
- [x] 0B-56 — Consumer public listing detail UI (`/ilan/[listingId]`; `GET /v1/public/listings/{listingId}` + exact Master Data form; processed media URLs; no map/contact/favorites)
- [x] 0B-57 — Consumer MapLibre map/list sync on `/ara` (markers from Search coordinates; explicit “Bu alanda ara” viewport; URL north/south/east/west; no clustering/geocoder/geolocation)
- [x] 0B-59 — Consumer owner publish action (`/ilan-ver` manage stage; `POST /v1/listings/{id}/publish`; no Search poll/EİDS/Konumlu Verified)
- [x] 0B-60 — Favorites foundation (`internal/favorites`; `favorites.listing_favorites`; authenticated save/unsave/list; public-listing eligibility via Listings contract; consumer `/favoriler` + card/detail action; no saved search/notifications)
- [x] 0B-61 — Saved Search foundation (`internal/savedsearch`; `saved_search.saved_searches`; authenticated save/list/get/delete of current Search filters; consumer `/ara` save + `/kayitli-aramalar`; no alerts/notifications)
- [x] 0B-62 — Messaging foundation (`internal/messaging`; listing-scoped conversations + text messages + per-user read state; authenticated HTTP; consumer `/mesajlar` + listing “Mesaj Gönder”; no realtime/media/notifications)
- [x] 0B-63 — Konumlu Verified interaction core (`internal/verified`; appointments + hash-only verification challenges + listing_inspection interactions; authenticated HTTP; `verified.interaction.completed` outbox; no reviews/trust/PDF/NFC/IMEI)
- [x] 0B-64 — Consumer Konumlu Verified UI (`/ilan/[listingId]` appointment request; authenticated `/randevular`; provider accept/reject/cancel/start; requester cancel + out-of-band token finish; opaque token in memory only; no reviews/trust/QR bitmap/numeric OTP)
- [x] 0B-65 — Trust projection foundation (`internal/trust`; `trust.user_profiles` + `user_verified_history`; `verified.interaction.completed` v1 ingest with processed-event idempotency; `GET /v1/trust/me`; transparent new/verified/established levels from counts only; no public passport UI / scoring)
- [x] 0B-66 — Consumer Güven Pasaportum UI (`/guven-pasaportum`; session `GET /v1/trust/me`; count-based level labels; no public profile / score / badges)
- [x] 0B-67 — Verified reviews foundation (`internal/reviews`; one review per completed `listing_inspection`; listing accuracy and provider service 1..5 kept separate; authenticated create/mine/eligibility HTTP; `reviews.verified.created` v1 outbox without body; no public reviews / aggregates / media)
- [x] 0B-68 — Consumer verified-review UI (`/randevular` requester eligibility entry; `/degerlendirme/[verifiedInteractionId]` create form; `/degerlendirmelerim` mine list; cookie session + CSRF POST; no public reviews / aggregates / Trust ingest / media)
- [x] 0B-68A — Appointment list/detail DTO `verifiedInteractionId` from Verified storage (completed + interaction only; participant-only; consumer `/randevular` uses it; no Reviews fallback)
- [x] 0B-69 — Review aggregates foundation (`internal/reviewaggregates`; listing accuracy + provider service projections from `reviews.verified.created` v1; processed-event idempotency; public listing summary + authenticated self provider summary; no public review bodies / Trust ingest)
- [x] 0B-70 — Consumer review-summary UI (`/ilan/[listingId]` listing accuracy; `/guven-pasaportum` self provider service; public + session summary HTTP; no public review bodies / Trust mix-in)
- [x] 0B-72 — Trust ingest of verified review signals (`internal/trust`; `reviews.verified.created` v1 via Reviews contracts; derived review counters/average on `trust.user_profiles`; processed-event idempotency shared with interaction events; `GET /v1/trust/me` public-safe review fields; trust level still interaction-count only; no listing accuracy mix-in / public passport / scoring)
- [x] 0B-73 — Consumer Güven Pasaportum review signals UI (`/guven-pasaportum`; session `GET /v1/trust/me` verified-review counters + provider-service average; separate from interaction-count trust level; no duplicate `GET /v1/review-summary/me`; no public passport / score / badges)
- [x] 0B-74 — Public identity profile foundation (`identity.public_profiles`; opaque `public_profile_id`; lazy-create on self/contract user resolve; `GET`/`PATCH /v1/profile/me`; `GET /v1/public/profiles/{publicProfileId}`; Identity `contracts.PublicProfileResolver`; no avatars/bio/public trust)
- [x] 0B-75 — Public Güven Pasaportu (`GET /v1/public/profiles/{publicProfileId}/trust`; Identity public-profile resolve then Trust projection; smaller public DTO; consumer `/profil/[publicProfileId]`; no `/v1/public/trust/{userId}`)
- [x] 0B-76 — Listing seller public profile link (`GET /v1/public/listings/{listingId}` optional `seller.publicProfileId` + nullable `displayName` via Identity `PublicProfileResolver.ResolveByUserID`; omit seller if resolve fails; no owner `user_id`; consumer `/ilan/[listingId]` Satıcı section links to `/profil/{publicProfileId}`)
- [x] 0B-77 — Verified QR challenge bitmap (`method=qr` start DTO `qrPayload` = opaque token; client-side `qrcode.react` SVG on `/randevular`; hash-only at rest; no QR image persistence; OTP start unchanged)
- [x] 0B-78 — Verified numeric OTP UX (`method=otp` 6-digit crypto/rand code; hash-only at rest; raw OTP once on start; consumer `/randevular` prominent code + numeric finish input; QR unchanged)
- [x] 0B-79 — Public verified reviews cursor pagination (`GET /v1/public/listings/{listingId}/reviews`; opaque `nextCursor`; `created_at DESC, id DESC`; default/max 20; consumer listing detail load-more; no reviewer identity)
- [x] 0B-80 — Verified transaction/delivery verification foundation (`listing_inspection` unchanged; `transaction`/`delivery` flows + same QR/OTP hash-only challenges; `verified.interaction.completed` v1 with type; no payments/logistics/Trust scoring/reviews)
- [x] 0B-81 — Trust interaction-type semantics (`listing_inspection` still drives count/level; `transaction`/`delivery` ingested + history only; unknown types rejected without scoring; no new DTO counters / weights / ranking)
- [x] 0B-82 — Consumer transaction/delivery verification UI (listing-owner start from authenticated listing conversation; QR/OTP reuse; requester finish by flow id + secret; completed via finish or GET interaction; no Trust DTO/badge/review changes)
- [x] 0B-83 — Participant-safe verification-flow list/read (`GET /v1/verified/verification-flows`, `GET /v1/verified/verification-flows/{flowId}`; no user UUIDs or secrets; consumer conversation rediscovers flows after reload)
- [x] 0B-84 — Moderation report foundation (`internal/moderation`; `moderation.reports`; authenticated `POST /v1/moderation/reports` + `GET /v1/moderation/reports/mine`; listing/public_profile targets via contracts; fixed V1 reason codes; no staff queue/cases/Trust/punishment)
- [x] 0B-85 — Staff moderation queue foundation (list/get/status transitions on reports; optional staff note; no Case/Trust/takedown; staff HTTP not production-wired — Staff IAM missing)
- [x] 0B-86 — Moderation case foundation (`moderation.cases` + `case_reports` + append-only `case_history`; create/read/list/update; report attach without copying report bodies; no production staff routes / appeals / takedown)
- [x] 0B-87 — Moderation case evidence foundation (`moderation.case_evidence` append-only; staff_note/external_reference/internal_reference/snapshot_reference; `evidence_added` history atomic with create; staff HTTP not production-wired)
- [x] 0B-88 — Moderation action / enforcement foundation (`moderation.case_actions`; proposed/approved/executed/cancelled; listing/public_profile; no_action/warning/restrict/suspend/remove; history `action_*` atomic; future `contracts.Enforcer` unwired; staff HTTP not production-wired)
- [x] 0B-89 — Moderation appeals foundation (`moderation.appeals`; subject-owned appeal of executed actions; submitted/under_review/accepted/rejected/withdrawn; one active appeal per action/user; 14-day window; history `appeal_*` atomic; accept does not reverse enforcement; staff HTTP not production-wired)
- [x] 0B-90 — Listing moderation enforcement (`listings.listings.moderation_state` none/restricted/removed; Listings contract apply; restrict/remove hide public+search; no_action/warning record-only; listing `suspend` unsupported; executed only after Listings success; search outbox rebuild; staff HTTP not production-wired)
- [x] 0B-91 — Listing appeal restoration (accepted listing restrict/remove appeal calls Listings `ClearModerationState` first; owner status unchanged; `appeal_restoration_applied` history; failed Listings clear does not accept; public_profile restore started in 0B-92)
- [x] 0B-92 — Public profile moderation enforcement (`identity.public_profiles.moderation_state` none/restricted/removed; Identity contract apply; restrict/remove hide public profile + public Trust route; no_action/warning record-only; public_profile `suspend` unsupported; executed only after Identity success; accepted restrict/remove appeal clears Identity state first; no login/session/account change)
- [x] 0B-93 — Moderation warning notification foundation (`notifications.moderation.warning` v1 outbox; in-app delivery + `notifications.warning_intents`; listing/public_profile owner resolved via contracts; no email/SMS vendor; warning does not change listing/profile/Trust; execute only after intent enqueue)
- [x] 0B-94 — Business Profiles foundation (`internal/businesses`; `businesses.profiles`; draft/active/suspended/closed; one profile per user; owner HTTP + public active detail; no services/workspace/verification/search)
- [x] 0B-95 — Business Services foundation (`internal/businesses`; `businesses.services`; draft/active/paused/closed catalog per business; owner HTTP + public active-only; no booking/matching/search/payments)
- [x] 0B-96 — Need / Request foundation (`internal/needs`; `needs.needs`; draft/open/fulfilled/cancelled/expired; session owner HTTP; PostGIS point; optional published category via Master Data contract; no matching/offers/public discovery/payments)
- [x] 0B-97 — Need → service candidate matching foundation (`GET /v1/needs/{needId}/candidates`; Businesses candidate-discovery contract; optional business profile PostGIS location; optional OfferedService categoryId; query-time derived candidates; no offers/notifications/assignment)
- [x] 0B-98 — Need offer foundation (`internal/offers`; `offers.offers`; submitted/withdrawn/accepted/rejected/expired; requester list/accept/reject + provider create/mine/withdraw; Businesses `OfferEligibility` + Needs `Lookup`; Need stays open on accept; no transaction/payment/notification/public Need marketplace)
- [x] 0B-99 — Transaction foundation (`internal/transactions`; `transactions.transactions`; pending/active/completed/cancelled from accepted Offer; requester-triggered idempotent create; Offers `Lookup` + Needs `Lookup`; no payment/escrow/delivery/disputes/Need fulfillment/notifications)
- [x] 0B-100 — Transaction completion → Need fulfillment coordination (`transactions.transaction.completed` v1 outbox + Needs `TransactionFulfillment` contract; worker handler; no payment/escrow/delivery)
- [x] 0B-101 — Payment foundation (`internal/payments`; `payments.payments`; pending/authorized/captured/failed/cancelled intent for a priced Transaction; Transactions `Lookup` contract; no PSP/card data/escrow/refunds/Transaction completion)
- [x] 0B-102 — Delivery foundation (`internal/deliveries`; `deliveries.deliveries`; participant-requested logistics state for an eligible Transaction; Transactions `Lookup` contract; no courier/GPS/address/PoD/payment/Trust/Verified wiring)
- [x] 0B-103 — Dispute foundation (`internal/disputes`; `disputes.disputes` + append-only `disputes.evidence`; participant-opened commercial dispute for an eligible Transaction within a 14-day window; Transactions Lookup + optional Deliveries read; no refunds/chargebacks/payment reversal/Trust/Moderation/outbox)
- [x] 0B-104 — Staff IAM / authorization foundation (`internal/staffauth`; separate staff realm from consumer Identity; RBAC roles/permissions; `StaffIdentityProvider` port; production fail-closed without a registered IdP adapter; Moderation/Dispute staff HTTP registered only when a provider is wired; no fake staff login)
- [x] 0B-105 — EİDS production integration foundation (`internal/eids`; listing property vs vehicle verification lifecycle; Master Data category `eids_requirement` none/property/vehicle; Listings publish eligibility via contract; owner HTTP start/get; unconfigured provider is unavailable not verified; no official adapter, no person identity, no admin bypass)
- [x] 0B-106 — Production runtime / infrastructure foundation (process topology without cloud vendor/K8s/Kafka; `APP_ENV` modes; production fail-closed config; liveness vs readiness; pool/outbox/worker runtime config; trusted proxies; structured logs + request id; Dockerfiles; backup/restore expectations documented)
- [x] 0B-108 — Need status gate on Transaction lifecycle (create/start require Need `open`; complete allows `open`/`fulfilled` and rejects cancelled/expired/draft; Deliveries provider progression refuses cancelled Transaction; fulfillment remains outbox/worker)
- [x] Production hardening sprint 2 (local) — PostgreSQL logical backup → isolated restore drill + operator runbook (`docs/operations/BACKUP-RESTORE.md`). Production WAL/PITR hosting still a documented gap.
- [x] Production hardening sprint 3 (local) — listing-image presigned PUT → private MinIO quarantine → confirm/outbox → worker WebP processing → processed delivery; orphan sweeper; no media schema migration. Live `cmd/worker` drain proven (claim SQL `RETURNING e.id`; empty poll is not `claim_failed`).

---

## What Is In Progress

**Phase 0B** — backend scaffold in progress (0B-01 through 0B-108 complete as listed above). NOTIFY-A remains frozen (`000052`). NOTIFY-B producers + dispatcher are **READY_FOR_REVIEW** (providers, push endpoints, saved-search match, and moderation cutover remain later).

**Phase 0A remaining docs (not architecture blockers):** ADR-006, ADR-008, ADR-010; Group 4 domain specs not started.

**Group 5 — Architecture gate review (0A-32):** ✅ PASS — V1 blocking gates none; G-06 deferred V1.5 / non-blocking

---

## What Is Deferred

### Deferred to V1 implementation (after Phase 0A gates resolved)

- Remaining application initialization (React Native app; consumer and admin web are scaffolded)
- Further Go backend (auth endpoints, passkeys/passwords, other domains)
- Remaining domain tables (Identity credentials, Users profile, marketplace, …)
- CI/CD pipeline
- Domain implementations

### Deferred to V1.5

- Fraud: advanced scoring, pattern detection, automation, graph analysis
- Case Engine: advanced automation, SLA orchestration, appeals, complex routing
- Management Center: advanced automation, fraud graph tooling, ML-assisted moderation
- Corporate Workspace (pending O-006 resolution)
- Moderation: automated signal processing, full Case Engine integration

### Deferred to V2

- Finance / Billing
- Payments / escrow (payment intent records exist as 0B-101; no PSP integration, card storage, escrow, or real money movement)
- Delivery (logistics-state records exist as 0B-102; no courier providers, GPS, addresses, or proof-of-delivery)
- Disputes
- Analytics (full BI)

### Permanently deferred (architectural non-starters without measurement-backed ADR)

- gRPC inside the monolith
- Event sourcing
- Microservices decomposition before scale demands it
- OpenSearch or H3 without measured need (D-018, D-019)
- Any AI system serving as sole authorization or EİDS verification authority (D-011)

---

## Active Domain Set (Phase 0A design → V1 implementation)

| Domain | Tier | V1 Scope |
|---|---|---|
| Identity / Auth | CORE | Full auth: Passkeys, passwords (fallback), sessions |
| Users | CORE | Profile CRUD, account status |
| Location / Geo | CORE | PostGIS queries, region/boundary management |
| Trust / Verification | CORE | Trust profile, verification badges |
| Master Data / Categories | CORE | Category tree, reference data, pilot geography seed |
| Listings | MARKETPLACE | Full listing lifecycle (Slice A) |
| Business Profiles | MARKETPLACE | Business profile CRUD, service area |
| Services | MARKETPLACE | Service catalog per business |
| Needs / Matching | MARKETPLACE | Full Need lifecycle + provider ranking (Slice B) |
| Search / Discovery | MARKETPLACE | PostgreSQL full-text + PostGIS geo search |
| Media | MARKETPLACE | Upload, storage, URL serving |
| Favorites / Saved | ENGAGEMENT | Save/unsave, saved list per user |
| Messaging | ENGAGEMENT | Conversation + message lifecycle |
| Notifications | ENGAGEMENT | Dispatch, delivery status, preferences |
| Reviews | ENGAGEMENT | Review lifecycle, aggregate rating |
| Moderation (basic) | TRUST & OPS | Report queue, basic decision workflow |
| Fraud (signal ingestion) | TRUST & OPS | Signal recording, basic review interface |
| Case Engine (minimal) | TRUST & OPS | Case open/assign/close for moderation, compliance, support, fraud review |
| Compliance / EİDS | TRUST & OPS | EİDS verification flow |
| Management Center (V1 surfaces) | Surface | Per-domain operational views; user/listing/EİDS/case management |
| Audit | PLATFORM | Immutable audit log |
| Feature Flags | PLATFORM | Flag management, rollout control |
| Observability | PLATFORM | Structured logging, health checks |

---

## Open Architecture Gates

**V1 blocking gates: none.** G-06 is V1.5 and does not block V1 Phase 0A.

See [docs/MASTER-SPEC.md](./docs/MASTER-SPEC.md#open-architecture-gates) and [DECISIONS.md](./DECISIONS.md) for full gate detail.

| Gate | Decision | Status |
|---|---|---|
| G-01 | O-001: Go module layout | ✅ Resolved (ADR-001) |
| G-02 | O-002: Server-side session backing store | ✅ Resolved (ADR-002) |
| G-03 | O-003: Outbox relay strategy | ✅ Resolved (ADR-003) |
| G-04 | O-004: Search backend for V1 | ✅ Resolved (ADR-005) |
| G-05 | O-005: Notification channels V1 | ✅ Resolved (ADR-011) |
| G-06 | O-006: Corporate Workspace boundary | 🔄 Open / Deferred V1.5 (does not block V1 Phase 0A) |
| G-07 | O-007: Pilot geography data seeding | ✅ Resolved (ADR-012) |
| G-08 | O-008: Language rollout sequencing + i18n strategy | ✅ Resolved architecture (ADR-009); rollout sequencing + library/vendor remain OPEN implementation items (not Phase 0A blockers) |
| G-09 | O-009: Management Center frontend model | ✅ Resolved (ADR-013; separate Next.js app `web/apps/admin`) |

---

## Known Conflicts / Ambiguities

See [docs/MASTER-SPEC.md](./docs/MASTER-SPEC.md#detected-conflicts-and-ambiguities) for the full list.

All conflicts from the initial reconciliation review have been resolved or correctly classified.

---

## Next Steps

1. Continue Phase 0B as tasked. Authenticated users can submit listing/public-profile moderation reports (`POST /v1/moderation/reports`) and list their own (`GET /v1/moderation/reports/mine`); reporter identity is session-owned; reports are signals only. Staff queue list/get/status transitions exist in the moderation domain (submitted→triaged→closed); optional internal staff note is not exposed on consumer endpoints. Moderation cases (`moderation.cases`) group one or more reports for the same subject with append-only `case_history`; attaching a report does not copy report text or auto-close the report. Case evidence (`moderation.case_evidence`) is append-only internal investigation material (`staff_note`, `external_reference`, `internal_reference`, `snapshot_reference`); `evidence_added` is written atomically with the evidence row; closed cases still accept evidence (same policy as case staff notes); snapshot_reference is metadata only (no object storage). Case actions (`moderation.case_actions`) record staff decisions (`no_action`/`warning`/`restrict`/`suspend`/`remove`) on listing/public_profile subjects matching the case; lifecycle is proposed→approved→executed with cancel from proposed/approved; executed and cancelled are terminal; `action_*` history is atomic with create/transition; closed cases reject new proposed actions. Listing-target `restrict`/`remove` execution calls Listings `ModerationEnforcement` first (`moderation_state` on the listing, not owner `archived`); Listings commits the row with a search outbox event; the action is marked `executed` only if that call succeeds; listing `suspend` is explicitly unsupported; `no_action` is audit-only (no listing/profile mutation, no notification); executed `warning` does not hide listing/profile or change Trust/sessions and enqueues `notifications.moderation.warning` v1 (in-app) for the contract-resolved owner before the action is marked executed; recipient is never taken from the staff body; public_profile-target `restrict`/`remove` execution calls Identity `PublicProfileModeration` first (`moderation_state` on `identity.public_profiles`, not user disabled/deleted/session); Identity commits the profile row; the action is marked `executed` only if that call succeeds; public_profile `suspend` is explicitly unsupported. Restricted/removed listings remain owner-readable and are omitted from public detail, Search, new favorites, new messages, and new appointment starts (privacy-safe not-found). Appeals (`moderation.appeals`) let the target owner/subject contest an executed action (`submitted`→`under_review`→`accepted`/`rejected`, or `submitted`→`withdrawn`); appellant identity is session-owned; one active appeal per action/user; default 14-day window from execution; `appeal_*` history is atomic; accepting a listing `restrict`/`remove` appeal calls Listings `ClearModerationState` first (moderation_state→`none`, owner status unchanged, search outbox rebuild) and writes `appeal_restoration_applied` then `appeal_accepted`; if Listings restore fails the appeal stays `under_review`; accepting a public_profile `restrict`/`remove` appeal calls Identity `ClearModerationState` first (moderation_state→`none`, account/auth/session unchanged) and writes `appeal_restoration_applied` then `appeal_accepted`; if Identity restore fails the appeal stays `under_review`; `no_action`/`warning` accepts restore nothing; executed actions stay `executed`. Restricted/removed public profiles remain self-readable (`GET`/`PATCH /v1/profile/me`) and return privacy-safe 404 on `GET /v1/public/profiles/{publicProfileId}` and `GET /v1/public/profiles/{publicProfileId}/trust` (Trust projection data is not deleted). Staff HTTP (`/v1/staff/moderation/reports`, `/v1/staff/moderation/cases`, `/v1/staff/moderation/cases/{caseId}/evidence`, `/v1/staff/moderation/cases/{caseId}/actions`, `/v1/staff/moderation/cases/{caseId}/appeals`, `/v1/staff/disputes/...`) requires a dedicated staff principal and permission from `internal/staffauth`. Production mux registers those routes only when a real `StaffIdentityProvider` adapter is wired (issuer/audience/JWKS set **and** an adapter registered). No fake staff login; consumer sessions are not staff auth. Staff IdP vendor remains open. Trust punishment, account-level enforcement/suspension, consumer notification inbox UI, and AI decisioning are not started. Official taxonomy/Fethiye seed, official EİDS provider adapters, and CDN invalidation/vendor are not started. Listing EİDS verification foundation is in place (category policy + owner start/get + publish gate; unconfigured provider is unavailable). Public Identity profile is lazy-created (`GET`/`PATCH /v1/profile/me`, `GET /v1/public/profiles/{publicProfileId}`; opaque `public_profile_id`). Public Güven Pasaportu is `GET /v1/public/profiles/{publicProfileId}/trust` (Identity resolve → Trust projection; missing Trust row is new/zero; no `/v1/public/trust/{userId}`). Consumer `/profil/{publicProfileId}` shows public identity + public Trust. Public listing detail includes optional seller `{publicProfileId, displayName}` from Identity `PublicProfileResolver` (listing remains readable if resolve fails; owner `user_id` is not exposed); consumer `/ilan/{listingId}` links Satıcı to `/profil/{publicProfileId}` when present. Public listing detail loads verified review bodies from `GET /v1/public/listings/{listingId}/reviews` (published listing only; opaque `nextCursor` load-more; newest-first `created_at DESC, id DESC`; no reviewer identity). Derived listing/provider review summaries ingest `reviews.verified.created` v1 (`GET /v1/public/listings/{listingId}/review-summary`, `GET /v1/review-summary/me`). Consumer `/ilan/{listingId}` shows listing accuracy summary plus public verified review bodies. Authenticated consumer `/guven-pasaportum` loads one `GET /v1/trust/me` for count-based Güven Pasaportu plus separate verified-review signals (no duplicate `/v1/review-summary/me`; Trust level remains interaction-count only; no hidden score, no listing accuracy on user Trust). Authenticated consumer `/degerlendirme/{verifiedInteractionId}` submits a verified listing_inspection review over cookie session; `/degerlendirmelerim` lists the session user's reviews; `/randevular` review entry uses appointment `verifiedInteractionId` after reload. Provider lookup, PDF, and NFC/IMEI are not started. Provider `/randevular` OTP start shows a 6-digit code (hash-only at rest). Consumer `/randevular` lists session-owned Verified appointments; provider QR start renders a client-side QR of the opaque token (raw token remains fallback); provider OTP start shows a 6-digit code once (hash-only at rest). public listing detail can request an appointment via “Randevu Talep Et”. Consumer `/mesajlar` lists listing-scoped conversations over cookie session; public listing detail can start/reuse a thread via “Mesaj Gönder”. Consumer `/kayitli-aramalar` lists session-owned saved Search filters; `/ara` can save the current filters over cookie session. Consumer `/favoriler` lists the session user's publicly visible favorited listings; search cards and public detail can save/unsave over cookie session. Consumer `/ilan-ver` can publish a ready/verification_pending listing via owner HTTP; Search projection remains outbox/worker. Consumer `/ara` lists Search HTTP hits with MapLibre markers and explicit viewport search; `/ilan/{listingId}` shows public detail from published listing + exact schema form. Consumer `/ilan-ver` now loads published categories/forms over catalog HTTP; empty catalog until taxonomy seed. Media HTTP attach still does not call Listings ownership contracts. Malware/moderation vendors remain open. Media orphan cleanup now reclaims expired pending/rejected quarantine objects (never `ready`). Passkey registration during signup and verification send (email/SMS vendor) are not started. Remember Me is not a current KONUMLU requirement.
2. Remaining Phase 0A docs (not V1 architecture blockers): ADR-006, 008, 010; domain specs in `/docs/domains/`
3. AUTH-C is locally complete at **PRODUCTION_CODE_READY_PROVIDER_BLOCKED**. Production HumanChallenge, email, and SMS vendor onboarding remain launch blockers. Do not call full 0C-AUTH production-ready until those adapters are selected and wired.
4. NOTIFY-B application layer is at **NOTIFY_B_APPLICATION_READY_FOR_REVIEW**. Marketplace producers and a provider-neutral dispatcher exist. Do not treat this as provider-ready or legally reviewed. Push registration, email/SMS/push vendors, saved-search match, and moderation warning cutover remain later.
