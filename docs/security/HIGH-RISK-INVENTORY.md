# High-risk security inventory

Repository-owned catalog of implemented high-risk HTTP surfaces. Endpoints are taken from `mux.HandleFunc` registrations and tests. Do not treat this file as an API spec for unimplemented product work.

Authorization outcomes live in [AUTHORIZATION-MATRIX.md](./AUTHORIZATION-MATRIX.md).

**Staff HTTP note:** `/v1/staff/*` is registered on the production mux only when a `StaffIdentityProvider` adapter is wired. Unconfigured production/staging returns **404** for those paths (fail-closed, routes absent). Outcomes below assume the staff mux is registered.

---

## Consumer identity / auth

| Method | Path | Risk | Notes |
|---|---|---|---|
| POST | `/v1/auth/passkey/register/begin` | Session bind, CSRF, origin, rate limit | Authenticated passkey registration |
| POST | `/v1/auth/passkey/register/finish` | Session bind, CSRF, origin, rate limit | Ceremony bound to session user |
| POST | `/v1/auth/passkey/login/begin` | Public auth, origin, rate limit | Discoverable login |
| POST | `/v1/auth/passkey/login/finish` | Session issuance, origin, rate limit | Issues `__Host-` cookies |
| POST | `/v1/auth/password/login` | Session issuance, origin, rate limit | Generic 401; disabled/deleted generic 401 |
| POST | `/v1/auth/signup/verification/start` | Public, origin, rate limit | Existence-hiding |
| POST | `/v1/auth/signup/verification/finish` | Public, origin, rate limit | Hash-only proof |
| POST | `/v1/auth/signup/complete` | Account create + session, rate limit | Proof consume |
| POST | `/v1/auth/password/reset/start` | Recovery, origin, rate limit | Hash-only proof |
| POST | `/v1/auth/password/reset/verify` | Recovery, origin, rate limit | |
| POST | `/v1/auth/password/reset/complete` | Recovery, session revoke, rate limit | No auto-login |
| GET | `/v1/auth/session` | Session read | Cookie session |
| POST | `/v1/auth/logout` | Session destroy, CSRF, origin | |
| GET | `/v1/profile/me` | Identity self-read | Session |
| PATCH | `/v1/profile/me` | Identity self-write, CSRF | Restricted/removed still self-readable |
| GET | `/v1/public/profiles/{publicProfileId}` | Public identity | Restricted/removed → privacy 404 |
| GET | `/v1/public/profiles/{publicProfileId}/trust` | Public Trust | Restricted/removed → privacy 404 |
| GET | `/v1/trust/me` | Authenticated Trust | Session |

## Staff IAM

Staff authenticates with `Authorization: Bearer` only. Consumer cookies and `X-Staff-*` headers never grant staff rights.

| Method | Path | Required permission |
|---|---|---|
| GET | `/v1/staff/moderation/reports` | `moderation.report.read` |
| GET | `/v1/staff/moderation/reports/{reportId}` | `moderation.report.read` |
| POST | `/v1/staff/moderation/reports/{reportId}/status` | `moderation.case.write` |
| GET | `/v1/staff/moderation/cases` | `moderation.case.read` |
| POST | `/v1/staff/moderation/cases` | `moderation.case.write` |
| GET | `/v1/staff/moderation/cases/{caseId}` | `moderation.case.read` |
| POST | `/v1/staff/moderation/cases/{caseId}/reports` | `moderation.case.write` |
| POST | `/v1/staff/moderation/cases/{caseId}/status` | `moderation.case.write` |
| POST | `/v1/staff/moderation/cases/{caseId}/priority` | `moderation.case.write` |
| POST | `/v1/staff/moderation/cases/{caseId}/assignment` | `moderation.case.write` |
| POST | `/v1/staff/moderation/cases/{caseId}/notes` | `moderation.case.write` |
| POST | `/v1/staff/moderation/cases/{caseId}/evidence` | `moderation.case.write` |
| GET | `/v1/staff/moderation/cases/{caseId}/evidence` | `moderation.case.read` |
| POST | `/v1/staff/moderation/cases/{caseId}/actions` | `moderation.case.write` |
| GET | `/v1/staff/moderation/cases/{caseId}/actions` | `moderation.case.read` |
| GET | `/v1/staff/moderation/cases/{caseId}/actions/{actionId}` | `moderation.case.read` |
| POST | `/v1/staff/moderation/cases/{caseId}/actions/{actionId}/status` | `moderation.case.write`; **approve** requires `moderation.action.approve` |
| GET | `/v1/staff/moderation/cases/{caseId}/appeals` | `moderation.case.read` |
| GET | `/v1/staff/moderation/cases/{caseId}/appeals/{appealId}` | `moderation.case.read` |
| POST | `/v1/staff/moderation/cases/{caseId}/appeals/{appealId}/status` | `moderation.appeal.review` |
| GET | `/v1/staff/identity/profiles/{publicProfileId}` | `identity.profile.read` |
| GET | `/v1/staff/listings` | `listings.read` (requires `ownerPublicProfileId`) |
| GET | `/v1/staff/listings/{listingId}` | `listings.read` |
| GET | `/v1/staff/trust/profiles/{publicProfileId}` | `trust.read` |
| GET | `/v1/staff/review-aggregates/listings/{listingId}` | `listings.read` |
| GET | `/v1/staff/disputes/{disputeId}` | `disputes.read` |
| GET | `/v1/staff/disputes/{disputeId}/evidence` | `disputes.read` |
| POST | `/v1/staff/disputes/{disputeId}/review` | `disputes.review` |
| POST | `/v1/staff/disputes/{disputeId}/resolve` | `disputes.review` |
| POST | `/v1/staff/disputes/{disputeId}/close` | `disputes.review` |
| POST | `/v1/staff/disputes/{disputeId}/evidence` | `disputes.review` |

## Listings

| Method | Path | Risk |
|---|---|---|
| POST | `/v1/listings` | Owner create; owner from session |
| GET | `/v1/listings/{listingId}` | Owner read; non-owner privacy 404 |
| PATCH | `/v1/listings/{listingId}` | Owner mutation; CSRF |
| POST | `/v1/listings/{listingId}/location` | Owner mutation; CSRF |
| POST | `/v1/listings/{listingId}/media` | Owner attach; CSRF |
| POST | `/v1/listings/{listingId}/ready` | Owner lifecycle; CSRF |
| POST | `/v1/listings/{listingId}/publish` | Owner publish; CSRF; EİDS gate |
| POST | `/v1/listings/{listingId}/archive` | Owner lifecycle; CSRF |
| GET | `/v1/public/listings/{listingId}` | Public published-only; restricted/removed privacy 404 |

## EİDS

| Method | Path | Risk |
|---|---|---|
| POST | `/v1/listings/{listingId}/eids-verifications` | Owner start; CSRF; unconfigured provider = unavailable, never verified |
| GET | `/v1/listings/{listingId}/eids-verification` | Owner read; non-owner privacy 404 |

No admin bypass route exists.

## Verified

| Method | Path | Risk |
|---|---|---|
| POST | `/v1/verified/appointments` | Session + CSRF; listing eligibility |
| GET | `/v1/verified/appointments` | Session participant list |
| GET | `/v1/verified/appointments/{appointmentId}` | Participant-only; foreign 404 |
| POST | `/v1/verified/appointments/{appointmentId}/accept` | Provider; CSRF; foreign 404 |
| POST | `/v1/verified/appointments/{appointmentId}/reject` | Provider; CSRF |
| POST | `/v1/verified/appointments/{appointmentId}/cancel` | Participant; CSRF |
| POST | `/v1/verified/appointments/{appointmentId}/verification/start` | Provider QR/OTP; hash-only at rest |
| POST | `/v1/verified/appointments/{appointmentId}/verification/finish` | Requester secret |
| POST | `/v1/verified/transaction/verification/start` | Listing-owner start; CSRF |
| POST | `/v1/verified/delivery/verification/start` | Listing-owner start; CSRF |
| GET | `/v1/verified/verification-flows` | Participant list |
| GET | `/v1/verified/verification-flows/{flowId}` | Participant-only |
| POST | `/v1/verified/verification-flows/{flowId}/verification/finish` | Participant secret |
| GET | `/v1/verified/interactions/{interactionId}` | Participant-only |

## Moderation (consumer)

| Method | Path | Risk |
|---|---|---|
| POST | `/v1/moderation/reports` | Session + CSRF; reporter from session |
| GET | `/v1/moderation/reports/mine` | Reporter-only; no staff notes |
| POST | `/v1/moderation/appeals` | Subject-owner of executed action; CSRF |
| GET | `/v1/moderation/appeals/mine` | Appellant-only |
| GET | `/v1/moderation/appeals/{appealId}` | Appellant-only; unrelated 404 |
| POST | `/v1/moderation/appeals/{appealId}/withdraw` | Appellant-only; unrelated 404 |

Consumer mux does not register `/v1/staff/moderation/*` or `/v1/moderation/cases`.

## Transactions / payments / deliveries / disputes

| Method | Path | Risk |
|---|---|---|
| POST | `/v1/offers/{offerId}/transaction` | Requester of accepted offer; stranger/provider 404 |
| GET | `/v1/transactions` | Participant list |
| GET | `/v1/transactions/{transactionId}` | Participant-only; stranger 404 |
| POST | `/v1/transactions/{transactionId}/start` | Participant; CSRF; Need-open gate |
| POST | `/v1/transactions/{transactionId}/complete` | Participant; CSRF |
| POST | `/v1/transactions/{transactionId}/cancel` | Participant; CSRF; completed → 409 |
| POST | `/v1/transactions/{transactionId}/payment` | Participant; CSRF; stranger 404; no card fields |
| GET | `/v1/payments` | Participant list |
| GET | `/v1/payments/{paymentId}` | Participant-only |
| POST | `/v1/transactions/{transactionId}/delivery` | Participant; CSRF |
| GET | `/v1/deliveries` | Participant list |
| GET | `/v1/deliveries/{deliveryId}` | Participant-only |
| POST | `/v1/deliveries/{deliveryId}/ready` | Provider progression; CSRF |
| POST | `/v1/deliveries/{deliveryId}/in-transit` | Provider; CSRF |
| POST | `/v1/deliveries/{deliveryId}/delivered` | Provider; CSRF |
| POST | `/v1/deliveries/{deliveryId}/cancel` | Participant; CSRF |
| POST | `/v1/transactions/{transactionId}/dispute` | Participant; CSRF |
| GET | `/v1/disputes` | Participant list |
| GET | `/v1/disputes/{disputeId}` | Participant-only |
| GET | `/v1/disputes/{disputeId}/evidence` | Participant |
| POST | `/v1/disputes/{disputeId}/evidence` | Participant; CSRF |
| POST | `/v1/disputes/{disputeId}/withdraw` | Participant; CSRF |

`POST /v1/payments/{id}/capture` and `/authorize` are **not implemented** (404).

## Media

| Method | Path | Risk |
|---|---|---|
| POST | `/v1/media/listing-images` | Session owner; signed PUT; CSRF |
| POST | `/v1/media/listing-images/{assetId}/confirm` | Owner confirm; CSRF |
| GET | `/v1/media/listing-images/{assetId}` | Owner read; non-owner privacy 404 |

## Other authenticated surfaces (ownership-sensitive)

Implemented and session-scoped; see matrix for representative rows:

- Favorites, saved searches, messaging
- Needs (owner), offers (requester vs provider), businesses (owner)
- Reviews (eligibility), review-summary `/me`

Public catalog/search/review-summary routes are unauthenticated reads of published data only.
