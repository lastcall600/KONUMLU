# Phase 0A Task List

> Architecture Foundation phase. All deliverables are documentation only.
> No application source code, framework initialization, Docker files, migrations, or dependency installs.
> Update status in-place as tasks complete.

Status legend: `[ ]` = not started · `[x]` = complete · `[~]` = in progress · `[!]` = blocked

---

## Group 1: Root Orientation Files ✅ Complete

| ID | Task | Status | Notes |
|---|---|---|---|
| 0A-01 | Create `/README.md` | [x] | Complete |
| 0A-02 | Create `/DECISIONS.md` (20 frozen + 9 open decisions) | [x] | D-001–D-020, O-001–O-009 |
| 0A-03 | Create `/CURRENT-STATE.md` | [x] | Complete, reflects IN PROGRESS status |
| 0A-04 | Create `/ARCHITECTURE.md` (all 20 sections + locale/RTL subsection) | [x] | ~1095 lines |
| 0A-05 | Create `/PROJECT-MAP.md` | [x] | Complete |
| 0A-06 | Create `/AGENTS.md` | [x] | Complete |

---

## Group 2: Documentation Infrastructure ✅ Complete

| ID | Task | Status | Notes |
|---|---|---|---|
| 0A-07 | Create `/docs/MASTER-SPEC.md` | [x] | Complete |
| 0A-08 | Create `/docs/ADR/INDEX.md` | [x] | Index created; statuses maintained in INDEX.md |
| 0A-09 | Create `/docs/TASKS/`, `/docs/architecture/`, `/docs/domains/` directories | [x] | Complete |
| 0A-31 | Create `/docs/TASKS/PHASE-0A.md` (this file) | [x] | Complete |

---

## Group 3: Architecture Decision Records 🔄 In progress

Write each ADR in `/docs/ADR/` using the template in `INDEX.md`. Update `INDEX.md` status when each ADR is accepted.

Author in recommended order: 001 → 002 → 003 → 004 → 007 → 009 → 011 → 013 → remainder (skip 014).

| ID | ADR | Title | Status | Gates Resolved |
|---|---|---|---|---|
| 0A-10 | ADR-001 | Go Modular Monolith Structure and Import Boundary Enforcement | [x] | G-01 / O-001 resolved |
| 0A-11 | ADR-002 | Session Storage Strategy: Server-Side Backing Store | [x] | G-02 / O-002 resolved |
| 0A-12 | ADR-003 | Transactional Outbox Design | [x] | G-03 / O-003 resolved |
| 0A-13 | ADR-004 | Authentication: Passkeys + Argon2id | [x] | — (documents D-009) |
| 0A-14 | ADR-007 | EİDS Integration Design | [x] | D-010 (legal req) |
| 0A-15 | ADR-009 | Multi-Language Rollout Sequencing for V1 | [x] | G-08 architecture / O-008 architecture resolved (rollout sequencing + library/vendor remain open, not Phase 0A blockers) |
| 0A-16 | ADR-011 | Cross-Domain Communication Rules | [x] | G-05 / O-005 architecture resolved; D-015 |
| 0A-33 | ADR-013 | Management Center Architecture | [x] | D-020; G-09 / O-009 resolved (option A: `web/apps/admin`) |
| 0A-34 | ADR-014 | Platform Locale Architecture: Four-Language Set and RTL | [x] | SUPERSEDED / NOT NEEDED (duplicates ADR-009) |
| 0A-17 | ADR-005 | Search Backend for V1 | [x] | G-04 / O-004 resolved |
| 0A-18 | ADR-006 | Notification Delivery Channels V1 | [ ] | — (G-05 resolved by ADR-011; launch params remain open) |
| 0A-19 | ADR-008 | AI Capability Boundaries | [ ] | D-011 |
| 0A-20 | ADR-010 | Provider Abstraction Layer | [ ] | D-006, D-007 |
| 0A-21 | ADR-012 | Pilot Geography and Category Bootstrap | [x] | G-07 / O-007 resolved |

---

## Group 4: Domain Specification Files ⬜ Not started

Write each domain spec in `/docs/domains/` following the domain spec template at the bottom of this file.

**Priority set (V1 CORE domains — start here):**

| ID | File | Domain | Status | Activation Phase |
|---|---|---|---|---|
| 0A-22 | `identity.md` | Identity / Auth | [ ] | V1 |
| 0A-23 | `users.md` | Users | [ ] | V1 |
| 0A-24 | `location.md` | Location / Geo | [ ] | V1 |
| 0A-25 | `master-data.md` | Master Data / Categories | [ ] | V1 |
| 0A-26 | `trust.md` | Trust / Verification | [ ] | V1 |

**V1 MARKETPLACE + TRUST & OPS domains:**

| ID | File | Domain | Status | Activation Phase |
|---|---|---|---|---|
| 0A-27 | `listings.md` | Listings | [ ] | V1 |
| 0A-28 | `business-profiles.md` | Business Profiles | [ ] | V1 |
| 0A-29 | `needs-matching.md` | Needs / Matching (incl. ranking) | [ ] | V1 |
| 0A-30 | `compliance-eids.md` | Compliance / EİDS | [ ] | V1 |
| 0A-30h | `moderation.md` | Moderation | [ ] | V1 (basic) |
| 0A-30frd | `fraud.md` | Fraud (signal ingestion) | [ ] | V1 (signal ingestion) |
| 0A-30ce | `case-engine.md` | Case Engine (minimal) | [ ] | V1 (minimal) |
| 0A-30mc | `management-center.md` | Management Center | [ ] | V1 (V1 surfaces) |

**Additional V1 domain specs:**

| ID | File | Domain | Status | Activation Phase |
|---|---|---|---|---|
| 0A-30b | `media.md` | Media | [ ] | V1 |
| 0A-30c | `search-discovery.md` | Search / Discovery | [ ] | V1 (G-04 resolved) |
| 0A-30d | `messaging.md` | Messaging | [ ] | V1 |
| 0A-30e | `notifications.md` | Notifications | [ ] | V1 (G-05 resolved) |
| 0A-30f | `reviews.md` | Reviews | [ ] | V1 |
| 0A-30g | `favorites.md` | Favorites / Saved | [ ] | V1 |
| 0A-30i | `audit.md` | Audit | [ ] | V1 |
| 0A-30j | `feature-flags.md` | Feature Flags | [ ] | V1 |
| 0A-30k | `observability.md` | Observability | [ ] | V1 |
| 0A-30l | `integrations.md` | Integrations | [ ] | V1 |

---

## Group 5: Architecture Gate Review ✅ PASS

| ID | Task | Status | Notes |
|---|---|---|---|
| 0A-32 | Architecture gate review: V1 blocking gates G-01–G-05, G-07–G-09 resolved; G-06 deferred V1.5 | [x] | **PASS.** Implementation/provider/config leftovers (ADR-006/008/010, O-005/O-008 launch params) do not block Phase 0A. |

**Gate checkpoint:** Architecture gate **PASS**. Remaining V1 blocking gates: none. G-06 (Corporate Workspace) is V1.5 and must not block V1 Phase 0A completion. No V1 implementation until remaining Phase 0A documentation (Group 4) is complete.

---

## Domain Spec Template

When writing a domain spec in `/docs/domains/`, use this structure:

```markdown
# Domain: [Name]

**Tier:** CORE / MARKETPLACE / ENGAGEMENT / TRUST & OPERATIONS / COMMERCIAL / PLATFORM
**Activation Phase:** V1 / V1.5 / V2
**Owner:** (team or module name — TBD until team is formed)

---

## Responsibility

[One paragraph describing what this domain is responsible for.]

## Owned Data

[List of tables or data entities owned exclusively by this domain.]

## Exposed Contracts

[List of function signatures or HTTP endpoints that other domains may call.]

## Dependencies

[List of domains this domain depends on, and which contracts it uses.]

## Prohibited Responsibilities

[Explicit list of things this domain must NOT do.]

## Open Questions

[Any unresolved questions that must be answered before implementation begins.]

## Schema Notes

[High-level notes on key tables — not full migrations.]

## Event Contracts

[In-process events this domain emits or consumes.]

## Outbox Events

[Outbox events this domain writes. Include trigger condition and consumer.]

## Management Center Interface

[What operational views or actions this domain exposes to the Management Center via admin contracts.]
```
