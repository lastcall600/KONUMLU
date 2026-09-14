# Authorization matrix

Source of truth for implemented HTTP authorization. Outcomes are taken from handlers, contracts, and tests. If a cell cannot be determined from those sources it is `NEEDS_CONFIRMATION`.

**Legend**

| Token | Meaning |
|---|---|
| ALLOW | Authenticated request succeeds (2xx), subject to lifecycle/validation |
| 401 | Unauthenticated |
| 403 | Authenticated but forbidden (CSRF/origin, missing permission, non-staff) |
| 404 | Privacy-safe hide or route absent |
| 409 | Lifecycle / state conflict |
| 400 | Client spoof / bad request (not an authz grant) |
| N/A | Actor class does not apply |
| UNREG | Staff routes not registered (no IdP adapter) → 404 |

**Staff wiring:** Production/staging reject development Staff IdP. Incomplete Staff IdP config fails closed. Consumer cookies are not staff credentials. `X-Staff-*` never elevates.

**Consumer mutations:** Browser POSTs/PATCHes require allowed `Origin` and CSRF (`X-CSRF-Token` matches `__Host-konumlu_csrf`). Missing origin or CSRF → **403**. Missing session → **401**.

**Role permissions** (from `backend/internal/staffauth/contracts/roles.go`):

| Permission | moderator | senior_moderator | support | admin |
|---|---|---|---|---|
| `moderation.report.read` | yes | yes | no | yes |
| `moderation.case.read` | yes | yes | no | yes |
| `moderation.case.write` | yes | yes | no | yes |
| `moderation.action.approve` | no | yes | no | yes |
| `moderation.appeal.review` | no | yes | no | yes |
| `disputes.read` | no | no | yes | yes |
| `disputes.review` | no | no | yes | yes |
| `identity.profile.read` | yes | yes | no | yes |
| `listings.read` | yes | yes | no | yes |
| `trust.read` | yes | yes | no | yes |

Zero `StaffID` grants nothing, including admin.

---

## 1. Consumer identity / auth

| Endpoint | Unauth | Auth owner | Auth other | Disabled/deleted |
|---|---|---|---|---|
| POST `/v1/auth/passkey/login/begin` | ALLOW (origin) | ALLOW | ALLOW | N/A |
| POST `/v1/auth/passkey/login/finish` | ALLOW → session | ALLOW | ALLOW | Fail closed (no session) |
| POST `/v1/auth/password/login` | 401 generic if bad | ALLOW → session | ALLOW | 401 generic |
| POST `/v1/auth/passkey/register/*` | 401 | ALLOW after recent-strong **or** first-passkey bootstrap | N/A (session-bound) | NEEDS_CONFIRMATION (session resolve) |
| POST `/v1/auth/passkey/register/password-reauth` | 401 | ALLOW for zero-passkey + password (generic 401 if wrong); established passkey **403** | N/A | NEEDS_CONFIRMATION |
| GET `/v1/auth/passkeys` | 401 | ALLOW (self metadata) | other user 404/empty via owner list | NEEDS_CONFIRMATION |
| POST `/v1/auth/passkeys/{id}/remove` | 401 | ALLOW after recent-strong; last-factor 409 | other id **404** | NEEDS_CONFIRMATION |
| POST `/v1/auth/step-up/passkey/*` | 401 | ALLOW (own passkey) | cannot start/finish another user | NEEDS_CONFIRMATION |
| POST `/v1/auth/signup/*` | ALLOW (origin; existence-hiding) | N/A | N/A | N/A |
| POST `/v1/auth/password/reset/*` | ALLOW (origin) | N/A | N/A | NEEDS_CONFIRMATION (generic vs distinct) |
| GET `/v1/auth/session` | 401 | ALLOW | N/A | NEEDS_CONFIRMATION |
| GET `/v1/auth/sessions` | 401 | ALLOW (own active) | cannot read other user | NEEDS_CONFIRMATION |
| POST `/v1/auth/sessions/{id}/revoke` | 401 | ALLOW (own); current logs out | other user **404** | NEEDS_CONFIRMATION |
| POST `/v1/auth/sessions/revoke-others` | 401 | ALLOW (keeps current) | N/A | NEEDS_CONFIRMATION |
| POST `/v1/auth/logout` | 401 without session cookie; origin+CSRF required | ALLOW | N/A | NEEDS_CONFIRMATION |
| GET `/v1/profile/me` | 401 | ALLOW (incl. restricted/removed profile) | N/A | Staff sees disabled; public 404 |
| PATCH `/v1/profile/me` | 401 | ALLOW + CSRF | N/A | NEEDS_CONFIRMATION |
| GET `/v1/public/profiles/{id}` | ALLOW if public none | ALLOW | ALLOW | restricted/removed → 404 |
| GET `/v1/public/profiles/{id}/trust` | ALLOW if public none | ALLOW | ALLOW | restricted/removed → 404 |
| GET `/v1/trust/me` | 401 | ALLOW | N/A | NEEDS_CONFIRMATION |

Wrong origin on auth POSTs → **403**.

---

## 2. Staff IAM (routes registered)

| Endpoint | Unauth | Consumer cookie + `X-Staff-*` | support | moderator | senior_moderator | admin |
|---|---|---|---|---|---|---|
| GET `/v1/staff/moderation/reports` | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| GET `/v1/staff/moderation/reports/{id}` | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| POST `/v1/staff/moderation/reports/{id}/status` | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| GET `/v1/staff/moderation/cases` | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| POST `/v1/staff/moderation/cases` | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| GET `/v1/staff/moderation/cases/{id}` | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| POST case reports/status/priority/assignment/notes | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| POST/GET case evidence | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| POST `/v1/staff/moderation/cases/{id}/actions` | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| GET actions | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| POST action status `approved` | 401 | 401 | 403 | **403** | ALLOW | ALLOW |
| POST action status other (e.g. executed from approved) | 401 | 401 | 403 | ALLOW (`case.write`) | ALLOW | ALLOW |
| GET case appeals | 401 | 401 | 403 | ALLOW | ALLOW | ALLOW |
| POST appeal status | 401 | 401 | 403 | **403** | ALLOW | ALLOW |
| GET `/v1/staff/identity/profiles/{id}` | 401 | 401 | **403** | ALLOW (incl. disabled/restricted) | ALLOW | ALLOW |
| GET `/v1/staff/listings/{id}` | 401 | 401 | **403** | ALLOW | ALLOW | ALLOW |
| GET `/v1/staff/listings?ownerPublicProfileId=` | 401 | 401 | **403** | ALLOW; missing query 400; unknown owner 404 | ALLOW | ALLOW |
| GET `/v1/staff/trust/profiles/{id}` | 401 | 401 | **403** | ALLOW | ALLOW | ALLOW |
| GET `/v1/staff/review-aggregates/listings/{id}` | 401 | 401 | **403** | ALLOW | ALLOW | ALLOW |
| GET `/v1/staff/disputes/{id}` | 401 | 401 | ALLOW | **403** | **403** | ALLOW |
| POST `/v1/staff/disputes/{id}/review` | 401 | 401 | ALLOW | **403** | **403** | ALLOW |
| POST dispute resolve/close/evidence | 401 | 401 | ALLOW | **403** | **403** | ALLOW |

When staff routes are **unregistered**: all of the above → **UNREG (404)** even with consumer session.

Bearer with provider down → **503** (`unavailable`). Invalid bearer → **401**. Authenticated staff without permission → **403**.

---

## 3. Listings (consumer)

| Endpoint | Unauth | Owner | Non-owner | Restricted/removed listing |
|---|---|---|---|---|
| POST `/v1/listings` | 401 | ALLOW (draft; ignore client owner/status) | N/A | N/A |
| GET `/v1/listings/{id}` | 401 | ALLOW | **404** | Owner still ALLOW |
| PATCH `/v1/listings/{id}` | 401 | ALLOW + CSRF | **404** | Owner; public still hidden |
| POST `.../location` | 401 | ALLOW + CSRF | **404** | Owner |
| POST `.../media` | 401 | ALLOW + CSRF | **404** | Owner |
| POST `.../ready` | 401 | ALLOW + CSRF | **404** | Owner |
| POST `.../publish` | 401 | ALLOW if ready/EİDS; else **409** | **404** | Owner; public/search omit |
| POST `.../archive` | 401 | ALLOW + CSRF | **404** | Owner |
| GET `/v1/public/listings/{id}` | ALLOW if published+none | ALLOW if published | ALLOW if published | **404** (privacy) |

---

## 4. EİDS

| Endpoint | Unauth | Owner | Non-owner |
|---|---|---|---|
| POST `/v1/listings/{id}/eids-verifications` | 401 | ALLOW + CSRF (idempotent); unconfigured = unavailable not verified | **404** |
| GET `/v1/listings/{id}/eids-verification` | 401 | ALLOW | **404** |
| POST `/internal/tr-compliance/v1/verification-decisions` | **401** without TR ingress Bearer; cookies/staff Bearer **401** | N/A (not consumer) | N/A |

No staff EİDS HTTP. No client eligibility spoof (rejected).

---

## 5. Verified

| Endpoint | Unauth | Requester (own) | Listing provider | Unrelated |
|---|---|---|---|---|
| POST `/v1/verified/appointments` | 401 | ALLOW + CSRF | N/A | N/A (listing eligibility) |
| GET appointments / `{id}` | 401 | ALLOW if participant | ALLOW if participant | **404** |
| POST `.../accept` or `reject` | 401 | **404** | ALLOW + CSRF | **404** |
| POST `.../cancel` | 401 | ALLOW if allowed role | ALLOW if allowed role | **404** |
| POST `.../verification/start` | 401 | not provider | ALLOW (QR/OTP) | **404** |
| POST `.../verification/finish` | 401 | ALLOW with secret | NEEDS_CONFIRMATION | **404** / bad secret |
| POST transaction/delivery verification start | 401 | listing owner + CSRF | N/A | **404** |
| GET/finish verification-flows | 401 | participant | participant | **404** |
| GET `/v1/verified/interactions/{id}` | 401 | participant | participant | **404** |

---

## 6. Moderation (consumer)

| Endpoint | Unauth | Reporter / appellant (own) | Other user | Linked subject owner |
|---|---|---|---|---|
| POST `/v1/moderation/reports` | 401 | ALLOW + CSRF | N/A | N/A |
| GET `/v1/moderation/reports/mine` | 401 | ALLOW (own rows; no staffNote) | N/A | N/A |
| POST `/v1/moderation/appeals` | 401 | N/A | **404** if not subject owner | ALLOW of executed action |
| GET `/v1/moderation/appeals/mine` | 401 | ALLOW | N/A | N/A |
| GET `/v1/moderation/appeals/{id}` | 401 | ALLOW own | **404** | N/A |
| POST `.../withdraw` | 401 | ALLOW own | **404** | N/A |
| `/v1/staff/moderation/*` on consumer mux | **404** | **404** | **404** | **404** |

---

## 7. Transactions / payments / deliveries / disputes

| Endpoint | Unauth | Requester participant | Provider participant | Unrelated |
|---|---|---|---|---|
| POST `/v1/offers/{offerId}/transaction` | 401 | ALLOW if accepted+Need open; else **409** | **404** | **404** |
| GET `/v1/transactions/{id}` | 401 | ALLOW | ALLOW | **404** |
| POST `.../start` | 401 | ALLOW if Need open; else **409** | ALLOW if Need open | **404** |
| POST `.../complete` | 401 | ALLOW | ALLOW | **404** |
| POST `.../cancel` | 401 | ALLOW unless completed → **409** | ALLOW unless completed | **404** |
| POST `.../payment` | 401 | ALLOW if priced; unpriced **409** | ALLOW if priced | **404** |
| GET `/v1/payments/{id}` | 401 | ALLOW | ALLOW | **404** |
| POST `.../delivery` | 401 | ALLOW if eligible | ALLOW if eligible | **404** |
| GET `/v1/deliveries/{id}` | 401 | ALLOW | ALLOW | **404** |
| POST delivery ready/in-transit/delivered | 401 | NEEDS_CONFIRMATION (provider progression) | ALLOW if eligible; cancelled txn refused | **404** |
| POST `.../dispute` | 401 | ALLOW in window | ALLOW in window | **404** |
| GET `/v1/disputes/{id}` | 401 | ALLOW | ALLOW | **404** |
| POST payment `/capture` or `/authorize` | **404** (unimplemented) | **404** | **404** | **404** |

---

## 8. Media

| Endpoint | Unauth | Owner | Non-owner / foreign listing |
|---|---|---|---|
| POST `/v1/media/listing-images` | 401 | ALLOW + CSRF; owner from session | **404** if listing not owned |
| POST `.../confirm` | 401 | ALLOW + CSRF | **404** |
| GET `/v1/media/listing-images/{assetId}` | 401 | ALLOW | **404** |

Storage disabled → **503** on initiate (not an authz grant).

---

## 9. Messaging / favorites / needs / offers / businesses (representative)

| Endpoint | Unauth | Owner / participant | Other |
|---|---|---|---|
| POST `/v1/messaging/conversations` | 401 | ALLOW + CSRF | missing listing **404** |
| GET/POST messages | 401 | conversation participant | **404** |
| Favorites / saved-searches CRUD | 401 | session-owned | other id **404** |
| Needs owner HTTP | 401 | owner | **404** |
| POST `/v1/needs/{id}/offers` | 401 | eligible provider | NEEDS_CONFIRMATION vs 404 |
| POST offer accept/reject | 401 | need requester | **404** / not requester |
| POST offer withdraw | 401 | offering provider | **404** |
| Business owner HTTP | 401 | owner | **404** |
| GET `/v1/public/businesses/{id}` | ALLOW if active | ALLOW if active | non-active **404** |

---

## 10. Notifications (consumer)

| Endpoint | Unauth | Owner / participant | Other |
|---|---|---|---|
| GET `/v1/notification-preferences` | 401 | ALLOW (self effective settings) | cannot list another user |
| PATCH `/v1/notification-preferences` | 401 | ALLOW + CSRF | body `userId` **400**; other session cannot write A |
| GET `/v1/notification-consents` | 401 | ALLOW (self current) | cannot read another user |
| POST `/v1/notification-consents` | 401 | ALLOW + CSRF (append-only) | cannot append as another user |
| GET `/v1/notifications` | 401 | ALLOW (own inbox) | other user's items omitted |
| POST `/v1/notifications/{id}/read` | 401 | ALLOW + CSRF; already-read OK | foreign id **404** |
| POST `/v1/notifications/read-all` | 401 | ALLOW + CSRF (own unread) | does not touch other users |
| POST `/v1/push-endpoints` | 401 | ALLOW + CSRF; body `userId` **400** | other user's active endpoint hash **409** |
| GET `/v1/push-endpoints` | 401 | ALLOW (own active metadata) | other user's endpoints omitted |
| DELETE `/v1/push-endpoints/{id}` | 401 | ALLOW + CSRF; already-revoked OK | foreign id **404** |

Wrong origin / missing CSRF on notification mutations → **403**.

---

## Coverage map

Machine-tested in this baseline (see `authorization-security` CI job):

- Exhaustive staff role × permission table (`staffauth/contracts`)
- HTTP staff default-policy role matrix: listings 360, identity 360, trust, moderation approve/appeal, disputes
- Consumer cookie + `X-Staff-*` ignored (authorizer + staff HTTP + production mux)
- Production/staging reject dev Staff IdP; incomplete IdP fail-closed
- Listing owner vs non-owner mutations (404)
- Transaction/payment stranger 404; verified foreign 404; media non-owner 404; appeal unrelated 404

`NEEDS_CONFIRMATION` rows are not guessed and are not treated as PASS.
