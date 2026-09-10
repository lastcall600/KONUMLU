# ADR-013: Management Center Architecture

**Status:** ✅ Accepted (control-plane architecture + O-009 / G-09 frontend model)  
**Date:** 2026-09-05  
**Resolves:** D-020; O-009 / G-09; architectural requirements for admin vs domain ownership, server-side authz, Staff IAM, 360 views, Case Engine, audit/masking/approvals/exports, domain commands, read models, ops boundaries, bulk actions, flags/config  
**Frontend model:** Option A — separate Next.js app (`web/apps/admin`), host `admin.konumlu.com`. Option B (`/mgmt` in the consumer app) is rejected.

---

## 1. Decision

The Management Center (MC) is KONUMLU’s **professional control-plane**: a first-class product surface from **V1**, not a deferred admin panel (D-020).

It is **not a business domain**. It does **not** own listing, user, case, trust, or payment tables. It does **not** contain a second copy of domain rules.

MC **HTTP adapters** live in `internal/mgmt/` (ADR-001). They authenticate **staff** sessions, authorize coarsely at the edge, then call **per-domain admin contracts**. Mutating work is always a **domain command**. Reads for operator 360/dashboard views are **composed** from those contracts (and optional derived read models fed by outbox), never by joining another domain’s schema.

**Frontend packaging (O-009 / G-09):** a **separate Next.js app** in this monorepo at **`web/apps/admin`**, deployed and configured independently, served on **admin.konumlu.com**. It **must not share auth context** with the public consumer/business web surface (O-009 constraint, D-016 cookies). It **must not** be implemented as `/mgmt/*` inside the consumer app.

Shared design tokens/components are allowed (e.g. via planned `/shared/`); they are optional and must not carry domain business rules. The MC frontend calls the same Go **admin contracts** as specified in this ADR. Directory `web/apps/admin` is **frozen as the planned path**; it is not created until V1 implementation is authorized.

No individual screens are specified here.

---

## 2. Context

ARCHITECTURE.md §11: MC is an operational surface; it orchestrates via admin contracts; it must not hold domain business logic, domain tables, or bypass EİDS/authorization. Public vs admin HTTP are registered separately. D-020 requires an operational interface in the **same phase** as each domain that produces moderatable content, verifiable entities, or caseable events. Advanced automation, fraud graph, SLA orchestration, and ML-assisted moderation wait for V1.5.

ADR-001 places `mgmt/` at the top of the import tier: it may call domain contracts; domains must not import `internal/mgmt`. Composition root (`cmd/server`) wires admin handlers separately from public API.

D-015 forbids cross-domain table access. D-010 forbids EİDS bypass. D-011 forbids AI as sole authority for standing/compliance. Case Engine is minimal in V1 (open/assign/update/close). Feature Flags already expose admin `UpdateFlag`.

---

## 3. Core product from V1, developed alongside each domain

- A domain that ships user-visible or caseable behavior in a phase **ships its MC operational slice in that same phase**: queue, 360 contribution, and the admin commands operators need to run it.
- “We’ll add admin later” is out of policy for V1 domains in ARCHITECTURE.md §15 (moderation, EİDS, users/listings, cases, fraud signal review, flags, …).
- V1.5 may add depth (graphs, SLA, ML tooling) **on the same contract model**, not a shadow admin database.
- MC UI completeness may lag visual polish; **absence of the admin contract and handler path** is a domain-delivery defect.

---

## 4. Admin surface vs domain ownership

| Layer | Owns | Must not |
|---|---|---|
| **Owning domain** | Canonical data, invariants, FSMs, EİDS checks, who may be banned/unpublished | Import `mgmt`; expose repository types to HTTP |
| **Domain `contracts/` admin API** | `Admin*` query/command interfaces and DTOs used only by MC handlers | Be registered on the public consumer API mux |
| **`internal/mgmt`** | Staff HTTP, request mapping, 360 composition, bulk job orchestration **calls** | Reimplement eligibility, trust math, EİDS, listing FSM, payment rules |
| **Derived read model (optional)** | Operator-speed projections | Become source of truth (D-004) |

Admin interfaces are **defined next to the owning domain** (`internal/<domain>/contracts`), not as a separate “admin domain” with its own rules. `internal/mgmt` may hold HTTP DTO types; those are not a second policy engine.

---

## 5. Server-side authorization boundary

1. **Edge (mgmt HTTP):** valid **staff** session; coarse role (e.g. can open MC; can hit `/mgmt/api/...`). Unauthenticated or consumer-session tokens are rejected. Consumer cookies must not authorize MC (separate auth context).
2. **Domain admin method:** fine-grained checks (this staff may `CloseCase` of this `caseType`; this staff may see unmasked national id). **Authorization is not complete at the router alone** (ARCHITECTURE.md §10).
3. **Frontend** may hide buttons; hidden UI is not authorization.
4. Public API handlers must not import or invoke admin contract methods.
5. Staff acting on a user’s resource still goes through the **domain** that owns the resource; MC is not a superuser SQL role.

Fail closed: missing permission → deny. AI recommendations never grant a capability (D-011).

---

## 6. Staff IAM integration

- Operators are **staff principals**, not “the same session as a Fethiye resident account.” A person may have both a consumer user and a staff principal; those sessions **must not be interchangeable**.
- **Identity** (or a dedicated staff credential module behind Identity contracts) authenticates staff (Passkeys first-class, D-009). Exact IdP/SSO vendor is **OPEN**.
- **RBAC** (roles → permissions) is the V1 authorization model. Permission catalog is platform-defined; assignment is admin-of-admins, audited.
- Staff session: HttpOnly, Secure, `__Host-` (or equivalent host-locked) **distinct cookie name/path/host rules** from the consumer session so O-009’s “must not share auth context” holds even if both apps share a parent domain. Server backing store follows ADR-002; this ADR does not reopen O-002.
- Deprovision: disable staff principal → all MC sessions invalid. Consumer account status does not silently grant MC.
- Break-glass / super-admin: still domain-command + audit; not a database login.

---

## 7. User 360 / Listing 360 / Company 360

A **360** is an operator **composition**, not a table:

| 360 | Assembled from (via admin reads) |
|---|---|
| **User 360** | Identity/Users profile & status, Trust, Compliance/EİDS status, listings/needs summaries, cases, moderation/fraud signals, notification suppressions as allowed |
| **Listing 360** | Listings + owner stub, Location, Master Data category, Media refs, reports, cases |
| **Company 360** | Business Profiles (V1 company entity) + owners, services, listings, trust/EİDS, cases. Corporate Workspace (O-006) is not assumed. |

Rules:

- Compose in `mgmt` by **multiple contract calls** (or a single façade that itself only calls contracts). No `JOIN` across domain schemas.
- Masking applied at the **admin read** according to staff permission (§9).
- Stale derived indexes are allowed for search-within-MC; standing, EİDS, and case state used to **act** must be read live from the owning domain.
- Do not design the 360 page layout here.

---

## 8. Case Engine integration

- Case Engine owns case lifecycle (V1: open, assign, update, close).
- MC **displays and drives cases only through Case Engine admin contracts**.
- **Escalated / irreversible account or fraud standing** requires a case (ARCHITECTURE.md Moderation prohibition). Content-only V1 moderation decisions stay on **Moderation** `RecordDecision`; if the outcome is account-level, Moderation opens or updates a case rather than MC writing Users rows.
- Support, compliance, and fraud-review queues are case **types**, not separate MC databases.
- Closing a case may **result in** domain commands (unpublish, suspend) invoked from Case Engine / the owning domain as part of resolution — not from ad-hoc MC SQL. Sequence: staff command → Case Engine and/or domain; invariants stay in domains.
- V1.5 SLA/appeals/routing must not require MC to grow a parallel workflow engine.

---

## 9. Audit, masking, approvals, exports

**Audit:** Every mutating admin command writes an Audit event via outbox (ADR-003). Payload: staff id, permission, command type, resource type/id, correlation id. No PII in log lines; minimize PII in audit metadata.

**Masking:** Default operator views use **redaction** (partial phone/email, no national id). Unmask is an explicit, permissioned, audited action. Different roles see different fields. 360 dumps are not written to Observability.

**Approvals (four-eyes):** Irreversible standing (ban, long suspend, mass unpublish, compliance legal hold) requires **two-staff approval** and/or an **open Case Engine case** whose resolution executes the domain command. Simple queue actions (hide one listing per Moderation V1 workflow) may be single-staff if the domain contract says so. MC must not invent a third approval table that domains ignore.

**Exports:** Role-gated, purpose recorded, row/time limits, async generation, download through **Storage** abstraction, retention/KVKK. Each export job audited. No “download production DB.”

---

## 10. Domain commands, not direct DB mutation

MC may only change production state by calling admin **commands** such as (illustrative names): `SuspendUser`, `PublishListing`/`UnpublishListing`, `RecordModerationDecision`, `OpenCase`/`CloseCase`, `UpdateFlag`.

**Forbidden:** `internal/mgmt` repositories against `listings.*` / `users.*`; shared superuser SQL; “fix it in admin” migrations as an operator tool; copying FSM into TypeScript.

Idempotency keys on bulk and retried commands (ADR-003 pattern where the effect must not double-apply).

---

## 11. Read models and dashboards

- **Operational dashboards** (queue depth, oldest case, failed outbox, notification poison) come from Observability metrics, Audit, Case Engine, Notifications, and Feature Flags **admin queries**, or from **derived** projections filled via outbox/in-process events.
- **Analytics** `GetMetrics` (admin) may feed charts; Analytics is not canonical for enforcement.
- Dashboard lag is acceptable for counts. It is **not** acceptable for EİDS/trust gates on a command path.
- No MC-owned canonical warehouse in V1.

---

## 12. Finance / trust / moderation / compliance / platform-ops boundaries

| Concern | MC may | MC must not |
|---|---|---|
| **Moderation** | Queue, decide via Moderation contracts, open cases | Own report tables; skip Case Engine on escalated standing |
| **Trust** | View badges/signals; record allowed signals via Trust | Recompute or override trust as an unlogged UI number; let AI auto-ban |
| **Compliance / EİDS** | Initiate/view verification via Compliance; never treat `IsVerified==false` as true | Bypass EİDS (D-010); “mark verified” without Compliance |
| **Finance** | V2+: Billing/Transactions/Payments **admin contracts** only | Invent invoices or capture payments in V1; store PAN |
| **Platform ops** | Feature flags, health, worker/outbox visibility via those domains | Toggle flags to skip auth, EİDS, IYS, or audit |
| **Fraud** | V1 signal review; escalate to cases | Autonomous irreversible fraud verdicts (D-011) |

---

## 13. Safe bulk actions

Bulk is a **first-class command type**, not a loop of unchecked GETs from the browser.

- Explicit selection or a server-side filter snapshot; **hard cap** on batch size (numeric cap OPEN).
- Optional **dry-run** returning would-be results.
- Execution **async** (outbox/worker): per item a **domain command**; partial failure recorded; policy **stop vs continue** is explicit on the job.
- Same RBAC, masking, approvals, and audit **per item** (or per job with item ids).
- No unbounded “select all matching search.”
- Costly/irreversible bulks inherit four-eyes / case rules.

---

## 14. Feature flags and configuration boundaries

- **Feature Flags domain** owns flag records and evaluation (`IsEnabled`, `UpdateFlag`).
- MC is the **operator UI** for flags, not a second flag store.
- Flags may gate **product rollout**. They must **not** disable: EİDS checks, staff authn/authz, audit writes, IYS/consent checks, or outbox for regulated effects.
- **Domain configuration** (category taxonomy, geo seed, template policy) stays in **Master Data / owning domains**, edited through those admin commands — not a generic `mgmt_kv` table for business rules.
- Environment secrets stay in config/env (D-013), never in MC UI.

---

## 15. HTTP and frontend (packaging resolved)

**Frozen:**

- Separate **admin HTTP router** (or host) from public API (ADR-001 `mgmt` handlers).
- Staff auth context isolated from consumer/business web (distinct cookie name/host rules; `__Host-` on `admin.konumlu.com`).
- Same locale model as ADR-009 (`tr/en/ru/ar`, RTL) for MC chrome; operator default may be `tr`.
- Next.js/React/TS (D-002). No MC-only backend.
- **Separate Next.js application** at planned path **`web/apps/admin`**, independent build/deploy/config/security boundary, production host **admin.konumlu.com**.
- Consumer app must not mount MC as `/mgmt/*`.
- No duplicated domain rules in TypeScript; shared UI kits are presentation-only.

Do not specify screens, IA, or component libraries here.

---

## 16. What this ADR does not choose

- Staff SSO/IdP vendor.
- Numeric bulk caps, exact RBAC matrix, unmask dual-control matrix.
- Screen list and UX.
- Corporate Workspace vs Company 360 (O-006).
- Advanced Case Engine / fraud graph (V1.5).

---

## 17. Consequences

### Positive

- V1 can be operated (D-020) without a shadow admin schema.
- Domain invariants and EİDS remain the only writers of standing.
- 360 views scale as new domains add admin reads in the same phase.

### Negative / trade-offs

- 360 pages cost N contract calls (or a projection).
- Two Next.js deployments (consumer + admin) increase CI/config surface versus a single `/mgmt` app.
- Four-eyes and bulk async add latency vs “just UPDATE.”

### Constraints imposed

- Do not duplicate domain rules in `mgmt` or in the MC frontend.
- Do not mutate domain tables from MC.
- Do not share consumer auth context with staff.
- Do not use feature flags to bypass D-010 / D-011 / audit / consent.
- Do not treat this ADR as a screen spec.

---

## 18. Open items

| ID | Item | Notes |
|---|---|---|
| ~~O-009 / G-09~~ | Separate Next.js app at `web/apps/admin` | **Resolved.** `/mgmt` in consumer app rejected. |
| OPEN-STAFF-IDP | Staff SSO / external IdP | Identity contracts remain; vendor unset. |
| OPEN-BULK-CAP | Max bulk batch size | Policy before first bulk command ships. |
| OPEN-RBAC | Permission catalog and role matrix | Required before V1 MC implementation; not a screen design. |
| O-006 | Company 360 vs Corporate Workspace | V1 Company 360 = Business Profiles. |
