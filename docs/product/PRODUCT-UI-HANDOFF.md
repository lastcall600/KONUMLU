# PRODUCT-UI-HANDOFF.md

> Authoritative Product → Figma → Frontend contract for KONUMLU.
> Repository and accepted ADRs win over prior mockups.
> Supporting: [SCREEN-INVENTORY.md](./SCREEN-INVENTORY.md) · [STATE-MATRICES.md](./STATE-MATRICES.md) · [COMPONENT-INVENTORY.md](./COMPONENT-INVENTORY.md).

**Package:** Product/UI Handoff Freeze  
**Surfaces:** `web/apps/consumer` (konumlu.com) · `web/apps/admin` (Management Center, ADR-013)  
**Not in V1 P0:** React Native `/mobile` (D-003 exists; directory not initialized)

This document specifies **information architecture and behavior**, not color, type, or pixel breakpoints.

---

## How to use this file

1. Figma draws screens from the inventory and state matrices.
2. Cursor/frontend binds those screens to **existing** HTTP/domain contracts.
3. If a mockup conflicts with this file or an accepted ADR, **this file / ADR wins**.
4. Do not invent Market, Jobs, Tourism, Events, or Creator backends. Those are category/presentation patterns until Master Data seed exists (ADR-012 content still OPEN).

**Status codes** used throughout: `BACKEND_AND_UI_PRESENT` · `BACKEND_EXISTS_UI_PENDING` · `UI_CONTRACT_EXISTS_BACKEND_PENDING` · `DEFERRED_NON_V1` · `INTERNAL_NON_UI` · `PAYMENT_PROVIDER_PENDING` · `DELIVERY_PROVIDER_PENDING`.

---

## 1. Product shell

There is **no** implemented AppShell. Home and inner pages use ad-hoc `<Link>` lists. Figma must design the V1 shell; frontend should then implement **one** semantic shell.

### Desktop

| Region | Content | Anonymous | Authenticated |
|---|---|---|---|
| Header left | Brand → `/` | yes | yes |
| Header center | Search field → `/ara` | yes | yes |
| Header | Location control (pilot Fethiye/Muğla; catalog IDs later) | yes | yes |
| Header | Categories (published Master Data tree) | yes | yes |
| Header right | Messages, Notifications, Favorites | login wall | badges from unread/inbox/favorites |
| Header right | Profile / account menu | Giriş / Kayıt | Hesap, Güven Pasaportum, İlan ver, Güvenlik, Çıkış |
| Header | Primary create | İlan ver → login | `/ilan-ver` |
| Main | page | — | — |
| Footer | legal/help placeholders only; no new product | — | — |

Back: browser history. Listing detail already offers “Aramaya dön”. Do not invent a global history stack.

### Mobile

| Region | Content |
|---|---|
| Top bar | Brand, location, search icon, optional create |
| Bottom nav | Ana sayfa · Ara · İlan ver · Mesajlar · Hesap |
| Messages / notifications | Account tab or header icons; unread dots |
| Map/list | On `/ara`, toggle; “Bu alanda ara” remains the **only** viewport search (no implicit pan-search) |
| Modals | Full-screen or bottom sheet (report, filters, QR, OTP, confirm) |

Anonymous bottom nav: Ara + Giriş instead of Mesajlar/Hesap private items. **Do not** show staff destinations in consumer chrome.

### Auth differences

- Cookie session (`__Host-konumlu_session` HttpOnly, Secure, `__Host-konumlu_csrf`). Never persist session/JWT in web storage (D-016).
- Authenticated POSTs/PATCHes need Origin + CSRF. Missing → `403 forbidden` (not Turnstile).
- Staff uses a **separate** app and Bearer IAM. Consumer cookies never grant staff.

---

## 2. User types / actors

KONUMLU has **two auth realms**: consumer Identity and Staff IAM. Situational marketplace roles are not extra logins.

| Actor | Realm | Typical surfaces |
|---|---|---|
| Anonymous visitor | none | Home, `/ara`, listing detail, public profile, public business (when UI exists), login/signup |
| Authenticated consumer | Identity | all public + owned engagement |
| Listing owner / seller | Identity + listing ownership | wizard, publish, conversation as seller, start txn/delivery verification |
| Buyer / interested user | Identity | favorite, message, appointment request, review after inspection |
| Business owner | Identity; one `businesses.profiles` per user | business/service manage |
| Service provider | Identity + active offered service | Need offers, matching candidates |
| Creator | **not a domain** | do not design IAM; deferred |
| Job seeker | **not an IAM role** | deferred until Jobs taxonomy + apply HTTP |
| Employer / business | same as business owner if category exists | not a separate login |
| Moderator | Staff | reports, cases, evidence, propose actions; no action approve; no disputes |
| Senior moderator | Staff | + `moderation.action.approve` + `moderation.appeal.review` |
| Support | Staff | disputes only (`disputes.read` / `disputes.review`) |
| Admin | Staff | union of listed permissions; not a wildcard bypass |

**Surface classes:** public · authenticated · owner-only · participant-only · staff-only · internal-only.

Privacy-safe **404** hides other users’ owner resources (listings, conversations, transactions, appeals). Restricted/removed listings and public profiles are 404 to the public, still readable by the owner (listings) or self (`/v1/profile/me`).

---

## 3. Complete screen inventory

See [SCREEN-INVENTORY.md](./SCREEN-INVENTORY.md) (95 named surfaces).

**V1 product is Listings + Need/Matching + Trust**, not five vertical apps. Slice A (listings) and Slice B (need → offer) are both V1 goals; web-first Fethiye **P0** is Slice A plus safety/auth/messaging/verified. Slice B screens are **P1** with backend already present.

---

## 4. Page contracts (important screens)

Universal fields omitted when identical: **Loading** skeleton; **Empty** copy + CTA; **Error** generic retry; **Forbidden** CSRF/origin refresh; **Not-found** privacy-safe; **Offline** retry; **Destructive** confirm. Server is authoritative for status/permissions.

### 4.1 Home — `/`

| | |
|---|---|
| Audience | public + authenticated |
| Purpose | Enter search / account; V1 should become discovery hub |
| Required data | `GET /v1/auth/session` |
| Optional | published categories, recent search — **not loaded today** |
| Primary CTA | İlan ara → `/ara`; authenticated: İlan ver |
| Secondary | Giriş / Favoriler / Mesajlar / Bildirimler / Güven Pasaportum |
| Status | `BACKEND_AND_UI_PRESENT` as session stub; discovery modules `UI_CONTRACT_EXISTS_BACKEND_PENDING` |
| Privacy | no user UUID in chrome |
| V1 limits | empty taxonomy until seed; no personalized feed |

### 4.2 Search + map — `/ara`

| | |
|---|---|
| Audience | public |
| Purpose | Discover published listings |
| Required | `GET /v1/search/listings` (q, categoryId, price, cursor, north/south/east/west) |
| Optional | `GET /v1/master-data/categories`; save search; favorite |
| Primary CTA | open listing; “Bu alanda ara” |
| Secondary | save search (auth); load more (`nextCursor`) |
| Owner vs viewer | none (public projection) |
| Linked | Search projection (outbox); not live Listings tables |
| V1 limits | no clustering, geocoder, device geolocation, OpenSearch, H3 (D-018, D-019) |

### 4.3 Listing detail — `/ilan/[listingId]`

| | |
|---|---|
| Audience | public |
| Required | `GET /v1/public/listings/{id}` |
| Optional | exact schema form; review summary; public reviews cursor; favorite; message; appointment; seller profile |
| Primary CTA | Mesaj Gönder (auth, not owner); else Giriş |
| Secondary | Randevu Talep Et; Favori; Satıcı → `/profil/{publicProfileId}`; report (`BACKEND_EXISTS_UI_PENDING`) |
| Owner vs viewer | owner still sees public DTO here; owner manage is `/ilan-ver` |
| Not-found | unpublished, restricted, removed, unknown |
| Privacy | **never** owner `user_id`; seller is opaque `publicProfileId` + nullable `displayName`; reviews have no reviewer identity |
| Linked | Listings, Media, Identity, Reviews, ReviewAggregates, Messaging, Verified, Favorites, Moderation |
| V1 limits | no contact phone; no detail map; no EİDS badge on public DTO today |

### 4.4 Create listing — `/ilan-ver`

Steps: 1 Temel bilgiler (category+form) · 2 Konum (lat/lng) · 3 Fotoğraflar · 4 Oluştur · 5 Taslak yönetimi / Yayınla.

| | |
|---|---|
| Audience | owner-only (session) |
| Required | Master Data published tree + form; listing owner HTTP |
| Primary CTA | create → ready → **İlanı Yayınla** |
| Secondary | archive |
| Permissions | session owner; non-owner 404 |
| Linked | `POST/PATCH /v1/listings`, location, media signed PUT, ready, publish, archive |
| Status | `BACKEND_AND_UI_PRESENT`; EİDS panel `BACKEND_EXISTS_UI_PENDING` |
| Gap | lead copy still says publish/EİDS/map are absent; publish exists; EİDS HTTP exists; MapLibre on this page does not |
| V1 | category locked after create; empty catalog until seed; client cannot spoof status/owner |

### 4.5 Login / Signup / Reset — `/giris` `/kayit` `/sifre-sifirla`

Passkey-first (D-009) with password fallback. Turnstile slot when sitekey + operations / `challenge_required`. Existence-hiding on identifier checks. Reset does not log the user in.

### 4.6 Favorites / Saved searches / Messages / Appointments

Implemented routes: `/favoriler`, `/kayitli-aramalar`, `/mesajlar`, `/mesajlar/[id]`, `/randevular`. All session-owned. Messaging is listing-scoped buyer/seller text. Appointments drive QR/OTP listing inspection. Conversation may start transaction/delivery verification (listing owner).

### 4.7 Trust + public profile

`/guven-pasaportum` → `GET /v1/trust/me`.  
`/profil/[publicProfileId]` → public profile + `GET /v1/public/profiles/{id}/trust`.  
Labels: Yeni / Doğrulanmış / Yerleşik Güven. Counts + provider-service average **separate** from level.

Profile edit HTTP exists (`PATCH /v1/profile/me` displayName only; no avatar/bio) — screen `BACKEND_EXISTS_UI_PENDING`.

### 4.8 Notifications page — `/bildirimler`

Today: Web Push register/list/revoke UI. Inbox/preferences/consents HTTP exist and are **not** rendered. Figma: one Notification Center with Inbox · Preferences · Devices. Click mapping is conceptual from `eventType` + `resourceRef` (message → conversation, offer → need, etc.). OTP is **never** an inbox payload.

### 4.9 Business / Need / Offer / Transaction (backend present)

No consumer routes yet (`BACKEND_EXISTS_UI_PENDING`). Design owner and participant screens from [STATE-MATRICES.md](./STATE-MATRICES.md). Public Need marketplace is **out of V1**. Payment/delivery: status UI only with provider-pending banners. Do not fake capture or courier tracking.

### 4.10 EİDS owner panel

`POST /v1/listings/{id}/eids-verifications` · `GET /v1/listings/{id}/eids-verification`. DTO: `verificationId`, `listingId`, `verificationType`, `status`, optional `failureCode`, timestamps. No TR protocol fields. Unconfigured provider → `unavailable`. Publish blocked until `verified` when category requires property/vehicle.

### 4.11 Report / appeal (consumer)

Report: `POST /v1/moderation/reports` + `GET .../mine`. Appeal: create/mine/get/withdraw. Staff notes never on consumer DTOs. Warning arrives as in-app notification (legacy path).

### 4.12 Admin 360 / queues

Implemented: users, listings, reports, cases, evidence, actions, appeals (case-scoped). Desktop-first. Role matrix in [STATE-MATRICES.md](./STATE-MATRICES.md) §17. Global appeals list endpoint **does not exist**. Staff disputes UI pending. Staff login UX depends on a real IdP; **no fake staff login**. Unwired production = 404 on `/v1/staff/*`.

---

## 5. State matrices

Full tables: [STATE-MATRICES.md](./STATE-MATRICES.md).

Non-negotiables:

- Turnstile ≠ Step-Up ≠ EİDS ≠ Trust.
- Trust level ∈ {`new`,`verified`,`established`} from listing_inspection **count** (1 / 5).
- Transaction/delivery verification does not bump Trust level.
- Payments/deliveries are lifecycle records without live providers.

---

## 6. Trust / safety UX contract

Keep these **visually and verbally distinct**:

| Concept | Means | Must not imply |
|---|---|---|
| Auth / session / passkey / Step-Up | account security | government ID, listing inspection, Güven |
| Turnstile | human vs bot | identity |
| EİDS | listing property/vehicle compliance | person TCKN verified; Trust level |
| Listing inspection QR/OTP | parties met / inspected | payment completed |
| Transaction / delivery verification | those flows only | inspection count |
| Reviews | two 1–5 dimensions after inspection | Trust score |
| Güven Pasaportu | transparent count band | 0–100, guarantee |
| Staff moderation | policy enforcement | Trust punishment (not implemented) |

Allowed conceptual labels: “Doğrulanmış gösterim” (inspection-based level), “EİDS doğrulaması”, “Başarılı doğrulanmış işlemler” (history, not level), Trust level label (Yeni / Doğrulanmış / Yerleşik Güven).

Forbidden marketing: “100% güvenilir”, “garantili kullanıcı”, numeric reputation.

Report + block: **report is implemented**. Block is **not** a domain. Do not design block as if it exists.

---

## 7. Messaging UX contract

- Two participants: listing **buyer** + **seller**; no groups; no self-thread.
- Who can start: authenticated user on a **publicly visible** listing; not the owner.
- Context: listing id on the conversation; show listing title if loaded via public GET (404 if hidden).
- Verification actions: listing owner may start transaction/delivery QR/OTP on the thread; participants finish; rediscover via verification-flow list (no secrets in GET).
- Safety: report is listing/profile-level, not per-message HTTP. No block API.
- Unread: per-user `unreadCount` + mark read. Sending: in-flight / failed retry. Delivery/read receipts beyond that: **not implemented**.
- Typing / presence: **not implemented** (Valkey presence is architectural capacity, not a messaging feature today).
- Attachments / audio / video call: **deferred**. Plain text, max 4000 bytes.

---

## 8. Notification UX contract

| Layer | What users see |
|---|---|
| Event | catalog type → title/body from template + allowlisted variables |
| Channel | in-app inbox, web push, mobile push (native **pending**), email, SMS |
| Preference | per channel/category; cannot disable required `in_app` or security required channels |
| Consent | marketing commercial_electronic.* only; independent of preference |
| Browser permission | Web Push prompt **user-initiated**; ≠ preference row |
| Device list | id/channel/platform/provider/timestamps; **never** endpoint URL, p256dh, auth, FCM/APNs token |

Do not brand SES, Netgsm, FCM, APNs, Cloudflare to ordinary users. Operator names may appear in admin/ops docs only.

Click destinations (conceptual): security → `/guvenlik` or login; messaging → conversation; offer → need/offers; transaction/delivery/dispute → those detail screens; moderation.action_applied → owner listing/profile/appeals; saved_search.match → `/ara` (producer **deferred**).

Suppressed/failed deliveries: generic “could not notify” only if product-visible; prefer silent ops. `accepted` ≠ read ≠ displayed on device.

---

## 9. EİDS / TR compliance UX

Frontend consumes **business status** on the listing owner panel. Copy examples: Doğrulama gerekli / bekleniyor / Doğrulandı / başarısız / süresi doldu / hizmet geçici olarak kullanılamıyor.

**Never send or display:** TCKN, birth date, address, raw EİDS/e-Devlet response, provider credentials, `subject_ref`, decision signature, keys, replay rows, ingress Bearer, TR `decision_id`.

Official provider may be swapped without changing these statuses (ADR-015 / D-012). Germany app never becomes an EİDS authority. No admin bypass (D-010). Unconfigured = unavailable, not verified.

Person identity / e-Devlet login is **not** a V1 consumer screen.

---

## 10. Payment / delivery / provider-neutral UX

Status-driven. Amount/currency from Transaction agreed price. Methods `handoff` / `courier` / `pickup` are logistics **modes**, not brands.

Show persistent banners:

- `PAYMENT_PROVIDER_PENDING` — intent may be `pending`; no card form; no fake success
- `DELIVERY_PROVIDER_PENDING` — no tracking map, no carrier logo, no PoD photos

Object storage remains S3-compatible abstraction (MinIO local). Maps: MapLibre (D-007). Email/SMS/push adapters exist but live credentials are operator-owned — UI still uses provider-neutral channel names.

---

## 11. Responsive / mobile contract

No pixel breakpoints are frozen in the consumer app. Figma chooses them.

| Family | Desktop | Tablet | Mobile |
|---|---|---|---|
| Discovery | list + map side-by-side | stack; map collapsible | list/map toggle; sticky search; bottom nav |
| Listing detail | gallery + facts + seller rail | gallery top | gallery swipe; sticky ActionBar (message/favorite) |
| Create listing | wizard wide form | same | one step per screen; photo grid |
| Messaging | list | split optional | list → full-thread; verification as sheet |
| Trust / profile | two-column | stack | stack |
| Offers / txns | comparison table | table | cards; status stepper stacked |
| Admin | tables, 360 panes | still desktop-first | **acceptable to remain desktop-first** (ADR-013 ops) |
| Auth | centered card | card | full-width; Turnstile visible without overlap |
| RTL (ar) | mirror shell, icons, galleries | same | same; true RTL not right-aligned LTR (D-017, ADR-009) |

Languages: TR, EN, RU, AR. Default platform locale **tr**. Rollout completeness still OPEN (O-008 remainder) — Figma should still reserve RTL.

---

## 12. Design-system input

48 semantic components: [COMPONENT-INVENTORY.md](./COMPONENT-INVENTORY.md). No visual tokens in this package.

---

## 13. Figma page structure

Recommended file pages (adjust names, keep order):

| Page | Contents |
|---|---|
| 00 — Foundations | grid, type ramp **placeholders**, icon sizes, RTL notes — no final palette lock required |
| 01 — Components | inventory in §12 |
| 02 — Navigation & Shell | desktop header, mobile top + bottom nav, account menu |
| 03 — Home & Discovery | home modules, `/ara`, map/list, category **pattern**, favorites, saved search |
| 04 — Listings | detail, wizard, owner manage, gallery, seller link, EİDS slot |
| 05 — Market | **category pattern only** — same components as 03/04 |
| 06 — Services | public service + business services (backend present) |
| 07 — Jobs | exploration; stamp DEFERRED until taxonomy |
| 08 — Tourism | exploration; stamp DEFERRED (no booking API) |
| 09 — Events | exploration; stamp DEFERRED |
| 10 — Business | public + owner manage |
| 11 — Creator | archive/exploration only |
| 12 — Profile & Trust | public profile, Güven Pasaportum, reviews |
| 13 — Messaging | list, thread, verification actions, safety |
| 14 — Offers & Transactions | need, candidates, offers, txn; payment/delivery **pending** banners |
| 15 — Verification / EİDS | QR/OTP, appointment, EİDS statuses |
| 16 — Notifications | inbox, preferences, consents, web push permission |
| 17 — Auth & Security | login/signup/reset, passkey, Step-Up, Turnstile, sessions |
| 18 — Admin / Moderation | queues, 360, case timeline, role-limited actions, disputes (support) |
| 19 — Empty / Error / Loading | system + per-family |
| 20 — Mobile Patterns | sheets, sticky CTAs, map toggle |
| 99 — Archive / Explorations | old mockups; labelled non-authoritative |

---

## 14. Screen priority

**P0 — first Fethiye web pilot**

Home shell, search+map, listing detail, listing create/publish, auth (login/signup/reset + Turnstile slot), favorites, messaging, appointments QR/OTP, public profile + Güven Pasaportu, verified review on listing, report intake, notification **inbox** (design even though UI pending), EİDS owner status (regulated categories), Management Center report/case/listing/user 360.

**P1 — shortly after**

Account/security/passkeys/sessions/Step-Up, profile edit, notification preferences/consents, saved-search **alerts** (when producer exists), category landings, listing detail map, owner listing inventory (needs list HTTP), business + services, needs + matching + offers + transactions, consumer appeals, review mine list polish.

**P2 — does not block web-first pilot**

Native mobile, video/CDN, Jobs/Tourism/Events/Creator silos, payment checkout, courier tracking, disputes depth, typing/presence/attachments/calls, avatars/bio, onboarding carousel, admin Companies/Finance/Compliance/Platform placeholders, global staff appeals queue.

---

## 15. Figma → Cursor handoff rules

1. One semantic component system ([COMPONENT-INVENTORY.md](./COMPONENT-INVENTORY.md)).
2. No page-only duplicates of ListingCard, StatusChip, EmptyState, etc.
3. Machine states must match repository enums; map to copy in Figma, keep enum in code.
4. No client-side authority: publish eligibility, Trust level, EİDS, moderation, offer eligibility, staff permissions are **server**.
5. No client-generated verification or Trust decisions.
6. Consume existing contracts in `docs/security/AUTHORIZATION-MATRIX.md` / domain HTTP; do not invent endpoint names.
7. Accessibility: focus order, labels, live regions (consumer already uses `aria-live` on some forms), RTL.
8. Specify responsive behavior; do not freeze unowned breakpoints.
9. UI must not weaken cookie/CSRF/origin/staff-bearer boundaries.
10. Hide user UUIDs, `subject_ref`, object keys, staff notes (consumer), push secrets, TCKN.
11. Fixture data labelled fixture-only.
12. **Figma is presentation truth. Repository is behavior/security truth.**

---

## 16. Existing design references

Treat prior screens (home, mobile home, category, listing/market/service/job/tourism/event/business/creator, offer comparison, reservation, notification center, favorites, map, onboarding, security, business/creator/admin panels, profile, messenger) as **visual reference only**.

Overrides:

- No Creator/Jobs/Tourism/Events domains.
- No public Need marketplace.
- Messaging is text + listing context, not a call app.
- Trust is three labels, not a score ring.
- Admin is a separate Next app, not a consumer tab.
- `/bildirimler` is not yet an inbox.

Do not recreate those images in-repo.

---

## 17. Deferred work register (does not block Figma)

Verified against CURRENT-STATE / operations docs:

| Item | Classification |
|---|---|
| Official EİDS / e-Devlet adapter | later package; UI uses `unavailable` |
| Turnstile production widget credentials | `LIVE_TURNSTILE_E2E_PENDING` |
| SES live / DKIM / sandbox exit | PROVIDER-B ready, credentials operator-owned |
| Netgsm live account/header/OTP | `LIVE_NETGSM_TEST_PENDING` |
| VAPID production keys | `LIVE_WEBPUSH_TEST_PENDING` |
| FCM live project | `LIVE_FCM_TEST_PENDING` |
| APNs production | `LIVE_APNS_TEST_PENDING` |
| Payment PSP | `PAYMENT_PROVIDER_PENDING` |
| Delivery provider | `DELIVERY_PROVIDER_PENDING` |
| Mobile React Native client | `MOBILE_PUSH_CLIENT_PENDING`; `/mobile` missing |
| Video transcoding / CDN | not started |
| Production OTel / metrics dashboards | no-op metrics/tracing in platform |
| Large-scale load / failure testing | not this package |
| Official taxonomy / Fethiye seed | OPEN content (ADR-012); empty catalog |
| Staff IdP vendor adapter | fail-closed; no fake login |
| Saved-search match producer | catalog event exists; producer deferred |
| Moderation warning catalog cutover | legacy path on purpose |
| Passkey during signup | not started |
| Remember Me | not a requirement |
| Avatars / bio | not in public profile |
| Listing owner inventory HTTP | missing list-mine |
| Corporate Workspace | O-006 V1.5 |
| WAL/PITR production hosting | documented ops gap |

Designers should not wait on these to start pages 02–04, 12–13, 16–19.

---

## 18. Contradiction / gap report

### UI Contract Gaps

| Gap | Screen | Backend | Blocks Figma? | Blocks pilot? | Later package |
|---|---|---|---|---|---|
| No AppShell; ad-hoc nav | all consumer | n/a | no | UX only | frontend shell |
| Home is session stub | D-01 | search exists | no | weak discovery | home composition |
| Empty Master Data catalog | L-03, `/ara` | HTTP ok; seed missing | no | **yes** for real listings | taxonomy seed |
| No listing list-mine HTTP | L-05 | per-id only | no | owner ops | listings inventory |
| `/ilan-ver` copy vs publish/EİDS | L-03 | publish + EİDS HTTP exist | no | no | copy + EİDS UI |
| Detail has coords but no map | L-07 | public location DTO | no | no | listing map |
| Inbox HTTP unused by `/bildirimler` | D-07 | NOTIFY-A/B | no | ops/comms | inbox UI |
| Step-Up returns generic `forbidden` | A-05 | AUTH-B | no | security-center UX | distinct error code |
| No consumer report/appeal UI | R-01–R-03 | HTTP exists | no | safety reporting | moderation UI |
| No business/need/offer/txn UI | B/N/T | HTTP exists | no | Slice B | marketplace UI |
| Payment/delivery without providers | T-03/T-04 | records only | no | commercial money movement | PSP/courier packages |
| No owner listing EİDS UI | E-01 | HTTP exists | no | **regulated publish** | EİDS panel |
| Staff IdP unwired | S-18 | fail-closed 404 | no (design queues) | **production moderation** | Staff IdP |
| Category vertical mockups vs one Listings domain | C-01–C-05 | Master Data OPEN | no if stamped deferred | no | taxonomy + product decision |
| Owner listing DTO includes `ownerUserId` | L-04 | owner HTTP | no | leak if shown | hide in UI |
| Media attach vs Listings ownership contracts | L-03 | CURRENT-STATE notes gap | no | integrity | media bind follow-up |
| Saved-search alerts | D-06 | producer deferred | no | no | NOTIFY producer |
| Block user | messaging | **no API** | no | no | do not invent |
| Jobs/Creator/Tourism booking | C-* | no domains | no if deferred | no | do not invent |

No contradiction makes Figma **unsafe** if vertical silos stay stamped deferred and Trust/EİDS/Turnstile rules are followed.

### Non-UI / internal (must not become screens)

TR ingress, subject_ref mapping, outbox, worker, Valkey abuse keys, push ciphertext, staff evidence internals beyond the admin case UI, `cmd/trgateway`.

---

## Authority pointers

- Architecture: `/ARCHITECTURE.md`
- Frozen decisions: `/DECISIONS.md`
- Authz: `/docs/security/AUTHORIZATION-MATRIX.md`
- Notifications: `/docs/architecture/notifications-policy.md`
- EİDS protocol: `/docs/architecture/eids-tr-decision-protocol.md`
- Live status: `/CURRENT-STATE.md`
