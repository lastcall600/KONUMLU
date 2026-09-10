# ADR-003: Transactional Outbox — Schema, Relay Design, and Concurrency Strategy

**Status:** ✅ Accepted  
**Date:** 2026-09-05  
**Resolves:** D-008 (relay mechanics), O-003  
**Gates resolved:** G-03 — unblocks the first domain feature that requires a transactional outbox

---

## 1. Decision

KONUMLU implements a **single PostgreSQL transactional outbox**, owned as a **platform table**, written in the **same database transaction** as the producing domain’s business write, and dispatched by **`cmd/worker`** using **competing consumers** with `SELECT … FOR UPDATE SKIP LOCKED`.

Delivery is **at-least-once**. Consumers and external adapters MUST be **idempotent**. There is **no global ordering** guarantee. Per-aggregate ordering is opt-in and used only where a producer explicitly requires it.

This ADR specifies **how** the frozen D-008 pattern works. It does not replace direct contract calls with messaging, does not introduce event sourcing, and does not add Kafka, RabbitMQ, NATS, or any other broker.

---

## 2. Context

D-008 freezes three interaction modes:

- **Synchronous** cross-domain work: direct Go interface calls in-process.
- **Safe derived** effects: in-process events (loss on crash is acceptable).
- **Critical reliable** async and external effects: transactional outbox + relay.

O-003 left the relay open: single-goroutine poller vs. multiple workers, locking, polling interval.

ARCHITECTURE.md requires: outbox row in the same transaction as domain state; statuses pending / dispatched / failed; retry count; advisory locks **or equivalent** to avoid duplicate claim.

ADR-001 places the generic writer and relay in `internal/platform/outbox` (no domain payload knowledge) and the relay process in `cmd/worker`. ADR-002 requires regulated auth events to reach Audit through this outbox path.

PostgreSQL is the canonical store (D-004). External providers must support retry, backoff, idempotency, and degraded behavior (this ADR makes that binding for outbox-driven calls).

---

## 3. When a direct call is used

Use a **synchronous contract call** (`internal/<other-domain>/contracts`) when the caller needs the result in the **same request** to continue correctly:

- Validation and eligibility reads (`IsVerified`, `ValidateSession`, `GetCategory`, `ValidateCoordinate`)
- Same-request writes that are part of the user-visible operation and can share **explicit** transactional policy only where a future decision allows it (default remains: no shared cross-domain transactions — D-001 / D-015)
- Authorization-relevant standing (account status, trust checks used to admit the write)

Direct calls are **not** a delivery bus. They must not be used to “fire and hope” an external provider or a crash-sensitive side effect.

---

## 4. When an in-process event is used

Use the **in-process event bus** (`internal/platform/eventbus`) for **safe derived** effects inside the monolith:

- Cache key invalidation after a write
- Best-effort denormalized counters or UI-only projections that can be rebuilt
- Same-process notifications to listeners that may be skipped on crash without violating policy or money/compliance

Delivery may be synchronous in-process or a goroutine. **If the process dies before the handler runs, the effect is lost.** That is acceptable only for rebuildable derived state.

Do **not** use in-process events for: push/email/SMS, EİDS/provider follow-up, payment side effects, regulated audit, or search/index updates that must not drift indefinitely after commit.

---

## 5. When the transactional outbox is mandatory

Write an outbox record in the **same PostgreSQL transaction** as the business commit when the side effect **must not be lost** after that commit, including:

| Must use outbox | Why |
|---|---|
| Critical notification after a committed business event (need match, new message, etc.) | User-visible, external, crash-sensitive |
| EİDS / external compliance provider follow-up | Legal and retryable remote I/O |
| Payment-related external side effects (V2) | Money; must not run inside the domain commit |
| Search/index projection when the product requires the index to catch up after commit | Derived store; lag OK, silent drop not OK |
| Regulated / security-sensitive audit records | ARCHITECTURE.md; ADR-002 auth audit |

**Must not** automatically use the outbox for:

- Same-request validation or synchronous domain reads
- Cheap derived UI-only effects that can be retried or rebuilt (use in-process events)
- Replacing contract calls with async “eventually” when the caller still needs the answer now

If a maintainer is unsure: **loss on crash acceptable → in-process; loss after commit unacceptable → outbox; need the answer now → direct call.**

---

## 6. Outbox table ownership

| Concern | Owner |
|---|---|
| Physical table(s), migrations for the outbox schema, generic writer, claim SQL, status machine | **Platform** (`internal/platform/outbox`). Suggested schema: `platform.outbox` (exact schema name OPEN). |
| Meaning of `payload` and `event_type` | **Producing domain** (writes via the platform writer inside its transaction). |
| Dispatch handler (what to do with a type) | **Consuming domain or infrastructure adapter**, registered in `cmd/worker` composition root. |
| Domain business tables | **Owning domain** (unchanged). The outbox table is **not** a domain table and is **not** queried by other domains for business data. |

**One shared outbox table** for V1 (not per-domain outbox tables).

Rationale: one relay, one claim query, one operational metric. Multiple schemas in one PostgreSQL database still allow `listings` writes and `platform.outbox` inserts in a **single transaction**. Per-domain outbox tables would multiply claim logic without changing the transaction story.

Platform still **must not import domain packages** (ADR-001). The writer stores opaque bytes/JSON plus routing metadata. Handler registration lives in `cmd/worker`, which may import domain implementations and infrastructure adapters.

Domains MUST NOT insert into `platform.outbox` with ad-hoc SQL from another domain’s repository. They use the platform writer API in the producing domain’s transaction.

---

## 7. Minimum outbox record fields

Minimum columns (names illustrative):

| Field | Purpose |
|---|---|
| `id` | Stable unique identifier (UUID or equivalent). |
| `event_type` | Routing key for the worker handler registry. |
| `aggregate_type` / `aggregate_id` | Producer identity of the business record (for idempotency, ops, optional ordering). |
| `payload` | Opaque JSON (or equivalent). No provider SDK types. No secrets, session cookie values, raw passwords, or unnecessary PII (ARCHITECTURE.md logging/PII rules apply to stored payloads as well as logs). |
| `idempotency_key` | Producer-supplied unique key for this intended effect (unique constraint). |
| `status` | `pending` \| `processing` \| `dispatched` \| `failed` (poison after policy). |
| `attempt_count` | Retry counter. |
| `available_at` | Not-before time for claim (backoff). |
| `created_at` | Insert time (same transaction as business write). |
| `processing_started_at` | Claim time (nullable). |
| `dispatched_at` / `failed_at` | Terminal timestamps (nullable). |
| `last_error` | Short, non-PII error class/message for operators. |
| `correlation_id` / `trace_id` | Observability join to the originating request when available. |
| `ordering_key` | Nullable. Set **only** when per-aggregate order is required (§12). |

Statuses in ARCHITECTURE.md (`pending` / `dispatched` / `failed`) remain the **external** model; `processing` is the in-flight claim state so a crashed worker cannot leave a row locked forever without a reclaim rule (§8, §16).

---

## 8. Transaction boundary with the business write

1. Open one PostgreSQL transaction in the producing domain’s write path.
2. Write domain state.
3. Insert the outbox row via `internal/platform/outbox` **in that same transaction**.
4. Commit. **Both succeed or both roll back.**
5. **Never** call external HTTP, EİDS, FCM, email, payment, or other provider I/O inside this transaction.
6. After commit, the worker (not the request goroutine) claims and dispatches. The HTTP handler must not require the side effect to finish before returning, unless a product rule explicitly needs a sync path (that path is then a **direct call**, not outbox).

If the business write rolls back, there is no outbox row and no dispatch. If the process crashes after commit but before the worker runs, the row remains `pending` and will be claimed.

Cross-domain **shared** transactions remain forbidden by default (D-001). The outbox insert is platform SQL in the **producer’s** transaction, not a write into another domain’s tables.

---

## 9. Worker claim / locking strategy

**Selected:** multiple worker **processes** (and multiple goroutines per process if useful), competing on the same table with:

```text
SELECT … FROM platform.outbox
 WHERE status IN ('pending', 'processing')
   AND available_at <= now()
   AND (status = 'pending' OR processing_started_at < now() - lease)
 ORDER BY available_at
 FOR UPDATE SKIP LOCKED
 LIMIT n
```

Then set `status = processing`, `processing_started_at = now()`, increment `attempt_count` as appropriate, commit the claim (or use a single transaction for claim+handler only if the handler is **not** remote I/O — remote I/O must **not** hold the row lock for the provider RTT).

**Normative claim rules:**

- **`FOR UPDATE SKIP LOCKED`** is the competing-consumer mechanism (the “or similar” to advisory locks in ARCHITECTURE.md). Session-level advisory locks are **not** the primary claim method; they do not scale as cleanly to many rows.
- A **processing lease** exists: if a worker dies after claim, the row becomes claimable again when `processing_started_at` is older than the lease. **Lease duration: OPEN** (must be set before first outbox use; must exceed typical provider timeout).
- `cmd/worker` is the relay binary (ADR-001). `cmd/server` MUST NOT run the production claim loop (local-dev combined process is OPEN as a convenience, not the production topology).
- **Polling interval: OPEN** (configurable). Must be defined before first outbox use. Approach: short poll or `LISTEN/NOTIFY` as an **optional wake-up** plus poll as the source of truth (NOTIFY is best-effort; poll remains mandatory). Do not depend on NOTIFY alone.

A single global goroutine poller is **rejected** for production: one process crash or blocking handler stalls all critical effects. Multiple workers are required for V1 so that Identity audit, notifications, and later EİDS follow-up do not share a single-thread bottleneck.

---

## 10. Retry / backoff approach

- Retryable handler/provider failures: keep the row non-terminal; set `available_at` using **exponential backoff with jitter**; increment `attempt_count`.
- Backoff parameters (base, multiplier, cap) and **max attempts: OPEN**. Must be set before first outbox use. May vary by `event_type` (notifications vs. payments) without changing this pattern.
- Non-retryable errors (malformed payload, unknown type after deploy mismatch policy): go to `failed` without burning the full retry budget, with operator visibility.
- Success: `status = dispatched`, `dispatched_at` set. Handler must have completed its **idempotent** side effect first.

---

## 11. Idempotency requirements

- Producers MUST supply a stable `idempotency_key` for each intended effect (e.g. `audit:{action}:{resource}:{id}` or `notify:{type}:{business-event-id}`). Unique constraint prevents duplicate **inserts** from retried application writes.
- Handlers MUST treat dispatch as **at-least-once**: the same outbox `id` or `idempotency_key` may be delivered more than once (§12).
- External adapters (notifications, EİDS, future payments) MUST pass an idempotency key or equivalent to the provider when the provider supports it.
- In-process event handlers are not required to be as strict; outbox handlers are.

---

## 12. Duplicate delivery semantics

The system is **at-least-once**, **not** exactly-once.

Duplicates happen when: worker crashes after the provider accepts but before `dispatched` is committed; lease expiry causes a second claim; a provider times out but performed the work.

**Consumer contract:** processing the same payload twice must not create a second business-of-record (two charges, two distinct “official” compliance submissions without provider-level idempotency, two audit rows for one event unless Audit’s contract defines append-only duplicates as forbidden — Audit SHOULD use `idempotency_key` to insert-once).

There is no distributed transaction with FCM/EİDS/payment providers.

---

## 13. Poison / failure handling

After max attempts or a non-retryable error: `status = failed`. The worker **stops** automatic retry.

Poison rows:

- MUST increment an alert/metric and appear in operator views (Management Center later; structured logs now).
- MUST NOT block claim of other rows (`SKIP LOCKED` + `available_at` + status filter).
- Replay is a **conscious** operator/admin action (reset to `pending` with a new `available_at`), not an automatic loop. Replay API details OPEN.

Do not delete `failed` rows as the only record of the incident until retention policy allows (§15).

---

## 14. Ordering guarantees — only where truly required

**Default: no ordering.** Parallel workers may dispatch different aggregates and even the same aggregate’s events out of order if multiple pending rows exist.

**Opt-in per-aggregate order:** producer sets `ordering_key` (typically `aggregate_type:aggregate_id`). The worker claims **one** in-flight row per `ordering_key` at a time (or processes `created_at`/`id` sequence for that key). Use only when the consumer is not commutative (e.g. search projection “create then update” if the projector cannot reconstruct from PostgreSQL).

**Never required:** global FIFO across the platform, causal order across domains, or total order for notifications.

If the consumer can rebuild from PostgreSQL (search, cache), prefer **latest-state projection** over ordered event replay (not event sourcing).

---

## 15. External provider delivery behavior

Outbox handlers call **abstractions** (NotificationSender, EİDS client, later PaymentGateway), never SDKs inside domain use-cases (D-006 principle, ADR-001 infrastructure rules).

Every outbox-driven provider call MUST:

- Be **retry-safe** (idempotency key).
- Apply **timeouts** (OPEN per provider).
- Support **backoff** already implemented by the worker; adapters should not busy-loop.
- Define **degraded behavior**: provider down → retryable failure, not a domain rollback (the domain already committed). Product-visible degradation (e.g. “notification delayed”) is a Notifications/Compliance concern, not a hidden drop.
- Never run inside the producer’s business transaction.

---

## 16. Observability requirements

Platform and worker MUST emit (no PII in log lines):

- Queue depth by `status` and `event_type`
- Age of oldest `pending` / `processing` row
- Claim, success, retry, fail, poison counts
- Handler latency and provider error class
- Lease reclaim count (crash/slow handler signal)

Trace/correlation IDs on the row join request logs when present. Health of `cmd/worker` is a first-class health check (Observability domain). Alerting thresholds OPEN.

---

## 17. Cleanup / retention approach

- `pending` / `processing` / `failed`: retain until processed, replayed, or an explicit retention decision.
- `dispatched`: retain for **operational replay and audit of delivery**, then delete or archive. **Retention duration: OPEN** (must be set before production; KVKK: payloads must stay minimal).
- Cleanup is a worker or scheduled job deleting/archiving **terminal** rows older than retention. It must not delete rows that are not `dispatched` or approved-closed `failed`.
- Cleanup is **not** event-store compaction. There is no event log to replay the system from.

---

## 18. Failure / recovery scenarios

| Scenario | Outcome |
|---|---|
| Crash before commit | No domain write, no outbox row. User/request retries. |
| Crash after commit, before claim | Row `pending`; worker claims later. |
| Crash after provider success, before `dispatched` | Duplicate delivery; idempotent handler. |
| Worker stall holding `processing` | Lease expiry; another worker claims. |
| Handler panic | Lease expiry or explicit fail; no silent drop. |
| PostgreSQL down | No new outbox writes (business writes fail too). Worker idle. Fail closed. |
| Provider down | Rows retry with backoff; domain state already committed. |
| Unknown `event_type` (deploy skew) | Retryable until timeout or `failed` + alert; do not dispatch to a default “ignore” sink. |
| Poison storm | `failed` isolation; other types continue. |
| `cmd/worker` all replicas down | Outbox backlog grows; domain stays consistent; effects delayed. This is visible via oldest-row age. |

---

## 19. What is explicitly NOT being built

- Event sourcing, event-store replay as source of truth, CQRS-as-architecture
- Kafka, RabbitMQ, NATS, SQS-as-bus, or any standalone broker (D-008)
- Exactly-once delivery to external systems
- Global message order or a platform-wide saga orchestrator
- Making every domain interaction asynchronous
- Shared cross-domain business transactions by default
- Outbox as a public API or another domain’s query surface
- CDC/Debezium as the V1 relay (poll/SKIP LOCKED is the relay)
- Dual-write to a broker **and** outbox as two SoTs

---

## 20. Future broker extraction path (measured need only)

If outbox fan-out or worker throughput becomes a **measured** bottleneck (D-008):

1. Keep **PostgreSQL outbox insert in the same transaction** as the business write (this remains the reliability core).
2. Change **`cmd/worker` (or a subset of handlers)** to publish committed outbox rows to a broker.
3. Downstream consumers read the broker; **idempotency_key / outbox id** remains the dedupe key.
4. Do not let the broker become canonical domain state (D-004).
5. Requires a **new ADR** naming the broker and the measurement. This ADR does not select one.

Until that ADR, adding a broker is prohibited.

---

## Alternatives considered

### Single-goroutine poller, no SKIP LOCKED

**Rejected** for production. Simple, but one blocked handler or process freeze stalls audit, notifications, and EİDS follow-up together.

### PostgreSQL advisory locks as the primary claim

**Rejected** as the row-claim mechanism. Useful as an optional singleton leader for jobs that must not run twice cluster-wide; **not** a substitute for row-level `SKIP LOCKED` on a multi-row queue.

### Per-domain outbox tables

**Rejected** for V1. Isolation is already enforced by opaque payloads and handler registry. Multiple queues increase operational cost without improving the transaction boundary.

### LISTEN/NOTIFY only (no poll)

**Rejected.** Notifications can drop under load or restart. Poll is mandatory; NOTIFY optional.

### In-request dispatch after commit (no worker)

**Rejected** as the reliability mechanism. The request process can die after commit; that is the bug the outbox exists to fix. Optional best-effort “kick” of the worker after commit is allowed; it is not a substitute for `cmd/worker`.

---

## Consequences

### Positive

- Critical effects survive process crash without a broker.
- Clear split: direct vs in-process vs outbox, matching D-008.
- Competing workers scale with `cmd/worker` replicas; no sticky broker.
- Platform table + opaque payload preserves ADR-001 import rules.
- Measured path to a broker later without rewriting producers.

### Negative / trade-offs

- At-least-once duplicates must be designed into every handler.
- Worker and lease tuning are operational load (intervals OPEN).
- Outbox payload discipline (PII, size) must be reviewed per event type.
- Search and notifications can lag; product must tolerate delay, not silent loss.

### Constraints imposed

- First outbox-using feature cannot ship without `cmd/worker`, claim SQL, backoff, poison handling, and the OPEN values below being set.
- Domains must not call providers inside the business transaction for crash-sensitive effects.
- No broker and no event sourcing in V1.

---

## Implementation rules (when V1 code is allowed)

1. **E-O001** Producer writes domain state and outbox row in one PostgreSQL transaction via `internal/platform/outbox`.
2. **E-O002** `cmd/worker` is the production relay. Handlers registered only at the worker composition root.
3. **E-O003** Claim with `FOR UPDATE SKIP LOCKED` and a processing lease.
4. **E-O004** Handlers idempotent; unique `idempotency_key` on insert.
5. **E-O005** No provider I/O in the producer transaction.
6. **E-O006** Platform outbox package imports no domain packages.
7. **E-O007** Default unordered dispatch; `ordering_key` only when documented by the producer.
8. **E-O008** Accepting this ADR does not initialize Go, Docker, queues, or databases.

---

## Open values (must be decided before first outbox use)

| ID | Item |
|---|---|
| OI-003-01 | Poll interval and batch size (`LIMIT n`) |
| OI-003-02 | Processing lease duration |
| OI-003-03 | Backoff base / cap and max attempts (global and per `event_type` if split) |
| OI-003-04 | Provider timeouts per adapter |
| OI-003-05 | `dispatched` / `failed` retention duration |
| OI-003-06 | Exact table/schema name and payload size limit |
| OI-003-07 | Whether local-dev may colocate worker loops in `cmd/server` |
| OI-003-08 | Optional `LISTEN/NOTIFY` wake-up |
| OI-003-09 | Poison replay API (ops vs Management Center) |
| OI-003-10 | Alert thresholds for oldest pending age and fail rate |

These do **not** reopen competing consumers, the shared platform table, at-least-once semantics, or the ban on brokers and event sourcing.

---

*ADR-003 — authored Phase 0A, Task 0A-12.*
