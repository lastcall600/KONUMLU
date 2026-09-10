# ADR-004: Authentication — Passkeys (FIDO2/WebAuthn) First-Class, Argon2id Fallback

**Status:** ✅ Accepted  
**Date:** 2026-09-05  
**Resolves:** D-009 (formal record and implementation rules)  
**Gates resolved:** none of G-01–G-09 — this ADR **documents** frozen D-009; it does not reopen O-002 / G-02 (session backing) or O-005 (notification channels)

---

## Decision question (from INDEX / PHASE-0A)

**INDEX:** Authentication: Passkeys (FIDO2/WebAuthn) First-Class, Argon2id Fallback — **Resolves: D-009**.  
**PHASE-0A 0A-13:** Authentication: Passkeys + Argon2id — **Gates: — (documents D-009)**.  
**ARCHITECTURE.md §19:** High — documents frozen decision.

The frozen choice is already made: Passkeys are primary; passwords are fallback only; password hashes are Argon2id; MD5, SHA-1, SHA-256, and bcrypt are forbidden.

This ADR records **how** that credential model is owned and constrained inside the Identity domain. It does **not** choose session storage, EİDS verification, Management Center login, notification providers, or mobile token formats.

---

## 1. Decision

KONUMLU authenticates **public-surface users** as follows:

1. **FIDO2/WebAuthn passkeys are the first-class credential.** Registration, sign-in, and authenticator lifecycle are designed around discoverable passkeys. The primary product and API path is WebAuthn, not passwords.
2. **Password authentication is a fallback** for edge cases (no usable authenticator, recovery onto a new device until a passkey is registered). It is **not** a second first-class method: not feature-parity in UX, not the default onboarding, not a requirement to have a password in order to have an account.
3. **Passwords, when stored, are hashed with Argon2id only.** No MD5, SHA-1, SHA-256 (as a password hash), bcrypt, scrypt, Argon2i/Argon2d as the platform password algorithm, or reversible encryption of passwords.
4. **Identity / Auth owns** credentials, passkey registrations, password hashes, and authentication ceremonies. Other domains consume `Authenticate*` / `ValidateSession` / passkey lifecycle **contracts**; they never store or verify credentials.
5. **Successful authentication issues a session** under D-016 (browser `__Host-` HttpOnly Secure cookies; no durable tokens in `localStorage` / `sessionStorage`). Session persistence mechanics are **not** defined here.
6. **Authentication is not EİDS verification** (D-010). A logged-in user is not therefore `IsVerified`. AI is not an authenticator (D-011).

---

## 2. Context

D-009 freezes the credential policy. ARCHITECTURE.md §10 and the Identity domain map require Passkey registration/authentication, password management as owned data, and Audit of auth events via outbox (ADR-003).

Without this ADR, V1 could still “comply” with D-009 on paper while shipping password-primary signup, bcrypt “temporarily,” or WebAuthn as an optional extra. Those are reversals of D-009 and are prohibited.

Related frozen items this ADR must not contradict: D-016 cookie rules; D-005 Valkey for rate limits; D-004 PostgreSQL as credential SoT; D-015 contracts; ADR-001 Identity package layout; ADR-003 outbox for crash-sensitive audit.

---

## 3. First-class passkey model

### 3.1 Meaning of first-class

| First-class (passkey) | Not first-class (password) |
|---|---|
| Default registration and sign-in path on web (and mobile when the platform authenticator is available) | Offered as fallback / recovery, not as the headline method |
| Account can exist **with passkeys and no password** | Account **must not** require a password to complete primary signup |
| UX and APIs optimized for ceremony + credential list | No “complete profile by setting a password” gate on the happy path |
| Multiple authenticators per user are expected | At most one password hash per user |

Shipping V1 with only passwords “until passkeys are ready” is **out of scope and forbidden** by D-009.

### 3.2 WebAuthn responsibilities (Identity)

Identity is the **Relying Party** for KONUMLU public users:

- Issues and consumes **challenges** for registration (`create`) and assertion (`get`).
- Stores **credential ID**, **public key**, **sign counter**, user handle binding, transports/AAGUID as needed — **never** private keys.
- Verifies origin / RP ID / user verification according to WebAuthn (exact RP ID, origins list: **OPEN**, must be set before Identity V1).
- Rejects cloned or replayed assertions using the stored sign counter and single-use challenges.
- Binds credentials to the Identity user ID, not to profile display fields (Users domain owns profile).

Ceremony I/O (attestation/assertion bytes) stays in Identity HTTP handlers and use-cases. Other domains do not parse WebAuthn objects.

### 3.3 User verification

Registration and authentication ceremonies **require user verification** (UV) for the public web Relying Party. Passkeys are the phishing-resistant factor; UV must not be silently dropped to make a demo work.

Attestation conveyance (`none` vs `direct` vs `enterprise`): **OPEN**. V1 may use `none` unless a later security ADR requires attestation for specific authenticators. Do not block passkeys on attestation that is not yet specified.

### 3.4 Surfaces

| Surface | Passkeys |
|---|---|
| Web (Next.js) | First-class WebAuthn in the browser. Session cookie per D-016. |
| Mobile (React Native) | Platform passkeys / WebAuthn where the OS supports them. Token storage in Keychain/Keystore (ARCHITECTURE.md) — **token format OPEN**, not this ADR. |
| Management Center | **Out of scope.** Separate auth realm (D-020 / O-009 / ADR-013). Do not reuse public passkey credentials as admin proof. |

---

## 4. Password fallback model

### 4.1 When passwords exist

Passwords are allowed as **fallback**, including:

- User cannot complete passkey ceremony (unsupported client, authenticator unavailable).
- Recovery onto a new device **until** a passkey is registered again.

They are **not**:

- A parity login method marketed or designed equal to passkeys
- A required second credential for every user
- Stored or verified with any algorithm except Argon2id
- Written to logs, audit payloads, cookies, or client storage in reversible form

Identity MAY support “set or replace password” for fallback users. Identity MUST support passkey-only users.

### 4.2 Hashing

- Algorithm: **Argon2id**.
- Unique per-password **salt** (cryptographically random). Stored with the hash.
- **Parameters** (memory, iterations/time, parallelism, tag length): **OPEN**. Must be chosen before Identity V1 ships; must be revisable by rehash-on-login without a plaintext export. Do not invent values in this ADR.
- Optional **pepper** (server-side secret): **OPEN**. If used, it lives in environment/secrets, not in PostgreSQL next to the hash, and not in source control (D-013 / environment philosophy).
- Verify with a **constant-time** compare of the Argon2id output.
- On successful password login, Identity MAY rehash if parameters were upgraded (parameter version stored with the hash).

**Forbidden:** MD5, SHA-1, SHA-256, SHA-512, bcrypt, scrypt, PBKDF2 as the password hash, reversible encryption, “hash on the client and store that” as the only server check.

### 4.3 Password policy (strength)

Minimum length and composition rules: **OPEN** (must exist before V1; KVKK/UX copy follows i18n ADRs). Architecture constraint: policy is enforced in **Identity**, not in the frontend alone.

---

## 5. Credential storage and source of truth

| Data | Store | Notes |
|---|---|---|
| Passkey public material, credential IDs, counters | PostgreSQL, Identity schema | Canonical |
| Argon2id password hashes + salt + param version | PostgreSQL, Identity schema | Canonical |
| WebAuthn challenge / ceremony state | Short-lived; Valkey **or** PostgreSQL | **OPEN**. Not business SoT; TTL must cover the ceremony only. Duration **OPEN**. |
| Account status | Users domain (PostgreSQL) | Authentication MUST fail closed if account is not eligible; Identity reads via Users **contract**, not Users tables (D-015) |
| Rate-limit counters | Valkey (D-005) | Derived; limits **OPEN** (no invented RPS) |

Identity must not store profile PII beyond what credential management requires (ARCHITECTURE.md Identity prohibited list).

---

## 6. Authentication flows (architecture)

### 6.1 Passkey registration (logged-in or during signup)

1. Identity creates a challenge bound to the user (or to an in-progress signup handle).
2. Client performs WebAuthn `create`.
3. Identity verifies attestation/client data and stores the credential.
4. Same transaction: domain write + **outbox** audit event (ADR-003) — passkey added.
5. Session issuance rules: if this completes signup, issue session per D-016; if already logged in, session rotation/revocation follows the session ADR, not this file.

### 6.2 Passkey authentication

1. Identity issues a challenge (discoverable credentials preferred so the user is not identified by a password identifier first).
2. Client performs WebAuthn `get`.
3. Identity verifies assertion, origin, RP ID, UV, counter.
4. If Users status allows: issue session (D-016).
5. Outbox audit: successful (and failed, without PII/raw assertion dumps) authentication.

Identifier-first “type email then password” MUST NOT be the default passkey path. Discoverable passkeys are the default.

### 6.3 Password fallback authentication

1. Explicit fallback endpoint/use-case — not the default landing ceremony.
2. Rate-limited (Valkey). Failed attempts: generic error to the client (no user enumeration if avoidable; exact enumeration policy **OPEN**).
3. Argon2id verify. On success: session + audit outbox; **encourage passkey registration** as a follow-on Identity capability, not a password-parity dashboard.

### 6.4 Credential change

Password change and passkey removal are Identity operations. Session revocation effects are owned by the session design (D-016 / ADR-002); this ADR only requires that those events are **security-sensitive** and **audited via outbox**. Do not specify idle/absolute session durations here.

---

## 7. Recovery and edge cases

D-009 does not freeze a recovery channel.

**Normative:**

- Recovery MUST NOT introduce a weaker password hash or durable tokens in web storage.
- Recovery MUST NOT treat AI as identity proof (D-011).
- Recovery MUST NOT mark a user EİDS-verified (D-010).
- After recovery, **passkey (re)registration is the intended steady state**, not long-term password-only use.

**OPEN:** exact recovery mechanism (e.g. whether email/SMS links exist) until notification channels and i18n are decided (O-005, O-008). Do not add a new IdP, SMS vendor, or magic-link stack in this ADR.

Passkey loss with no fallback password and no recovery channel is an operational/product risk; it is **not** solved by silently making passwords first-class.

---

## 8. Authorization vs authentication

Identity **authenticates** (who holds this credential) and **issues/validates sessions**. Fine-grained authorization (owns listing, trust level, EİDS) remains in the **owning domain** (ARCHITECTURE.md §10). Routing may check “has session”; domains check resource rules.

`ValidateSession` does not return EİDS status. Callers that need verification use Compliance/EİDS `IsVerified`.

---

## 9. Audit, rate limits, observability

- Login success/failure, passkey add/remove, password set/change, recovery completion: **outbox → Audit** (ADR-003). Payloads: event type, user id, credential type (`passkey` \| `password`), error **class**. No passwords, no raw authenticator responses, no session secrets.
- Rate limits on ceremony start, assertion, and password verify: Valkey. Thresholds **OPEN**.
- Metrics: passkey vs password success/fail counts, ceremony failures by class. No PII in logs.

---

## 10. What this ADR does not decide

- Session cookie backing store, idle/absolute timeouts, SameSite (D-016 / ADR-002 / O-002)
- Admin / Management Center authentication (O-009 / ADR-013)
- EİDS provider integration (D-010 / ADR-007)
- Notification/email/SMS providers (O-005 / ADR-006)
- Mobile access-token format
- WebAuthn library / Go module selection (unapproved dependencies: propose later; do not add packages in Phase 0A)
- Social/OAuth login, TOTP-as-primary-MFA, SMS OTP as first-class auth — **not introduced**

---

## Alternatives considered

### Passwords first-class, passkeys later

**Rejected.** Direct contradiction of D-009.

### bcrypt or SHA-256 for “simplicity”

**Rejected.** D-009 enumerates forbidden algorithms.

### Password required in addition to passkey for every user

**Rejected.** That makes passwords parity (or a mandatory second factor of a weaker class), not fallback. Step-up UV is the passkey itself.

### Delegating auth to a hosted IdP (Cognito, Auth0, Firebase)

**Rejected** for V1 without a new ADR. Conflicts with local-first (D-013), provider-independence (D-012), and Identity as a core domain. Not in the frozen stack.

### Storing session JWTs or passkey material in `localStorage`

**Rejected.** D-016 / AGENTS.md.

---

## Consequences

### Positive

- Phishing-resistant default aligned with D-009.
- Passkey-only accounts are valid; Identity schema and UX cannot assume a password exists.
- Password hashing algorithm is unambiguous for review and forbidding weak hashes in CI/review.
- Clear domain ownership; EİDS remains a separate question.

### Negative / trade-offs

- WebAuthn RP configuration (origins, HTTPS, `__Host-` cookies) is stricter to operate than password forms.
- Fallback and recovery remain partially OPEN; V1 cannot invent a vendor-specific reset stack here.
- Clients without passkey support need the fallback path without letting it take over the product.

### Constraints imposed

- Identity V1 MUST ship passkey registration and authentication as the primary path.
- Password hashes, if any, MUST be Argon2id with unique salt.
- No forbidden hash algorithms in Identity or anywhere else for user passwords.
- Other domains MUST NOT verify passwords or WebAuthn assertions.
- Accepting this ADR does not initialize Go, WebAuthn libraries, or services.

---

## Implementation rules (when V1 code is allowed)

1. **E-A001** Primary signup/sign-in use-cases are WebAuthn; password use-cases are explicitly fallback.
2. **E-A002** Persist passkey public keys and Argon2id hashes only in Identity PostgreSQL tables.
3. **E-A003** Reject any password hash algorithm other than Argon2id.
4. **E-A004** Never write passwords, private keys, or durable session tokens to `localStorage` / `sessionStorage`.
5. **E-A005** Auth success/failure and credential lifecycle writes that must not be lost after commit use the transactional outbox for Audit (ADR-003).
6. **E-A006** Other domains import `internal/identity/contracts` only (ADR-001).
7. **E-A007** Authentication MUST NOT set or skip EİDS verification status.
8. **E-A008** Do not implement Management Center login in this credential design.

---

## Open values (must be set before Identity V1)

| ID | Item |
|---|---|
| OI-004-01 | Argon2id memory, time, parallelism, key length; rehash versioning |
| OI-004-02 | Pepper yes/no and secret storage |
| OI-004-03 | WebAuthn RP ID and allowed origins |
| OI-004-04 | Challenge storage (Valkey vs PostgreSQL) and ceremony TTL |
| OI-004-05 | Attestation conveyance policy |
| OI-004-06 | Password minimum policy |
| OI-004-07 | User-enumeration and lockout policy; rate-limit numbers |
| OI-004-08 | Recovery channel (blocked on O-005 / product) |
| OI-004-09 | Discoverable vs non-discoverable credential requirements per surface |
| OI-004-10 | Mobile passkey + secure-enclave token mapping |
| OI-004-11 | Exact Identity contract additions beyond ARCHITECTURE.md sketches |

These do **not** reopen D-009, passkey-first ranking, Argon2id, or forbidden hashes.

---

*ADR-004 — authored Phase 0A, Task 0A-13.*
