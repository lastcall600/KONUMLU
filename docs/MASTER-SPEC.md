# MASTER-SPEC.md

> Master specification for KONUMLU. Aggregates product vision, domain tier structure, vertical slices, phases, architecture risks, open gates, and ADR list.
> This document is a living reference. Update it as decisions are resolved and phases advance.

---

## Product Vision

KONUMLU is a location-first, trust-first local marketplace and community platform for Türkiye, initially piloted in Fethiye/Muğla.

The platform connects residents, local businesses, and service providers through a shared foundation of **Identity**, **Location**, and **Trust**. It supports two primary user flows that coexist without coupling:

- **Listings flow**: browse, post, discover, and save local classified listings.
- **Need/Matching flow**: express a local demand → system resolves category + location → identifies eligible providers → **ranks/matches** providers → notification → offer → messaging → (later) transaction + review.

The Need/Matching flow is KONUMLU's primary product differentiator. Provider ranking is a first-class step in this flow.

---

## Domain Tier Structure

See [ARCHITECTURE.md](../ARCHITECTURE.md#13-domain-map) for the full domain map with ownership, contracts, and activation phases.

| Tier | Domains |
|---|---|
| CORE | Identity/Auth, Users, Location/Geo, Trust, Master Data |
| MARKETPLACE | Listings, Business Profiles, Services, Needs/Matching, Search/Discovery, Media |
| ENGAGEMENT | Favorites/Saved, Messaging, Notifications, Reviews |
| TRUST & OPERATIONS | Moderation, Fraud, Case Engine, Compliance/EİDS |
| COMMERCIAL (deferred) | Billing, Transactions, Payments, Delivery, Disputes |
| PLATFORM | Analytics, Audit, Feature Flags, Observability, Integrations |

Dependency direction: lower tiers never depend on higher tiers. See [ARCHITECTURE.md §5](../ARCHITECTURE.md#5-allowed-dependency-directions).

---

## Vertical Slices

### Slice A — Listings Flow

**Goal:** Enable a user to register, list an item with photos and location, and let another user find and save it.

**Flow:**
```
register → login → create listing → upload photos → choose location → publish
→ search → map view → listing detail → favorite
```

**Domains exercised:** Identity, Users, Location, Master Data, Listings, Media, Search/Discovery, Favorites

**Target phase:** V1

---

### Slice B — Need/Matching Flow

**Goal:** Enable a user to express a local need and receive ranked responses from matched local providers.

**Flow:**
```
create local need → category/location resolution → identify eligible providers
→ rank/match providers → notify eligible providers → provider response/offer → messaging
```

**Domains exercised:** Identity, Users, Location, Master Data, Needs/Matching, Business Profiles, Notifications, Messaging, Trust

**Target phase:** V1

---

## Phased Delivery Plan

### Phase 0A — Architecture Foundation (current, IN PROGRESS)

**Deliverables:** Documentation only — no application source code.

- Root orientation files (README, CURRENT-STATE, DECISIONS, ARCHITECTURE, PROJECT-MAP, AGENTS)
- This document (MASTER-SPEC)
- ADR index and individual ADRs (14 ADRs — Groups 3 not yet started)
- Domain specification files (Group 4 not yet started)
- Phase 0A task list

**Gate to V1:** All architecture gates G-01 through G-09 resolved.

---

### V1 — Initial Implementation

**Goal:** End-to-end working Slice A and Slice B for pilot in Fethiye/Muğla.

**Active domains:** 22 domains/surfaces including Fraud (signal ingestion), Case Engine (minimal), and Management Center (V1 operational surfaces). See [ARCHITECTURE.md §15](../ARCHITECTURE.md#15-initial-active-domain-set).

**Excluded from V1:** Advanced fraud automation, full Case Engine automation, Management Center advanced tooling, Corporate Workspace, all COMMERCIAL domains.

---

### V1.5 — Growth and Operations

**Goal:** Operational tooling maturity, fraud detection, and corporate/business features.

**Added or expanded:** Fraud (advanced scoring, pattern detection, automation), Case Engine (SLA orchestration, appeals, complex routing), Management Center (advanced automation, fraud graph, ML-assisted moderation), Corporate Workspace (pending O-006 resolution), Moderation (automated signal processing).

---

### V2 — Commercial

**Goal:** Monetization and transaction layer.

**Added domains:** Billing, Transactions, Payments, Delivery, Disputes, Analytics (full).

---

## Architecture Risks

See [ARCHITECTURE.md §17](../ARCHITECTURE.md#17-architecture-risks) for the full risk register (AR-01 through AR-10).

Top risks requiring immediate attention:

| Risk | Mitigation |
|---|---|
| AR-03: EİDS integration complexity | Prototype early in V1; maintain sandbox |
| AR-05: Session strategy deferred | Resolve O-002 as first ADR |
| AR-06: Need/Matching provider eligibility query | Prototype PostGIS geo+category query before committing to schema |

---

## Open Architecture Gates

See [ARCHITECTURE.md §18](../ARCHITECTURE.md#18-open-architecture-gates) for the full gate list. All must be resolved before V1 implementation begins.

| Gate | Decision | Blocks |
|---|---|---|
| G-01 | O-001: Go module layout | All Go backend development |
| G-02 | O-002: Server-side session backing store | Identity/Auth V1 implementation |
| G-03 | O-003: Outbox relay strategy | First domain using outbox |
| G-04 | O-004: Search backend | Search/Discovery V1 |
| G-05 | O-005: Notification channels | Notifications V1 |
| G-06 | O-006: Corporate Workspace boundary | Corporate Workspace V1.5 |
| G-07 | O-007: Pilot geography seeding | Master Data V1 |
| G-08 | O-008: Language rollout sequencing + i18n strategy | All user-facing surfaces |
| G-09 | O-009: Management Center frontend model | Management Center V1 frontend |

---

## Detected Conflicts and Ambiguities

The following items were detected during Phase 0A reconciliation and have been resolved or correctly classified.

| ID | Description | Resolution |
|---|---|---|
| C-001 | Corporate Workspace vs. Business Profiles boundary | 🔄 Open — O-006 gates V1.5 scope only |
| C-002 | Session storage strategy | ✅ Split: cookie security attributes frozen (D-016); server-side backing store open (O-002) |
| C-003 | Multi-language scope | ✅ Split: TR/EN/RU/AR + RTL frozen (D-017); rollout sequencing open (O-008) |
| C-004 | Management Center classification | ✅ Resolved: core product control-plane, not deferrable (D-020) |
| C-005 | Fraud full deferral to V1.5 | ✅ Resolved: signal ingestion in V1; advanced automation V1.5 |
| C-006 | Case Engine full deferral to V1.5 | ✅ Resolved: minimal case lifecycle in V1; advanced features V1.5 |

---

## ADRs to Be Created

See [ARCHITECTURE.md §19](../ARCHITECTURE.md#19-adrs-to-be-created) and [docs/ADR/INDEX.md](./ADR/INDEX.md).

| ADR | Title | Priority | Gates |
|---|---|---|---|
| ADR-001 | Go Modular Monolith Structure and Import Boundary Enforcement | High | G-01 |
| ADR-002 | Session Storage Strategy (server-side backing store) | High | G-02 |
| ADR-003 | Transactional Outbox Design | High | G-03 |
| ADR-004 | Authentication: Passkeys + Argon2id | High | — |
| ADR-005 | Search Backend for V1 | Medium | G-04 |
| ADR-006 | Notification Delivery Channels V1 | Medium | G-05 |
| ADR-007 | EİDS Integration Design | High | — |
| ADR-008 | AI Capability Boundaries | Medium | — |
| ADR-009 | Multi-Language Strategy V1 | High | G-08 |
| ADR-010 | Provider Abstraction Layer | Medium | — |
| ADR-011 | Cross-Domain Communication Rules | High | — |
| ADR-012 | Pilot Geography and Category Bootstrap | Medium | G-07 |
| ADR-013 | Management Center Architecture | High | G-09 |
| ADR-014 | Platform Locale Architecture: Four-Language Set and RTL | High | G-08 |

---

## Phase 0A Task Sequence

See [docs/TASKS/PHASE-0A.md](./TASKS/PHASE-0A.md) for the full task list with status tracking.

**Groups 1 and 2 complete. Groups 3, 4, and 5 not yet started.**

Recommended ADR authoring order: ADR-001 → ADR-002 → ADR-003 → ADR-004 → ADR-007 → ADR-009 → ADR-011 → ADR-013 → ADR-014 → remainder.
