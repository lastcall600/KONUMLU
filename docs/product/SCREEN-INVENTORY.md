# SCREEN-INVENTORY.md

> Complete V1 surface inventory for KONUMLU Product → Figma → Frontend.
> Authoritative behavior: [PRODUCT-UI-HANDOFF.md](./PRODUCT-UI-HANDOFF.md).
> Domain states: [STATE-MATRICES.md](./STATE-MATRICES.md).
>
> Repository truth wins. Visual mockups from prior design work are references only.

**Status legend**

| Code | Meaning |
|---|---|
| `BACKEND_AND_UI_PRESENT` | HTTP contract and consumer/admin route exist |
| `BACKEND_EXISTS_UI_PENDING` | Domain HTTP exists; no product-complete screen yet |
| `UI_CONTRACT_EXISTS_BACKEND_PENDING` | Figma may design; missing list/discovery HTTP or producer |
| `DEFERRED_NON_V1` | Do not design as a live V1 product silo |
| `INTERNAL_NON_UI` | Operators/protocol only; never a consumer screen |

**Audience**

`public` · `authenticated` · `owner-only` · `participant-only` · `staff-only` · `internal-only`

**Priority:** P0 first Fethiye web pilot · P1 shortly after · P2 non-blocking for web-first pilot.

Conceptual routes in *italics* are not implemented App Router pages. Do not treat them as frozen URLs.

---

## 1. Global / Discovery

| ID | Screen | Route | Audience | Status | P | Domain |
|---|---|---|---|---|---|---|
| D-01 | Home | `/` | public + authenticated | `BACKEND_AND_UI_PRESENT` (session stub; no discovery modules) | P0 | Identity session; future Search/Master Data |
| D-02 | Search results + map/list | `/ara` | public | `BACKEND_AND_UI_PRESENT` | P0 | Search, Master Data, Location, Favorites, Saved Search |
| D-03 | Category landing | *`/kategori/[code]`* | public | `UI_CONTRACT_EXISTS_BACKEND_PENDING` (filter via `categoryId` on `/ara`; no dedicated landing; taxonomy seed OPEN) | P1 | Master Data, Search |
| D-04 | Full-screen map / Nearby | `/ara` (map pane) | public | `BACKEND_AND_UI_PRESENT` (no clustering/geolocation/geocoder) | P0 | Search + MapLibre |
| D-05 | Favorites | `/favoriler` | authenticated | `BACKEND_AND_UI_PRESENT` | P0 | Favorites + Listings public eligibility |
| D-06 | Saved searches | `/kayitli-aramalar` | authenticated | `BACKEND_AND_UI_PRESENT` (no match alerts) | P0 | Saved Search |
| D-07 | Notification Center (inbox) | `/bildirimler` (inbox section) | authenticated | `BACKEND_EXISTS_UI_PENDING` (HTTP inbox exists; page is Web Push settings only) | P0 | Notifications |
| D-08 | Notification preferences / consents | *`/bildirimler` sections* | authenticated | `BACKEND_EXISTS_UI_PENDING` | P1 | Notifications policy |
| D-09 | Web Push permission / devices | `/bildirimler` | authenticated | `BACKEND_AND_UI_PRESENT` | P1 | Notifications push endpoints |

---

## 2. Auth / Account

| ID | Screen | Route | Audience | Status | P | Domain |
|---|---|---|---|---|---|---|
| A-01 | Login | `/giris` | public | `BACKEND_AND_UI_PRESENT` | P0 | Identity (passkey + password + Turnstile) |
| A-02 | Signup | `/kayit` | public | `BACKEND_AND_UI_PRESENT` (no passkey during signup) | P0 | Identity |
| A-03 | Password reset | `/sifre-sifirla` | public | `BACKEND_AND_UI_PRESENT` (no auto-login) | P0 | Identity |
| A-04 | Passkey enroll (authenticated) | *`/guvenlik`* | authenticated | `BACKEND_EXISTS_UI_PENDING` | P1 | Identity AUTH-B |
| A-05 | Step-Up ceremony | overlay on sensitive action | authenticated | `BACKEND_EXISTS_UI_PENDING` (HTTP exists; error is generic `forbidden`) | P1 | Identity |
| A-06 | Session / security settings | *`/guvenlik`* | owner-only | `BACKEND_EXISTS_UI_PENDING` | P1 | Identity sessions + passkeys |
| A-07 | Password re-auth (first passkey) | *`/guvenlik`* | authenticated | `BACKEND_EXISTS_UI_PENDING` | P1 | Identity bootstrap |
| A-08 | Onboarding | *none* | authenticated | `UI_CONTRACT_EXISTS_BACKEND_PENDING` (no dedicated flow; signup complete issues session) | P2 | Identity |
| A-09 | Profile edit (self) | *`/hesap`* | owner-only | `BACKEND_EXISTS_UI_PENDING` (`GET`/`PATCH /v1/profile/me`; displayName only) | P0 | Identity public profiles |
| A-10 | Public profile | `/profil/[publicProfileId]` | public | `BACKEND_AND_UI_PRESENT` | P0 | Identity + Trust |
| A-11 | Verification / security / privacy | *`/guvenlik` + `/bildirimler`* | owner-only | `BACKEND_EXISTS_UI_PENDING` (compose A-06 + D-08; not EİDS) | P1 | Identity + Notifications |
| A-12 | Turnstile challenge slot | on A-01/A-02/A-03 | public | `BACKEND_AND_UI_PRESENT` | P0 | Identity HumanChallenge |

Remember Me is **not** a KONUMLU requirement. Do not design it.

---

## 3. Listings

| ID | Screen | Route | Audience | Status | P | Domain |
|---|---|---|---|---|---|---|
| L-01 | Listing browse (search) | `/ara` | public | `BACKEND_AND_UI_PRESENT` | P0 | Search |
| L-02 | Listing detail | `/ilan/[listingId]` | public | `BACKEND_AND_UI_PRESENT` | P0 | Listings public + Media + Reviews + Messaging + Verified + Favorites |
| L-03 | Create/edit listing wizard | `/ilan-ver` | owner-only | `BACKEND_AND_UI_PRESENT` (lat/lng not MapLibre; EİDS not wired; stale lead copy) | P0 | Listings, Media, Master Data, Location |
| L-04 | Owner listing manage / publish | `/ilan-ver` step 5 | owner-only | `BACKEND_AND_UI_PRESENT` | P0 | Listings publish + archive |
| L-05 | Owner listing inventory | *`/ilanlarim`* | owner-only | `UI_CONTRACT_EXISTS_BACKEND_PENDING` (per-id GET only; no list-mine HTTP) | P1 | Listings |
| L-06 | Media gallery (public) | part of L-02 | public | `BACKEND_AND_UI_PRESENT` (processed images; no video) | P0 | Media |
| L-07 | Location / map on detail | part of L-02 | public | `UI_CONTRACT_EXISTS_BACKEND_PENDING` (coords on public DTO; detail has no MapLibre) | P1 | Location |
| L-08 | Seller linkage | part of L-02 | public | `BACKEND_AND_UI_PRESENT` | P0 | Identity PublicProfileResolver |

Verticals (Market / Jobs / Tourism / Events) are **Master Data categories**, not separate domains. See §11.

---

## 4. Business / Services

| ID | Screen | Route | Audience | Status | P | Domain |
|---|---|---|---|---|---|---|
| B-01 | Business public detail | *`/isletme/[businessId]`* | public | `BACKEND_EXISTS_UI_PENDING` | P1 | Businesses (`active` only) |
| B-02 | Business public services list | part of B-01 | public | `BACKEND_EXISTS_UI_PENDING` | P1 | Offered services (`active` only) |
| B-03 | Service public detail | *`/hizmet/[serviceId]`* | public | `BACKEND_EXISTS_UI_PENDING` | P1 | Offered services |
| B-04 | Business manage (mine) | *`/isletmem`* | owner-only | `BACKEND_EXISTS_UI_PENDING` (one profile per user) | P1 | Businesses |
| B-05 | Service catalog manage | *`/isletmem/hizmetler`* | owner-only | `BACKEND_EXISTS_UI_PENDING` | P1 | Offered services |
| B-06 | Quote / booking of a service | — | — | `DEFERRED_NON_V1` (no booking HTTP; Need+Offer is the V1 match path) | P2 | — |

Corporate Workspace (O-006) is V1.5. Do not design multi-seat company IAM.

---

## 5. Needs / Matching / Offers

| ID | Screen | Route | Audience | Status | P | Domain |
|---|---|---|---|---|---|---|
| N-01 | Need create / edit | *`/ihtiyac-olustur`* | owner-only | `BACKEND_EXISTS_UI_PENDING` | P1 | Needs |
| N-02 | Need owner list | *`/ihtiyaclarim`* | owner-only | `BACKEND_EXISTS_UI_PENDING` | P1 | Needs |
| N-03 | Need owner detail + candidates | *`/ihtiyac/[needId]`* | owner-only | `BACKEND_EXISTS_UI_PENDING` | P1 | Needs + Businesses CandidateDiscovery |
| N-04 | Public Need marketplace | — | public | `DEFERRED_NON_V1` (explicitly no public Need discovery) | P2 | — |
| N-05 | Provider offer create | *`/ihtiyac/[needId]/teklif`* | authenticated eligible provider | `BACKEND_EXISTS_UI_PENDING` | P1 | Offers + OfferEligibility |
| N-06 | Requester offer comparison | part of N-03 | owner-only | `BACKEND_EXISTS_UI_PENDING` | P1 | Offers |
| N-07 | Provider my offers | *`/tekliflerim`* | owner-only (provider) | `BACKEND_EXISTS_UI_PENDING` | P1 | Offers |

Need stays **open** on offer accept. Transaction is a separate requester action.

---

## 6. Messaging

| ID | Screen | Route | Audience | Status | P | Domain |
|---|---|---|---|---|---|---|
| M-01 | Conversation list | `/mesajlar` | participant-only | `BACKEND_AND_UI_PRESENT` | P0 | Messaging |
| M-02 | Conversation detail | `/mesajlar/[conversationId]` | participant-only | `BACKEND_AND_UI_PRESENT` | P0 | Messaging + Verified flows |
| M-03 | Start thread from listing | CTA on L-02 | authenticated ≠ owner | `BACKEND_AND_UI_PRESENT` | P0 | Messaging |

No realtime, typing, presence, attachments, audio/video calling. Unread count exists. Read receipts beyond per-user last-read are not a product surface.

---

## 7. Verified interaction / Reviews / Trust

| ID | Screen | Route | Audience | Status | P | Domain |
|---|---|---|---|---|---|---|
| V-01 | Appointments list/detail | `/randevular` | participant-only | `BACKEND_AND_UI_PRESENT` | P0 | Verified |
| V-02 | QR/OTP challenge | part of V-01 + M-02 | participant-only | `BACKEND_AND_UI_PRESENT` | P0 | Verified |
| V-03 | Transaction/delivery verification | part of M-02 | listing owner start; participant finish | `BACKEND_AND_UI_PRESENT` | P1 | Verified |
| V-04 | Review create | `/degerlendirme/[verifiedInteractionId]` | requester, eligible | `BACKEND_AND_UI_PRESENT` | P0 | Reviews (`listing_inspection` only) |
| V-05 | My reviews | `/degerlendirmelerim` | owner-only | `BACKEND_AND_UI_PRESENT` | P1 | Reviews |
| V-06 | Public listing reviews | part of L-02 | public | `BACKEND_AND_UI_PRESENT` (no reviewer identity) | P0 | Reviews |
| V-07 | Güven Pasaportum | `/guven-pasaportum` | authenticated | `BACKEND_AND_UI_PRESENT` | P0 | Trust |
| V-08 | Public Güven Pasaportu | part of A-10 | public | `BACKEND_AND_UI_PRESENT` | P0 | Trust |

---

## 8. Transactions / Payments / Delivery / Disputes

| ID | Screen | Route | Audience | Status | P | Domain |
|---|---|---|---|---|---|---|
| T-01 | Transaction list | *`/islemlerim`* | participant-only | `BACKEND_EXISTS_UI_PENDING` | P1 | Transactions |
| T-02 | Transaction detail | *`/islem/[transactionId]`* | participant-only | `BACKEND_EXISTS_UI_PENDING` | P1 | Transactions |
| T-03 | Payment status | part of T-02 | participant-only | `BACKEND_EXISTS_UI_PENDING` + `PAYMENT_PROVIDER_PENDING` | P2 | Payments (intent only; no PSP) |
| T-04 | Delivery status | *`/teslimat/[deliveryId]`* | participant-only | `BACKEND_EXISTS_UI_PENDING` + `DELIVERY_PROVIDER_PENDING` | P2 | Deliveries (state only; no courier/GPS) |
| T-05 | Dispute create / detail | *`/anlasmazlik/[disputeId]`* | participant-only | `BACKEND_EXISTS_UI_PENDING` | P2 | Disputes |
| T-06 | My disputes | *`/anlasmazliklarim`* | participant-only | `BACKEND_EXISTS_UI_PENDING` | P2 | Disputes |

Do not design fake paid/shipped success. Card PAN, PSP brand, courier brand are forbidden unless a later approved adapter requires a legal name.

---

## 9. EİDS / TR Compliance

| ID | Screen | Route | Audience | Status | P | Domain |
|---|---|---|---|---|---|---|
| E-01 | Listing EİDS request/status | part of L-04 | owner-only | `BACKEND_EXISTS_UI_PENDING` | P0 for regulated categories | EİDS + Listings publish gate |
| E-02 | TR signed-decision ingress | `POST /internal/tr-compliance/v1/verification-decisions` | internal-only | `INTERNAL_NON_UI` | — | EİDS trdecision |
| E-03 | Person / e-Devlet identity UX | — | — | `DEFERRED_NON_V1` (listing property/vehicle only; no person identity) | P2 | — |

Frontend shows business status only. Official provider adapter is not implemented (`unavailable`, never `verified`).

---

## 10. Moderation (consumer)

| ID | Screen | Route | Audience | Status | P | Domain |
|---|---|---|---|---|---|---|
| R-01 | Report intake | sheet/modal from L-02 / A-10 | authenticated ≠ subject owner | `BACKEND_EXISTS_UI_PENDING` | P0 | Moderation reports |
| R-02 | My reports | *`/raporlarim`* | owner-only (reporter) | `BACKEND_EXISTS_UI_PENDING` | P1 | Reports |
| R-03 | Appeal create / detail | *`/itirazlarim`* | subject owner | `BACKEND_EXISTS_UI_PENDING` | P1 | Appeals |
| R-04 | Moderation warning inbox item | D-07 | subject owner | `BACKEND_EXISTS_UI_PENDING` (legacy warning path; not catalog cutover) | P1 | Notifications + Moderation |

Staff notes never appear on consumer report DTOs.

---

## 11. Category verticals (reference only)

Prior design work showed Market, Jobs, Tourism, Events, Creator as separate products. **Repository has one Listings domain + Master Data category tree.** Exact tree content is OPEN (ADR-012). Official Fethiye seed is not started.

| ID | Screen family | Status | P | Rule |
|---|---|---|---|---|
| C-01 | Market landing / product detail | Design as listing category landing + L-02 | P1 | No Market domain; no cart/checkout |
| C-02 | Jobs landing / job detail / apply | `DEFERRED_NON_V1` until taxonomy + application HTTP exist | P2 | Job seeker / employer are not IAM roles |
| C-03 | Tourism / activity / booking | `DEFERRED_NON_V1` (no booking HTTP) | P2 | Need+Offer may cover local service demand |
| C-04 | Events landing / event detail | `DEFERRED_NON_V1` until category exists | P2 | |
| C-05 | Creator discovery / profile / collab | `DEFERRED_NON_V1` (no Creator domain) | P2 | Do not invent collaboration HTTP |

Figma may show **category cards** on Home that resolve to `/ara?categoryId=`. Do not invent silos.

---

## 12. Admin / Management Center

Separate app `web/apps/admin` (ADR-013). Staff Bearer IAM; consumer cookies never elevate.

| ID | Screen | Route | Audience | Status | P |
|---|---|---|---|---|---|
| S-01 | Overview dashboard | `/` | staff-only | `BACKEND_AND_UI_PRESENT` (placeholder copy) | P1 |
| S-02 | Users lookup | `/users` | moderator+ / admin (`identity.profile.read`) | `BACKEND_AND_UI_PRESENT` | P0 |
| S-03 | User 360 | `/users/[userId]` | same | `BACKEND_AND_UI_PRESENT` | P0 |
| S-04 | Listings lookup | `/listings` | `listings.read` | `BACKEND_AND_UI_PRESENT` | P0 |
| S-05 | Listing 360 | `/listings/[listingId]` | `listings.read` | `BACKEND_AND_UI_PRESENT` | P0 |
| S-06 | Report queue | `/moderation` | `moderation.report.read` | `BACKEND_AND_UI_PRESENT` | P0 |
| S-07 | Report detail | `/moderation/[reportId]` | same | `BACKEND_AND_UI_PRESENT` | P0 |
| S-08 | Case queue | `/cases` | `moderation.case.read` | `BACKEND_AND_UI_PRESENT` | P0 |
| S-09 | Case detail | `/cases/[caseId]` | same | `BACKEND_AND_UI_PRESENT` | P0 |
| S-10 | Case evidence | `/cases/[caseId]/evidence` | read/write per permission | `BACKEND_AND_UI_PRESENT` | P0 |
| S-11 | Case actions | `/cases/[caseId]/actions` | write; **approve** = senior_moderator/admin | `BACKEND_AND_UI_PRESENT` | P0 |
| S-12 | Action detail | `/cases/[caseId]/actions/[actionId]` | same | `BACKEND_AND_UI_PRESENT` | P0 |
| S-13 | Case appeals | `/cases/[caseId]/appeals` | read; **review** = senior_moderator/admin | `BACKEND_AND_UI_PRESENT` | P0 |
| S-14 | Appeal detail | `/cases/[caseId]/appeals/[appealId]` | same | `BACKEND_AND_UI_PRESENT` | P0 |
| S-15 | Global appeals index | `/appeals` | staff | `BACKEND_AND_UI_PRESENT` (explains no global queue) | P2 |
| S-16 | Staff dispute detail | *admin disputes* | support/admin | `BACKEND_EXISTS_UI_PENDING` | P2 |
| S-17 | Nav placeholders: Companies, Trust, Finance, Compliance, Platform | shell labels only | — | `UI_CONTRACT_EXISTS_BACKEND_PENDING` / deferred | P2 |
| S-18 | Staff login | none in-app | staff-only | `UI_CONTRACT_EXISTS_BACKEND_PENDING` (IdP adapter not wired; no fake login) | P0 for production ops |

Production staff routes are **unregistered (404)** until a real `StaffIdentityProvider` is wired.

---

## 13. System / chrome

| ID | Screen | Route | Status | P |
|---|---|---|---|---|
| X-01 | Global not-found | `app/not-found.tsx` | `BACKEND_AND_UI_PRESENT` | P0 |
| X-02 | Segment error | `app/error.tsx` | `BACKEND_AND_UI_PRESENT` | P0 |
| X-03 | Global error | `app/global-error.tsx` | `BACKEND_AND_UI_PRESENT` | P0 |
| X-04 | Route loading | `app/loading.tsx` | `BACKEND_AND_UI_PRESENT` | P0 |
| X-05 | App shell / header / bottom nav | none (ad-hoc links) | `UI_CONTRACT_EXISTS_BACKEND_PENDING` | P0 |
| X-06 | React Native app | `/mobile` not initialized | `DEFERRED_NON_V1` | P2 |

---

## 14. Counts (for Figma planning)

| Family | Named screens in this inventory |
|---|---|
| Global / Discovery | 9 |
| Auth / Account | 12 |
| Listings | 8 |
| Business / Services | 6 |
| Needs / Offers | 7 |
| Messaging | 3 |
| Verified / Reviews / Trust | 8 |
| Transactions / Payment / Delivery / Dispute | 6 |
| EİDS | 3 |
| Consumer moderation | 4 |
| Category verticals (pattern, not silos) | 5 |
| Admin | 18 |
| System | 6 |
| **Total named surfaces** | **95** |

Of these, **~38** are `BACKEND_AND_UI_PRESENT`, **~32** are `BACKEND_EXISTS_UI_PENDING`, **~12** are `UI_CONTRACT_EXISTS_BACKEND_PENDING`, remainder deferred/internal.

Per-screen universal states (loading, empty, error, forbidden, not-found, offline/retry, ready) × domain lifecycle states yield **approximately 650–800 screen/state combinations**. See [STATE-MATRICES.md](./STATE-MATRICES.md).
