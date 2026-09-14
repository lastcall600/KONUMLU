# Auth abuse primitives (AUTH-A)

Identity-owned substrate for consumer authentication abuse protection. This is **not** a fraud engine, **not** Trust / Güven Pasaportu, and **not** complete 0C-AUTH.

AUTH-B session lifecycle/step-up is documented in `auth-session.md`. AUTH-C is implemented locally (`docs/operations/AUTH-SECURITY.md`). HumanChallenge production provider is Cloudflare Turnstile; production widget credentials, frontend widget, and email/SMS vendors remain launch blockers. Completing AUTH-A does not make consumer auth production-complete.

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

Identity policy → `HumanChallenge` port → `fake` (dev/test), `unconfigured` (fail-closed stub), or production **Cloudflare Turnstile**.

- Frontend tokens are never trusted alone
- Server verifies token + expected action (`AuthOperation`, server-owned) + exact hostname allowlist
- Solving a challenge is **not** identity verification, Step-Up, Trust, EİDS, or the Türkiye Compliance Gateway
- Production adapter: stdlib `POST https://challenges.cloudflare.com/turnstile/v0/siteverify` (`secret` + `response` only). V1 does **not** send `remoteip`. No Cloudflare SDK.
- Success requires `success=true` **and** hostname on the allowlist **and** `action` equal to the server operation. `success=true` alone is not enough.
- Token length max 2048; empty/oversized tokens are rejected before Siteverify
- One Siteverify attempt with bounded timeout (default 3s). No retry and no `idempotency_key` in V1 (tokens are single-use; ambiguous network/provider results fail closed — require a fresh token)
- Cloudflare `timeout-or-duplicate` maps to the internal expired/replay class (`challenge_failed`), not a provider outage
- Secret and challenge token are never logged or returned

Provider modes: `none` | `fake` | `unconfigured` | `turnstile`.

Staging/production reject `fake`. Challenge-required operations with provider `none` fail process start. `unconfigured` + required operations is allowed at start and **fail-closed** on every required request. `turnstile` with required operations and missing secret or empty/wildcard hostname allowlist fails process start.

### Action mapping (server-owned)

The widget `action` must equal the Identity `AuthOperation` for that HTTP handler. The client cannot choose the expected action. Current challengeable config operations:

`password_login`, `passkey_login_begin`, `passkey_login_finish`, `signup_start`, `signup_finish`, `signup_complete`, `reset_start`, `reset_verify`, `reset_complete`, `passkey_register_begin`, `passkey_register_finish`

### FRONTEND_WIDGET_PENDING

Consumer auth requests already accept `challengeToken`. There is no Turnstile widget yet. Expected contract:

browser Turnstile widget (public sitekey) → token → existing auth JSON `challengeToken` → backend Siteverify.

Do not embed the secret key in the frontend.

## Replay

When a challenge is required and the verifier succeeds, AUTH-A records a hashed-token Valkey replay key (max 1 / window). Cloudflare tokens are also single-use. **Both** controls coexist. AUTH-A does not add a PostgreSQL CAPTCHA ledger.

## Configuration

Grouped policy (existing login IP/account env vars remain required). Optional overrides inherit those windows:

- `IDENTITY_AUTH_TARGET_*` (signup/reset verify-by-challenge-id; password-login identifier target uses `IDENTITY_AUTH_PASSWORD_USER_*`)
- `IDENTITY_AUTH_COMPLETE_*` (signup/reset complete)
- `IDENTITY_AUTH_SENSITIVE_*` (authenticated passkey enrollment, passkey remove, step-up, session revoke; password re-auth also uses this session window plus password-login IP/account)
- `IDENTITY_HUMAN_CHALLENGE_PROVIDER` (`none` / `fake` / `unconfigured` / `turnstile`)
- `IDENTITY_HUMAN_CHALLENGE_OPERATIONS`
- `IDENTITY_HUMAN_CHALLENGE_HOSTNAME` (comma-separated exact allowlist; no wildcards)
- `IDENTITY_HUMAN_CHALLENGE_REPLAY_TTL`
- `IDENTITY_HUMAN_CHALLENGE_TIMEOUT` (Turnstile Siteverify; default 3s)
- `IDENTITY_HUMAN_CHALLENGE_TURNSTILE_SECRET` (server-only; never committed for production)
- `IDENTITY_HUMAN_CHALLENGE_TURNSTILE_SITEKEY` (public widget key)

Production Cloudflare secrets must not be committed. Official dummy test credentials may be used only in test/dev proof.

## AUTH-B / AUTH-C

AUTH-B is implemented locally (`docs/architecture/auth-session.md`): session rotation, idle Touch, security-center HTTP, passkey list/remove, Valkey Step-Up. HumanChallenge is not Step-Up. Device binding is not AUTH-C.

AUTH-C (this package): durable `identity.auth.security` v1 outbox events (safe metadata only; unknown-account login failures omit user id and identifiers). HumanChallenge production provider is Cloudflare Turnstile (`fake` rejected in staging/production; production credentials not in git). Email/SMS vendors are **not** selected (`disabled` default; `external` requires an adapter at worker start; Identity never claims "sent"). Classification: **HUMAN_CHALLENGE_ADAPTER_READY_CREDENTIALS_PENDING**. Operator runbook: `docs/operations/AUTH-SECURITY.md`.

