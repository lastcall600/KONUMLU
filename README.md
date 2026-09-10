# KONUMLU

**Location-first. Trust-first. Local marketplace for Türkiye.**

KONUMLU connects people with local businesses, services, and each other through a shared foundation of Identity, Location, and Trust. The platform is initially piloted in Fethiye/Muğla and designed to scale across Türkiye.

---

## What KONUMLU Is

KONUMLU is a local marketplace and community platform where:

- **Buyers and residents** post Needs, browse Listings, find local services, and connect with trusted providers.
- **Local businesses and service providers** build verified profiles, respond to local demand, post Listings and service offers, and grow community trust.
- **Operations and compliance teams** manage moderation, fraud, EİDS verification, and cases through the Management Center.

The platform differentiates through its **Need/Matching flow**: a user states a local demand → the system resolves category and location → finds and notifies eligible verified providers → enables response, offer, and messaging — all grounded in geography and trust.

---

## Pilot Geography

**Fethiye / Muğla, Türkiye** — Phase 0A and V1 focus.

---

## Tech Stack

| Layer | Technology |
|---|---|
| Backend | Go — modular monolith |
| Web | Next.js / React / TypeScript |
| Mobile | React Native |
| Primary DB | PostgreSQL + PostGIS |
| Cache / Session / Rate / Presence | Valkey |
| Object Storage | S3-compatible abstraction; MinIO for local dev |
| Maps | MapLibre + PostGIS |
| Auth | Passkeys (FIDO2/WebAuthn) first-class; Argon2id passwords |

---

## Key Documents

| Document | Purpose |
|---|---|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Authoritative architecture specification |
| [DECISIONS.md](./DECISIONS.md) | Frozen and open technical decisions |
| [CURRENT-STATE.md](./CURRENT-STATE.md) | Active phase, progress, deferred work |
| [PROJECT-MAP.md](./PROJECT-MAP.md) | Repository layout and directory guide |
| [AGENTS.md](./AGENTS.md) | AI agent operating norms and constraints |
| [docs/MASTER-SPEC.md](./docs/MASTER-SPEC.md) | Full product scope, slices, and phases |
| [docs/ADR/INDEX.md](./docs/ADR/INDEX.md) | Architecture Decision Records index |

---

## Current Phase

**Phase 0A — Architecture Foundation**

Documentation and architecture baseline only. No application source code, no framework initialization, no migrations, no running services.

See [CURRENT-STATE.md](./CURRENT-STATE.md) for the full phase status.

---

## Repository Structure

```
/                        → Root orientation files
/docs/                   → Architecture docs, ADRs, domain specs, task lists
/docs/ADR/               → Architecture Decision Records
/docs/architecture/      → Detailed architecture diagrams and supplements
/docs/domains/           → Individual domain specification files
/docs/TASKS/             → Phase and sprint task lists
```

Application source directories are not yet initialized. See [PROJECT-MAP.md](./PROJECT-MAP.md) for the intended layout.

---

## Architecture Principles (Summary)

1. Every domain has one clear owner.
2. Domain data ownership must be explicit.
3. Arbitrary cross-domain DB access is forbidden.
4. Cross-domain interaction uses explicit interfaces/contracts.
5. PostgreSQL remains canonical; cache/search/analytics are derived.
6. Business rules must not be duplicated across surfaces.
7. Provider-specific infrastructure details must not leak into business logic.
8. Complex technology inside, simple UX outside.
9. Security, audit, privacy, and compliance are architecture concerns from day one.
10. Do not optimize prematurely.

Full detail in [ARCHITECTURE.md](./ARCHITECTURE.md).

---

## Compliance Notes

- **EİDS** mandatory verification must never be illegally bypassed.
- **AI** must never become the authorization, identity-verification, EİDS-verification, or irreversible fraud authority.
- Browser authentication must avoid durable localStorage auth tokens.

---

## Contributing

Read [AGENTS.md](./AGENTS.md) before making changes if you are an AI agent.
Read [DECISIONS.md](./DECISIONS.md) before proposing changes to frozen technical direction.
Follow the active phase task sequence in [docs/TASKS/](./docs/TASKS/).
