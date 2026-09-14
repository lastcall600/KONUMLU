# ADR-007: EİDS Integration Design and Compliance Domain Ownership

**Status:** ✅ Accepted  
**Date:** 2026-09-05  
**Resolves:** D-010 (non-bypassable EİDS; Compliance/EİDS domain ownership)  
**Gates resolved:** none of G-01–G-09 — legal/architecture record for D-010; does not select official APIs, fees, or onboarding (those remain OPEN / REQUIRES OFFICIAL VERIFICATION)  
**Residency:** Geographic split of raw official data vs product decisions is **ADR-015** (🔒 FROZEN). This ADR is not superseded.

---

## Decision question (from INDEX / PHASE-0A)

**INDEX:** EİDS Integration Design and Compliance Domain Ownership — **Resolves: D-010**.  
**PHASE-0A:** 0A-14 — EİDS Integration Design — **D-010 (legal req)**.  
**ARCHITECTURE.md §19:** High — legal requirement.

How KONUMLU owns official **EİDS listing authorization** (Property vs Vehicle as separate checks), talks to providers only through an interface, behaves under outage without illegal bypass, and stays distinct from login and from any **e-Devlet person-identity** flow.

---

## 1. Decision

1. **Compliance / EİDS** (`internal/compliance`) is the **only** owner of EİDS verification state and of `IsVerified` / status contracts used for regulated operations (D-010).
2. **EİDS Property** and **EİDS Vehicle** are **separate** verification kinds. One does not imply the other. Contracts and storage are keyed by **subject + kind**.
3. **e-Devlet / person identity verification is not EİDS listing authorization.** Login (ADR-004) is not verification. A future person-identity provider, if legally required, is a **different** check kind and ADR — this ADR does **not** invent a generic public e-Devlet API.
4. **Listings** (and Business Profiles where regulated) **gate publish** on Compliance results. They do not call official systems. They do not store parallel “verified” flags that can disagree with Compliance.
5. **Provider I/O** is an interface defined for Compliance; **mock** locally; **real adapter** only under `internal/infrastructure/` (or Integrations adapters implementing that interface). Domain logic never embeds official URLs or SDKs.
6. For legally mandatory checks, the only legal paths are **pending / queue / retry / official result**. **Admin approval must not bypass** a missing or failed official check. Outage ≠ PASS.
7. Official follow-up after a committed “submit for verification” uses the **transactional outbox** (ADR-003).
8. **AI must never** produce or override an EİDS result (D-011).

Exact official endpoints, field names, credentials procedure, test/prod onboarding, fees, legal retention, and SLAs are **not** decided here (**REQUIRES OFFICIAL VERIFICATION**).

---

## 2. Domain ownership of EİDS state

| Concern | Owner | Must not |
|---|---|---|
| Verification records, kind (property/vehicle), status, official correlation refs, expiry/revoke | **Compliance / EİDS** | Listings, Trust, Case Engine, Identity, Management Center |
| Whether a **category** requires which EİDS kind(s) | **Master Data** (flag/metadata) + Compliance interprets at gate time | Hardcoding kinds in Listings |
| Listing content and listing lifecycle **except** forging verification | **Listings** | Calling the official provider; ignoring `false` / non-PASS |
| Trust badges that **display** EİDS-derived standing | **Trust** (reads Compliance contract) | Performing EİDS (already prohibited in ARCHITECTURE.md) |
| Cases for support/review queues | **Case Engine** | Making EİDS verification decisions (already prohibited) |
| Person login credentials | **Identity** | Setting EİDS status |
| HTTP client to official or mock EİDS | **Infrastructure adapter** | Business rules |

**Contract refinement:** ARCHITECTURE.md sketches `IsVerified(ctx, entityID) -> bool`. That shape is **insufficient**. The exported contract MUST take **verification kind** and **subject** (e.g. listing ID + `EIDS_PROPERTY` | `EIDS_VEHICLE`). A boolean without kind is forbidden for publish gates.

Illustrative (not source code): `IsVerified(ctx, subject, kind) bool` and `GetVerificationStatus(ctx, subject, kind) -> Status`. `InitiateVerification(...)` starts a session for one subject+kind.

PostgreSQL in the Compliance schema is the **source of truth** for verification records (D-004). Valkey may cache status with invalidation; a stale cache must not authorize publish (ARCHITECTURE.md).

---

## 3. Property vs Vehicle verification separation

| Rule | Meaning |
|---|---|
| Separate kinds | `EIDS_PROPERTY` and `EIDS_VEHICLE` are independent rows and independent provider operations (even if one vendor hosts both — still two logical checks). |
| Independent outcomes | Property PASS does not authorize a vehicle listing; vehicle PASS does not authorize a property listing. |
| Independent lifecycle | Expiry, revoke, retry, and reverification are per kind. |
| Category mapping | Which listing categories require which kind(s) is **OPEN** until legal + Master Data taxonomy (O-007 / ADR-012) and official rules. Architecture: a listing may require **zero, one, or both** kinds. |
| No bundled `IsVerified(user)` | User- or business-level “EİDS verified” must not replace listing-kind checks. Business-level EİDS, if required, is a **third subject type** with its own kind(s) — **OPEN** which business operations require it. |

---

## 4. Provider interface boundary

Compliance use-cases call a **provider interface** (name illustrative: `EIDSVerificationProvider`) with methods such as: submit check, fetch result, cancel if the official model supports it — **method set OPEN / REQUIRES OFFICIAL VERIFICATION**.

The interface:

- Speaks **KONUMLU types** (subject id, kind, correlation id, idempotency key, coarse result enum).
- Does **not** leak official XML/JSON schema into Listings or Compliance domain rules beyond a documented adapter mapping layer.
- Returns only: success payload mapping to PASS/FAIL/REVIEW_REQUIRED, or retryable/non-retryable errors.
- Never returns “admin override” or “skip because outage”.

No generic “e-Devlet API” client is introduced. Person-identity (e-Devlet) would be a **separate interface** if ever required.

---

## 5. Local mock provider model

Local (D-013): a **mock adapter** implements the same interface.

- Configurable scripted outcomes: PASS, FAIL, RETRYABLE_ERROR, REVIEW_REQUIRED, timeout — via env/config, not a secret backdoor in production wiring.
- No network to official hosts.
- Same outbox → worker → adapter path as production so developers exercise pending/queue/retry.
- Must **not** be compilable as the production provider for the production environment (wiring is env-selected at composition root). Production configuration MUST NOT point at the mock.

---

## 6. Real provider adapter location

- Interface: defined next to Compliance (e.g. `internal/compliance/contracts` or a Compliance-owned provider port). Platform must not import Compliance (ADR-001); adapters import the contract.
- **Real adapter:** `internal/infrastructure/` (EİDS client), implementing the interface. Official SDK/HTTP stays there (D-012).
- **Wiring:** `cmd/server` and `cmd/worker` composition roots only.
- Credentials: environment/secrets; never in git.
- Official URLs, field names, certificates: **OPEN / REQUIRES OFFICIAL VERIFICATION** — live only in env and adapter config, not in this ADR.

---

## 7. Listing lifecycle states around EİDS

Listings own **listing** states. Compliance owns **verification attempt** states. They couple only through contracts.

### 7.1 Listing-side (regulated categories)

Required progression for a listing that **requires** EİDS:

```
DRAFT → READY → VERIFICATION_PENDING → (blocked on Compliance) → publish only on PASS
```

| Listing-facing state | Meaning |
|---|---|
| `DRAFT` | Seller editing. No official call. |
| `READY` | Content/category/location valid; seller submitted. Not public. |
| `VERIFICATION_PENDING` | Official check queued or in flight. **Not public.** |
| Publish / live | **Only if** every required kind is Compliance **PASS** (and not expired/revoked). |

Unregulated categories: EİDS gate **does not apply**; Listings must still ask Compliance/Master Data “is this category regulated?” rather than guessing.

Listings MUST refuse `PublishListing` when required kinds are not PASS. “Publish” APIs must not take an admin `skipEids` flag.

### 7.2 Compliance-side result states (after official check)

```
VERIFICATION_PENDING
    → official check (adapter)
    → PASS | FAIL | RETRYABLE_ERROR | REVIEW_REQUIRED
```

| Compliance status | Listing effect | Admin |
|---|---|---|
| `PASS` | May publish (other listing rules still apply). | Cannot be forged; only adapter+Compliance. |
| `FAIL` | Must not publish. Seller may correct and resubmit (**OPEN** whether that is a new attempt). | Must not flip to PASS. |
| `RETRYABLE_ERROR` | Remain pending; retry/queue. Not public. | Must not flip to PASS. |
| `REVIEW_REQUIRED` | Remain not public. Optional Case Engine case for **ops follow-up**. Case close must **not** set PASS. PASS still requires official result (or a later official clarification mapped by Compliance after a **new** official check). | Review ≠ bypass. |

`REVIEW_REQUIRED` is for **ambiguous official outcomes** that humans may help **prepare a retry or record**, not for substituting the authority.

---

## 8. Required verification flow

1. Seller saves `DRAFT`; Listings validates content via Master Data / Location as usual.
2. Seller submits → listing `READY`. If category requires EİDS kind(s), Listings calls `InitiateVerification` per missing kind (direct contract, same request for **starting** the record).
3. Compliance writes verification row `VERIFICATION_PENDING` and an **outbox** row in the **same PostgreSQL transaction** (ADR-003). Commit. HTTP returns pending — **no official I/O in that transaction**.
4. `cmd/worker` claims outbox, calls provider adapter (timeout, idempotency key, correlation id).
5. Adapter result maps to PASS / FAIL / RETRYABLE_ERROR / REVIEW_REQUIRED. Compliance updates SoT; Listings is notified via contract and/or in-process event for **rebuildable** UI, with outbox if Listings must not miss the transition.
6. Only PASS unlocks publish for that kind.

Multiple required kinds: **all** must PASS. Partial PASS must not publish.

---

## 9. Outage behavior

If the official system is down, times out, or returns retryable failure:

- Status stays **`VERIFICATION_PENDING` or `RETRYABLE_ERROR`**.
- Listing stays **not public**.
- **No** auto-PASS, **no** admin-PASS, **no** “trust the seller documents instead” for legally mandatory checks.
- Queue retries (ADR-003 backoff). Product may show pending/degraded copy (i18n OPEN).

If Compliance PostgreSQL is down: fail closed (cannot publish regulated listings; cannot mint PASS).

---

## 10. Retry / queue behavior

- Official calls run from **outbox workers**, not from the seller’s request goroutine (except optional non-authoritative “kick”).
- Retryable errors: exponential backoff, `available_at`, max attempts **OPEN** (ADR-003 OI values; may be stricter for EİDS — still OPEN).
- Poison/failed outbox: listing remains pending/not public; alert; **not** PASS.
- Duplicate worker delivery: idempotent provider call + idempotent Compliance transition.

---

## 11. Idempotency

- Each initiate produces a stable **idempotency key** (subject + kind + attempt identity). Unique on Compliance + outbox.
- Retried submits must not create two in-flight official obligations if the official API would double-charge or double-file — **how the official API dedupes: REQUIRES OFFICIAL VERIFICATION**. Until known, KONUMLU must still dedupe internally and pass the key to the adapter.
- PASS/FAIL applied once per attempt id; later duplicates no-op.

---

## 12. Correlation / request IDs

Every attempt stores:

- KONUMLU `verification_id`
- `correlation_id` / trace id (from the user request when present)
- **Official reference** if and only if the provider returns one — field name **REQUIRES OFFICIAL VERIFICATION**

Logs and metrics use these IDs, not payloads.

---

## 13. Audit requirements

Via outbox → Audit (ADR-003), at least:

- Initiate, adapter success/fail class, terminal PASS/FAIL, REVIEW_REQUIRED, retry, revoke, expiry, admin **view** actions
- Actor (user vs worker vs admin id)
- Subject, kind, verification_id, correlation_id

Must **not** put official sensitive bodies, national IDs, or secrets in audit `metadata` beyond what legal minimum **REQUIRES OFFICIAL VERIFICATION**.

---

## 14. What data is stored vs not stored

**Store (PostgreSQL Compliance, Germany product DB):** subject type/id, kind, status, attempt ids, timestamps, expiry/revoke flags, KONUMLU correlation ids, adapter error **class**, and (per ADR-015) the **minimum signed decision** fields only.

**Store (Türkiye Compliance Gateway DB):** official/provider transaction state, government tokens, raw or reconstructed official evidence as legally required. See [ADR-015](./ADR-015-tr-compliance-gateway-data-residency.md).

Germany **must not** store official **reference/government tokens**, TCKN, or raw provider bodies (ADR-015 refines the earlier “official reference tokens if issued” line).

**Do not store** unless official/legal rules later require (then a new decision):

- Full official request/response dumps
- Unnecessary copies of title deeds, VIN, TCKN, or other official PII in application tables
- Duplicate “verified” booleans on listings

**OPEN / REQUIRES OFFICIAL VERIFICATION:** minimum data the official process requires KONUMLU to send and retain.

Listing attributes needed to **form** a check (e.g. location, category) stay in Listings; Compliance receives **intent + identifiers** through the contract, not by querying listing tables (D-015).

---

## 15. Sensitive payload handling

- In transit: official payloads only on the TR gateway and official provider links (ADR-015). Germany receives the signed decision only. TLS as required by official docs — **OPEN**.
- At rest: minimize; encrypt-at-rest follows platform DB standards (**OPEN** if official requires extra).
- Logs: no payload bodies, no credentials, no raw official XML/JSON.
- Support tools / Management Center: show status, ids, timestamps, error class — not raw official payloads by default.

---

## 16. Admin permissions and explicit no-bypass rule

Management Center (when built) MAY:

- View verification status and history
- Trigger **retry** of a retryable attempt
- Open a Case Engine case for seller support

Management Center / admins MUST NOT:

- Set status to PASS
- Publish a regulated listing without Compliance PASS
- Disable the gate with a feature flag that skips EİDS in production for mandatory categories
- Use the **local mock** against production
- Treat Case Engine resolution as verification
- Use AI to “approve” EİDS

**No-bypass is legal, not a preference (D-010).** Feature flags must not circumvent this in production. A flag that points staging at mock is an **environment** choice, not an admin approve button.

---

## 17. Reverification / expiry / revoke

Compliance records MAY include `expires_at` and revoke.

- **Expiry and revoke rules: OPEN / REQUIRES OFFICIAL VERIFICATION.**
- Architecture: when expired or revoked, `IsVerified(...)` is **false**; live listings that required that kind MUST be unpublished or blocked from remaining public via Listings reacting to Compliance (outbox event). Exact product (unpublish vs pause): **OPEN**.
- Reverification is a **new attempt** (new idempotency key), not editing PASS in place.
- Seller-initiated listing edits that change identity-relevant attributes (which attributes: **OPEN**) MUST invalidate or re-queue the relevant kind rather than keep a stale PASS.

---

## 18. Provider timeout / circuit-breaker / degraded-state expectations

- Every adapter call has a **timeout** (**OPEN** — do not invent ms).
- Circuit breaker: after repeated retryable failures, **open = stop hammering**; outcomes remain pending/retryable, **never PASS**. Thresholds **OPEN**.
- Degraded state is **visible pending**, not reduced assurance. TR gateway outage: main app stays up; new official verification unavailable; never PASS (ADR-015).
- Worker lease must exceed provider timeout (ADR-003 lease OPEN).

---

## 19. Observability requirements

Metrics (no PII): attempts by kind and status, age of oldest pending, adapter latency/error class, circuit state, mismatch attempts (Listings publish denied).

Alerts: pending age, poison outbox for EİDS types, unexpected FAIL spikes, any code path that would skip the gate (must be impossible; if detected, incident).

Health: worker + adapter reachability in staging/prod; local mock health is not production health.

---

## 20. Local / staging / production differences

| Environment | Provider | Gate |
|---|---|---|
| Local | Mock behind same interface; full outbox path | Same state machine; mock may script PASS for DX **only locally** |
| Staging | Official **sandbox** if it exists (**REQUIRES OFFICIAL VERIFICATION**); else mock with production-like wiring | Same code; sandbox ≠ production PASS legal meaning |
| Production | Real adapter + official production credentials | Mock forbidden; no admin bypass |

Same codebase (ARCHITECTURE.md parity). Differences: config and secrets only.

---

## 21. What remains OPEN until official documentation is obtained

| ID | Item |
|---|---|
| OI-007-01 | Official Property API: existence, URLs, fields, auth, environments |
| OI-007-02 | Official Vehicle API: same |
| OI-007-03 | Onboarding, credentials, fees, contracts |
| OI-007-04 | Legal retention and what PII must/mustn’t be stored |
| OI-007-05 | SLA, timeouts, rate limits |
| OI-007-06 | Official idempotency and correlation field names |
| OI-007-07 | Expiry, revoke, and reverification legal rules |
| OI-007-08 | Which Master Data categories require which kind(s) |
| OI-007-09 | Whether any **business-profile** operations require EİDS kinds |
| OI-007-10 | Whether person-level e-Devlet identity is required for any V1 flow (separate from this listing EİDS model) |
| OI-007-11 | Mapping of official result codes → PASS/FAIL/REVIEW_REQUIRED/RETRYABLE_ERROR |
| OI-007-12 | Attribute changes that invalidate PASS |
| OI-007-13 | Product behavior on expiry (unpublish vs pause) |

These do **not** reopen D-010, Property≠Vehicle, no admin bypass, mock-vs-real interface, or “auth ≠ EİDS”.

---

## Alternatives considered

### Admin “manual verify” during outage

**Rejected.** Illegal bypass of mandatory official verification (D-010; task baseline).

### Single `IsVerified(user)` for all listings

**Rejected.** Mixes person identity, property, and vehicle; fails listing authorization.

### Listings domain calls official HTTP

**Rejected.** Provider leak, bypass risk, no single SoT, ADR-001 violation.

### Treating e-Devlet login as listing EİDS

**Rejected.** Different legal instrument; would invent a generic e-Devlet API here.

### In-request official call inside the listing submit transaction

**Rejected.** ADR-003: no provider I/O in the business transaction; outage would roll back content or hold DB locks.

### Case Engine or AI as verifier

**Rejected.** ARCHITECTURE.md + D-011.

---

## Consequences

### Positive

- Non-bypassable gate with a testable state machine.
- Property/Vehicle isolation.
- Local-first via mock; production cannot “approve around” official systems.
- Outbox retries without losing submit.

### Negative / trade-offs

- Official details still block adapter implementation until documentation exists.
- Sellers wait in pending during outages (required).
- Two FSMs (listing vs verification) must stay consistent via contracts.

### Constraints imposed

- No production publish of regulated listings without Compliance PASS per required kind.
- No e-Devlet API invented in this ADR.
- Accepting this ADR does not initialize official integrations or credentials.

---

## Implementation rules (when V1 code is allowed)

1. **E-E001** Publish of regulated listings requires Compliance PASS per required kind.
2. **E-E002** Admins cannot set PASS or skip the gate.
3. **E-E003** Official I/O only in infrastructure adapters via outbox workers. Production official government I/O and raw payloads run on the **Türkiye Compliance Gateway** (ADR-015); Germany consumes a signed decision, never raw identity/provider bodies.
4. **E-E004** Local mock implements the same interface; not wired in production.
5. **E-E005** AI and Case Engine cannot verify EİDS.
6. **E-E006** Do not embed official URLs or fee tables in domain code or this decision as facts.
8. **E-E008** Germany consumes only the ADR-015 signed decision. V1 encoding is Ed25519 over repository-owned `SigningBytes` in `internal/eids/trdecision`. Official provider adapters remain out of this ADR. See [eids-tr-decision-protocol.md](../architecture/eids-tr-decision-protocol.md).

---

*ADR-007 — authored Phase 0A, Task 0A-16 as instructed (INDEX/PHASE-0A: ADR-007 / D-010).*
