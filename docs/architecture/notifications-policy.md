# Notification policy foundation (NOTIFY-A)

Provider-neutral product/privacy foundation for KONUMLU notifications. This document is **not** legal advice and does not encode unreviewed KVKK/İYS conclusions.

**Package status:** NOTIFY-A frozen. NOTIFY-B producers + provider-neutral dispatch foundation: `NOTIFY_B_APPLICATION_READY_FOR_REVIEW`. Transactional email transport: Amazon SES (`PROVIDER-B`). Transactional/OTP SMS transport: Netgsm (`PROVIDER-C`). Push vendor remains unselected.

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

**Still absent:** durable push endpoint tables (deferred; no 000053), historical backfill, consumer UI, moderation warning cutover. SES and Netgsm adapters exist; production credentials remain operator-owned.

### Code

- `internal/notifications` — legacy `notifications.intent` v1 and `notifications.moderation.warning` v1 unchanged; `DeliveryService` remains verification-only; `Materializer` plans 000052 rows; `Dispatcher` claims `channel_deliveries` (email/SMS/push) with SKIP LOCKED; AUTH-C + marketplace consumers materialize selected events.
- `internal/notifications/policy` — one catalog remains authoritative. Durable model is `(channel, scope_type, scope_key)`. No row means catalog default. Stored `enabled=false` does not mute server-required channels.
- `internal/notifications/httpapi` — session CSRF/Origin consumer APIs for preferences, consents, inbox. **No public send-notification API. No push registration HTTP.**
- `internal/identity/contracts.NotificationEligibilityReader` — verified email/phone **booleans**. `NotificationContactResolver` returns a verified destination **in memory only** for the dispatcher (not HTTP, not stored, redacted in fmt).
- `internal/infrastructure/notifications` — verification email/SMS bind (`disabled` / `external`).
- `internal/infrastructure/email/ses` — Amazon SES API v2 `SendEmail` transport (AWS SDK v2). Not notification policy.
- `internal/infrastructure/sms/netgsm` — Netgsm REST v2 `/send` (dispatcher SMS) and `/otp` (Identity verification). Not notification policy. No marketing/`iysfilter`.
- Identity signup/reset enqueue `notifications.intent` in the same PostgreSQL transaction as the challenge. **OTP stays on this legacy path.**
- Moderation warning remains legacy `notifications.moderation.warning` v1 (not cut over; avoids double-notify).
- AUTH-C still emits `identity.auth.security` v1. Worker runs Identity audit logging then Notifications materialization as a **single sequential handler**.
- `cmd/worker` also registers marketplace domain events and a sibling dispatcher poll loop (does not starve outbox `RunWorkers`).
- Consumer web has **no** notification inbox/preferences UI.
- No push token / device registration tables or HTTP.

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
4. Destination eligibility (Identity-verified email/phone; push endpoint later)
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
| `web_push` / `mobile_push` | `suppressed` / `channel_unavailable` | no pending row without a destination; no endpoint table |

Claim: bounded batch, `FOR UPDATE SKIP LOCKED`, channels with a configured `ChannelSender` only. Unconfigured production adapters **do not claim** (rows stay `pending`; one operational log per poll loop). No Redis delivery truth.

**Processing recovery without `lease_until`:** stranded `processing` rows are reclaimable when `updated_at <= now - processing_hold` (worker uses `OUTBOX_LEASE` as the hold). This is why 000053 was not required for dispatch.

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

**Deferred (no 000053).** Inventory: no web/mobile push registration tables or HTTP exist. Dispatch currently **requires the original endpoint material**, so hashed-only storage is insufficient. A future migration must be provider-neutral (user-owned endpoints, encrypt-at-rest using the existing application keyring, revoked_at, multiple endpoints per user, no browser fingerprinting, no vendor column as domain truth). Until then `web_push`/`mobile_push` stay `channel_unavailable`.

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

Never log: email, phone, push token, message body, consent evidence payload, OTP, session.

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

Keep `notifications.deliveries` for verification until a dedicated follow-up. Do not mix `verification_challenge` into inbox. `warning_intents` untouched. No push-token table. No historical backfill in 000052. No PostgreSQL ENUM. No retention interval.

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

HumanChallenge production widget credentials. Push vendor. Amazon SES is the frozen transactional email provider; production sending still requires verified identity, region sandbox exit, and runtime credentials. Do not claim mailbox delivery from SendEmail accept. Netgsm is the frozen Türkiye SMS provider; production sending still requires API credentials, approved `msgheader`, credit, and the OTP package for Identity OTP. Do not claim handset delivery from send/otp accept. Do not invent İYS policy in the provider package.

---

## 25. Remaining after NOTIFY-B / PROVIDER-C

1. Push **vendor** selection and production adapter (launch blocker); SES email and Netgsm SMS adapters exist
2. Push endpoint schema (000053+) after encryption/ownership review
3. Optional cutover of `notifications.moderation.warning` onto `notifications.intents`
4. Saved-search match producer (needs an upstream match event)
5. Optional İYS port **after** legal review
6. Consumer inbox/preferences UI
7. Operator Netgsm live send (`LIVE_NETGSM_TEST_PENDING`) and SES live send (`LIVE_SES_TEST_PENDING`)
