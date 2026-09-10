# DECISIONS.md

> Frozen decisions must not be re-opened without an explicit instruction from the project lead.
> Open decisions must be resolved before the work they gate can begin.
> Every decision that rises to architectural significance should become an ADR in `/docs/ADR/`.

---

## Frozen Technical Decisions

These decisions are final for the foreseeable future. Do not propose reversals without strong evidence and explicit authorization.

---

### D-001 — Backend: Go modular monolith

**Status:** 🔒 FROZEN

**Decision:** The backend is a single deployable Go binary organized as a modular monolith with enforced inter-domain dependency rules.

**Rationale:** Go provides performance, simplicity, and strong concurrency primitives. A modular monolith avoids premature decomposition while maintaining evolvability.

**Prohibited by this decision:**
- gRPC inside the monolith (all internal cross-domain calls use Go interfaces and direct function calls)
- Microservices from day one
- Event sourcing
- Shared database transactions across domain boundaries (except with explicit architectural permission logged as a decision)

---

### D-002 — Web Frontend: Next.js / React / TypeScript

**Status:** 🔒 FROZEN

**Decision:** The web frontend is built with Next.js, React, and TypeScript.

---

### D-003 — Mobile: React Native

**Status:** 🔒 FROZEN

**Decision:** The mobile application is built with React Native, sharing logic and component patterns with the web surface where practical.

---

### D-004 — Primary Database: PostgreSQL + PostGIS

**Status:** 🔒 FROZEN

**Decision:** PostgreSQL with the PostGIS extension is the canonical data store for all domain state. All geographic data is stored and queried using PostGIS primitives.

**Prohibited by this decision:** Writing canonical domain state to any derived store (Valkey, search index, analytics).

---

### D-005 — Cache / Session / Rate / Presence: Valkey

**Status:** 🔒 FROZEN

**Decision:** Valkey is used for cache, session management, rate limiting, and presence/online-status tracking. Valkey is a derived store only — it is never the source of truth for any domain state.

---

### D-006 — Object Storage: S3-compatible abstraction; MinIO for local development

**Status:** 🔒 FROZEN

**Decision:** All object storage access goes through an S3-compatible abstraction layer. MinIO is used in local development. Production can use any S3-compatible provider.

---

### D-007 — Maps: MapLibre + PostGIS

**Status:** 🔒 FROZEN

**Decision:** The frontend uses MapLibre GL JS (and MapLibre React Native) for map rendering. Backend geographic logic uses PostGIS. MapLibre is fully open-source with no usage-based billing.

---

### D-008 — Async: In-process events + transactional outbox; direct calls for sync

**Status:** 🔒 FROZEN

**Decision:**
- Synchronous cross-domain interactions use direct Go interface calls within the monolith process. No message bus or broker mediates synchronous calls.
- Safe derived effects within the monolith use in-process events.
- Reliable critical async and external effects use a transactional outbox pattern (outbox table written atomically with domain state, polled by a relay).

**Prohibited by this decision:** A standalone message broker (Kafka, RabbitMQ, NATS) is not introduced until outbox fan-out becomes a measured bottleneck. Adding a broker requires an explicit ADR.

---

### D-009 — Authentication: Passkeys first-class; Argon2id for passwords (fallback only)

**Status:** 🔒 FROZEN

**Decision:** FIDO2/WebAuthn Passkeys are the primary and preferred authentication method. Passkeys are the first-class path; password authentication exists as a fallback for edge cases only — not a parity option. Passwords use Argon2id hashing. No MD5, SHA-1, SHA-256, or bcrypt.

---

### D-010 — EİDS compliance is mandatory and non-bypassable

**Status:** 🔒 FROZEN

**Decision:** EİDS mandatory verification must never be illegally bypassed for operations that require it. Compliance/EİDS is a first-class domain. No domain may ignore a `false` result from `IsVerified` for regulated operations.

---

### D-011 — AI capability boundaries

**Status:** 🔒 FROZEN

**Decision:** AI may be used as an assistive and ranking tool (need categorization, provider ranking, spam detection signals, search ranking, content moderation signals). AI must never serve as the sole authority for authorization, identity verification, EİDS verification, or irreversible fraud determinations. Any AI-assisted decision affecting account standing or legal compliance requires a human review step.

---

### D-012 — Türkiye-first, provider-independent

**Status:** 🔒 FROZEN

**Decision:** The platform is designed for Türkiye's regulatory, geographic, and cultural context first. All infrastructure must be provider-independent through abstraction layers.

---

### D-013 — Local-first development

**Status:** 🔒 FROZEN

**Decision:** The full development stack must run locally without cloud services. If a new dependency cannot run locally, it requires an explicit ADR before adoption.

---

### D-014 — GitHub private monorepo

**Status:** 🔒 FROZEN

**Decision:** All platform code lives in a single private GitHub monorepo.

---

### D-015 — Cross-domain data access is forbidden; contracts are required

**Status:** 🔒 FROZEN

**Decision:** No domain may directly query or write another domain's tables. All cross-domain interaction must go through explicitly defined contracts (Go interfaces, HTTP handlers, or in-process events with defined payloads).


---

### D-016 — Browser session cookie security attributes

**Status:** 🔒 FROZEN

**Decision:** All browser authentication sessions must use cookies with the following security attributes enforced:
- `__Host-` prefix (prevents subdomain cookie injection attacks)
- `HttpOnly` (not accessible via JavaScript)
- `Secure` (HTTPS-only transmission)
- `SameSite=Strict` or `SameSite=Lax` (CSRF protection — exact value resolved in ADR-002)

Durable auth tokens (JWTs, session tokens, refresh tokens) must never be written to `localStorage` or `sessionStorage`. This is an absolute prohibition.

**Rationale:** The `__Host-` prefix is a stronger security posture than generic HttpOnly cookies. It prevents cookie injection from subdomains and is a security compliance constraint, not a preference.

**Open part:** The server-side session backing store is resolved by ADR-002 (O-002 / G-02).

---

### D-017 — Platform languages: TR, EN, RU, AR with true RTL for Arabic

**Status:** 🔒 FROZEN

**Decision:** The KONUMLU platform supports four languages: Turkish (TR), English (EN), Russian (RU), and Arabic (AR). Arabic requires true right-to-left (RTL) layout support across all user-facing surfaces (web and mobile). This is a baseline architecture requirement — not a future option — that affects text direction, font loading, date/number formatting, and layout mirroring.

**Rationale:** The Fethiye/Muğla pilot geography has significant EN, RU, and AR-speaking populations. RTL for Arabic has architecture-level implications that cannot be retrofitted cheaply.

**Open part:** Locale architecture (four-language set, RTL, i18n architecture) is resolved by ADR-009 (G-08). Language rollout sequencing and library/vendor selection remain OPEN implementation items (not Phase 0A blockers). See O-008.

---

### D-018 — OpenSearch deferred until measured need

**Status:** 🔒 FROZEN

**Decision:** OpenSearch (or any dedicated full-text search engine) is not introduced until PostgreSQL full-text search has been measured and found insufficient. Adoption requires an explicit ADR citing measured production data.

**Prohibited:** Adding OpenSearch or equivalent in V1 or V1.5 without a measurement-backed ADR.

---

### D-019 — H3 geospatial indexing deferred until measured need

**Status:** 🔒 FROZEN

**Decision:** Uber H3 hierarchical hexagonal geospatial indexing is not introduced until PostGIS spatial queries have been measured and found insufficient at production scale. Adoption requires an explicit ADR citing measured data.

**Prohibited:** Adding H3 in V1 or V1.5 without a measurement-backed ADR.

---

### D-020 — Management Center is a core product control-plane, not a deferrable feature

**Status:** 🔒 FROZEN

**Decision:** The Management Center is a core product control-plane that must be developed alongside each relevant domain from V1. It is not an admin panel that can be deferred. Every domain that produces moderatable content, verifiable entities, or caseable events must have a corresponding Management Center operational interface in the same phase as that domain.

**What may be deferred to V1.5+:** Advanced case automation, SLA orchestration, fraud graph tooling, complex cross-domain workflow orchestration, and ML-assisted moderation tooling.

**Rationale:** Launching V1 without operational tooling means the platform cannot be operated. EİDS compliance oversight, moderation decisions, and user management are required at launch.

---

## Open Decisions

These decisions are not yet made. The work they gate must not begin until they are resolved. Each open decision should be resolved via an ADR.

---

### O-001 — Go module layout: flat packages vs. explicit internal domain modules

**Status:** ✅ RESOLVED — superseded by ADR-001 (G-01 resolved)

**Question:** Should the Go monolith use a flat internal package layout (`internal/identity/`, `internal/listings/`) or a more structured module-boundary approach with explicit import guards?

**Options:**
- A) Flat `internal/` packages with linting rules to enforce domain isolation
- B) Go workspace with per-domain sub-modules
- C) Single module with custom linter or `go-module-boundary` tool

---

### O-002 — Server-side session backing store

**Status:** ✅ RESOLVED — superseded by ADR-002 (G-02 resolved)

**Question:** Should authenticated sessions use short-lived signed JWT payloads embedded in the `__Host-` HttpOnly cookie, or opaque session tokens stored in Valkey and referenced by the cookie?

**Constraint:** Browser cookie security attributes are frozen (D-016). This decision concerns only the server-side persistence mechanism.

**Options:**
- A) Signed JWT in cookie: stateless, no Valkey round-trip per request, revocation requires expiry or denylist
- B) Opaque token in cookie + Valkey session store: stateful, immediate revocation, one Valkey lookup per request

---

### O-003 — Outbox relay implementation: polling interval and competing consumer strategy

**Status:** ✅ RESOLVED — superseded by ADR-003 (G-03 resolved)

**Question:** How is the outbox relay implemented? Single-goroutine poller? Multiple workers with advisory locks? What is the target polling interval?

---

### O-004 — Search backend: PostgreSQL full-text vs. dedicated search engine

**Status:** ✅ RESOLVED — superseded by ADR-005 (G-04 resolved)

**Question:** For V1, is PostgreSQL full-text search sufficient, or does the Search/Discovery domain require a dedicated lightweight engine (e.g. Typesense, Meilisearch)?

**Constraint:** OpenSearch is frozen-deferred (D-018). Any search engine is a derived store; canonical data stays in PostgreSQL.

---

### O-005 — Notification delivery channels for V1

**Status:** ✅ RESOLVED (architecture) — superseded by ADR-011 (G-05 resolved)

**Question:** Which notification delivery channels are in scope for V1? (Push via FCM/APNs, in-app, SMS, email?) Which providers are used?

**Constraint:** Provider details must not leak into business logic (D-006 abstraction principle).

**Remaining (not G-05):** implementation/launch parameters still OPEN — provider/vendor selection; exact quiet-hour window; numeric rate/cost caps; IYS connector details; security-SMS degradation policy; V1 channel rollout completeness.

---

### O-006 — Corporate Workspace boundary vs. Business Profiles

**Status:** 🔄 OPEN / DEFERRED — V1.5 (G-06); does not block V1 Phase 0A completion

**Question:** What is the precise boundary between the Business Profiles domain (MARKETPLACE tier) and the Corporate Workspace (multi-user business account surface)? Same domain at different access levels, or distinct domains?

---

### O-007 — Pilot geography data seeding: Fethiye/Muğla boundary and category taxonomy

**Status:** ✅ RESOLVED — superseded by ADR-012 (G-07 resolved)

**Question:** What is the source of truth for Fethiye/Muğla geographic boundaries (OSM, custom, official administrative boundaries)? What is the initial category taxonomy?

---

### O-008 — Language rollout sequencing and backend localization strategy

**Status:** ✅ RESOLVED (architecture) — superseded by ADR-009 (G-08 resolved)

**The four-language set (TR, EN, RU, AR + RTL) is frozen (D-017). Locale architecture is decided in ADR-009. ADR-014 is not required (superseded / not needed; duplicates ADR-009).**

**Remaining (not Phase 0A blockers):** implementation items still OPEN — language rollout sequencing (which languages are production-complete at V1 vs. V1.5); i18n library/vendor selection for Go, Next.js, and React Native.

---

### O-009 — Management Center: separate Next.js app vs. route-namespace within web app

**Status:** ✅ RESOLVED — option A; recorded in ADR-013 (G-09 resolved)

**Decision:** The Management Center frontend is a **separate Next.js application** in the same monorepo, intended production host **admin.konumlu.com**, planned path **`web/apps/admin`** (not initialized until Phase 0A gates allow V1). It is not a `/mgmt/*` namespace inside the consumer web app.

**Constraint (still binding):** Staff auth context is isolated from the public/consumer web surface. Independent deployment, config, and security boundary. Shared design tokens/components are allowed where useful; business rules are not duplicated in the frontend; all mutations/reads go through the same backend domain admin contracts. MC remains a core V1 control-plane (D-020).

---

## Decisions Log

| ID | Title | Status | Phase Gated |
|---|---|---|---|
| D-001 | Go modular monolith (no gRPC, no event sourcing, direct sync calls) | 🔒 Frozen | — |
| D-002 | Next.js / React / TypeScript | 🔒 Frozen | — |
| D-003 | React Native mobile | 🔒 Frozen | — |
| D-004 | PostgreSQL + PostGIS (canonical source of truth) | 🔒 Frozen | — |
| D-005 | Valkey (derived/cache only, never source of truth) | 🔒 Frozen | — |
| D-006 | S3-compatible abstraction / MinIO local | 🔒 Frozen | — |
| D-007 | MapLibre + PostGIS | 🔒 Frozen | — |
| D-008 | In-process events + transactional outbox + direct sync calls | 🔒 Frozen | — |
| D-009 | Passkeys first-class + Argon2id fallback only | 🔒 Frozen | — |
| D-010 | EİDS mandatory, non-bypassable | 🔒 Frozen | — |
| D-011 | AI capability boundaries | 🔒 Frozen | — |
| D-012 | Türkiye-first, provider-independent | 🔒 Frozen | — |
| D-013 | Local-first development | 🔒 Frozen | — |
| D-014 | GitHub private monorepo | 🔒 Frozen | — |
| D-015 | Cross-domain contracts required; direct DB access forbidden | 🔒 Frozen | — |
| D-016 | Browser session: __Host- cookie, HttpOnly, Secure, SameSite | 🔒 Frozen | — |
| D-017 | Platform languages: TR, EN, RU, AR + true RTL for Arabic | 🔒 Frozen | — |
| D-018 | OpenSearch deferred until measured need | 🔒 Frozen | — |
| D-019 | H3 deferred until measured need | 🔒 Frozen | — |
| D-020 | Management Center is core control-plane, developed alongside domains | 🔒 Frozen | — |
| O-001 | Go module layout | ✅ Resolved (ADR-001) | — (G-01 resolved) |
| O-002 | Server-side session backing store | ✅ Resolved (ADR-002) | — (G-02 resolved) |
| O-003 | Outbox relay strategy | ✅ Resolved (ADR-003) | — (G-03 resolved) |
| O-004 | Search backend for V1 | ✅ Resolved (ADR-005) | — (G-04 resolved) |
| O-005 | Notification channels V1 | ✅ Resolved (ADR-011 architecture; G-05 resolved) | — (launch params remain open, not G-05) |
| O-006 | Corporate Workspace boundary | 🔄 Open / Deferred V1.5 | Corporate V1.5 (G-06; does not block V1 Phase 0A) |
| O-007 | Pilot geography data seeding | ✅ Resolved (ADR-012) | — (G-07 resolved) |
| O-008 | Language rollout sequencing + backend i18n strategy | ✅ Resolved (ADR-009 architecture; G-08 resolved) | — (rollout sequencing + library/vendor remain open, not Phase 0A blockers) |
| O-009 | Management Center: separate app vs. route namespace | ✅ Resolved (ADR-013; option A — `web/apps/admin`) | — (G-09 resolved) |
