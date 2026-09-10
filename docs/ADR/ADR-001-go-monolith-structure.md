# ADR-001: Go Modular Monolith Structure and Import Boundary Enforcement

**Status:** ✅ Accepted  
**Date:** 2026-09-05  
**Resolves:** D-001, O-001  
**Gates resolved:** G-01 — unblocks all Go backend development

---

## Context

KONUMLU is a location-first marketplace platform with ~25 discrete business domains spanning authentication, listings, needs/matching, trust, compliance, messaging, and more. The team must:

1. Ship a working V1 quickly without introducing distributed-systems complexity before the domain model is proven.
2. Maintain clear domain ownership so that any domain can be extracted to an independent service later, once its API contract is stable and scale demand justifies the cost.
3. Prevent the codebase from drifting into a "big ball of mud" where any package can depend on any other package, making the system impossible to reason about or safely change.

Decision D-001 (frozen) establishes the Go modular monolith as the backend architecture. Open decision O-001 asked specifically how the Go module and package layout should be structured and how domain isolation should be enforced at CI time.

Three options were considered:

- **Option A:** Flat `internal/` packages (e.g., `internal/identity/`, `internal/listings/`) with a linting rule enforcing import boundaries at CI time.
- **Option B:** Go workspace (`go.work`) with per-domain sub-modules inside the monorepo.
- **Option C:** Single module with a custom linter or third-party `go-module-boundary` tool.

Option A is selected. Option B and C are rejected (see Alternatives Considered).

---

## Decision

The KONUMLU Go backend is a **single Go module** (`go.mod` at `backend/`) organized into a structured internal package hierarchy. Domain isolation is enforced through **import boundary linting and an explicit contract sub-package structure** at CI time, not through Go module boundaries.

### Why a dedicated `contracts/` sub-package, not a `contract.go` file

Go visibility is package-level, not file-level. A `contract.go` file that lives inside `internal/listings/` belongs to `package listings` — the same package as `repository.go`, `usecase.go`, and every other file in that directory. Any other package that imports `internal/listings` to reach the interface types also gains access to every exported symbol in that package, including repository types, use-case structs, and domain internals that should never be visible outside the domain.

The only Go-native way to express "import this surface but not that one" is to put the two surfaces in separate packages. Therefore, each domain exposes its contract through a dedicated `contracts/` sub-package:

```
internal/listings/contracts/   ← importable by other domains
internal/listings/             ← implementation package; must NOT be imported by other domains
```

The `contracts/` package is intentionally minimal: it contains only interface definitions and the types those interfaces use (input/output value types). It has no dependencies on the implementation package. Other domains import `internal/listings/contracts` and nothing else from the listings tree.

`contracts/` is preferred over `ports/` because it maps directly to the language used in ARCHITECTURE.md ("contract interface") and is self-documenting to a new reader without prior knowledge of hexagonal architecture terminology.

### Package Layout

```
backend/
├── cmd/
│   ├── server/          # HTTP server binary entry point
│   ├── migrate/         # SQL migration CLI (golang-migrate); see OI-002
│   └── worker/          # Outbox relay / background worker binary entry point
├── migrations/          # Numbered SQL migrations only (no ORM, no Go schema migrations)
│
├── internal/
│   ├── platform/        # Shared platform primitives (see Platform Package rules)
│   │   ├── db/          # Database connection pool, transaction helpers
│   │   ├── outbox/      # Outbox table schema, writer, relay interface
│   │   ├── eventbus/    # In-process event bus (publish / subscribe)
│   │   ├── cache/       # Cache abstraction (Valkey adapter)
│   │   ├── config/      # Environment-variable-driven configuration
│   │   ├── observability/ # Structured logging, tracing, metrics interfaces
│   │   └── testutil/    # Shared test helpers (only imported by _test.go files)
│   │
│   ├── identity/
│   │   ├── contracts/   # Exported contract interface and its types — ONLY import surface
│   │   └── ...          # Implementation files (handler, usecase, domain, repository, events)
│   ├── users/
│   │   ├── contracts/
│   │   └── ...
│   ├── location/
│   │   ├── contracts/
│   │   └── ...
│   ├── trust/
│   │   ├── contracts/
│   │   └── ...
│   ├── masterdata/
│   │   ├── contracts/
│   │   └── ...
│   ├── listings/
│   │   ├── contracts/
│   │   └── ...
│   ├── business/
│   │   ├── contracts/
│   │   └── ...
│   ├── services/
│   │   ├── contracts/
│   │   └── ...
│   ├── needs/
│   │   ├── contracts/
│   │   └── ...
│   ├── search/
│   │   ├── contracts/
│   │   └── ...
│   ├── media/
│   │   ├── contracts/
│   │   └── ...
│   ├── favorites/
│   │   ├── contracts/
│   │   └── ...
│   ├── messaging/
│   │   ├── contracts/
│   │   └── ...
│   ├── notifications/
│   │   ├── contracts/
│   │   └── ...
│   ├── reviews/
│   │   ├── contracts/
│   │   └── ...
│   ├── moderation/
│   │   ├── contracts/
│   │   └── ...
│   ├── fraud/
│   │   ├── contracts/
│   │   └── ...
│   ├── caseengine/
│   │   ├── contracts/
│   │   └── ...
│   ├── compliance/
│   │   ├── contracts/
│   │   └── ...
│   ├── audit/
│   │   ├── contracts/
│   │   └── ...
│   ├── featureflags/
│   │   ├── contracts/
│   │   └── ...
│   ├── mgmt/            # Management Center route handlers and admin contracts
│   │   └── ...
│   │
│   └── infrastructure/  # Provider adapter implementations
│       ├── storage/     # S3-compatible StorageService implementation (MinIO)
│       ├── notifications/ # NotificationSender implementation (FCM, APNs, etc.)
│       ├── maps/        # TileProvider implementation
│       └── payments/    # PaymentGateway implementation (V2 only)
```

### Why `infrastructure/` lives under `internal/`

Provider adapter implementations (MinIO client wrappers, FCM adapters, etc.) are internal to this binary. They are not a published library and have no consumers outside this repository. Placing them under `internal/` gives the Go toolchain's own visibility rules as a first line of defense: no code outside the `backend/` module can import these packages. This is a stricter guarantee than a linting rule alone. The composition root in `cmd/server/` — which is inside the same module — can still import them for wiring.

### Domain Package Internal Structure

Each domain (e.g., `internal/listings/`) follows this layout:

```
internal/listings/
├── contracts/
│   ├── service.go       # Service interface definition
│   └── types.go         # Input/output value types used by the interface
├── handler.go           # HTTP route handlers (calls use-case layer)
├── usecase.go           # Use-case / application orchestration logic
├── domain.go            # Domain types, value objects, domain rules
├── repository.go        # Database access — private to this domain
├── events.go            # In-process event definitions (published by this domain)
└── listings_test.go     # Domain tests
```

Larger domains may further subdivide `handler.go`, `usecase.go`, or `repository.go` into sub-directories. The rule that must not change: `contracts/` is the only sub-tree that other domains may import.

### Contract Interface Rules

Every domain that is called by other domains **must** define its contract in `internal/<domain>/contracts/`. The contract package:

1. Expresses intent in domain language — method names reflect business operations, not data operations.
2. Returns domain types, not raw database rows or ORM structs.
3. Has **no import** of the parent domain's implementation package (`internal/<domain>/`). The contract package must be importable in isolation.
4. Is explicitly versioned when breaking changes are required (add a `v2/` sub-package; do not mutate a stable interface in place).

Example (illustrative — not source code):

```go
// internal/identity/contracts/service.go
package contracts

import "context"

// Service is the contract interface for the Identity domain.
// Other domains import internal/identity/contracts — never internal/identity directly.
type Service interface {
    AuthenticateUser(ctx context.Context, credentials Credentials) (Session, error)
    ValidateSession(ctx context.Context, token string) (UserID, error)
    RegisterPasskey(ctx context.Context, userID UserID, attestation []byte) (PasskeyID, error)
    InvalidateSession(ctx context.Context, token string) error
}
```

The implementation package satisfies this interface:

```go
// internal/identity/usecase.go
package identity

import "backend/internal/identity/contracts"

type service struct { /* ... */ }

// Compile-time assertion that service satisfies the contract.
var _ contracts.Service = (*service)(nil)
```

### Application / Use-Case Orchestration

Use-case / application orchestration logic lives **inside the domain implementation package** (`usecase.go` or equivalent), not in a shared application layer. The `cmd/server/` entry point wires together domain implementations and starts the HTTP server. There is no separate `application/` package that coordinates across domains — cross-domain coordination happens through contract calls from within use-case layers.

The `cmd/server/` wiring layer (composition root):

- Instantiates each domain's concrete implementation.
- Injects dependencies (contract implementations from `internal/<domain>/contracts/`) into each domain that needs them.
- Registers HTTP handlers.
- Does not contain business logic.

### Platform Package Rules

`internal/platform/` contains **shared primitives** that are not owned by any single domain. Rules:

1. Platform packages must have **no imports** from any domain package (`internal/<domain>/` or `internal/<domain>/contracts/`). Platform is the lowest tier.
2. Platform packages expose **interfaces**, not concrete implementations (except DB connection helpers, which are necessarily concrete).
3. The outbox writer (`internal/platform/outbox`) writes outbox records; it does not know about domain-specific event payloads.
4. The event bus (`internal/platform/eventbus`) dispatches typed events; it does not contain domain logic.
5. `internal/platform/testutil/` is only imported in `_test.go` files; it must never appear in production import paths.

### Infrastructure Package Rules

`internal/infrastructure/` packages implement the provider abstraction interfaces defined by the platform or by individual domain contracts packages.

1. Infrastructure packages may import domain contract packages (e.g., `internal/media/contracts` for the `StorageService` interface, `internal/notifications/contracts` for `NotificationSender`).
2. Infrastructure packages must NOT import domain implementation packages (`internal/<domain>/` directly — only `internal/<domain>/contracts/`).
3. Infrastructure packages must NOT be imported by domain implementation packages. The dependency flows: domain implementation → contracts interface ← infrastructure (implements the interface). Wiring happens at the composition root only.
4. Infrastructure-specific SDKs (AWS SDK, MinIO client, FCM SDK) are confined to `internal/infrastructure/` packages.

### Allowed Import Directions (Summary)

```
cmd/*
  → internal/<domain>/           (for constructors / wiring)
  → internal/<domain>/contracts/ (for interface types)
  → internal/platform/
  → internal/infrastructure/

internal/<domain>/  (implementation package)
  → internal/platform/
  → internal/<other-domain>/contracts/   (ONLY — never the implementation package)
  NOT → internal/<other-domain>/          (implementation)
  NOT → internal/infrastructure/

internal/<domain>/contracts/
  → internal/platform/           (for shared primitive types only, if needed)
  NOT → internal/<domain>/        (must not import own implementation)
  NOT → internal/<other-domain>/  (no cross-domain implementation imports)
  NOT → internal/infrastructure/

internal/platform/
  → (stdlib and approved third-party packages only)
  NOT → internal/<domain>/
  NOT → internal/<domain>/contracts/
  NOT → internal/infrastructure/

internal/infrastructure/<adapter>/
  → internal/platform/
  → internal/<domain>/contracts/ (implements a contract interface)
  NOT → internal/<domain>/        (implementation package)
  NOT → cmd/
```

### Dependency Tier Enforcement

The import rules above implement the tier dependency order defined in ARCHITECTURE.md §5:

```
PLATFORM tier packages  (internal/platform/)
  ↑ imported by
CORE tier domains       (identity, users, location, trust, masterdata)
  ↑ imported by
MARKETPLACE tier domains (listings, business, services, needs, search, media)
  ↑ imported by
ENGAGEMENT tier domains  (favorites, messaging, notifications, reviews)
  ↑ imported by
TRUST & OPS tier domains (moderation, fraud, caseengine, compliance)
  ↑ imported by
COMMERCIAL tier domains  (billing, transactions, payments, delivery, disputes) [V2]
  ↑ imported by
MANAGEMENT CENTER        (mgmt/)
```

Lower-tier domains must not import higher-tier domains. Within a tier, circular imports are forbidden.

---

## Alternatives Considered

### Option B — Go workspace (`go.work`) with per-domain sub-modules

**Rejected.** Go workspace with per-domain sub-modules (`backend/domains/identity/go.mod`, etc.) provides hard module boundaries enforced by the Go toolchain. However:

- It introduces significant operational complexity: separate `go.mod` and `go.sum` files per domain, cross-module replace directives, and workspace-level dependency management.
- It does not fit the monorepo publishing model (we are not publishing these as independent importable modules).
- It makes atomic refactoring across domain boundaries unnecessarily difficult, which is counterproductive in the early phase when domain boundaries are still being validated.
- The Go workspace model was designed for multi-module repos with published libraries, not for internal modular monolith enforcement.
- Linting rules achieve the same enforcement goal with far less toolchain ceremony.

### Option C — Single module with a third-party `go-module-boundary` tool or custom linter

**Considered and partially adopted.** The selected approach (Option A) already uses a custom linting rule. The difference from Option C as originally stated is that Option C implied relying on a pre-existing third-party linter that may not have the specific import path semantics needed. The approach adopted writes a `depguard`-based linting rule configuration that encodes the exact allowed/forbidden import paths for this project. Third-party tools may be used as the enforcement mechanism (see Enforcement Rules); the decision is that we own the rule definitions, not that we write a linter from scratch.

### `ports/` instead of `contracts/` sub-package naming

**Rejected.** `ports/` is the standard term in hexagonal (ports and adapters) architecture. However, ARCHITECTURE.md and DECISIONS.md consistently use "contract" and "contract interface" throughout. Using `contracts/` keeps the directory name consistent with the rest of the project vocabulary and is immediately understandable to a reader who has not studied hexagonal architecture.

---

## Consequences

### Positive

- **Single `go.mod`** means simple, unified dependency management. `go get`, `go mod tidy`, and `go build ./...` work on the whole backend without workspace complexity.
- **`contracts/` sub-packages** enforce the import surface at the Go package level. The compiler itself prevents importing `internal/listings` when you only declared a dependency on `internal/listings/contracts`. A linter is still required to prevent importing `internal/listings` directly when `internal/listings/contracts` is what should be imported, but the boundary between "public contract" and "private implementation" is now structural rather than purely conventional.
- **No gRPC inside the monolith.** Cross-domain calls are ordinary Go method calls on interface values — zero serialization overhead, full type safety, debuggable with a standard Go debugger.
- **Use-case logic inside domain packages** keeps business rules co-located with the data they operate on.
- **Platform package** provides a genuine shared foundation for outbox, event bus, cache, and observability without creating a shared dumping ground.
- **`internal/infrastructure/`** confines all provider SDKs and is protected by Go's own `internal/` visibility rules — no external consumer can import these adapters.
- **Service extraction path is clear:** when a domain must be extracted, its `contracts/` package becomes the service API with minimal translation, and its `repository.go` becomes the new service's data layer.

### Negative / Trade-offs

- **Linting rules are still needed.** The contracts sub-package prevents importing the wrong surface, but a domain could still import `internal/<other-domain>` directly rather than `internal/<other-domain>/contracts`. The linter must block this. CI must fail on linting errors; linting cannot be skipped.
- **`contracts/` adds a sub-directory per domain.** ~25 domains means ~25 additional directories. This is mechanical and predictable; the tooling overhead is minimal.
- **All domains build and test together.** In a large team, slow test suites affect everyone. Mitigation: maintain fast unit test suites per domain; integration tests are tagged and run separately.
- **Composition root can become complex.** With ~25 domains, `cmd/server/main.go` wiring can grow. Mitigation: use structured constructor injection; consider splitting wiring into per-tier wire files if it exceeds ~300 lines.

### Constraints Imposed

- Every domain that receives calls from other domains must have an `internal/<domain>/contracts/` package exporting a stable interface. The domain implementation package (`internal/<domain>/`) must not be imported by other domains.
- `internal/<domain>/contracts/` must never import its own domain's implementation package.
- `internal/platform/` must have zero imports from any `internal/<domain>/` or `internal/<domain>/contracts/` package.
- `internal/infrastructure/` packages must not be imported by domain implementation packages. Dependency direction is: domain implementation → contracts interface ← infrastructure; wiring at composition root only.
- There must be no generic "shared" package that accumulates miscellaneous domain types from multiple domains. Shared primitives live in `internal/platform/` under a named sub-package. Cross-domain types are accessed only through contract return types.
- Future commercial-tier domains (Billing, Transactions, Payments, Delivery, Disputes) must not be created until their activation phase (V2) per DECISIONS.md and ARCHITECTURE.md §16.
- Adding a new domain requires creating both `internal/<domain>/` and `internal/<domain>/contracts/`, and updating this ADR's package layout.

---

## Enforcement Rules

These rules must be implemented before any Go source code is written. They are not optional. The CI pipeline must fail if any rule is violated.

### Rule E-001 — Import boundary linter

A linting rule (using `depguard`, `go-cleanarch`, or an equivalent tool agreed upon before V1 implementation begins) must be configured with the following enforced constraints:

| Rule | Import from | Must NOT import |
|---|---|---|
| Domain implementation | `internal/<domain>/` | `internal/<other-domain>/` (implementation) |
| Domain implementation | `internal/<domain>/` | `internal/infrastructure/` |
| Domain contracts | `internal/<domain>/contracts/` | `internal/<domain>/` (own implementation) |
| Domain contracts | `internal/<domain>/contracts/` | `internal/infrastructure/` |
| Platform packages | `internal/platform/` | `internal/<domain>/` |
| Platform packages | `internal/platform/` | `internal/<domain>/contracts/` |
| Platform packages | `internal/platform/` | `internal/infrastructure/` |
| Infrastructure packages | `internal/infrastructure/` | `internal/<domain>/` (implementation) |
| Infrastructure packages | `internal/infrastructure/` | `cmd/` |

Violations are **build-blocking errors**, not warnings.

### Rule E-002 — No cross-domain database access

No domain package (`internal/<domain>/`) may contain a SQL query or ORM call that references another domain's tables. Table ownership is defined by the domain's `repository.go`. Enforcement: SQL query string linting (via `sqlvet` or grep-based CI check) plus code review. Any query referencing another domain's schema prefix in a domain package is a blocker.

### Rule E-003 — Contract package completeness

Every domain listed as a dependency by another domain in ARCHITECTURE.md §4 must have an `internal/<domain>/contracts/` package with an exported service interface. The CI pipeline includes a structural check (or code review gate) that verifies this.

### Rule E-004 — No shared dumping-ground packages

No package named `shared/`, `common/`, `util/`, `helpers/`, or similar may exist under `internal/` at the cross-domain level. Shared primitives live in `internal/platform/` under a named sub-package that clearly describes their purpose. Cross-domain types that are not platform primitives are accessed only through contract return types.

### Rule E-005 — Architecture test (dedicated, not `go test ./...`)

`go test ./...` by itself does not enforce import boundaries — it only runs test functions. A dedicated architecture check must be run as a separate CI step that inspects the compiled import graph and fails the build when any rule in E-001 is violated.

The recommended approach is a small Go program or `golangci-lint` configuration that uses `golang.org/x/tools/go/packages` to load the full import graph and assert:

1. No `internal/<domain>/` package imports another domain's implementation package.
2. No `internal/<domain>/contracts/` package imports its own domain's implementation package.
3. No `internal/platform/` package imports any domain or infrastructure package.
4. No `internal/<domain>/` package imports `internal/infrastructure/`.
5. No import cycle exists within any tier.
6. No package path matching `internal/shared`, `internal/common`, `internal/util`, or `internal/helpers` exists.

This check runs as `make arch-check` (or equivalent) in CI, separate from `go test ./...`, and must pass before any merge.

### Rule E-006 — Composition root is the only wiring point

Dependency injection (passing concrete implementations to domain constructors) must happen only in `cmd/server/main.go` (or files directly imported by it for wiring purposes). Domain packages must not use `init()` functions, global variables, or singleton patterns to resolve dependencies. Constructor injection is the required pattern.

---

## Open Items

### OI-001 — Linter tool selection

The specific tooling used to enforce Rule E-001 and Rule E-005 is not selected in this ADR. Before any Go source files are committed, a follow-up task must select and configure one of:

- `depguard` (rule-based import guard, widely used, integrates with `golangci-lint`)
- `go-cleanarch` (tier-aware architecture linter)
- A small bespoke binary using `golang.org/x/tools/go/packages`

The chosen tool must be runnable locally with a single command (`make arch-check`) and integrated into CI before the first domain package is created.

**Owner:** Architecture lead / first Go engineer on the team.  
**Deadline:** Before Task 0A-22 (domain spec for Identity / Auth) is implemented in V1.

### OI-002 — Migration tooling

**Status:** ✅ RESOLVED (Phase 0B-04)

**Decision:** Use `golang-migrate/migrate` v4 with the PostgreSQL driver. Schema changes are numbered SQL files only under `backend/migrations/`. There is no ORM and no Go-based schema migration.

**CLI:** `backend/cmd/migrate` — `up`, `down 1`, `version`. `DATABASE_URL` comes from the environment. Credentials must never be logged.

**Policy:**
- Migrations are immutable once applied or shared. Fix forward with a new numbered SQL file; do not edit old migrations.
- Platform extensions (PostGIS) are enabled by SQL migration `000001_enable_postgis`. Down for that migration is an intentional no-op — do not automatically `DROP EXTENSION postgis`.
- Product/domain tables and seed/taxonomy data are not part of this foundation.

**Dependency:** Closed. Domain `repository.go` work may proceed after tables are added via new SQL migrations.

### OI-003 — Domain package naming for two-word domains

Some domains have two-word names (e.g., "Master Data", "Business Profiles", "Needs / Matching", "Feature Flags", "Case Engine"). The Go package name must be a single lowercase identifier. This ADR uses:

| Domain | Package path |
|---|---|
| Master Data / Categories | `internal/masterdata/` |
| Business Profiles | `internal/business/` |
| Needs / Matching | `internal/needs/` |
| Feature Flags | `internal/featureflags/` |
| Case Engine | `internal/caseengine/` |
| Search / Discovery | `internal/search/` |
| Favorites / Saved | `internal/favorites/` |
| Compliance / EİDS | `internal/compliance/` |

These names are **provisional**. Before the first Go file in any domain is committed, the team must confirm or revise the package naming. Once a package path is used in a committed contract interface, renaming requires a coordinated refactor.

---

*ADR-001 — authored Phase 0A, Task 0A-10. Corrected Phase 0A (contracts sub-package, architecture test, infrastructure visibility).*
