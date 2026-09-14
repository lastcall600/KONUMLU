# COMPONENT-INVENTORY.md

> Semantic component inventory for KONUMLU design system input.
> Not visual tokens (color, type scale, spacing). Figma owns appearance.
> Parent: [PRODUCT-UI-HANDOFF.md](./PRODUCT-UI-HANDOFF.md).

Reuse across domains. Do not fork page-specific copies when the states below apply.

**Count: 48 components.**

| Component | Purpose | Required states | Domains | Sensitive-data | Desktop / mobile |
|---|---|---|---|---|---|
| AppShell | Page chrome, skip-link, RTL `dir` | authenticated, anonymous, staff | all consumer | no tokens in DOM beyond CSRF cookie (HttpOnly session) | desktop header; mobile header + bottom nav |
| Header | Brand, search entry, location, account | anon vs auth; unread dots | discovery, account | unread counts only | compact search on mobile |
| MobileBottomNav | Primary destinations | active route; auth-gated items | discovery | none | mobile only |
| SearchBar | Query + submit to `/ara` | idle, typing, submitting, error | Search | none | sticky on mobile search |
| LocationSelector | Pilot geography / viewport intent | unset, set, unavailable | Location catalog (IDs only) | no raw GPS dump in URL beyond bbox already used | bottom sheet on mobile |
| CategoryCard | Published category entry | default, empty catalog | Master Data | none | grid → 2-col mobile |
| ListingCard | Search/favorites hit | default, no price, no geo, favorited | Search, Favorites | never `user_id` | map popup + list row |
| BusinessCard | Public active business | default, no location | Businesses | none | same |
| ServiceCard | Public active offered service | default, paused hidden (not shown) | Services | none | same |
| CreatorCard | **Do not ship V1** | — | none | — | deferred |
| ProfileSummary | Display name + publicProfileId link | missing name, restricted 404 | Identity | no user UUID | inline on listing/messages |
| VerificationBadge | Single meaning per badge | listing inspection vs EİDS vs auth | Trust, EİDS, Verified | never imply one proves another | wrap labels |
| TrustSummary | Level + counts + review signals | `new`/`verified`/`established`; zero | Trust | **no numeric score** | stack on mobile |
| ReviewCard | Public verified review body | ratings split, no identity | Reviews | no reviewer id | card |
| ReviewForm | Accuracy + service 1–5 + body | eligible, not eligible, submitting | Reviews | none | full-width mobile |
| Price | Amount + ISO currency | missing price | Listings, Offers, Needs | none | tabular nums |
| OfferCard | Provider offer row | submitted/withdrawn/accepted/rejected/expired | Offers | no other-user UUID | compare table → cards |
| StatusChip | Domain status label | one chip per machine state | all FSMs | use product copy, not raw enums if hostile | wrap |
| NotificationItem | Inbox row | unread/read | Notifications | allowlisted variables only; no OTP | swipe/read on mobile |
| ConversationItem | Thread preview | unread count, listing context | Messaging | preview text only | list |
| MessageBubble | Plain text message | mine/theirs, sending, failed | Messaging | no HTML/Markdown render as code | max-width bubbles |
| EmptyState | No results / zero inbox | actionable CTA | all | none | full pane |
| ErrorState | Generic retry | error, unavailable, rate limited | all | no stack traces / request internals | inline |
| Skeleton | First paint | loading | all | none | match layout |
| ConfirmationDialog | Destructive confirm | open/busy | archive, revoke, withdraw, cancel | none | modal → full-screen mobile |
| BottomSheet | Mobile overlays | open/closed | report, filters, location | none | mobile; desktop modal |
| MediaGallery | Listing images | empty, processing, rejected, ready | Media | no object keys | lightbox desktop; swipe mobile |
| MapMarker / MapCard | Search map | selected, cluster **deferred** | Search | lat/lng only | MapLibre; list/map toggle |
| ActionBar | Primary/secondary CTAs | sticky, disabled, forbidden | listing, need, txn | none | sticky bottom mobile |
| FormField | Master Data widget | required, invalid, locked category | Master Data forms | none | stacked |
| OTPInput | 6-digit verified OTP | idle, submitting, expired | Verified | never persist raw OTP | large numeric |
| QRChallenge | Client SVG of opaque token | pending, expired | Verified | token not logged; fallback text | large scan target |
| PasskeyAction | WebAuthn begin/finish | unsupported, busy, error | Identity | ceremony token memory-only | button |
| TurnstileSlot | HumanChallenge widget | hidden, required, expired token | Identity auth only | public sitekey; memory token | below auth forms |
| Pagination / LoadMore | Opaque cursor | has more, exhausted | Search, reviews, inbox | cursor opaque | button |
| AdminTable | Queue/lookup | empty, loading, forbidden | staff | staff notes internal | desktop-first; cards optional |
| AdminCard | 360 summary | same | staff | may show internal ids **staff-only** | desktop |
| CaseTimeline | History kinds | append-only | Moderation | no copy of report body into evidence | desktop |
| FilterChip | Search filters | selected | Search | none | horizontal scroll mobile |
| FavoriteToggle | Save/unsave | anon (login), saved, error | Favorites | none | icon+label |
| ReportControl | Open report intake | reasons enum | Moderation | no staff note | sheet |
| SessionRow | Device/session list | current vs other | Identity | no token/hash | cards mobile |
| PushDeviceRow | Endpoint metadata | active/revoked | Notifications | **no** URL/p256dh/auth/token | cards |
| EidsStatusPanel | Listing EİDS | pending…unavailable | EİDS | no TCKN/subject_ref | inline on manage |
| TransactionStatus | pending/active/completed/cancelled | + PAYMENT_PROVIDER_PENDING | Transactions | no PSP ids | stepper → stack |
| DeliveryStatus | pending…delivered | + DELIVERY_PROVIDER_PENDING | Deliveries | no courier brand | stepper → stack |
| NeedCandidateCard | Matching candidate | eligible/not | Needs+Businesses | query-time derived | cards |
| ConsentToggle | Marketing consent | granted/withdrawn | Notifications | not legal advice copy | settings |

## Implementation notes

- Existing consumer files that already encode semantics: `ListingCard`, `ListingDetail`, `SearchMap`, `FavoriteAction`, `MessageAction`, `AppointmentAction`, `FlowVerificationAction`, `IssuedChallengePanel`, `VerifiedQRCode`, `TurnstileWidget`, `WebPushSettings`.
- Replace ad-hoc page links with AppShell + Header + MobileBottomNav in the Figma system; current pages are functional stubs.
- Mock/fixture data in Figma must be labelled **fixture-only**. Never imply client-generated Trust, EİDS, or publish eligibility.
