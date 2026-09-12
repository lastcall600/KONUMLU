# Auth abuse primitives (AUTH-A)

Identity-owned substrate for consumer authentication abuse protection. This is **not** a fraud engine, **not** Trust / Güven Pasaportu, and **not** complete 0C-AUTH.

AUTH-B session lifecycle/step-up is documented in `auth-session.md`. AUTH-C remains open. Completing AUTH-A does not make consumer auth production-complete.

## Rate-limit dimensions

Public auth operations use independent Valkey counters. One generic IP bucket is not used.

| Operation | IP | Account | Target | Proof / session | Device |
|---|---|---|---|---|---|
| Password login | yes | after resolve (UUID) | submitted identifier (before resolve) | — | unused |
| Passkey login begin | yes | — | — | — | unused |
| Passkey login finish | yes | after successful assertion | — | — | unused |
| Signup verification start | HTTP IP + issuance dest/IP | — | issuance dest hash | — | unused |
| Signup verification finish | yes | — | challenge id hash | — | unused |
| Signup complete | yes | — | — | signup-proof hash | unused |
| Reset start | HTTP IP + issuance dest/IP | — | issuance dest hash | — | unused |
| Reset verify | yes | — | challenge id hash | — | unused |
| Reset complete | yes | — | — | reset-proof hash | unused |
| Passkey register begin/finish | yes | session user | — | session id hash | unused |
| Passkey remove | yes | session user | — | session id hash | unused |
| Step-up begin/finish | yes | session user | — | session id hash | unused |
| Session revoke / revoke-others | yes | session user | — | session id hash | unused |
| Password re-auth (first passkey) | yes | session user | — | session id hash | unused |

Device is a defined dimension and stays unused. AUTH-A does not invent browser fingerprinting.

Signup/reset **send** throttles remain `IssuanceLimiter` (SMS-pumping: hashed destination + hashed IP). HTTP IP limits on start are a separate auth-abuse family.

## Valkey key strategy

Family: `identity:auth:rl:{operation}:{dimension}:{sha256hex}`

Issuance (unchanged prefix, IP now hashed):

- `identity:verify:issue:dest:{purpose}:{kind}:{sha256hex}`
- `identity:verify:issue:ip:{purpose}:{sha256hex}`

Challenge replay (hashed token, ephemeral): `identity:auth:challenge:replay:{sha256hex}`

Keys never contain raw email, phone, proofs, challenge tokens, session cookies, or auth tokens. Account and IP subjects are hashed.

Password login hashes the submitted identifier **before** account resolution (`email:` / `phone:` + canonical). Known and unknown identifiers therefore share the same 429 threshold. A resolved account UUID remains an additional internal dimension for cross-identifier credential stuffing and must not be the only throttle that can 429.

## Identifier hashing

Rate-limit and issuance subjects currently use **unsalted SHA-256**. Raw email/phone never appear in Valkey keys or `auth_abuse` logs.

KONUMLU does not currently have a dedicated keyed-HMAC / identifier-pseudonymization secret that is safe to reuse here. The verification **AES-256 material keyring** encrypts challenge delivery secrets; it is a different purpose and must not be reused as an HMAC key.

A future AUTH follow-up may add an explicit Identity HMAC secret for low-entropy identifier keys. That is **not** an AUTH-A blocker: Valkey is a derived store, keys expire with the policy window, and HTTP responses stay generic. Account UUIDs / other high-entropy internal IDs do not require the same treatment as email/phone.

## TTL / window

The existing Valkey primitive is used: atomic `INCR` with `PEXPIRE` only when the key is created. The policy window is the TTL. Dimensions do not share counters. `Retry-After` on HTTP 429 is the policy window in seconds (not remaining counter state).

## Fail-open / fail-closed

| Check | Valkey down | Notes |
|---|---|---|
| Auth abuse / issuance rate limits | **Fail closed** (`unavailable`) | Must not silently allow |
| Human challenge when required | **Fail closed** (`unavailable` or generic `forbidden`) | Token omission is not success |
| Session hot cache | **Fail open to PostgreSQL** | Unchanged (ADR-002) |
| Challenge when **not** required | Provider is not called | Unrelated auth must not block on the provider |

## RiskDecision

Internal Identity contract only: `allow`, `challenge`, `step_up`, `restrict`, `review`.

- No public 0–100 score
- No Trust writes or Trust level reuse (`new` / `verified` / `established`)
- Client cannot submit an authoritative risk decision
- V1 orchestration is deterministic (rate-limit + challenge policy)
- `step_up` is a reserved AUTH-B hook only

Internal reason codes (telemetry/policy, not user copy): `velocity_ip`, `velocity_account`, `velocity_target`, `challenge_required`, `challenge_failed`, `provider_unavailable`, `storage_unavailable`, `suspicious_auth_state`.

HTTP mapping stays generic: `rate_limited`, `unavailable`, `forbidden`. Internal reason codes are not returned to the client.

## HumanChallenge port

Identity policy → `HumanChallenge` port → development/test `fake` or `unconfigured` stub.

- Frontend tokens are never trusted alone
- Server verifies token + expected action (`AuthOperation`) + configured hostname when set
- Solving a challenge is **not** identity verification
- Production vendor is **not** selected in AUTH-A (no Cloudflare/Turnstile SDK, no production secret)

Provider modes: `none` | `fake` | `unconfigured`.

Staging/production reject `fake`. Challenge-required operations with provider `none` fail process start. `unconfigured` + required operations is allowed at start and **fail-closed** on every required request.

## Replay

When a challenge is required and the fake/local verifier succeeds, AUTH-A records a hashed-token Valkey replay key (max 1 / window). Production providers must either guarantee single-use or be used with this replay layer. AUTH-A does not add a PostgreSQL CAPTCHA ledger.

## Configuration

Grouped policy (existing login IP/account env vars remain required). Optional overrides inherit those windows:

- `IDENTITY_AUTH_TARGET_*` (signup/reset verify-by-challenge-id; password-login identifier target uses `IDENTITY_AUTH_PASSWORD_USER_*`)
- `IDENTITY_AUTH_COMPLETE_*` (signup/reset complete)
- `IDENTITY_AUTH_SENSITIVE_*` (authenticated passkey enrollment, passkey remove, step-up, session revoke; password re-auth also uses this session window plus password-login IP/account)
- `IDENTITY_HUMAN_CHALLENGE_PROVIDER`
- `IDENTITY_HUMAN_CHALLENGE_OPERATIONS`
- `IDENTITY_HUMAN_CHALLENGE_HOSTNAME`
- `IDENTITY_HUMAN_CHALLENGE_REPLAY_TTL`

No production challenge secrets are required for local proof.

## AUTH-B / AUTH-C

AUTH-B is implemented locally (`docs/architecture/auth-session.md`): session rotation, idle Touch, security-center HTTP, passkey list/remove, Valkey Step-Up. HumanChallenge is not Step-Up. Device binding and auth audit events are not AUTH-B.

AUTH-C and remaining 0C-AUTH: production human-challenge vendor adapter, durable auth security events, email/phone change and authenticated password-change product endpoints (reuse Step-Up), device binding schema, periodic session rotation (OI-002-03). Remember Me is **not** a current KONUMLU requirement. Passkey-first + optional Argon2id password fallback is unchanged.
