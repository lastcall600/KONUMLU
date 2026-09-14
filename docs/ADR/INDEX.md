# ADR Index

> All Architecture Decision Records for KONUMLU.
> Each ADR captures a significant architectural decision: context, decision, consequences, and status.
> ADRs are numbered sequentially. Never renumber an existing ADR.
>
> File naming: `ADR-NNN-short-title.md` (e.g., `ADR-001-go-monolith-structure.md`)

---

## Index

| # | Title | Status | Resolves | Priority |
|---|---|---|---|---|
| ADR-001 | Go Modular Monolith Structure and Import Boundary Enforcement | ✅ Accepted | D-001, O-001 | High |
| ADR-002 | Session Storage Strategy: Server-Side Backing Store | ✅ Accepted | D-016, O-002 | High |
| ADR-003 | Transactional Outbox: Schema, Relay Design, and Concurrency Strategy | ✅ Accepted | D-008, O-003 | High |
| ADR-004 | Authentication: Passkeys (FIDO2/WebAuthn) First-Class, Argon2id Fallback | ✅ Accepted | D-009 | High |
| ADR-005 | Search Backend for V1: PostgreSQL Full-Text vs. Dedicated Engine | ✅ Accepted | D-018, O-004 (G-04 resolved) | Medium |
| ADR-006 | Notification Delivery Channels and Provider Abstraction for V1 | 📋 Pending | O-005 | Medium |
| ADR-007 | EİDS Integration Design and Compliance Domain Ownership | ✅ Accepted | D-010 | High |
| ADR-008 | AI Capability Boundaries and Human Review Requirements | 📋 Pending | D-011 | Medium |
| ADR-009 | Multi-Language Rollout Sequencing for V1 | ✅ Accepted | D-017, O-008 (architecture / G-08) | High |
| ADR-010 | Provider Abstraction Layer: Storage, Notifications, Maps | 📋 Pending | D-006, D-007 | Medium |
| ADR-011 | Cross-Domain Communication: Contract Interfaces and Forbidden Patterns | ✅ Accepted | D-015, O-005 (G-05) | High |
| ADR-012 | Pilot Geography: Fethiye/Muğla Data Sources and Category Taxonomy Bootstrap | ✅ Accepted | O-007 (G-07 resolved) | Medium |
| ADR-013 | Management Center Architecture: Control-Plane Design and Domain Interface Requirements | ✅ Accepted | D-020, O-009 (G-09 resolved) | High |
| ADR-014 | Platform Locale Architecture: Four-Language Set, RTL Support, and i18n Strategy | 🔄 Superseded / NOT NEEDED (ADR-009) | — (duplicates ADR-009) | — |
| ADR-015 | Türkiye Compliance Gateway and Data Residency Boundary | 🔒 FROZEN / ✅ Accepted | D-010, D-012, ADR-007 (residency; does not reopen no-bypass) | High |

---

## Recommended Authoring Order

ADR-001 → ADR-002 → ADR-003 → ADR-004 → ADR-007 → ADR-009 → ADR-011 → ADR-013 → ADR-005 → ADR-006 → ADR-008 → ADR-010 → ADR-012

High-priority ADRs 001, 002, 003, 004, 007, 009, 011, 013 are accepted. ADR-005 and ADR-012 are accepted (G-04, G-07 resolved). ADR-014 is superseded / not needed (duplicates ADR-009). ADR-015 freezes the Türkiye Compliance Gateway / data residency boundary (main platform remains Germany/Hetzner). Remaining V1 blocking gates for 0A-32: none. G-06 (Corporate Workspace) is V1.5 and does not block V1 Phase 0A.

---

## Statuses

| Symbol | Meaning |
|---|---|
| 📋 Pending | ADR not yet written |
| ✅ Accepted | Decision is final |
| 🔄 Superseded | Replaced by a newer ADR (link provided) |
| ❌ Rejected | Proposed and rejected (kept for record) |

---

## ADR Template

When writing a new ADR, use this structure:

```markdown
# ADR-NNN: Title

**Status:** Accepted / Pending / Superseded by ADR-XXX
**Date:** YYYY-MM-DD
**Resolves:** D-XXX / O-XXX (if applicable)

## Context

[What situation or problem is this decision addressing?]

## Decision

[What was decided?]

## Consequences

### Positive
- [benefit]

### Negative / Trade-offs
- [trade-off]

### Constraints imposed
- [what this decision prohibits or requires]
```

