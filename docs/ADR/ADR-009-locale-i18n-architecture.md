# ADR-009: Locale / i18n Architecture

**Status:** ✅ Accepted (architecture). Rollout sequencing, catalog file format, and translation vendors remain OPEN (O-008 remainder).  
**Date:** 2026-09-05  
**Resolves:** D-017 (locale set, true RTL); architectural portion of O-008 (ownership, catalogs, content vs structured fields, RTL/bidi, fallback, formatting, search normalization implications, key versioning, surface consistency)  
**Does not resolve:** O-008 language completeness at V1 vs V1.5; Go / Next.js / React Native i18n library and catalog format; any translation vendor or MT provider  
**Gates:** G-08 remains open until O-008 sequencing and library/format decisions are recorded (expected companion: ADR-014)

---

## 1. Decision

KONUMLU’s locale architecture is built for the frozen set **Turkish (`tr`), English (`en`), Russian (`ru`), Arabic (`ar`)**, with **true RTL layout for Arabic** on every user-facing surface (web, mobile, Management Center, business-facing UI).

Canonical rules:

1. **Original user content is immutable source of truth.** Derived translations never overwrite it.
2. **AI-assisted translations of free text may be stored** as sidecar artifacts on the owning domain, where practical. They are assistive (D-011), never the canonical body.
3. **Structured fields are not freely rewritten by an LLM.** Enums, IDs, categories, prices, geo, hours, and similar fields are translated only via owned catalogs keyed by stable identifiers.
4. **Turkish `İ/i/ı/I` correctness is mandatory** for case folding, search normalization, and UI casing of Turkish text.
5. **Arabic is true RTL**, not LTR layout with right-aligned text.
6. **The same locale model, key space, fallback, and RTL rules apply** to web, mobile, admin, and business surfaces. There is no surface-specific language set.
7. **No translation vendor, MT provider, or i18n library is chosen here.** Provider- and library-specific wiring stays OPEN.

Default / source locale for **platform-authored** catalogs is **Turkish (`tr`)** (D-012, Türkiye-first). User-generated content has no “source locale rewrite”; it is stored as authored.

---

## 2. Context

D-017 freezes four languages and true RTL for Arabic. That constraint affects layout, fonts, formatting, search, and every string that leaves the backend.

O-008 left three questions open:

1. Which languages are production-complete at V1 vs V1.5.
2. Backend strategy for errors, notifications, and system-generated copy.
3. i18n library/format per Go, Next.js, and React Native.

ARCHITECTURE.md already requires logical CSS properties, RTL mirroring from first scaffolding, Arabic-capable fonts, locale-aware date/number formatting, and localized backend user-facing text. Users owns `locale preference`. Master Data owns category and reference data. Search/Discovery is a derived reader (O-004 still open). AI must not be an authority (D-011). PII must not appear in logs or error text.

This ADR freezes **how locale works**. It does not freeze **when** each language is launch-complete or **which** packages/vendors implement catalogs.

---

## 3. Canonical locale identifiers

| Platform language (D-017) | Canonical BCP 47 tag | Script | Default text direction |
|---|---|---|---|
| TR | `tr` | Latin (Turkish) | LTR |
| EN | `en` | Latin | LTR |
| RU | `ru` | Cyrillic | LTR |
| AR | `ar` | Arabic | RTL |

Rules:

- Persist and exchange these four tags only for V1-era product locale. Region variants (`tr-TR`, `ar-EG`, …) are **not** product locales unless a later ADR adds them.
- `Accept-Language` and similar headers are mapped onto this closed set. Unrecognized tags do not create a fifth product locale.
- Active UI locale and `dir` are always one of: `tr` LTR, `en` LTR, `ru` LTR, `ar` RTL.

---

## 4. Locale ownership

Locale is not a marketplace domain. Ownership is split so D-015 is preserved: no domain writes another domain’s tables.

| Concern | Owner | Notes |
|---|---|---|
| User’s preferred UI locale | **Users** | Stored on the user profile. Other domains read via Users contract. |
| Unauthenticated / anonymous locale | **Presentation layer** | Resolved from explicit picker, then `Accept-Language` mapped to the closed set, then `tr`. Not stored as domain truth. |
| Authenticated request locale | **Presentation + Users** | Profile preference wins over headers unless the user has just set a session override (see §8). |
| Platform UI catalogs (web, mobile, MC, business chrome) | **PLATFORM i18n catalogs** | Shared monorepo catalogs. Not listing/need content. |
| Domain error / business-message codes | **Owning domain** emits stable codes; **PLATFORM i18n** holds user-visible templates | Domains do not ship four copies of prose in business logic. |
| Notification templates (system copy) | **Notifications** + PLATFORM catalogs | Payload is structured; localized body is resolved at dispatch using **recipient** locale. |
| Category / reference / taxonomy labels | **Master Data** | Per-locale labels keyed by category/reference ID. |
| Geographic display names | **Location / Geo** | Per-locale names keyed by region/place ID. Original official names preserved. |
| Listing / Need / Service / Business / Review / Message **body** | **Owning content domain** | Original text + language tag as authored. |
| Derived UGC translations | **Same owning content domain** | Sidecar rows/objects. Never replace original columns. |
| Legal / EİDS / compliance user copy | **Compliance / EİDS** | Human-controlled translations only. AI must not be the sole source of legal wording. |
| Audit / observability | **Audit / Observability** | Locale-independent codes and English or code-only operator text. No PII. No localized prose as the audit payload. |

**Forbidden:** a domain other than Users persisting “the” user locale as its own source of truth; Search/Discovery owning canonical translated listing bodies; Management Center storing a parallel string catalog that diverges from product catalogs; LLM writes into structured Master Data or Location identifier fields.

---

## 5. UI translation strategy

All product chrome (buttons, navigation, empty states, validation copy, Management Center labels, business dashboard chrome) uses **keyed catalogs**, not hardcoded literals.

- One **shared key space** across Next.js, React Native, and Management Center. Surfaces may **namespace by area** (`web.*`, `mobile.*`, `mgmt.*`, `business.*`) only for layout-specific strings. Semantic strings used in more than one surface live under a shared prefix (`common.*`, `listing.*`, …) and must not be forked per surface.
- Catalogs are compiled or loaded per locale. Missing keys follow §8; they are not silently invented at runtime by an LLM.
- Interpolation, plurals, and select-ordinals must be **ICU MessageFormat-capable** in behavior. The on-disk format (JSON, PO, XLIFF, …) is OPEN (O-008).
- RTL surfaces use **CSS logical properties** and platform layout mirroring from first scaffolding (ARCHITECTURE.md). Physical `left`/`right` in product UI is a defect unless the property is direction-independent (e.g. map geometry).
- Font loading must cover Turkish Latin, Cyrillic, and Arabic script (e.g. a Noto-family or equivalent). Exact font files are an implementation detail, not a vendor decision in this ADR.
- String freeze for a surface requires catalogs for every locale that surface **claims** to ship. Which locales are claimed at V1 vs V1.5 is OPEN.

---

## 6. Backend message and error localization

### 6.1 Errors and API responses

Domains return **stable machine codes** plus **typed interpolation parameters** (IDs, counts, non-PII identifiers). They do not return only a pre-localized sentence as the contract.

Public API shape (conceptual):

- `code` — stable, versioned identifier (`listing.publish.category_required`)
- `params` — JSON-safe scalars for interpolation
- `message` — optional localized string for the **active request locale**, for clients that display server text directly
- Logs and traces record `code` + safe params, never the user’s prose, never PII

Clients (web, mobile, MC) SHOULD prefer local catalogs keyed by `code` so offline and stale-app behavior stays consistent. Server `message` is a convenience, not a second source of meaning.

Internal/admin APIs use the same codes. Management Center localizes with the operator’s locale, not the reported user’s locale, unless the screen is explicitly showing what the user saw.

### 6.2 Notifications and system-generated content

- **Recipient locale** (Users preference, else last known UI locale, else `tr`) selects the template.
- Template input is structured (need ID, listing title snapshot, deep link). **Do not** concatenate translated fragments in domain code.
- Channel bodies (push, in-app, email, SMS — channels themselves OPEN per O-005) are localized at **dispatch** in Notifications / worker, not in the producing domain.
- If a template is missing in the recipient locale, apply §8. Do not send an empty body.

### 6.3 What is never localized as product UI

- Audit records, fraud signal type enums stored as codes, feature-flag names, storage keys, metric names.
- Operator-facing log lines. Use codes + English/technical identifiers.

---

## 7. Content vs structured-field translation

| Class | Examples | Canonical store | Translation |
|---|---|---|---|
| **UGC free text** | Listing title/description, Need text, review body, chat messages, business about | Original bytes + `source_lang` (detected or declared; detection is assistive) | Optional sidecar: `locale`, `text`, `engine` (human \| ai \| unknown), `catalog_or_model_version`, timestamps. Display: original first; translation is an overlay. |
| **Structured / enumerated** | Category ID, listing status, condition, pricing model, verification badge ID | IDs / enums in the owning domain | Labels only in Master Data or PLATFORM catalogs. **LLM must not rewrite IDs, enums, or numeric/geo fields.** |
| **Reference copy** | Category names, unit labels, canned reasons | Master Data / PLATFORM | Human-maintained (or approved) per-locale strings keyed by ID. |
| **Mixed fields** | Business hours, prices, coordinates | Structured columns | Format for display per §9. Do not “translate” `19:00` or `TRY 250` by LLM paraphrase. |

**Preserve original user content:** updates edit the original; a new original version invalidates or re-queues sidecars. Sidecars are derived. Search and moderation operate on original text; translated text may be indexed as additional language fields without replacing the original.

**AI translations:** allowed for UGC free text as assistance to readers. Stored where practical on the owning domain. Must not be the sole input to authorization, EİDS, or irreversible fraud decisions (D-011). Must not be written into Master Data structured labels without a human approval path.

---

## 8. Locale resolution and fallback

### 8.1 Resolution order (UI and API localization)

1. Explicit user choice in the current session (language picker), if present.
2. Authenticated **Users.locale preference**, if set and in `{tr,en,ru,ar}`.
3. Mapped `Accept-Language` / client locale, if it maps to the closed set.
4. Default **`tr`**.

Arabic selection always sets `dir=rtl` for product chrome. The other three set `dir=ltr`.

### 8.2 Catalog fallback (missing string)

For **platform catalogs** and **system templates**:

```
requested locale → tr
```

Rationale: Turkish is the primary KONUMLU product language and the authored source for platform copy. English is **not** an implicit global fallback. There is **no** `ar → ru` or `ru → ar` fallback. Locale-specific chains (for example inserting `en` for a given locale) are OPEN if desired later.

If `tr` is also missing: in production, show a safe generic message for that **code** (not an empty control, not a raw LLM fill). In non-production, failing tests / visible key is required so gaps cannot ship silently.

**Do not** change `dir` because a single string fell back (e.g. Turkish sentence inside an Arabic layout). Wrap fallback / mixed-direction runs with Unicode bidi isolation (FSI/PDI or equivalent `dir="auto"` isolation on the run).

### 8.3 UGC fallback

If a sidecar translation is missing: show **original**. Never invent a translation at read time in V1 architecture. Never substitute another user’s language as if it were the author’s.

---

## 9. Date, number, and currency formatting

- Formatting follows **CLDR locale data** for the active UI locale (`tr`, `en`, `ru`, `ar`). Library choice is OPEN; behavior must be CLDR-compatible.
- **Calendar for product time:** Gregorian. This is a Türkiye-first marketplace; hijri (or other) calendars are not a V1 requirement.
- **Time zone:** instant stored in canonical UTC (or timestamptz) in PostgreSQL; displayed in a user-relevant zone (profile or device). Zone source details may be specified in a Users/domain spec; this ADR only requires locale-aware **presentation**.
- **Currency:** amounts stored as structured decimal + currency code (pilot: **TRY** unless a later commercial ADR says otherwise). Symbol, grouping, and fraction display follow the **UI locale**, not a rewritten string from an LLM.
- **Digits:** default to locale-appropriate CLDR numbering for chrome. User-entered digits in UGC are stored as entered. Whether Arabic UI uses Arabic-Indic digits for **all** chrome numbers is an OPEN presentation detail; mixed bidi isolation still applies.
- **Maps and coordinates:** Geo JSON / PostGIS remain LTR numeric; chrome around the map follows UI `dir`. Map canvas is not mirrored in a way that inverts geography.

---

## 10. RTL and bidirectional text

True RTL for `ar` means:

- Document/root `dir="rtl"` (web) and equivalent I18nManager / layout direction (React Native) for the whole product chrome.
- Start/end padding, navigation, tabs, chevrons, and back affordances **mirror**.
- Logical properties over physical left/right.
- **User content** (LTR Turkish/English/Russian pasted into an Arabic UI, or Arabic pasted into LTR UI) is isolated so punctuation and digits do not corrupt surrounding layout (bidi isolate per message/title/field).
- Input fields: caret and alignment follow field language / `dir=auto` as appropriate; forms in Arabic chrome remain RTL.
- Media galleries and maps do not geographically mirror. Control chrome around them does.
- Management Center and business surfaces follow the same RTL rules. RTL is not “consumer app only.”

Fonts, truncation, and line breaking must support Arabic script. Ellipsis and letter-spacing hacks that break Arabic joining are defects.

---

## 11. Search normalization implications

Search/Discovery remains a **derived** reader (D-004, O-004 open). This ADR does not choose PostgreSQL FTS vs another engine. It constrains **normalization** so any V1 index remains correct for the frozen languages.

| Locale | Required behavior |
|---|---|
| `tr` | Case folding and equality for Turkish text MUST use Turkish rules: **`I` ↔ `ı`, `İ` ↔ `i`**. English/root locale folding is a defect for TR content and TR queries. |
| `ar` | Index normalized form may strip tatweel, unify alef/hamza variants, and ignore Arabic diacritics (tashkeel) **for matching only**. Stored and displayed text remains original. |
| `ru` | Case fold in Russian locale. Do not treat unrelated Latin folding as sufficient for Cyrillic. |
| `en` | Root/English Unicode case fold is acceptable for EN. |

Additional:

- Queries are normalized with the **query language / UI locale** plus, where content language is known, language-specific analyzers. A single “English stemmer for all listings” is not acceptable for TR/AR/RU bodies.
- UGC original text is the primary searchable body. Sidecar translations may be extra fields; they must not replace original tokens.
- Structured filters (category ID, region ID, price) are **not** full-text translated at query time; they use IDs and Master Data/Location labels only as display.
- Exact engine, `tsvector` configs, and ngram strategy stay with O-004 / ADR-005. This ADR only forbids locale-unsafe folding.

---

## 12. Translation keys and versioning

- Keys are **stable identifiers**: `domain.feature.element` (example: `listings.publish.submit`). They are not English sentences used as keys.
- **Never reuse a key** for a new meaning. Change of meaning → new key (or explicit version suffix). Deprecate old keys; do not silently repurpose.
- ICU placeholders are named (`{categoryName}`, `{count}`), never positional-only where argument order differs by language.
- Catalogs carry a **catalog version** (or content hash) so clients can cache and bust. Backend `code` values version the same way: breaking change of params or meaning → new code.
- Plurals and gender/select follow ICU, not hardcoded `+ "s"`.
- Process (who translates, CI lint for missing keys, freeze windows) may be documented in TASKS; architecture requires missing-key detection before release of a claimed locale.

---

## 13. Mobile / web / admin / business consistency

| Rule | Binding |
|---|---|
| Language set | Same four locales on all surfaces that show product UI. No “admin English-only” architecture. Operator preference may default to `tr`. |
| Keys | Shared key space (§5). Duplicate copy with different wording for the same meaning is a defect. |
| RTL | Same true-RTL requirement on RN, Next.js consumer, business, and Management Center. |
| Fallback | Same chain (§8). |
| Formatting | Same CLDR locale for the active UI locale. |
| API | Same error codes; each client localizes consistently. |
| Locale preference | One Users-owned preference drives all authenticated surfaces unless a surface-specific override is explicitly stored as a Users preference field later. |

Web and mobile share the backend API (ARCHITECTURE.md). Locale is a **request/presentation** concern plus Users preference — not a mobile-only header scheme and a different web scheme.

---

## 14. What this ADR does not choose

Left OPEN (do not invent in implementation without a follow-on decision):

- V1 vs V1.5 **production completeness** per language (O-008.1). Architecture still requires RTL-ready scaffolding and catalogs that can accept all four locales.
- i18n **libraries and on-disk format** for Go, Next.js, React Native (O-008.3).
- **Translation vendor**, TMS, or machine-translation provider. If a provider is added, it is an infrastructure adapter (D-012); it must not leak into domain business rules.
- Search engine selection (O-004).
- Notification channels (O-005).
- Arabic-Indic vs Latin digits as a global chrome default.
- Region subtags and extra locales.

---

## 15. Consequences

### Positive

- RTL and four-script support are designed in, not retrofitted (D-017).
- Original UGC and structured IDs stay canonical; AI stays assistive (D-011).
- Turkish casing and Arabic normalization are explicit, reducing search and display defects.
- Surfaces stay linguistically consistent; Management Center is not a second product language-wise (D-020 surfaces still in the same locale model).
- G-08 can proceed on **architecture**; string implementation still waits on remaining O-008 items.

### Negative / trade-offs

- Dual storage (original + sidecar) costs space and invalidation logic.
- Shared catalogs require discipline across four surfaces.
- Fallback `requested locale → tr` can show mixed-direction runs in Arabic UI; isolation is mandatory but not free.
- G-08 is **not** fully closed; user-facing string freeze still waits on sequencing and library/format.

### Constraints imposed

- Do not hardcode user-facing strings once catalogs exist for a claimed locale (AR-08).
- Do not overwrite original UGC with a translation.
- Do not LLM-rewrite structured fields or legal/EİDS copy as sole authority.
- Do not use non-Turkish locale rules to case-fold Turkish.
- Do not implement Arabic as LTR + `text-align: right`.
- Do not add languages or vendors without a decision that updates O-008 / this model.
- Do not put durable auth tokens in `localStorage` as part of locale persistence (D-016); locale preference lives in Users (authenticated) or non-auth presentation storage that is **not** an auth token.

---

## 16. Relationship to ADR-014

INDEX.md lists ADR-014 as platform locale architecture overlapping D-017 / O-008. **This ADR (009) is the locale/i18n architecture record.** ADR-014 should resolve the **remaining O-008 items** (rollout sequencing, library/format) without re-opening §§1–13 of this document.

---

## 17. Open items

| ID | Item | Notes |
|---|---|---|
| O-008.1 | Language rollout sequencing | Which of TR/EN/RU/AR are production-complete at V1 vs V1.5. Does not change the frozen set or RTL requirement. |
| O-008.3 | i18n library and catalog format | Go backend, Next.js, React Native; ICU-capable behavior is required, package names are not. |
| O-008.P | Translation provider / TMS / MT | Explicitly unset. No vendor in this ADR. |
| OPEN-DIGITS | Arabic chrome numbering system | CLDR default vs Latin digits for chrome numbers. |
| OPEN-FALLBACK-CHAINS | Locale-specific catalog fallback chains | Default is requested locale → `tr` only. Per-locale chains (e.g. inserting `en`) require an explicit later decision. |
| G-08 | User-facing string freeze | Blocked until O-008.1 and O-008.3 are decided. Architecture in this ADR is binding in the meantime. |
