# ADR-015: Türkiye Compliance Gateway and Data Residency Boundary

**Status:** 🔒 FROZEN (Accepted) — remains frozen unless superseded by a later ADR  
**Date:** 2026-09-14  
**Constrained by:** D-010, D-011, D-012, D-015, ADR-007  
**Does not reopen:** D-001 (Germany modular monolith), D-004 (PostgreSQL as canonical store), D-008 (no broker until measured), ADR-007 (Compliance ownership, Property ≠ Vehicle, no illegal bypass)

---

## Context

KONUMLU’s main platform is hosted primarily in Germany (Hetzner). Türkiye-specific government / identity verification (EİDS, e-Devlet-related verification) may require sensitive identity and official-provider data to remain inside Türkiye.

ADR-007 already owns Compliance/EİDS product rules: listing Property vs Vehicle kinds, provider interface, outage ≠ PASS, no admin bypass, login ≠ EİDS. It does **not** decide geographic residency of raw government payloads.

This ADR freezes a **Türkiye Compliance Gateway**: a separate Türkiye-hosted process and PostgreSQL for official verification I/O and sensitive evidence. It is **not** a general KONUMLU application server and is **not** a marketplace microservices split (D-001 remains: listings, messaging, search, and other domains stay in the Germany modular monolith).

---

## Decision

### 1. Split of runtimes

| Location | Role |
|---|---|
| **Germany / Hetzner** | Main KONUMLU platform (modular monolith `cmd/server` + `cmd/worker`, consumer/admin web, marketplace data). Continues to enforce EİDS **product gates** using only a verified signed decision. |
| **Türkiye Compliance Gateway** | Dedicated Türkiye VDS for government/EİDS/e-Devlet provider integration, sensitive verification state, audit evidence, and **signing** of verification decisions. |

Initial TR server profile (pilot, accepted):

- 4 vCPU
- 8 GB RAM
- 200 GB SSD
- 5 TB traffic

This capacity is accepted because the node does **not** run general KONUMLU workloads. Later capacity must be driven by real metrics. Do not implement HA prematurely.

### 2. Exact TR responsibilities

Türkiye **may** process/store only what is required for:

- government / EİDS / e-Devlet verification
- provider integration (official adapter, credentials, provider sessions)
- sensitive verification transaction state
- security / audit evidence for those verifications
- verification **decision generation and signing**

Türkiye **must not** host general:

- listings
- messaging
- search
- feeds
- media processing
- marketplace traffic

### 3. Exact Germany responsibilities

Germany:

- runs the main product and existing Compliance/EİDS **product** lifecycle (`internal/eids` and Listings publish gates)
- stores only the **minimum verification decision/state** required by the product (status, ids, validity, opaque subject mapping, signature metadata)
- **verifies** TR-signed decisions before treating a check as PASS
- maps `subject_ref` to KONUMLU subjects (listing / kind / owner) without receiving raw identity payloads
- **must not** call official government APIs with raw identity data from Germany
- **must not** open a database connection to TR PostgreSQL
- **must not** fail-open: TR unavailability or invalid signature never becomes verified / approved

Local development may continue to use a mock provider behind the same Germany-facing contract (ADR-007). Production official I/O belongs on the TR gateway.

### 4. Data residency boundary

**Raw sensitive verification / government data remains in Türkiye.**

Germany **MUST NOT** receive:

- TCKN
- raw e-Devlet / EİDS provider responses
- government tokens
- birth date
- address
- raw identity payload
- full sensitive provider transaction payload

ADR-007 §14 “official reference tokens if issued” is **refined**: government tokens and official correlation secrets stay in TR. Germany may store KONUMLU `decision_id` and coarse status only.

### 5. Allowed TR → DE payload

Germany receives only a **minimal signed verification decision**. Conceptual fields (names illustrative; encoding is an implementation item):

| Field | Meaning |
|---|---|
| `schema_version` | Decision document version Germany must accept or reject |
| `verification_type` | Kind (e.g. listing property / vehicle / future person-identity kind) |
| `status` | Coarse result class (PASS / FAIL / PENDING / unavailable class — never a raw provider body) |
| `decision_id` | Unique id for replay protection |
| `subject_ref` | **Opaque** subject reference; not TCKN, name, or address |
| `issued_at` | Decision issue time |
| `valid_until` | Expiry of this decision |
| `signature` | TR signature over the canonical decision bytes |

Optional future fields **must remain non-identifying and controlled** (new ADR or explicit schema_version bump). Client JSON such as `approved=true` is **never** authoritative.

### 6. Signing, verification, and replay

1. Türkiye **signs** the verification decision.
2. Germany **verifies**, fail-closed, at least:
   - signature (trusted TR key material; rotation is an ops item)
   - `decision_id` **replay** (a decision_id may be applied once per intended subject/kind; duplicates are rejected or treated as idempotent replay of the same stored decision, never as a new approval channel)
   - `schema_version` (unknown versions rejected)
   - `verification_type` (must match the required kind)
   - `issued_at` (not unreasonably in the future; clock skew policy is an implementation item)
   - `valid_until` (expired → not verified)
   - **subject mapping** (`subject_ref` must bind to the expected KONUMLU subject; mismatch → reject)
3. Transport: **TLS minimum**; **service authentication**; **mTLS preferred** where practical; replay protection as above.
4. A plain client-supplied `approved=true` (or equivalent header/query) **must never** be authoritative.

Exact signature algorithm, key ceremony, and mTLS CA are implementation/ops items; the **authority model** is frozen here.

### 7. Databases

- Türkiye has **its own PostgreSQL**.
- Do **not** share the Germany PostgreSQL.
- Do **not** create a cross-country DB connection.
- Germany stores only minimum decision/state required by the main product.
- Both stores remain PostgreSQL (D-004). TR PostgreSQL is not a replica of Germany and is not PostGIS marketplace state.

### 8. Logging boundary

Application/security logs (both sides) **may** contain:

- `request_id`
- `decision_id`
- `verification_type`
- status / result class
- provider result **class** (not body)
- latency
- HTTP status

Normal logs **must not** contain:

- TCKN
- authorization tokens
- provider tokens
- cookies
- full name when unnecessary
- birth date
- address
- raw provider response

**Audit evidence** (TR, for legal/provider reconstruction) and **application logs** are separate stores and pipelines. Audit evidence of raw official payloads stays in Türkiye.

### 9. Backup boundary

Sensitive TR database backups **must remain within the Türkiye data boundary**.

Preferred path:

`TR PostgreSQL` → encrypted backup → **separate Türkiye** storage/location

Do **not** automatically back up sensitive TR data to Germany.

Encryption keys **must not** be stored alongside backup objects.

**Retention duration** is a separate legal/product decision (not frozen here).

Germany backups (`docs/operations/BACKUP-RESTORE.md`) cover the main platform database only. They must not be extended to dump TR sensitive tables into DE object storage.

### 10. Network boundary

- TR PostgreSQL **must not** be public.
- Public exposure should be limited to **required gateway / provider endpoints**.
- SSH: key-based; password and root login disabled; preferably management IP / VPN restricted.
- Do **not** automatically put foreign CDN / proxy infrastructure in front of **raw government-provider traffic**.
- Cloudflare Turnstile (or equivalent) for normal KONUMLU **consumer auth** is a **separate** concern (Germany consumer edge). It is not a front for TR official-provider calls.

### 11. Failure / degraded mode

V1 **may** run on **one** Türkiye VDS. Single-node failure is accepted for pilot.

If the TR Compliance Gateway is unavailable:

- main KONUMLU **should remain operational** where possible (search, messaging, unregulated listings, sessions, etc.)
- **new** government / EİDS verification becomes **temporarily unavailable**
- Germany **must not** bypass verification
- **no fail-open approval** (D-010; ADR-007 outage ≠ PASS)

Later scaling **may** add: second TR gateway node, TR database HA/replica, separate TR backup infrastructure. Do **not** implement HA in this freeze.

### 12. Capacity decision

4 vCPU / 8 GB RAM / 200 GB SSD / 5 TB traffic is **accepted for initial pilot** because this node does not run general KONUMLU workloads. Revisit from measured CPU, RAM, disk, and verification QPS — not from marketplace traffic on the Germany app.

---

## Relationship to ADR-007 and D-001

| Topic | Still governed by | This ADR adds |
|---|---|---|
| Who owns `IsVerified` / listing kinds / no bypass | ADR-007, D-010 | Geographic split of **raw** vs **decision** data |
| Official adapter location | ADR-007 “infrastructure adapter” | Production official I/O and raw payloads on **TR gateway**; Germany adapter talks to TR decision API, not to official PII APIs |
| Outage | ADR-007: pending, never PASS | Same, plus main app stays up without new verifications |
| Monolith | D-001 | Exception is a **compliance residency process**, not splitting listings/search/messaging into services |

Person-level e-Devlet (ADR-007 OI-007-10) remains **not invented** as a public API. If it is later required, it still uses this TR gateway and the same signed-decision export — it does not move raw person payloads to Germany.

---

## Unresolved legal / product decisions

These remain **OPEN**. This ADR does not invent them:

| ID | Item |
|---|---|
| OI-015-01 | Backup and verification-evidence **retention duration** (legal/product) |
| OI-015-02 | Official provider onboarding, contracts, fees, environments (ADR-007 OI-007-01–03) |
| OI-015-03 | Legal minimum PII TR **must** retain vs destroy (ADR-007 OI-007-04), still TR-only |
| OI-015-04 | Whether V1 requires person-level e-Devlet identity (ADR-007 OI-007-10) |
| OI-015-05 | Signature algorithm, key ceremony, mTLS CA, clock-skew numeric bounds |
| OI-015-06 | Türkiye hosting legal entity / KVKK processor terms for the VDS and backup location |
| OI-015-07 | Whether Germany may store listing/user UUIDs next to `subject_ref` (product mapping; must still exclude TCKN and raw identity) |

OI-015-07 default architectural stance until legal says otherwise: Germany **may** store KONUMLU subject UUIDs (listing id, verification kind, owner user id) plus opaque `subject_ref` and decision metadata. It **must not** store TCKN or raw identity fields to “help mapping.”

---

## Alternatives considered

### Run official EİDS/e-Devlet adapters on the Germany monolith

**Rejected.** Places raw government/identity data outside the Türkiye residency boundary this ADR freezes.

### Move the entire KONUMLU platform to Türkiye

**Rejected** for this decision. Main platform continues primarily in Germany/Hetzner. The gateway is scoped to sensitive verification only.

### Shared PostgreSQL or DE→TR replica / FDW

**Rejected.** Cross-country DB connection would leak or sync sensitive rows. Separate databases; signed decision over authenticated TLS only.

### Fail-open or admin PASS while TR is down

**Rejected.** D-010 / ADR-007.

### Put Cloudflare (or other foreign CDN) in front of official-provider traffic

**Rejected** as an automatic default. Consumer-auth Turnstile on the main site is unrelated.

### Treat the TR VDS as a second general app server (listings, search, media)

**Rejected.** Capacity and threat model assume a narrow compliance node.

---

## Consequences

### Positive

- Clear residency split: raw official data in TR; product in DE.
- D-010 remains enforceable without copying TCKN into Germany logs or tables.
- Main marketplace can stay up when TR is down, without illegal verification bypass.
- Pilot hardware is sized to the actual workload.

### Negative / trade-offs

- Second runtime, second PostgreSQL, second backup plane, second SSH/network boundary.
- Single TR VDS is a pilot SPOF for **new** verifications.
- Germany EİDS rows become decision-projections, not copies of official payloads (ADR-007 storage list refined).

### Constraints imposed

- No TR→DE transfer of the forbidden data list.
- No shared or cross-country database.
- No fail-open verification.
- No general KONUMLU workloads on the TR node.
- No automatic backup of sensitive TR data to Germany.
- Implementation of the gateway, migrations, and adapters is **out of scope** for this ADR.

---

## Documentation (no application code)

This ADR is the source of truth for the residency boundary. Pointers only: `docs/architecture/production-runtime.md`, `docs/operations/BACKUP-RESTORE.md`, ADR-007, `docs/security/HIGH-RISK-INVENTORY.md`.
