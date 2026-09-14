# STATE-MATRICES.md

> Domain and product state matrices for KONUMLU UI.
> Source: backend domain packages and HTTP DTOs. Do not invent states.
> Parent contract: [PRODUCT-UI-HANDOFF.md](./PRODUCT-UI-HANDOFF.md).

Universal UI states for every interactive screen: **loading · empty · error · forbidden · not-found · offline/retry · ready**. Privacy-safe 404 is preferred over 403 when hiding another user's resource.

---

## 1. Authentication (consumer Identity)

| Product state | How it appears | HTTP / cookie truth | UI rule |
|---|---|---|---|
| Anonymous | no session | `GET /v1/auth/session` → 401 | public browse; CTAs to `/giris` |
| Authenticated | session cookie | 200 session DTO | never store JWT in `localStorage` / `sessionStorage` |
| Step-Up required | sensitive action blocked | `403` `{ "error": "forbidden" }` today | **do not** treat as Turnstile; design a distinct Step-Up passkey flow. Gap: not distinguishable from CSRF `forbidden` |
| Turnstile challenge required | bot/human widget | `403` `{ "error": "challenge_required" }` | widget only; **not** identity verification, EİDS, Trust, or Step-Up |
| Session expired | cookie invalid / idle | 401 `unauthenticated` | return to `/giris`; do not keep a fake logged-in chrome |
| Forbidden (CSRF / origin) | mutation rejected | `403` `{ "error": "forbidden" }` | refresh page / retry; do not show Turnstile |
| Rate limited | auth abuse | `429` `rate_limited` + `Retry-After` | generic wait copy; not Trust |
| Generic login failure | bad password / unknown account | `401` generic | existence-hiding; never “email not found” |

Turnstile public sitekey only (`NEXT_PUBLIC_TURNSTILE_SITEKEY`). Token is memory-only `challengeToken`. Empty sitekey = no widget.

Signup: identifier → verification start/finish → complete (optional password). No passkey during signup. Password reset: start → verify → complete; **no auto-login**; all sessions revoked.

---

## 2. Listing (owner lifecycle)

`draft` → `ready` → (`verification_pending`) → `published` → `archived`

| Status | Owner UI | Public / Search |
|---|---|---|
| `draft` | editable wizard | hidden |
| `ready` | publish CTA | hidden |
| `verification_pending` | wait / EİDS status | hidden |
| `published` | archive; public link | visible if moderation `none` |
| `archived` | owner readable | hidden |

Client cannot set `status` or `ownerUserId` on create/patch (rejected / ignored). Publish eligibility is **server** (Master Data EİDS requirement + EİDS `IsVerified`). Client `eligible` on EİDS start is `400`.

### Listing moderation_state (orthogonal)

| State | Public | Owner |
|---|---|---|
| `none` | follows owner status | normal |
| `restricted` | privacy-safe **not found** | owner readable; not archive |
| `removed` | privacy-safe **not found** | owner readable |

Restricted/removed listings cannot start new favorites, messages, or appointments. Listing `suspend` is **unsupported**.

---

## 3. Media (listing image)

`pending_upload` → `uploaded` → `processing` → `ready` | `rejected` | `deleted`

Public listing `media[]` contains **processed ready URLs only**. Storage disabled: empty `media` on public GET; initiate upload → 503. Video/CDN is deferred.

---

## 4. Verified interaction

### Appointment

`requested` → `accepted` | `rejected` | `cancelled`  
`accepted` → `cancelled` | `completed` | `no_show`

Participants: requester + listing provider. No self-appointment.

### Challenge (QR / OTP)

| Method | Start | Finish | At rest |
|---|---|---|---|
| `qr` | opaque token once; client SVG (`qrPayload`) | token | hash-only |
| `otp` | 6-digit code once | numeric input | hash-only |

TTL default **5 minutes**. States: not started · pending (open flow) · verified (`completed`) · expired · consumed/failed.

### Flow types

| Type | Drives Trust level? | Reviews? |
|---|---|---|
| `listing_inspection` | **yes** (count) | yes, requester, 24h window |
| `transaction` | history only | no |
| `delivery` | history only | no |

Flow status: `open` | `completed`. Listing-owner starts transaction/delivery verification from an authenticated listing conversation.

---

## 5. Review

| State | Meaning |
|---|---|
| Eligible | completed `listing_inspection`; reviewer is requester; listing published; within 24h; not already reviewed |
| Not eligible | window expired, already reviewed, wrong role, unpublished listing, non-inspection type |
| Submitted | one review per interaction; listing accuracy 1–5 and provider service 1–5 kept **separate** |
| Public body | published listing only; **no reviewer identity** |
| Moderation of reviews | **not implemented** (no review-target reports) |

Public aggregates: listing accuracy summary. User Trust shows provider-service average separately from Trust **level**.

---

## 6. Trust / Güven Pasaportu

**No public 0–100 score. No “100% güvenilir”. No “garantili kullanıcı”.**

Level from **listing_inspection verified-interaction count only** (`DefaultLevelPolicy`):

| Level | Threshold | Consumer label (`trustLevelLabel`) |
|---|---|---|
| `new` | 0 | Yeni |
| `verified` | ≥ **1** | Doğrulanmış |
| `established` | ≥ **5** | Yerleşik Güven |

Transaction/delivery history may be stored internally; it **must not** change visible level. Review counters/average are displayed **beside** level, never mixed into a hidden score. Listing accuracy is **not** on user Trust.

Missing Trust row → new/zero. Restricted/removed public profile → public Trust 404 (data not deleted). Self `GET /v1/trust/me` still works.

---

## 7. Need

`draft` → `open` → `fulfilled` | `cancelled` | `expired`

Owner HTTP only. No public discovery. Optional published `categoryId` + PostGIS point + radius 0.1–50 km.

---

## 8. Offered service / Business

**Business profile** (one per user): `draft` → `active` → `suspended` | `closed`. Public GET only if `active`.

**Offered service:** `draft` → `active` ⇄ `paused` → `closed`. Public only `active`. Closed cannot reactivate.

---

## 9. Offer

`submitted` → `withdrawn` | `accepted` | `rejected` | `expired`

Requester: list/accept/reject. Provider: create/mine/withdraw. Need stays **open** on accept. Eligibility via Businesses `OfferEligibility`.

---

## 10. Transaction

`pending` → `active` → `completed` | `cancelled`

Create: **requester-only** from accepted Offer (`POST /v1/offers/{offerId}/transaction`). Not auto-created. Create/start require Need `open`. Complete allows `open` or already `fulfilled`; rejects cancelled/expired/draft. Need fulfillment is outbox/worker, not a client write.

---

## 11. Payment

`pending` → `authorized` → `captured` | `failed` | `cancelled`

One intent per priced Transaction. **No** `/capture` or `/authorize` HTTP. **No PSP, no card data, no escrow, no refunds.** Mark UI `PAYMENT_PROVIDER_PENDING`. Never fake `captured`.

---

## 12. Delivery

`pending` → `ready` → `in_transit` → `delivered` | `cancelled`

Methods: `handoff` | `courier` | `pickup` (state labels, not branded carriers). Participant-requested; not auto-created. `delivered` is provider logistics state; requester acceptance is **not** implied. Cancelled Transaction refuses provider progression. Mark UI `DELIVERY_PROVIDER_PENDING`. No GPS/address/PoD.

---

## 13. Dispute (commercial, not moderation)

`open` → `under_review` → `resolved` → `closed`  
or `open`/`under_review` → `withdrawn`

14-day window. Reasons: `item_or_service_not_as_described` · `non_delivery` · `damaged_or_incomplete` · `payment_issue` · `cancellation_issue` · `other`. Resolution does **not** refund, reverse payment, cancel Transaction, change Trust, or open Moderation.

Staff: support/admin `disputes.read` / `disputes.review`. Moderators **cannot** review disputes.

---

## 14. Notifications

### Event vs channel

Event = catalog type (server-owned). Channel = `in_app` | `web_push` | `mobile_push` | `email` | `sms`. Preference ≠ browser permission ≠ marketing consent.

### Inbox item (user-facing)

| Field shown | Notes |
|---|---|
| unread / read | `readAt` null vs set |
| `eventType`, `purpose`, `templateKey`, allowlisted `variables` | no OTP/secrets |
| `resourceRef` | opaque; map conceptually by event type — do not dump raw UUIDs as titles |

### Channel delivery (mostly internal)

`pending` → `processing` → `accepted` | `retryable_failed` | `permanently_failed` | `suppressed`

User-facing: destination unavailable / not delivered only as generic copy. Do not expose suppression reason enums, provider names (SES, Netgsm, FCM, APNs), or `ProviderRef`.

### Catalog events (V1)

`security.login_new` · `security.passkey_added` · `security.passkey_removed` · `security.password_reset_completed` · `security.sessions_revoked` · `security.account_security_event` · `messaging.message_received` · `offer.received` · `offer.accepted` · `offer.rejected` · `transaction.created` · `transaction.completed` · `transaction.cancelled` · `delivery.status_changed` · `dispute.updated` · `moderation.action_applied` · `saved_search.match` (**producer deferred**) · `marketing.campaign` · Identity verification templates (OTP path, not inbox).

`in_app` cannot be turned off. Web Push permission is **user-initiated**.

---

## 15. EİDS (listing property / vehicle)

| Status | Suggested copy | Publish |
|---|---|---|
| (not started / required) | Doğrulama gerekli | no |
| `pending` | Doğrulama bekleniyor | no |
| `in_progress` | Doğrulama bekleniyor | no |
| `verified` | Doğrulandı | yes |
| `failed` | Doğrulama başarısız | no |
| `expired` | Doğrulamanın süresi doldu | no |
| `unavailable` | Doğrulama hizmeti geçici olarak kullanılamıyor | no |

Types: `property` | `vehicle` (never substitute). Failure codes may drive copy but **must not** dump provider payloads. Unconfigured gateway → `unavailable`, **never** `verified`.

**Forbidden in UI:** TCKN, raw government response, provider credentials/tokens, `subject_ref`, decision signature, private/public keys, replay state, service token, internal `decision_id` unless a later product decision explicitly allows an opaque correlation id (current owner DTO exposes `verificationId` only — listing-owned, not TR protocol id).

---

## 16. Moderation

### Report

`submitted` → `triaged` → `closed`  
Targets: `listing` | `public_profile`. Reasons: `spam` · `scam_or_fraud` · `prohibited_item` · `harassment` · `impersonation` · `inappropriate_content` · `other`. Self-report forbidden. Duplicate window 24h.

### Case

`open` → `investigating` → `resolved` → `closed`  
Priority: `low` | `normal` | `high` | `urgent`.

### Action

Types: `no_action` · `warning` · `restrict` · `suspend` · `remove`  
Status: `proposed` → `approved` → `executed` (or `cancelled` from proposed/approved).  
Listing/profile `suspend` **unsupported**. `no_action` audit-only. `warning` in-app notify, no hide, no Trust change. `restrict`/`remove` hide public surfaces.

Approve requires `moderation.action.approve` (senior_moderator, admin). Moderator can propose/execute other transitions with `case.write` but **cannot approve**.

### Appeal

`submitted` → `under_review` → `accepted` | `rejected`  
or `submitted` → `withdrawn`  
14-day window; one active appeal per action/user. Accept restore: Listings/Identity `ClearModerationState` first; executed action stays `executed`. Staff withdraw is not a staff transition.

---

## 17. Staff IAM

Roles: `moderator` · `senior_moderator` · `support` · `admin`.  
Separate realm from consumer Identity. Unwired production → staff HTTP **404**. Consumer cookie + `X-Staff-*` never elevates.

| Role | Moderation | Disputes | Identity/Listings/Trust read |
|---|---|---|---|
| moderator | report/case read+write; **no** approve/appeal review | 403 | yes |
| senior_moderator | + approve + appeal review | 403 | yes |
| support | 403 | read+review | 403 |
| admin | union of listed permissions (not a wildcard bypass) | yes | yes |
