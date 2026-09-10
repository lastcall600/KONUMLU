# ADR-011: Notification Architecture

**Status:** ✅ Accepted (architecture). G-05 resolved. Launch parameters remain OPEN (not G-05): V1 channel completeness, vendors, IYS connector, quiet-hour window, numeric caps, security-SMS degradation.  
**Date:** 2026-09-05  
**Resolves:** O-005 architecture; G-05 (ownership, templates, localization, preferences, transactional vs marketing, consent/IYS boundary, idempotency, retry, outbox, abstractions, push tokens, quiet hours, rate/cost controls)  
**Does not resolve:** Which channels are production-complete at V1 vs later; any SMS, email, or push vendor; IYS operator/integrator; exact quiet-hour window; numeric rate/cost caps; security-SMS degradation; exact backoff numbers (ADR-003 leftovers)  
**Gates:** G-05 resolved by this ADR

---

## 1. Decision

KONUMLU notifies people through a single **Notifications** domain that orchestrates:

| Channel class | Role |
|---|---|
| **In-app** | Canonical inbox / activity record for product events that have a user-visible notification. |
| **Push** | Device fan-out for the same product events when tokens and preferences allow. |
| **Transactional email** | Service messages tied to an account or committed business event (not campaigns). |
| **Transactional SMS** | High-cost service messages; used only when the template’s channel policy allows SMS. |
| **Marketing** | Promotional or commercial content on allowed channels **only** with valid consent (and IYS where applicable). |

Producing domains never call FCM, APNs, SMTP, or an SMS API. They write a **structured notification intent** via the **transactional outbox** (ADR-003) in the same PostgreSQL transaction as the business commit. `cmd/worker` delivers that intent to Notifications. Notifications persists inbox/delivery state, then talks to **channel ports** in Integrations.

No SMS, email, or push **provider** is chosen here. Adapter implementations are OPEN.

---

## 2. Context

ARCHITECTURE.md: Notifications owns dispatch orchestration, channel routing, preferences, and delivery status; Messaging owns message bodies; Needs/Matching and Messaging notify through the outbox; `NotificationSender` is a mandatory abstraction; staging must exercise notification delivery.

D-008 / ADR-003: push, email, and SMS are **critical async** effects — outbox, at-least-once, idempotent adapters, no provider I/O in the producer transaction, no in-process events for those sends.

O-005 architecture (G-05) is resolved by this ADR; V1 channel completeness and vendors remain launch parameters. D-012 is Türkiye-first: commercial electronic messages are regulated (**IYS** — İleti Yönetim Sistemi). D-015: no cross-domain table access. ADR-009: recipient locale, structured template params, catalog fallback `requested locale → tr`, no LLM-authored legal copy as sole source.

This ADR freezes **how notifications work**. It does not freeze **which** paid channels ship on day one or **which** vendors send them.

---

## 3. Domain ownership

| Concern | Owner | Notes |
|---|---|---|
| Notification intent after a business commit | **Producing domain** | Outbox `event_type` + opaque payload + `idempotency_key`. Does not pick vendor or render body copy. |
| Inbox records, delivery attempts, routing, template binding | **Notifications** | Only domain that writes `notifications`, deliveries, preference rows, device tokens. |
| Channel preferences (category × channel) | **Notifications** | `UpdatePreferences` / `GetNotifications` contracts. |
| Email / phone as identity attributes | **Users** (or Identity if that spec owns contact) | Notifications **reads** via contract at send time. Does not become source of truth for contact. |
| Locale | **Users** | Recipient locale per ADR-009. |
| Chat body | **Messaging** | Notifications may snapshot a non-PII preview param (id, deep link, truncated safe title). Full thread stays in Messaging. |
| Push device tokens | **Notifications** | Routing data, not session credentials (D-016). |
| Commercial consent evidence, IYS check-at-send, marketing suppression | **Notifications** (operational) + **Compliance** (legal/audit contract) | Notifications must not send marketing without a passing consent check. Compliance owns what “IYS-compliant” means legally; Integrations owns any IYS adapter (vendor OPEN). |
| Provider HTTP/SDK | **Integrations** | Implements channel ports. No SDK types in Notifications use-cases. |
| Audit of regulated send/consent decisions | **Audit** (via outbox) | Codes, not rendered bodies or PII. |
| Operator views | **Management Center** | Reads admin contracts. No parallel dispatcher. |

**Forbidden:** Listings/Needs/Messaging importing an SMS SDK; Notifications owning message threads; using in-process events as the only path for push/email/SMS; storing durable auth tokens as “push tokens” in `localStorage`.

Producer contract (conceptual): `Notify(ctx, intent)` is **not** a synchronous send. The supported path is **outbox → worker → Notifications**. A direct in-process `Notify` that itself only writes Notifications tables **without** outbox is forbidden for crash-sensitive sends.

---

## 4. Intent model (producer → Notifications)

A notification **intent** is structured:

- `type` — stable template key (`needs.match.eligible_provider`)
- `recipient_user_id`
- `purpose` — `security` \| `transactional` \| `marketing` (see §7)
- `category` — preference bucket (`messages`, `matches`, `listing`, `account`, `marketing`, …)
- `priority` — `critical` \| `normal` \| `low` (quiet hours and rate policy)
- `params` — JSON-safe interpolation and deep-link ids; **no** pre-concatenated localized sentences; **no** unnecessary PII
- `business_event_id` — producer’s natural id (match id, message id, …)
- `idempotency_key` — unique intended effect (ADR-003), e.g. `notify:{type}:{business_event_id}:{recipient_id}`

Producers do **not** set `channel=sms`. Channel expansion is Notifications policy for that `type` (plus user preferences, consent, cost gates).

---

## 5. Template and version model

- Each `type` maps to a **template** with a **version**. Breaking change of meaning or params → new `type` or new template version; never reuse a key (ADR-009).
- Template declares: purpose, category, priority, **allowed channels**, whether an **in-app row** is required, security bypass flags, SMS eligibility.
- Bodies are ICU-capable strings per channel (push title/body, email subject/body, SMS body, in-app title/body) in PLATFORM catalogs, bound by Notifications at **dispatch**.
- Named placeholders only. UGC fragments in params are isolated for bidi (ADR-009); they are not rewritten by LLM at send time.
- Rendered body is a **derived** snapshot on the delivery row for support/debug (retention/KVKK: minimize). Canonical wording remains the catalog version id + params.

Legal / EİDS wording in notifications follows ADR-009: human-controlled catalogs, not sole-source LLM.

---

## 6. Localization

Binding ADR-009 for notification copy:

- Resolve **recipient** locale: Users preference, else last known UI locale, else `tr`.
- Render at **dispatch** in Notifications/worker, not in the producing domain.
- Catalog fallback: **requested locale → `tr`**. Do not send an empty body. If `tr` is missing: production safe generic for that `type`; non-production fail loud.
- Do not change product `dir` for a single fallback string; isolate mixed-direction runs.
- Date/number/currency in copy: CLDR for recipient locale; TRY/amounts stay structured in params.

V1 completeness of `en`/`ru`/`ar` catalogs remains O-008. Architecture still requires templates that can exist in all four locales.

---

## 7. Transactional vs marketing

| Purpose | Meaning | Consent | Typical channels |
|---|---|---|---|
| **security** | Auth, recovery, account-takeover, mandatory legal notice where product policy says immediate | Not marketing consent. Must not be used to smuggle campaigns. | In-app (if logged in), email and/or SMS per template; push optional |
| **transactional** | Tied 1:1 to a user-initiated or system-committed product event (need match, new message, listing status) | Not IYS commercial consent. User may mute **optional** channels; in-app may remain. | In-app + push; email/SMS only if template allows |
| **marketing** | Offers, digests-as-promo, re-engagement, commercial electronic messages | **Explicit consent** + IYS where the send is a ticari elektronik ileti to a Türkiye-regulated destination | Only channels with a live consent grant; never “upgrade” a transactional type to marketing copy in the same `type` |

**Misclassification is a defect.** A campaign must not use `purpose=transactional` to skip IYS. A password reset must not be delayed for marketing quiet-hours policy.

---

## 8. Preferences

Stored by Notifications, keyed by `user_id` + `category` + `channel`.

- Defaults: transactional in-app on; transactional push on; marketing all off until consent; SMS off except templates that require SMS (security) or user opted in.
- **security** templates ignore mute for the channels the template requires.
- **transactional** in-app: default cannot be fully “delete the audit of the event” via preference; user may hide from UI, but the domain may still record the row (product spec may narrow this; architecture forbids using preference to skip a legally required notice).
- **marketing**: every channel independently opt-in; global marketing off is honored.
- Web and mobile share the same preference document (one user). Device-level push still requires a valid token.

---

## 9. Consent and IYS boundary

Türkiye-first commercial messaging:

1. **Marketing** SMS/email (and any other channel classified as commercial electronic message) MUST pass a **consent check** immediately before provider send.
2. That check includes **IYS** when the message is in-scope for IYS. The IYS **connector and operator** are OPEN. Notifications calls a **Consent/IYS port**, not a named vendor.
3. Consent is **not** inferred from having an account, from transactional history, or from an AI model (D-011).
4. Withdrawal / IYS reject / bounce-as-complaint → suppress before send; record a non-PII reason code.
5. **Transactional** and **security** sends do not query IYS as if they were campaigns. They still obey KVKK minimization and preference rules in §8.
6. Proof of consent (who, when, source, scope) is stored for audit; payloads in Audit/outbox stay codes + ids, not full message bodies or extra PII.
7. Local/staging must be able to run with a **fake Consent port** (D-013). Production wiring is configuration, not domain logic.

If legal scope of IYS vs a given template is unclear, **do not send** as marketing. Surface the gate; do not invent a bypass.

---

## 10. Channel behavior

### 10.1 In-app

- Created in Notifications’ database when the template requires an inbox row.
- Unique on (`idempotency_key`) or (`recipient`, `type`, `business_event_id`) so at-least-once dispatch does not duplicate the inbox item.
- Read/unread, list, and pagination are Notifications contracts. Deep link ids only.
- In-app is **not** a substitute for outbox: the inbox write happens in the **Notifications handler transaction**, after the producer already committed.

### 10.2 Push

- Fan-out to all **active** tokens for the user (and optionally the active business context later — not designed here).
- Invalid token / canonical unregistered error → mark token inactive (no retry storm).
- Provider choice OPEN (FCM/APNs appear in ARCHITECTURE.md as examples only, not a vendor freeze).

### 10.3 Transactional email / SMS

- Same intent; separate **delivery** rows per channel.
- SMS is **opt-in at template policy** plus cost gates (§16). Default: most product events are **not** SMS.
- Email/SMS adapters receive already-rendered or structured+locale according to the port; **no** provider template language in domain code unless the port hides it. Provider-side template IDs are OPEN and must stay in Integrations.

### 10.4 Marketing

- Separate `purpose=marketing` types. Batch/digest generation is still Notifications (or a later campaign worker) writing intents with consent already required at send.
- No marketing send if consent/IYS/preference/quiet hours/cost circuit says no.

---

## 11. Outbox integration

Follow ADR-003 exactly for producer → Notifications:

1. Producer transaction: domain write + outbox insert (`idempotency_key`, `event_type` routing to Notifications).
2. No provider I/O in that transaction.
3. Worker claims with `SKIP LOCKED`; at-least-once.
4. Notifications handler, in **its** transaction: upsert inbox + **per-channel delivery** rows (pending). Optionally insert **follow-up outbox** rows (same shared `platform.outbox`) for each external send so push/email/SMS retry independently of each other.
5. External send runs **after** that commit, via channel ports. Success/failure updates the delivery row.

**Rejected:** producer HTTP handler calling the provider after commit; in-process event as the only delivery of SMS/email/push; Notifications querying another domain’s tables for “who to notify.”

Ordering: **not required** for notifications (ADR-003 §14). Duplicate pushes must be harmless (idempotent inbox + provider keys).

`SendNotification` in ARCHITECTURE.md is the **worker-facing / domain** entry after relay, not a public “fire and forget HTTP to FCM.”

---

## 12. Idempotency and dedupe

| Layer | Mechanism |
|---|---|
| Outbox insert | Producer `idempotency_key` unique (ADR-003). |
| Inbox | Unique business key; second delivery is a no-op update (e.g. `sent_at`). |
| Per-channel delivery | Unique (`notification_id` or intent key + `channel`). |
| Provider | Pass idempotency/dedupe key when the port supports it. |
| User-visible spam | Cooldown per (`recipient`, `type`) where the template sets it (e.g. collapse repeats). |

Duplicates after provider-success / worker-crash are expected. They must not create a second inbox item or a second billed SMS if the provider/key prevents it. If a provider cannot idempotently SMS, Notifications must still unique-lock the delivery row to `sending`/`sent` so a second worker does not start a second send (best effort; not exactly-once).

---

## 13. Retry and failure handling

Align with ADR-003 retry/poison:

- Retryable provider errors: exponential backoff with jitter on the **delivery** (or follow-up outbox row). Max attempts and numeric backoff: OPEN (ADR-003), may be **stricter for SMS** (cost).
- Non-retryable: bad payload, unknown `type`, unregistered push token, permanent email bounce, IYS/consent deny → terminal failure for that delivery; **do not** retry as if transient.
- Token invalidation is a success-path for “do not retry this token,” not a poison of the whole intent (other tokens / in-app may succeed).
- Poison / max attempts: `failed`, metrics, operator visibility; replay is conscious (ADR-003). In-app row may exist with channel `failed`.
- Provider down: domain already committed; user-visible delay is allowed; **silent drop is not**.

---

## 14. Provider abstraction

Notifications depends only on ports, e.g.:

- `PushSender`
- `EmailSender`
- `SmsSender`
- `ConsentChecker` (IYS/marketing)

ARCHITECTURE.md’s `NotificationSender` may be a façade over these. **No** vendor names, account IDs, or SDK types in Notifications or producing domains. Credentials in environment config. Local fakes mandatory (D-013). Switching vendor = Integrations + wiring.

Web Push vs native push: both behind `PushSender` if both exist; enablement OPEN.

---

## 15. Push-token lifecycle

| Event | Action |
|---|---|
| App registers/refreshes token | Upsert token: user, platform, token, device stable id, app version, `active`. |
| User logs out / session revoked | Deactivate tokens bound to that session/device as Identity/Users signal via contract/outbox — do not leave a logged-out device receiving account push. |
| User disables push | Preference off; tokens may remain but are not used until re-enabled. |
| Provider unregistered / not-registered | Deactivate that token immediately. |
| Token replaced on device | New token active; old token inactive. |
| Account deactivated | Stop all sends; deactivate tokens. |

Tokens are **not** authentication secrets of the KONUMLU session. They are still credentials for a push vendor: store server-side in Notifications, encrypt at rest per platform secret policy (details OPEN), never in browser `localStorage` as an auth substitute.

Multiple devices: all `active` tokens receive push unless a future device-level mute exists.

---

## 16. Quiet hours

- Clock: recipient timezone if Users stores one; else **Europe/Istanbul** (D-012). Instant storage remains UTC.
- **Default window:** OPEN (product decision). Architecture requires a window to be configurable per user and a platform default.
- **marketing:** do not send during quiet hours; hold delivery until `available_at` (outbox/delivery not-before). Consent still re-checked at actual send.
- **transactional** + `priority=normal|low`: honor user quiet-hours preference (default OPEN: on or off).
- **security** and `priority=critical` transactional: **bypass** quiet hours.
- In-app upsert is **not** delayed by quiet hours (the inbox can wait to be seen). Push/email/SMS are what quiet hours hold.

---

## 17. Rate and cost controls

Notifications enforces, before provider send:

- Per-template **cooldown** (dedupe window).
- Per-user **daily caps** per channel (SMS lowest). Exact numbers OPEN.
- Platform **SMS (and email if needed) budget / circuit breaker**: approaching cap → fail open to in-app/push only, alert ops; do not silently omit security templates without an explicit degraded policy (OPEN for security SMS fallback).
- Marketing subject to stricter caps than transactional.
- Feature flags may disable a costly channel globally without code changes in producers.

Producing domains must not “help” by sending five outbox intents for one event. One intent per `idempotency_key`.

---

## 18. Observability and privacy

- Metrics: queued, sent, retry, fail, suppress (preference, consent, quiet hours, cap), token invalidations — by `type` and channel. **No PII** in logs (ARCHITECTURE.md).
- Payloads: ids and codes; do not log full SMS/email bodies.
- Management Center: delivery/consent suppressions via admin contract (D-020), no bypass of IYS.

---

## 19. What this ADR does not choose

- V1 production channel set (O-005): architecture includes in-app, push, transactional email/SMS, and marketing-with-consent; **launch completeness** is OPEN.
- Any **vendor** for SMS, email, push, or IYS.
- Provider timeouts, lease, poll interval (ADR-003 OPEN).
- Exact quiet-hours default clock window and numeric rate/SMS budgets.
- Whether digest jobs are V1 or later.
- Corporate Workspace as a recipient principal (O-006).

---

## 20. Consequences

### Positive

- Crash-safe product notifications aligned with ADR-003.
- Türkiye marketing/IYS cannot be bypassed by domain “just this once” SMS.
- Locale and template versioning aligned with ADR-009.
- Costly SMS is a policy, not an accident in Listings/Needs code.

### Negative / trade-offs

- Two-hop outbox (intent then per-channel) adds latency vs in-request send.
- At-least-once can still double-push; inbox uniqueness is the user-visible lock.
- Launch parameters (vendors, V1 channel completeness, quiet hours, caps, IYS connector, security-SMS degradation) stay OPEN; they are not G-05.

### Constraints imposed

- Do not send marketing without consent/IYS check-at-send.
- Do not put provider SDKs in domain use-cases.
- Do not use in-process events as the reliability path for push/email/SMS.
- Do not classify campaigns as transactional.
- Do not invent an SMS/email/push vendor in implementation without a follow-on decision.

---

## 21. Relationship to ADR-006 and ADR-010

INDEX.md lists ADR-006 (V1 channels + providers) and ADR-010 (storage/notifications/maps abstractions). **This ADR (011) is the notification architecture record.** ADR-006 should freeze V1 channel completeness and name adapters **without** re-opening §§1–18. ADR-010 must not replace these ports with a second notification model.

INDEX.md also lists ADR-011 as cross-domain communication. **This file is Notification Architecture.** Cross-domain contracts remain D-015 / a separate ADR; this document does not redefine that topic.

---

## 22. Open items

| ID | Item | Notes |
|---|---|---|
| O-005.1 | V1 channel production set | Which of in-app / push / email / SMS / marketing are launch-complete vs later. Architecture still designs all of them. |
| O-005.P | SMS, email, push vendors | Explicitly unset. |
| O-005.IYS | IYS operator / technical connector | Port required; vendor unset. |
| OPEN-QH | Default quiet-hours window | Configurable; default clock span unset. |
| OPEN-CAPS | Numeric rate and SMS/email budgets | Policy required before costly channels go live. |
| OPEN-SECURITY-SMS | Degraded path if SMS circuit open | Whether security SMS may fail closed vs alternate channel. |
