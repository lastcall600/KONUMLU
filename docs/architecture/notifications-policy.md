# Notification policy foundation (NOTIFY-A)

Provider-neutral product/privacy foundation for KONUMLU notifications. This document is **not** legal advice and does not encode unreviewed KVKK/İYS conclusions.

**Package status:** NOTIFY-A frozen. NOTIFY-B producers + provider-neutral dispatch foundation: `NOTIFY_B_APPLICATION_READY_FOR_REVIEW`. NOTIFY-C push endpoint registry: `NOTIFY_C_APPLICATION_READY_FOR_REVIEW` (`000053`; encrypted storage). NOTIFY-D production push transports: Web Push (VAPID), FCM HTTP v1, APNs token HTTP/2. Transactional email transport: Amazon SES (`PROVIDER-B`). Transactional/OTP SMS transport: Netgsm (`PROVIDER-C`).

Additive schema `000052_notifications_policy_core` is **unchanged after approval**. Preference/consent/inbox HTTP, PostgreSQL stores, AUTH-C selected-event materialization, and in-app channel planning are implemented. SES and Netgsm adapters exist; push remains unselected. This is **not** production notification readiness.

---

## 1. What already exists (inventory)

### Tables (`notifications` schema)

| Object | Role |
|---|---|
| `notifications.deliveries` (000009) | One row per `intent_id`. Channels `email`/`sms` (+ `in_app` from 000040). Statuses `pending`/`sending`/`sent`/`failed`. Unique on `intent_id` only. Unchanged by 000052. |
| `notifications.warning_intents` (000040) | Moderation warning template payload. Recipient lives on `deliveries`. Unchanged by 000052. |
| `notifications.preference_settings` (000052) | Mutable UPSERT preference rows. Shape CHECKs only. |
| `notifications.consent_decisions` (000052) | Append-only consent evidence. Current consent is max `recorded_seq`. |
| `notifications.intents` (000052) | Durable user-facing intent parent. |
| `notifications.channel_deliveries` (000052) | Per-channel delivery/suppression lifecycle. |
| `notifications.inbox_items` (000052) | In-app inbox row with `read_at`; composite FK to intent recipient. |
| `notifications.push_endpoints` (000053) | User-owned web/Android/iOS endpoints. Encrypted provider material. Soft-revoke via `revoked_at`. |

**Still absent:** historical backfill, consumer inbox/preferences UI, Web Push frontend registration (`WEB_PUSH_FRONTEND_PENDING`), mobile push client (`MOBILE_PUSH_CLIENT_PENDING`), moderation warning cutover. SES, Netgsm, and push transports exist; production credentials remain operator-owned.

### Code

- `internal/notifications` — legacy `notifications.intent` v1 and `notifications.moderation.warning` v1 unchanged; `DeliveryService` remains verification-only; `Materializer` plans 000052 rows; `Dispatcher` claims `channel_deliveries` (email/SMS/push) with SKIP LOCKED; AUTH-C + marketplace consumers materialize selected events.
- `internal/notifications/policy` — one catalog remains authoritative. Durable model is `(channel, scope_type, scope_key)`. No row means catalog default. Stored `enabled=false` does not mute server-required channels.
- `internal/notifications/httpapi` — session CSRF/Origin consumer APIs for preferences, consents, inbox, and push endpoint register/list/revoke. **No public send-notification API.** List/revoke never return tokens, endpoint URLs, `p256dh`, or `auth`.
- `internal/identity/contracts.NotificationEligibilityReader` — verified email/phone **booleans**. `NotificationContactResolver` returns a verified destination **in memory only** for the dispatcher (not HTTP, not stored, redacted in fmt).
- `internal/infrastructure/notifications` — verification email/SMS bind (`disabled` / `external`).
- `internal/infrastructure/email/ses` — Amazon SES API v2 `SendEmail` transport (AWS SDK v2). Not notification policy.
- `internal/infrastructure/sms/netgsm` — Netgsm REST v2 `/send` (dispatcher SMS) and `/otp` (Identity verification). Not notification policy. No marketing/`iysfilter`.
- Identity signup/reset enqueue `notifications.intent` in the same PostgreSQL transaction as the challenge. **OTP stays on this legacy path.**
- Moderation warning remains legacy `notifications.moderation.warning` v1 (not cut over; avoids double-notify).
- AUTH-C still emits `identity.auth.security` v1. Worker runs Identity audit logging then Notifications materialization as a **single sequential handler**.
- `cmd/worker` also registers marketplace domain events and a sibling dispatcher poll loop (does not starve outbox `RunWorkers`).
- Consumer web has **no** notification inbox/preferences UI.
- `internal/infrastructure/push` — Web Push / FCM HTTP v1 / APNs HTTP/2 adapters. Endpoint existence ≠ accepted delivery. Accept ≠ displayed.

### Outbox types related to notification

| Type | Version | Producer |
|---|---|---|
| `notifications.intent` | 1 | Identity verification (signup/reset) |
| `notifications.moderation.warning` | 1 | Moderation warning execute |
| `identity.auth.security` | 1 | AUTH-C (audit + selected user notifications) |

Marketplace producers enqueue **domain** outbox events (not client-invented names). `cmd/worker` maps them to catalog events and `MaterializeFanout` (actor excluded). Saved-search match is **deferred** (no upstream match event).

### Preference / consent storage

`notifications.preference_settings` (UPSERT) and append-only `notifications.consent_decisions`. No preference rows at signup. Account creation, Terms, verified email/phone, and push use do **not** grant marketing consent.

### Email/SMS intent behavior today

Verification material is resolved from Identity at send time. Destination is not stored on `deliveries`. `disabled` mode never marks sent. `external` requires a registered adapter. OTP/token stay in Identity material, not in outbox JSON.

### In-app API

Registered, session-owned:

- `GET`/`PATCH /v1/notification-preferences`
- `GET`/`POST /v1/notification-consents`
- `GET /v1/notifications` (opaque cursor) · `POST /v1/notifications/{inboxId}/read` · `POST /v1/notifications/read-all`
- `POST`/`GET /v1/push-endpoints` · `DELETE /v1/push-endpoints/{endpointId}`

Mutations require Origin + CSRF. Session actor only; client `user_id` is rejected as an unknown field.

### Gaps that cannot be stuffed into JSON on `deliveries`

- Unique `(intent_id)` cannot represent one intent → many channels.
- Status `sent` conflates provider accept, user read, and in-app persist.
- No owner-scoped inbox list/read_at.
- No versioned consent evidence.
- No hierarchical preferences.
- No suppression reason column.
- Channel CHECK excludes `web_push` / `mobile_push`.

---

## 2. Purpose taxonomy (not a channel)

| Purpose | Meaning | Marketing consent |
|---|---|---|
| `security` | Account/auth/recovery/security-center notices | Never required |
| `transactional` | Committed service events (offer, transaction, delivery, dispute, moderation action) | Never required |
| `social` | Messages, replies, community interaction | Never required |
| `product` | Saved-search matches, optional product activity | Never required (preference-controlled) |
| `marketing` | Campaigns / commercial electronic messages | Required per channel via policy identifiers |

Purpose is server-owned. Clients cannot set it.

---

## 3. Channel taxonomy (provider-neutral)

`in_app` · `web_push` · `mobile_push` · `email` · `sms`

Forbidden in domain values: firebase, sendgrid, twilio, onesignal, FCM, APNs.

---

## 4. Controlled event catalog

Implemented in `internal/notifications/policy` (`CatalogVersion = 1`). Clients cannot invent event names.

Each spec maps: purpose, category, urgency, allowed channels, required channels, preference key, consent required, batchable, dedupe scope, template key, allowlisted variables.

Security examples: `security.login_new`, `security.passkey_added`, `security.passkey_removed`, `security.password_reset_completed`, `security.sessions_revoked`.

Service examples: `offer.*`, `transaction.*`, `delivery.status_changed`, `dispute.updated`, `moderation.action_applied`.

Social: `messaging.message_received` (dedupe = message id, not conversation).

Product: `saved_search.match`.

Marketing: `marketing.campaign`.

Existing verification templates remain `identity.verification.signup` / `identity.password.reset` (challenge recipient, not user inbox).

---

## 5. Preference model

Hierarchical **per channel + scope** (schema-authoritative):

1. Channel switch `(channel, scope_type=channel, scope_key=*)`. `in_app` cannot be turned off via API; a stored false still cannot suppress required in-app.
2. Category switch `(channel, category, key)` e.g. messages+mobile_push=false and messages+in_app=true are independent.
3. Event × channel overrides for non-security events, never for required channels.

Defaults live only in catalog/resolver. No SQL default inserts.

---

## 6. Consent model

Separate from preference. Controlled types (product-policy identifiers, **not** legal conclusions):

- `commercial_electronic.email`
- `commercial_electronic.sms`
- `commercial_electronic.push`

Fields: user_id, type, status (`granted`/`withdrawn`), version, captured_at (**server**), withdrawn_at, source (`settings_web`/`settings_mobile`), policy/document version.

**Not stored in this design:** raw IP history, user-agent history, device fingerprint.

If counsel later requires IP as evidence: hash + truncate + short retention on a dedicated evidence column, never full IP logs in notification payloads. **Do not implement until that product/legal decision exists.**

Consent is not inferred from signup, Terms, having email/phone, using push, or enabling general notifications.

---

## 7. Consent vs preference precedence

Resolution order (deterministic):

1. Unknown event → suppress (`unknown_event`)
2. Account policy (deleted: non-security suppressed; disabled: non-security suppressed; security may remain)
3. Channel in catalog allow-list
4. Destination eligibility (Identity-verified email/phone; active encrypted push endpoint for web/mobile push)
5. Consent if `ConsentRequired` (missing / withdrawn win over preference=on)
6. System-required channels skip user mute
7. Global channel preference
8. Category preference
9. Event override
10. Eligible delivery (provider later)

Withdrawal of marketing consent **always** beats marketing preference enabled. No SMS fallback when email is ineligible.

HumanChallenge / Trust / Step-Up have **no** role in marketing consent.

---

## 8. Security notification policy

AUTH-C security event ≠ user notification.

| AUTH-C event | User notification |
|---|---|
| `auth.passkey.added` / `removed` | yes → `security.passkey_*` |
| `auth.password_reset.completed` | yes → `security.password_reset_completed` |
| `auth.session.revoked` / `sessions.revoked_others` | yes → `security.sessions_revoked` |
| `auth.login.success` | yes → `security.login_new` (in-app required; email not muteable for passkey/reset, muteable for login_new) |
| `auth.login.failed` | **no** (do not email every failure) |
| logout, step-up, rate-limit, challenge | **no** (audit only) |

Do not duplicate AUTH-C logic. Worker composes Identity audit + Notifications materialization. Dedupe is `event_type + recipient + outbox event id + catalog_version` (not user+event_type globally).

---

## 9. Marketing eligibility

Both required:

1. Catalog purpose/channel is marketing-eligible
2. Current granted consent for **that** channel + marketing category preference on

Missing or withdrawn → suppress. Independent per channel.

---

## 10. Notification intent (durable row)

Implemented on `notifications.intents`. Recipient user, controlled event type, purpose (from catalog), actor/resource ids, template key, allowlisted variables, urgency, dedupe key, created_at.

Do not persist rendered copy, secrets, OTP, full domain entities, or raw destinations.

Existing `notifications.intent` v1 stays for verification. Do not put verification OTP into `notifications.intents.variables` or inbox.

---

## 11. Delivery lifecycle

`pending` → (`processing`) → `accepted` | `retryable_failed` | `permanently_failed` | `suppressed`

| Channel | Eligible planning | Dispatch |
|---|---|---|
| `in_app` | `accepted` only after inbox row commits in the same transaction | not sent externally (`accepted` ≠ read) |
| `email` / `sms` | persist `pending` + `next_attempt_at` | provider-neutral `Dispatcher` claims with SKIP LOCKED; never fake success |
| `web_push` / `mobile_push` | no active endpoint → `suppressed` / `channel_unavailable`; active endpoint → `pending` | `PushSender` claims only when a transport adapter is wired; registration alone never `accepted` |

Claim: bounded batch, `FOR UPDATE SKIP LOCKED`, channels with a configured sender only. Unconfigured production adapters **do not claim** (rows stay `pending`; one operational log per poll loop). No Redis delivery truth.

**Processing recovery without `lease_until`:** stranded `processing` rows are reclaimable when `updated_at <= now - processing_hold` (worker uses `OUTBOX_LEASE` as the hold). 000053 is the push-endpoint table, not a dispatch-lease column.

No accepted → pending. No suppressed → accepted without a new plan.

---

## 12. Suppression reasons (internal)

`user_preference` · `consent_missing` · `consent_withdrawn` · `channel_unavailable` · `no_destination` · `deduplicated` · `policy_suppressed` · `account_disabled` · `unknown_event`

Do not expose policy internals on public APIs beyond a generic “not available”.

---

## 13. Dedupe / idempotency

`event_type + recipient + domain_ref + catalog_version`

Messaging uses **message id**, not conversation id. Outbox `idempotency_key` remains the producer unique insert. Worker retries must not create a second intent.

---

## 14. Destination eligibility

Email/SMS require Identity verified+active identifier via **contract** (`NotificationEligibilityReader` at plan time). Unverified address/phone → `no_destination`. Push without a registered endpoint → `channel_unavailable` (never pending).

At send time the dispatcher resolves the verified canonical contact through `NotificationContactResolver` **in memory**. The value is not written to notification tables and must not be logged (`NotificationContact` redacts fmt).

Consent-required and optional channels are **re-checked at dispatch** (withdrawn consent, current preference, disabled/deleted account). Already persisted in-app inbox rows are not retroactively deleted.

---

## 15. Localization / templates

`template_key` + allowlisted variables + locale at render time. Fallback `requested → tr` (ADR-009). Catalog forbids password, OTP, session, cookie, CSRF, passkey challenge, provider secrets in persisted variables. OTP delivery stays on the Identity verification path, not the general inbox payload.

---

## 16. Push endpoints

`notifications.push_endpoints` (migration `000053_notifications_push_endpoints`, additive, reversible). User-owned, not session-owned.

**Combinations (model only):** `web_push`+`web`+`webpush`; `mobile_push`+`android`+`fcm`; `mobile_push`+`ios`+`apns`. Provider is transport metadata. Mobile vendor choice is not domain truth.

**Storage:** provider material is AES-256-GCM (`PUSH_ENDPOINT_ENCRYPTION_KEY`). Lookup/dedupe uses keyed HMAC-SHA256 (`PUSH_ENDPOINT_HASH_KEY`) over `v1|channel|platform|provider|canonical_endpoint_identity`. For `web_push`, identity is the subscription endpoint URL (not p256dh/auth). For `mobile_push`, identity is the opaque provider token. Ciphertext is a versioned JSON payload (`webpush`: endpoint URL + p256dh + auth; `mobile`: opaque token). Same web endpoint with refreshed p256dh/auth updates ciphertext on the same row. Never plaintext FCM/APNs/Web Push secrets in PostgreSQL or logs. List HTTP returns id/channel/platform/provider/timestamps/revoked only. Endpoint URLs are never returned.

**Uniqueness:** partial unique index on `endpoint_hash` where `revoked_at IS NULL`. Same user + same active endpoint refreshes `last_seen_at` and ciphertext (stable id). Multiple devices = multiple rows. Revoked rows may be reactivated for the same user. Cross-user active hash → HTTP 409 `conflict` (no silent takeover). V1 does not transfer ownership.

**Logout:** Identity session revoke/logout does **not** revoke push endpoints. One physical browser/app endpoint can outlive session rotation. Explicit `DELETE /v1/push-endpoints/{id}` is the V1 revoke path. Future device/session binding may supersede this.

**Dispatch:** no active endpoint → suppress `channel_unavailable`. Active endpoint → `pending`. Worker decrypts provider material JIT per endpoint, selects Web Push / FCM / APNs from stored `(channel, platform, provider)`, and never fake-accepts an unconfigured provider. Endpoint registration must not mark push `accepted`.

**Fanout (V1, no per-endpoint delivery rows):** every active endpoint on that channel is attempted once. Channel `accepted` if **at least one** configured endpoint is provider-accepted. Authoritative invalid endpoints are revoked. If nothing was accepted, retryable failures stay `retryable_failed` (dispatcher backoff). Mixed accept + retryable is `accepted` and is **not** retried (some devices may miss this intent). Full-channel retry is at-least-once and may duplicate. No `000054` per-endpoint rows.

**Invalid endpoint mapping (revoke only these):**
- Web Push: HTTP 404 / 410
- FCM: `UNREGISTERED`; `INVALID_ARGUMENT` when the body is authoritatively about the registration token
- APNs: `BadDeviceToken`, `Unregistered`, `DeviceTokenNotForTopic` (including HTTP 410)

Generic timeouts, 429, 5xx, and network errors do **not** revoke. Adapters do not retry. Dispatcher owns backoff. `Retry-After` is not persisted (existing bounded backoff only).

**Accepted semantics:** provider queued/accepted the push. Not displayed, not read, not delivered to the user. No `delivered` state.

**Payload privacy:** lock-screen / third-party-visible. Transport payload (future) may carry category, opaque reference id, template key, and generic title/body. Forbidden: message body, dispute evidence, contact info, address, payment data, TCKN, OTP, auth/reset tokens, raw listing private data.

**Encryption key id:** `endpoint_key_id` is the AEAD key identifier written at seal time. V1 push wiring uses `crypto.NewSingleKey`, so the stored id is always `v1` (one active encryption key). A previous-key ring and automatic/manual rotation are **not implemented**. Replacing `PUSH_ENDPOINT_ENCRYPTION_KEY` makes existing ciphertext unreadable until clients re-register. Do not treat `endpoint_key_id` as evidence that rotation exists.

**HTTP:** `POST/GET /v1/push-endpoints`, `DELETE /v1/push-endpoints/{endpointId}`. Mutations: session + CSRF + Origin. Client must not send `user_id`.

---

## 17. Outbox vs domain durability

Domain mutation remains the source of truth. Notification intent is a **derived effect** via `platform.outbox_events` (ADR-003). Core transaction must not wait on provider I/O.

Wired producers enqueue a **minimum domain event** in the same `runMaybeTx` as the mutation (`Enqueue` uses `db.TxFrom`). Worker maps those events to catalog types. Outbox retry is idempotent via `UNIQUE(recipient_user_id, dedupe_key)`. No Kafka/NATS.

**Wired:** `messaging.message.received`, `offers.offer.submitted|accepted|rejected`, `transactions.transaction.created|completed|cancelled`, `deliveries.delivery.status_changed`, `disputes.dispute.updated` (create/status apply, not evidence).

**Deferred:** saved-search match (no durable match job), offer withdraw/expired, transaction started (no product notify), moderation warning cutover.

---

## 18. Retry

Retry only adapter error classes (`retryable` / `timeout` / `permanent` / `unconfigured`). Consent/preference/unknown/no-destination are suppressed or permanent, not hot-retried. Bounded exponential backoff (cap 8 attempts / 300s). Unconfigured production senders **do not claim**. No vendor HTTP status codes in the domain layer. No duplicate `accepted` on retry.

---

## 19. Account state

| Account | Marketing/social/product | Security/recovery |
|---|---|---|
| active | per policy | per catalog |
| disabled/restricted | suppressed | may remain eligible |
| deleted | suppressed | security-only catalog flag; retention is a **legal/product decision**, not hardcoded |

---

## 20. APIs (registered)

Session actor, CSRF+Origin on mutations, no client `user_id`:

- `GET`/`PATCH /v1/notification-preferences`
- `GET`/`POST /v1/notification-consents` (type + grant/withdraw; server owns `user_id`, `recorded_at`, `recorded_seq`, `policy_version`, `source=settings_web`)
- `GET /v1/notifications` (cursor) · `POST .../{inboxId}/read` · `POST .../read-all`

Staff IAM is unchanged; staff is not auto-granted inbox access.

**Fail-safe:** consent lookup errors fail closed for consent-required channels (not granted). Preference lookup errors do not guess optional channels on; security-required channels still follow catalog. Store errors during persist abort the transaction (outbox retry).

---

## 21. Observability

Safe fields: purpose, event type, suppression reason, delivery outcome, channel code, request/trace id.

Never log: email, phone, push token, Web Push endpoint URL, p256dh, auth secret, decrypted provider material, encryption keys, message body, consent evidence payload, OTP, session.

---

## 22. Türkiye legal-review boundary

KONUMLU is Türkiye-first (D-012). This package uses **policy identifiers** (`commercial_electronic.*`, `product.notification.v1`) so counsel can later bind them to KVKK, ticari elektronik ileti rules, and İYS **without** claiming in code that a legal basis is automatically valid.

Open legal questions (do not invent answers):

- Exact İYS connector/operator and which templates are in-scope
- Whether in-app marketing is commercial electronic communication
- Whether IP is required as consent evidence
- Deletion/retention periods for inbox and consent evidence
- Whether security email is mandatory vs product-required

---

## 23. Schema `000052` (approved, unchanged in this package)

Files: `backend/migrations/000052_notifications_policy_core.up.sql` and `.down.sql`. No 000053.

### Still missing after NOTIFY-A application layer

Push endpoints, provider workers, Messaging/Offers/Transactions producers, consumer UI, legal/İYS binding.

### Approved tables

**`notifications.preference_settings`** — mutable UPSERT; PK `(user_id, channel, scope_type, scope_key)`. Shape CHECKs only (channels, scope types, key shape, timestamps). **No** CHECK that `in_app` or `security` cannot be stored `enabled=false`. Catalog/resolver decides whether a stored row may suppress a required notification.

**`notifications.consent_decisions`** — append-only. `recorded_seq BIGINT GENERATED ALWAYS AS IDENTITY` with **UNIQUE(`recorded_seq`)**. Current consent is `ORDER BY recorded_seq DESC LIMIT 1`. `recorded_at` is audit time, not the resolver key. `consent_type` / `source` are nonempty bounded TEXT; catalog values are **not** SQL CHECKs. `decision` remains `granted`/`withdrawn`. No IP/UA. No Identity FK.

**`notifications.intents`** — `UNIQUE (recipient_user_id, dedupe_key)` plus **`UNIQUE (id, recipient_user_id)`** so inbox can composite-FK. No destination/provider JSON.

**`notifications.channel_deliveries`** — `UNIQUE (intent_id, channel)`; FK to `intents(id)` `ON DELETE NO ACTION`. Provider-neutral states. Do not alter `notifications.deliveries`.

**`notifications.inbox_items`** — retain `user_id` for list/unread indexes. `UNIQUE (intent_id)`. **`FOREIGN KEY (intent_id, user_id) REFERENCES notifications.intents (id, recipient_user_id) ON DELETE NO ACTION`**.

Keep `notifications.deliveries` for verification until a dedicated follow-up. Do not mix `verification_challenge` into inbox. `warning_intents` untouched. No historical backfill in 000052. No PostgreSQL ENUM. No retention interval. Push endpoints are **000053** (do not alter 000052).

### Current-consent query

```sql
SELECT decision, policy_version, recorded_at, source, id, recorded_seq
FROM notifications.consent_decisions
WHERE user_id = $1 AND consent_type = $2
ORDER BY recorded_seq DESC
LIMIT 1;
```

Concurrent grant/withdraw: both INSERT; winner is **max `recorded_seq`** (identity allocation order), even if `recorded_at` disagrees or a lower-seq transaction commits later.

### Indexes

- Preferences: PK four-tuple
- Consents: UNIQUE `recorded_seq`; `(user_id, consent_type, recorded_seq DESC)`
- Intents: UNIQUE recipient+dedupe; UNIQUE `(id, recipient_user_id)`; `(recipient_user_id, created_at DESC, id DESC)`
- Channel deliveries: UNIQUE `(intent_id, channel)`; partial worker `(next_attempt_at, id) WHERE state IN ('pending','retryable_failed')`
- Inbox: UNIQUE `intent_id`; `(user_id, created_at DESC, id DESC)`; partial unread same keys `WHERE read_at IS NULL`

No FK to `identity.users`.

### Retention

Undecided. Do not encode statutory periods in SQL.

### Rollback / roll-forward

- Roll-forward: add five tables; old verification path unchanged; no dual-write required while empty.
- DOWN: drop inbox_items → channel_deliveries → intents → consent_decisions → preference_settings. Composite inbox FK and `UNIQUE (id, recipient_user_id)` vanish with those tables. Never drop `deliveries` / `warning_intents`.

### Why existing tables cannot support this

`deliveries.intent_id` UNIQUE blocks multi-channel. No `read_at`, purpose, event_type, consent, or preference. JSON on `warning_intents` or delivery `provider_ref` would mix moderation-only shape with a general inbox.

### What can proceed without migration (done)

- Purpose/channel/event catalog
- Deterministic resolver + matrix tests
- Consent vs preference rules
- AUTH-C mapping (no AUTH code change)
- Preference/consent/inbox **memory** ownership tests
- Redaction keys
- This document

HTTP APIs, PostgreSQL stores, AUTH-C selected consumption, and in-app planning are in this package. Schema presence alone was not NOTIFY-A complete; application behavior now exists but is **not** vendor-ready.

---

## 24. Provider blockers

HumanChallenge production widget credentials. Amazon SES is the frozen transactional email provider; production sending still requires verified identity, region sandbox exit, and runtime credentials. Do not claim mailbox delivery from SendEmail accept. Netgsm is the frozen Türkiye SMS provider; production sending still requires API credentials, approved `msgheader`, credit, and the OTP package for Identity OTP. Do not claim handset delivery from send/otp accept. Do not invent İYS policy in the provider package. Push transports are implemented; live Web Push/FCM/APNs credentials are operator-owned (`LIVE_*_TEST_PENDING`). Frontend/mobile registration clients remain pending.

---

## 25. Remaining after NOTIFY-B / PROVIDER-C

1. Consumer Web Push registration UI / service worker (`WEB_PUSH_FRONTEND_PENDING`)
2. React Native FCM/APNs registration (`MOBILE_PUSH_CLIENT_PENDING`)
3. Optional cutover of `notifications.moderation.warning` onto `notifications.intents`
4. Saved-search match producer (needs an upstream match event)
5. Optional İYS port **after** legal review
6. Consumer inbox/preferences UI
7. Operator live sends: `LIVE_WEBPUSH_TEST_PENDING`, `LIVE_FCM_TEST_PENDING`, `LIVE_APNS_TEST_PENDING`, `LIVE_NETGSM_TEST_PENDING`, `LIVE_SES_TEST_PENDING`
