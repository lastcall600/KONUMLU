# Requirements Document

## Introduction

KONUMLU is a location-first, trust-first local marketplace and core platform for Türkiye, initially piloted in Fethiye/Muğla. The platform connects local buyers, sellers, and service providers through a shared foundation of Identity, Location, and Trust.

This requirements document covers the Architecture Foundation deliverable: establishing repository memory, architecture baseline documentation, and domain ownership contracts before any implementation begins. The deliverable is documentation-only — no application source code, no framework initialization, no dependency installs, no Docker services, and no database migrations.

## Glossary

- **Platform**: The KONUMLU system as a whole, running as a modular monolith in Go.
- **Domain**: A bounded unit of responsibility with a single owning team/module; examples: Identity, Location, Listings.
- **Domain Owner**: The module responsible for the domain's data, business rules, and exposed contracts.
- **Contract**: An explicit, versioned interface (function signature, HTTP handler, or in-process event) through which other domains interact with a domain's owned data.
- **Modular Monolith**: A single deployable unit organized into clearly bounded, independently evolvable modules with enforced inter-module dependency rules.
- **Source of Truth**: The authoritative data store for a piece of data; PostgreSQL + PostGIS for all canonical data.
- **Derived Store**: A secondary store (Valkey, search index, analytics DB) populated from the source of truth; never written to directly by business logic.
- **Outbox**: A transactional outbox table written atomically with domain state changes, used to drive reliable asynchronous and external effects.
- **In-Process Event**: A synchronous or async event dispatched within the monolith process boundary, used for safe derived effects that do not require at-least-once delivery guarantees.
- **EİDS**: Türkiye's Electronic Identity Verification System; mandatory for regulated operations.
- **Passkey**: A FIDO2/WebAuthn credential; the preferred first-class authentication method on KONUMLU.
- **Management Center**: The internal back-office surface for operations, moderation, compliance, and support teams.
- **Need**: A structured, location-tagged local demand created by a user that triggers provider matching.
- **Provider**: A verified business or individual service provider eligible to respond to Needs.
- **Slice**: A vertical end-to-end flow crossing multiple domains, used to sequence initial implementation work.
- **ADR**: Architecture Decision Record; a lightweight document capturing a significant architectural decision and its rationale.
- **Phase**: A named delivery increment (Phase 0A, V1, V1.5, V2) used to sequence domain activation.

---

## Requirements

### Requirement 1: Repository Memory and Orientation Files

**User Story:** As a developer or AI agent onboarding to KONUMLU, I want a set of root-level orientation files, so that I can immediately understand the project's purpose, current state, open decisions, and contribution norms.

#### Acceptance Criteria

1. THE Platform Repository SHALL contain a `/README.md` file at the repository root that describes the project purpose, pilot geography, tech stack, and links to key documentation.
2. THE Platform Repository SHALL contain a `/CURRENT-STATE.md` file that records the active phase, what has been built, what is in progress, and what is deferred.
3. THE Platform Repository SHALL contain a `/DECISIONS.md` file that lists frozen technical decisions with brief rationale and flags open decisions that require resolution.
4. THE Platform Repository SHALL contain an `/AGENTS.md` file that defines operating norms, scope, and constraints for AI agents contributing to the repository.
5. WHEN a new orientation file is created, THE Platform Repository SHALL place it at the repository root or in `/docs/` according to the file's defined location in the project map.
6. IF a decision recorded in `/DECISIONS.md` is frozen, THEN THE Platform Repository SHALL mark it explicitly as frozen so that agents and contributors do not re-open it without deliberate intent.

---

### Requirement 2: Architecture Specification

**User Story:** As a platform architect or senior engineer, I want a single authoritative architecture document, so that all design and implementation decisions can be validated against a shared baseline.

#### Acceptance Criteria

1. THE Platform Repository SHALL contain an `/ARCHITECTURE.md` file that defines system context, product surfaces, and modular monolith philosophy.
2. THE Architecture Document SHALL define domain boundaries, ownership rules, and the complete domain map organized by tier (CORE, MARKETPLACE, ENGAGEMENT, TRUST & OPERATIONS, COMMERCIAL, PLATFORM).
3. THE Architecture Document SHALL specify allowed dependency directions between tiers and between domains within a tier.
4. THE Architecture Document SHALL specify cross-domain communication rules: domains MUST interact only through explicit contracts; arbitrary cross-domain database access is forbidden.
5. THE Architecture Document SHALL specify source-of-truth rules: PostgreSQL + PostGIS is the canonical store; cache, search, and analytics stores are derived and MUST NOT be written to directly by business logic.
6. THE Architecture Document SHALL specify the async and outbox policy: in-process events are used for safe derived effects; the transactional outbox is used for reliable critical async and external effects.
7. THE Architecture Document SHALL specify provider abstraction rules: provider-specific infrastructure details MUST NOT leak into business logic.
8. THE Architecture Document SHALL define security boundaries, including authentication requirements (Passkeys first-class, Argon2id for passwords, no durable localStorage auth tokens in the browser).
9. THE Architecture Document SHALL state that EİDS mandatory verification MUST NEVER be illegally bypassed and that AI MUST NEVER serve as the authorization, identity-verification, EİDS-verification, or irreversible fraud authority.
10. THE Architecture Document SHALL describe the Management Center relationship to other domains.
11. THE Architecture Document SHALL define the local/staging/production environment philosophy.
12. THE Architecture Document SHALL include a domain dependency diagram expressed in Mermaid syntax.

---

### Requirement 3: Domain Map and Contracts

**User Story:** As a domain engineer, I want each domain's ownership, data, contracts, dependencies, and prohibited responsibilities documented, so that I can build within clear boundaries without duplicating business rules or accessing data I do not own.

#### Acceptance Criteria

1. THE Architecture Document SHALL define every domain listed in the domain map with the following attributes: responsibility, owned data, exposed contracts, dependencies on other domains, prohibited responsibilities, and activation phase.
2. THE Domain Map SHALL cover all six tiers: CORE (Identity, Users, Location, Trust, Master Data), MARKETPLACE (Listings, Business, Services, Needs/Matching, Search/Discovery, Media), ENGAGEMENT (Favorites/Saved, Messaging, Notifications, Reviews), TRUST & OPERATIONS (Moderation, Fraud, Case Engine, Compliance/EİDS), COMMERCIAL (Billing, Transactions, Payments, Delivery, Disputes), PLATFORM (Analytics, Audit, Feature Flags, Observability, Integrations).
3. WHEN a domain lists a dependency on another domain, THE Architecture Document SHALL reference the owning domain's exposed contract rather than its internal data model.
4. IF a domain is assigned to an activation phase later than Phase 0A, THEN THE Architecture Document SHALL mark it as deferred and exclude it from the initial active-domain set.
5. THE Architecture Document SHALL list the initial active-domain set and the deferred-domain set as explicit sections.

---

### Requirement 4: Project Map

**User Story:** As a contributor navigating a fresh repository, I want a project map document, so that I can locate any component, documentation directory, or domain module without guessing.

#### Acceptance Criteria

1. THE Platform Repository SHALL contain a `/PROJECT-MAP.md` file that lists every top-level directory and key file with its purpose.
2. THE Project Map SHALL describe the intended directory layout for domain modules, shared libraries, frontend surfaces, mobile surface, infrastructure-as-code, and documentation.
3. WHEN a new top-level directory is added to the repository, THE Project Map SHALL be updated to reflect the addition before the change is merged.

---

### Requirement 5: Master Specification

**User Story:** As an architect or product lead, I want a master specification document that aggregates all product domains, vertical slices, and implementation phases, so that any agent or contributor can understand the full intended scope without reading every individual spec.

#### Acceptance Criteria

1. THE Platform Repository SHALL contain a `/docs/MASTER-SPEC.md` file that describes the full product vision, domain tier structure, and phased delivery plan.
2. THE Master Spec SHALL define Slice A (register → login → create listing → upload photos → choose location → publish → search → map → detail → favorite) as the first vertical slice.
3. THE Master Spec SHALL define Slice B (create local need → category/location resolution → provider matching → notify eligible providers → response/offer → messaging) as the second vertical slice.
4. THE Master Spec SHALL map each slice to the domains it exercises and the phase in which it is targeted.
5. THE Master Spec SHALL record architecture risks, open architecture gates, and the list of ADRs that must be created.
6. THE Master Spec SHALL include the recommended Phase 0A task sequence.
7. IF a conflict or ambiguity is detected in the product or architecture direction, THEN THE Master Spec SHALL surface it explicitly rather than silently resolving it.

---

### Requirement 6: Architecture Decision Records Infrastructure

**User Story:** As a team member tracking significant technical decisions, I want an ADR directory and index, so that decisions are recorded, discoverable, and linked from architecture documents.

#### Acceptance Criteria

1. THE Platform Repository SHALL contain a `/docs/ADR/` directory.
2. THE ADR Directory SHALL contain an `INDEX.md` file listing all ADRs by number, title, status, and date.
3. THE Architecture Document SHALL reference the ADR index for all decisions that require formal recording.
4. WHEN a new ADR is created, THE ADR index SHALL be updated with the new entry before the ADR is considered complete.
5. THE Master Spec SHALL list the ADRs that must be created as part of the Phase 0A task sequence.

---

### Requirement 7: Documentation Directory Structure

**User Story:** As a contributor or AI agent, I want a well-defined documentation directory structure, so that domain specs, architecture diagrams, and task lists are always placed in predictable locations.

#### Acceptance Criteria

1. THE Platform Repository SHALL contain the following directories: `/docs/ADR/`, `/docs/TASKS/`, `/docs/architecture/`, `/docs/domains/`.
2. THE `/docs/architecture/` directory SHALL be the location for detailed architecture diagrams and supplementary architecture documents.
3. THE `/docs/domains/` directory SHALL be the location for individual domain specification files.
4. THE `/docs/TASKS/` directory SHALL be the location for phase and sprint task lists.
5. IF a document does not fit an existing directory, THEN THE contributor SHALL propose a new directory in `/PROJECT-MAP.md` before placing the file.

---

### Requirement 8: Need/Matching Flow Architecture

**User Story:** As a product architect, I want the Need/Matching flow defined as a first-class architecture concern, so that it coexists cleanly with traditional Listings without coupling the two flows.

#### Acceptance Criteria

1. THE Architecture Document SHALL define the Need/Matching flow as a distinct domain (Needs/Matching) within the MARKETPLACE tier.
2. THE Needs/Matching Domain SHALL own the lifecycle of a Need from creation through resolution without owning the Listing or Business domains' data.
3. WHEN a Need is created, THE Needs/Matching Domain SHALL resolve category and location through the Master Data and Location domains' exposed contracts.
4. WHEN eligible providers are identified, THE Needs/Matching Domain SHALL notify them through the Notifications domain's exposed contract.
5. THE Architecture Document SHALL specify that the Needs/Matching flow and the Listings flow share the Location and Identity domains but MUST NOT directly access each other's owned data.

---

### Requirement 9: AI Capability Boundaries

**User Story:** As a platform security and compliance lead, I want explicit AI capability boundaries documented in the architecture, so that AI features are never permitted to make irreversible trust, identity, or fraud decisions autonomously.

#### Acceptance Criteria

1. THE Architecture Document SHALL state that AI capabilities are permitted as assistive and ranking tools across appropriate domains.
2. THE Architecture Document SHALL state that AI MUST NOT serve as the sole authority for authorization decisions.
3. THE Architecture Document SHALL state that AI MUST NOT serve as the sole authority for identity verification or EİDS verification decisions.
4. THE Architecture Document SHALL state that AI MUST NOT make irreversible fraud determinations without a human review step.
5. WHEN an AI-assisted decision affects a user's account standing or legal compliance, THE System SHALL require a human review step before the decision is applied.

---

### Requirement 10: Agent Operating Norms

**User Story:** As a team lead managing AI-assisted development, I want an AGENTS.md file that constrains AI agent behavior, so that agents cannot silently bypass frozen decisions, install unapproved dependencies, or modify architecture baseline files without explicit instruction.

#### Acceptance Criteria

1. THE Platform Repository SHALL contain an `/AGENTS.md` file that lists permitted and prohibited agent actions.
2. THE Agents Document SHALL specify that agents MUST NOT re-open frozen decisions listed in `/DECISIONS.md` without explicit user instruction.
3. THE Agents Document SHALL specify that agents MUST NOT initialize application frameworks, install dependencies, or create Docker service configurations unless explicitly instructed.
4. THE Agents Document SHALL specify that agents MUST NOT modify `/ARCHITECTURE.md`, `/DECISIONS.md`, or `/AGENTS.md` without explicit user instruction.
5. THE Agents Document SHALL specify that agents MUST surface detected conflicts or ambiguities rather than silently resolving them.
6. THE Agents Document SHALL specify that agents MUST follow the task sequence defined in the active phase before starting new work.
