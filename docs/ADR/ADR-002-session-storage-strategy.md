# ADR-002: Browser Session Backing Store Strategy

**Status:** ✅ Accepted  
**Date:** 2026-09-05  
**Resolves:** D-016 (open part: server-side backing store and SameSite), O-002  
**Gates resolved:** G-02 — unblocks Identity / Auth V1 session implementation (browser realm only)

---

## 1. Decision

KONUMLU browser sessions use **Option C (hybrid)**, implemented as:

1. An **opaque, high-entropy session identifier** in a `__Host-` cookie (not a self-contained JWT or encrypted claims blob).
2. **Hot session state in Valkey** (derived store; fast path).
3. **Durable session, device, and revocation state in PostgreSQL**, owned by the Identity domain (canonical for whether a session exists and whether it has been revoked).
4. **Account and security records in PostgreSQL** (Users / Identity). A live Valkey session record is **never** sufficient to authorize a suspended, deactivated, or credential-invalidated account.

Valkey is used for sessions, cache, rate limits, and presence (D-005). It is **never** the business source of truth (D-004, D-005).

This ADR does **not** design admin/Management Center authentication. That is a separate realm (O-009 / ADR-013). This ADR does **not** specify mobile token formats; it only constrains how browser sessions coexist with a future mobile channel.

---

## 2. Context

D-016 freezes browser cookie attributes (`__Host-`, `HttpOnly`, `Secure`, SameSite Strict or Lax) and forbids durable auth tokens in `localStorage` / `sessionStorage`. O-002 left the **server-side persistence mechanism** open:

- Signed JWT payload in the cookie vs. opaque token backed by Valkey.

KONUMLU also requires operational session control that a purely stateless cookie cannot provide without secretly reintroducing a store:

- Immediate logout and stolen-session response
- Logout-all-devices
- A session/device security center
- Revocation after password, passkey, or other security-sensitive changes
- Account suspension taking effect without waiting for cookie expiry

ARCHITECTURE.md already states: session data in Valkey may be authoritative for **active** sessions, but session validity is backed by Identity’s PostgreSQL records; a stale cache must not cause incorrect authorization; a cache miss must fall back to PostgreSQL.

Passkeys are first-class; passwords are Argon2id fallback only (D-009). Those credential rules are unchanged. This ADR only chooses how an **already authenticated browser** proves continuity of that authentication.

---

## 3. Selected session model

**Name:** Opaque cookie identifier + Valkey hot cache + PostgreSQL durable session/device/revocation records.

```
Browser
  └── __Host- cookie: opaque session ID (raw secret)
        │
        ▼
Identity (same process, contract-owned)
  ├── Valkey (derived): hashed-ID → hot session record
  └── PostgreSQL (canonical): hashed-ID → durable session/device row
                              user/account status, credential version / session epoch
```

**Request validation order (normative):**

1. Reject if cookie missing, malformed, or missing required attributes on issuance path.
2. Look up hot session in Valkey by **hash of** the cookie value.
3. On Valkey miss: look up the durable session row in PostgreSQL. If valid, **rehydrate** Valkey. If absent or revoked, reject.
4. Independently enforce Identity/Users PostgreSQL facts that Valkey must not override: account status, session epoch / credential generation, explicit revocation. If these fail, invalidate hot and durable session material and reject.
5. Apply idle and absolute timeout rules (durations: OPEN). On success, optionally refresh idle watermark (see §8).

**Rejected as the V1 browser model:**

- Self-contained signed/encrypted session JWT as the sole source of session truth (Option B).
- Valkey as the only session store with no durable session/device/revocation rows (Option A as originally framed).

---

## 4. Cookie requirements

All browser authentication cookies MUST:

| Attribute | Requirement |
|---|---|
| Name prefix | `__Host-` (D-016). Suggested name: `__Host-konumlu_session` (exact name OPEN). |
| `HttpOnly` | Required. Not readable by JavaScript. |
| `Secure` | Required. HTTPS only. |
| `Path` | `/` (required by `__Host-`). |
| `Domain` | **Must not be set** (required by `__Host-`). |
| `SameSite` | **`Lax`** (resolved here; see §14). |
| Persistence | Session cookie vs. persistent `Max-Age`/`Expires`: OPEN (must not exceed absolute timeout once that value is set). |
| Storage | Cookie only. **Never** `localStorage`, `sessionStorage`, or non-HttpOnly JS-readable cookies for auth tokens, refresh tokens, or session IDs. |

A second cookie for CSRF (non-HttpOnly) is **not** selected in this ADR. CSRF is handled without exposing the session secret (see §14).

---

## 5. Session ID / token characteristics

The cookie value is an **opaque session secret**, not a JWT.

| Property | Rule |
|---|---|
| Contents | Cryptographically random bytes. No user ID, roles, locale, or other claims in the cookie. |
| Entropy | At least 256 bits of randomness. |
| Encoding | URL-safe encoding in the cookie (e.g. base64url). Exact encoding OPEN. |
| Guessability | Must be infeasible to brute-force; reject non-conforming values without hitting stores when cheap to do so. |
| Server storage | Stores persist only a **one-way hash** of the secret (e.g. SHA-256). The raw cookie value is not written to PostgreSQL, Valkey, logs, or audit payloads. |
| Presentation | Sent only via the `__Host-` cookie. Not placed in query strings, WebSocket URLs, or `Authorization` headers for the browser web app. |
| Lifetime of the identifier | Rotated per §7. The identifier is not a long-lived refresh token and must not be treated as one. |

No signed self-contained browser session JWT is introduced for V1. Adding one requires a new ADR.

---

## 6. Server-side stored session fields

### 6.1 PostgreSQL (Identity-owned, durable)

Minimum durable session/device record (field names illustrative):

- Session record ID (internal)
- **Hash** of session secret
- User ID
- Session epoch or credential-generation counter **copy at issue time** (for bulk revoke)
- Channel: `web` (mobile tokens, if later, are a different channel — out of scope)
- Device/session label material: user-agent family, coarse client hints, created-at IP **classification only** (not a second auth factor)
- Created at, last-seen at, idle-deadline watermark, absolute-expires-at
- Revoked at / revoke reason (nullable)
- Rotation parent/child linkage (session family) so old IDs can be rejected after rotation
- Display metadata needed by a session/device security center (see §15)

PostgreSQL also holds **account/security records that are not hot session state**: credentials, passkey registrations, password hashes (Argon2id), account status, and a **user-level session epoch** (or equivalent) incremented on logout-all and security-sensitive revocation.

PII minimization: do not store raw IP+UA dumps unbounded; retention and exact columns are OPEN (KVKK/GDPR constraint). Full values must not appear in application logs (ARCHITECTURE.md).

### 6.2 Valkey (derived, hot)

Hot record keyed by session-secret hash. Values are a cache of the durable row plus idle watermark. TTL must not outlive the remaining absolute timeout.

Valkey MUST be treated as **rebuildable**. Flushing Valkey must not create sessions, restore revoked sessions, or authorize suspended accounts. It may cause extra PostgreSQL reads until rehydration.

### 6.3 What PostgreSQL is not

PostgreSQL is not required to serve every request’s session lookup when Valkey hits. Hot path performance may use Valkey. **Revocation, existence, epoch, and account standing** remain PostgreSQL-backed as specified in §3.

---

## 7. Rotation rules

Session **identifier rotation** is mandatory. Rotation issues a new opaque secret, writes a new hashed row (or successor), sets the cookie to the new value, and invalidates the predecessor after a short overlap window if needed to absorb in-flight requests.

Rotate at least when:

- Initial login (passkey or password fallback) completes
- Authentication step-up completes
- A security-sensitive change binds to the current browser (after re-auth)
- Privilege of the session changes in a way Identity owns (if any such change exists in V1)
- Periodic rotation: **interval OPEN** (must be defined before Identity V1 ships)

Do not rotate on every request (write amplification and race conditions). Concurrent requests during rotation must not leave two long-lived valid secrets; predecessor overlap duration is OPEN.

---

## 8. Idle timeout policy approach

Idle timeout is a **sliding inactivity limit**. Last-seen / idle watermark is updated on authenticated activity according to a refresh cadence (every request vs. quantized refresh: OPEN, to limit write load).

If idle timeout is exceeded: session is revoked; cookie must be cleared on the next response that the server can issue; user must authenticate again.

**Duration: OPEN.** Not frozen in D-016 or O-002. Must be set before Identity V1 implementation, with the same value used to bound Valkey TTL and durable idle-deadline.

---

## 9. Absolute timeout policy approach

Absolute timeout is a **hard lifetime from session creation** (or from the login that created the session family). It is not extended by activity. When exceeded: revoke; require re-authentication.

**Duration: OPEN.** Must be set before Identity V1 implementation. Absolute expiry is stored on the durable row and enforced even if Valkey still holds a hot record.

---

## 10. Logout / revocation semantics

**Logout (this session):**

1. Mark the durable session revoked in PostgreSQL.
2. Delete the Valkey hot key.
3. Clear the `__Host-` cookie (`Secure`, `Path=/`, no `Domain`, matching issuance).
4. Record an audit event via the Audit path required for auth events (outbox; D-008). Audit payload must not include the raw session secret.

**Stolen-session / single-session revoke** (security center or support via Identity admin contract): same invalidation for that session ID/family member. Other sessions remain until separately revoked.

**Fail closed:** unknown, malformed, revoked, expired, epoch-mismatched, or hash-mismatch credentials yield unauthenticated. Do not “repair” a cookie by minting a new session except through an explicit login.

---

## 11. Logout-all-devices behavior

Logout-all (user-initiated or security-triggered):

1. Increment the user’s **session epoch** (or equivalent generation counter) in PostgreSQL.
2. Mark all durable sessions for that user revoked (web now; future channels as they exist).
3. Delete all Valkey hot keys for that user (scan-by-user index or key namespace owned by Identity).
4. Clear the current browser cookie if a response is available.
5. Audit the bulk revoke.

Any hot record whose stored epoch is stale is invalid even if Valkey delete lags. Epoch is the safety net for incomplete cache deletes.

---

## 12. Security-sensitive change behavior

The following **must** revoke sessions as specified. Exact product copy is out of scope.

| Event | Session effect |
|---|---|
| Password change (fallback path) | Logout-all (epoch bump + revoke all durable sessions). User re-authenticates. |
| Passkey registration | Does **not** by itself require logout-all. OPEN whether other sessions should be notified. |
| Passkey removal / authenticator loss recovery | Logout-all. |
| Account suspension / deactivation | Logout-all plus account status in PostgreSQL; subsequent requests fail even if a hot key remains until deleted. |
| Credential compromise / forced reset by operations | Logout-all via Identity admin contract. |
| User-initiated “this device” logout | Single session (§10). |
| User-initiated “all devices” | Logout-all (§11). |

A Valkey hit **must not** keep a session alive across these events. Implementation must re-check epoch and account status (every request, or with an invalidation path that cannot miss suspension — OPEN as an optimization, not as a permission to skip PostgreSQL SoT).

---

## 13. Valkey outage / degraded-mode behavior

| Condition | Behavior |
|---|---|
| Valkey miss, PostgreSQL healthy | Look up durable session; if valid, rehydrate Valkey; continue. |
| Valkey down, PostgreSQL healthy | **Degraded mode:** validate against PostgreSQL only. Do not fail-open. Do not skip epoch/account checks. New logins may persist durable rows; hot cache writes are retried when Valkey returns. |
| PostgreSQL down | **Fail closed** for login and for session validation that cannot confirm durable validity and account standing. Do not treat Valkey-only as sufficient authorization while account status cannot be confirmed. |
| Valkey data loss / flush | All users rehydrate from PostgreSQL; revoked rows stay revoked; no session resurrection. |
| Split brain / stale hot record | PostgreSQL revocation and epoch win. |

Do not add an in-memory-only session store on the Go process as a substitute SoT (incompatible with horizontal scaling and process restart).

---

## 14. CSRF strategy implications

**SameSite=`Lax`** is selected so that:

- Cross-site POST from hostile pages does not include the session cookie (modern browsers).
- Top-level GET navigations from email/notification links (Slice B) can keep the user signed in. `Strict` would drop the cookie on those arrivals and look like random logout.

`Lax` is not a complete CSRF program:

- Mutating requests MUST additionally be rejected unless they are same-site / same-origin per server checks (`Origin` / `Referer` and/or `Sec-Fetch-Site`). Exact header policy is OPEN but must be defined before Identity V1 ships.
- Cookie session secret remains `HttpOnly`; CSRF must **not** require JavaScript to read the session ID.
- Cross-site cookies for a separate site or subdomain cannot set `__Host-` cookies for the app host.

WebSocket or cookie-authenticated cross-origin API patterns, if added later, need their own CSRF/origin rules; they are not licensed by this ADR.

---

## 15. Session / device security center implications

The public web app MUST be able to show the current user a list of **active sessions/devices** sourced from Identity durable records (via Identity contract), including the current session, last-seen, channel, and revoke actions.

Implications:

- Durable rows cannot be Valkey-only; TTL eviction would erase the security center.
- The current session must be identifiable without echoing the raw cookie secret (compare hash in request context).
- Revoke-one and revoke-all are Identity operations; Management Center may call **admin** contracts later (ADR-013) but must not store a parallel session table.
- Display strings follow platform i18n (O-008 / ADR-009 / ADR-014); this ADR does not freeze copy.

---

## 16. Alternatives considered

### Option A — Opaque ID in cookie; session state only in Valkey; PostgreSQL holds account/security records but not session rows

**Rejected as the complete model.**

- Immediate logout and stolen-session kill work only while Valkey keys exist and are correctly addressed.
- Logout-all and security-center history need a durable index; reconstructing devices from Valkey TTL keys is operationally fragile.
- Valkey flush logs everyone out **and** wipes device inventory.
- Conflicts with “Valkey is never business SoT” if the only proof of “this device is logged in” lives in Valkey.
- Valkey outage without PostgreSQL session rows forces fail-closed for all browsers or an unsafe fail-open.

Valkey as **hot cache** is retained inside Option C.

### Option B — Short-lived signed/encrypted self-contained token in the cookie; little or no hot store

**Rejected.**

- Logout, logout-all, stolen-session, password/passkey change, and suspension require a denylist, epoch store, or waiting for expiry. A denylist **is** a session store, without a security center.
- Cookie size grows with claims and signatures; `__Host-` cookies compete for a small per-domain budget.
- Signing-key rotation and JWT library misuse (algorithm confusion, none, weak secrets) add risk without removing PostgreSQL checks for account status.
- “Stateless” does not match ARCHITECTURE.md: authorization-relevant standing stays in PostgreSQL; skipping Valkey does not skip SoT reads for suspension.
- Horizontal scaling is easy; **incident response** is not.

A short-lived signed cookie **plus** a server denylist is a worse hybrid (larger cookie, two revocation mechanisms) than opaque ID + hashed durable row.

### Option C — Hybrid (selected)

Short-lived/rotating **opaque** cookie secret + server-side session/device/revocation state (PostgreSQL) + Valkey hot path.

Fits KONUMLU: revocation, security center, PostgreSQL SoT, Valkey as derived, local-first (PostgreSQL + Valkey run locally, D-013), Türkiye portability (no vendor IdP session service), monolith replicas without sticky sessions.

### Other rejects

- **Sticky in-process sessions:** incompatible with multiple `cmd/server` instances.
- **Auth tokens in `localStorage` / `sessionStorage`:** forbidden (D-016).
- **Shared cookie with Management Center:** out of scope; separate realm.

---

## 17. Consequences / tradeoffs

### Positive

- Immediate, targeted, and bulk revocation without waiting for token expiry.
- Cookie stays small and claim-free; session secret is not a readable JWT.
- Security center and incident response have durable records.
- Valkey outage degrades to PostgreSQL instead of inventing a second SoT.
- Account suspension cannot be bypassed by a warm cache if epoch/status checks are enforced.
- Aligns with D-004, D-005, D-016, and ARCHITECTURE.md cache-fallback rules.
- Mobile can later share device/session **inventory** without sharing the browser cookie mechanism.

### Negative / trade-offs

- Every authenticated request needs Valkey or PostgreSQL (plus account/epoch checks). Not zero-store.
- Must implement rotation races, hashed storage, user-keyed Valkey deletion, and cookie clearing.
- Degraded mode increases PostgreSQL load during Valkey outages.
- SameSite=`Lax` is weaker than `Strict` for some CSRF edge cases; compensated by origin/fetch-site checks (OPEN details).
- Idle/absolute durations still OPEN; shipping Identity without them is not allowed.

### Constraints imposed

- Identity owns browser session persistence. Other domains validate the session through Identity contracts (`ValidateSession`, `InvalidateSession`, and future list/revoke methods). No domain reads Valkey session keys or `identity` session tables directly (D-015, ADR-001).
- Valkey access goes through the platform cache abstraction, not a Valkey client inside domain “business rules” scattered across modules (D-005, provider-abstraction principle).
- No durable auth material in web storage.
- No self-contained browser session JWT as V1 mechanism.
- Admin auth must not reuse this cookie realm.

---

## 18. Implementation rules

These apply when Identity V1 is implemented (after Phase 0A). They are architecture rules, not permission to write code in Phase 0A.

1. **E-S001** Cookie issuance and clearing MUST set `__Host-`, `HttpOnly`, `Secure`, `Path=/`, no `Domain`, `SameSite=Lax`.
2. **E-S002** Persist only the hash of the session secret. Never log the raw cookie value.
3. **E-S003** Valkey is optional for **latency**, forbidden as **sole** session authority.
4. **E-S004** Suspension, deactivation, epoch mismatch, and explicit revoke MUST fail the request even if Valkey still has a key.
5. **E-S005** Logout-all MUST bump epoch and revoke durable rows; cache delete is necessary but not sufficient.
6. **E-S006** Composition-root wiring only: Identity implementation may use `internal/platform/cache` and DB; other domains import `internal/identity/contracts` only (ADR-001).
7. **E-S007** Browser session validation MUST NOT be the EİDS authority and MUST NOT skip `IsVerified` where D-010 requires it. Session ≠ verification.
8. **E-S008** Fail closed when durable validity and account standing cannot be confirmed.
9. **E-S009** CSRF origin/same-site checks for mutating HTTP methods MUST be implemented before exposing cookie-authenticated mutations on the web origin.
10. **E-S010** Do not initialize Valkey, PostgreSQL, Go modules, or frameworks as part of accepting this ADR.

---

## 19. Open values (measure or decide later)

Not frozen here; **must** be decided before Identity / Auth V1 implementation. None of these re-open Option C or D-016 attributes.

| ID | Item | Notes |
|---|---|---|
| OI-002-01 | Idle timeout duration | Not in baseline. |
| OI-002-02 | Absolute timeout duration | Not in baseline. |
| OI-002-03 | Periodic rotation interval and predecessor overlap | — |
| OI-002-04 | Idle watermark refresh cadence | Every request vs. quantized updates. |
| OI-002-05 | Cookie `Max-Age` vs. session cookie | Must not exceed absolute timeout. |
| OI-002-06 | Exact cookie name | Must remain `__Host-` prefixed. |
| OI-002-07 | Secret encoding and hash algorithm confirmation | Hash-at-rest is required; algorithm default SHA-256 unless a later ADR says otherwise. |
| OI-002-08 | Mutating-request CSRF header policy | `Origin` / `Referer` / `Sec-Fetch-Site` details. |
| OI-002-09 | Optimization: how often to re-read account status vs. rely on epoch invalidation | Must not allow stale authorization. |
| OI-002-10 | Session/device metadata retention (KVKK) | — |
| OI-002-11 | Passkey **registration** (not removal) fan-out to other sessions | — |
| OI-002-12 | Mobile token format and mapping onto the same device table | Out of this ADR’s decision; coexistence only. |
| OI-002-13 | Rate-limit and lockout on session lookup failures | Complements Valkey rate limiting (D-005). |

---

## Evaluation summary (KONUMLU criteria)

| Criterion | Option A (Valkey-only sessions) | Option B (self-contained cookie token) | Option C (selected) |
|---|---|---|---|
| Immediate logout / stolen session | Yes, if key known | No, unless denylist | Yes |
| Logout-all-devices | Fragile | Denylist/epoch anyway | Yes (epoch + durable rows) |
| Device/session security center | Weak (TTL) | No inherent list | Yes |
| Password/passkey/security-change revoke | Possible | Needs store | Yes |
| Account suspension | Needs PG anyway | Needs PG anyway | PG SoT + cache drop |
| Horizontal scaling | Yes (shared Valkey) | Yes | Yes |
| Valkey outage | All sessions fail | Sessions continue until expiry (revocation blind) | Degrade to PostgreSQL |
| PostgreSQL SoT | Partial | Claims vs. standing split | Sessions + standing in PG; Valkey derived |
| Session rotation | Possible | Possible | Required |
| Idle / absolute timeout | Possible | Possible | Required; durations OPEN |
| CSRF | Cookie + SameSite | Same | SameSite=`Lax` + origin checks |
| Cookie size | Small | Larger | Small |
| Operational complexity | Medium | Low until incident | Higher, explicit |
| Incident response | Cache-dependent | Poor | Strong |
| Mobile coexistence | Separate later | Tempting to reuse JWT badly | Shared inventory, separate credential |
| Performance | Fast | Fastest CPU/cookie | Fast path Valkey; miss → PG |
| Türkiye / local-first portability | Valkey+PG | Crypto-only + still PG for users | Valkey+PG, no vendor session SaaS |

---

*ADR-002 — authored Phase 0A, Task 0A-11.*
