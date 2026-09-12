# Auth session lifecycle, security center, and step-up (AUTH-B)

Identity-owned consumer session lifecycle and recent-strong authentication. This is **not** Trust / Güven Pasaportu, **not** Staff IAM, and **not** complete 0C-AUTH.

AUTH-A abuse primitives remain in force. HumanChallenge proves human-like interaction; Step-Up proves possession of an existing user-owned passkey. Completing AUTH-B does not make consumer auth production-complete.

## Durable truth

PostgreSQL (`identity.sessions`) is the session source of truth. The cookie is `__Host-konumlu_session`: Secure, HttpOnly, Path=/, no Domain, existing SameSite=Lax. The raw cookie is hashed (SHA-256) before storage. Valkey holds:

- session hot cache keyed by token hash (fail-open to PostgreSQL; ADR-002)
- step-up elevation keyed by session id (fail-closed; loss requires stepping up again and never grants elevation)
- first-passkey bootstrap keyed by session id (fail-closed; scoped only to `passkey_add`; loss never grants enrollment authority)

`session_epoch` on `identity.users` is bumped only on revoke-all / password-reset completion. It is not copied onto the session row. It is the Valkey-stale safety net for logout-all.

Multiple concurrent consumer sessions are supported. Login does not revoke other devices.

## Rotation semantics

| Event | New session id/token | Predecessor in this browser | Other devices |
|---|---|---|---|
| Fresh password login | yes | revoke that cookie session only | keep |
| Fresh passkey login | yes | revoke that cookie session only | keep |
| Signup complete | yes | revoke that cookie session only | keep |
| Password reset complete | no session issued | n/a (no auto-login) | revoke **all** + epoch bump |
| Step-up finish | yes (successor) | revoke predecessor after Grant on successor | keep |
| User logout | n/a | revoke current | keep |
| Revoke one owned session | n/a | if current, clear cookies | others unchanged |
| Revoke others | n/a | keep current | revoke other rows; **no** epoch bump |
| Successful passkey remove | n/a | logout-all + epoch (ADR-002 §12) | logout-all |

Pre-auth / leftover browser cookies must not become the authenticated session identifier. Signup proofs and WebAuthn ceremony tokens are never session cookies.

Periodic rotation (ADR-002 OI-002-03) remains OPEN and is not implemented.

## Idle Touch

ADR-002 idle timeout is sliding. `Sessions.Touch` runs from `Sessions.Resolve` (authenticated API session use), not from static/asset requests.

- Quantum: `min(1m, idle/4)`, floor 5s (derived; no extra env knob)
- Absolute expiry is never extended
- SQL update requires `revoked_at IS NULL` and both watermarks still in the future
- n=0 does not revive the row; concurrent revoke wins
- Touch store unavailability does not fail the request; revoked/expired/ineligible discovered during Touch fail closed

## Security-center HTTP

Authenticated consumer APIs (Identity handler):

| Method | Path | CSRF + Origin | Notes |
|---|---|---|---|
| GET | `/v1/auth/sessions` | session only | Active sessions; `current` flag; no token/hash/IP |
| POST | `/v1/auth/sessions/{sessionId}/revoke` | yes | Owner only; current session clears cookies |
| POST | `/v1/auth/sessions/revoke-others` | yes | Keeps current; does not bump epoch |
| GET | `/v1/auth/passkeys` | session only | Management id + created/last used + transports |
| POST | `/v1/auth/passkeys/{passkeyId}/remove` | yes | Step-up required; last-factor 409; success logout-all |
| POST | `/v1/auth/passkey/register/password-reauth` | yes | First-passkey password re-auth only; AUTH-A limits; generic 401 |
| POST | `/v1/auth/step-up/passkey/begin` | yes | Existing user-owned passkey assertion |
| POST | `/v1/auth/step-up/passkey/finish` | yes | Rotates session; Grant on successor |

List/revoke never return session tokens, hashes, cookies, CSRF, WebAuthn challenges, or credential secrets. Foreign ids are `not_found`. Client `step_up=true` / `bootstrap=true` is ignored.

Existing AUTH-A sensitive rate-limit buckets apply to passkey remove, step-up begin/finish, and session revoke (`IDENTITY_AUTH_SENSITIVE_*`). First-passkey password re-auth uses password-login IP + account buckets plus the sensitive session bucket (`password_reauth`).

## Device metadata

`identity.devices` exists but HTTP still issues sessions with `device_id=nil`. The table has no UA/label columns. Device binding and security-center device labels are **deferred** (would need a migration). AUTH-B does not fingerprint browsers and does not persist IP history.

## Step-Up contract

Internal only: recent-strong elevation for a **specific session + user**, TTL-bounded.

- Password login does **not** grant recent-strong
- Passkey login **attempts** Grant for `IDENTITY_STEP_UP_TTL` (best-effort: login still succeeds if Valkey Grant fails; then sensitive ops require an explicit step-up)
- Sensitive operations in AUTH-B: passkey add begin/finish, passkey remove
- Passkey add may also proceed with **first-passkey bootstrap** (below). That path is not a Step-Up grant.
- Future email/phone/password-change endpoints are not implemented here; they should reuse `Require(session, operation)`
- Not a 0–100 score. Not Trust. Not HumanChallenge. Not staff authorization

Storage: Valkey `identity:stepup:{sessionID}` JSON `{user_id, session_id, issued_at}` with TTL. Process restart / Valkey flush drops elevation (fail closed). Revoked/expired sessions cannot use leftover keys because HTTP always `Resolve`s first. Elevation is not transferable by `user_id`.

Config: required `IDENTITY_STEP_UP_TTL` (`>0`, `<=15m`, `<= IDENTITY_SESSION_IDLE`). V1 local/test value is `5m`. The same TTL bounds first-passkey bootstrap.

Factor: existing user-owned passkey (UV required). Login ceremonies store `UserID=nil`; step-up ceremonies store the session user. Cross-use is `ceremony kind mismatch`. SMS/email step-up is not added. Password re-auth is **not** a Step-Up factor.

## First-passkey bootstrap

Accounts may complete signup with zero passkeys. Password login and signup do **not** grant recent-strong Step-Up. Step-Up itself requires an existing user-owned passkey. Without a bootstrap path, those accounts cannot enroll a first passkey.

Bootstrap is a separate Valkey grant: `identity:passkey-bootstrap:{sessionID}` JSON `{user_id, session_id, scope: passkey_add, kind, issued_at}`. It is bound to one session + user, TTL-bounded (`IDENTITY_STEP_UP_TTL`), fail-closed, and scoped **only** to `passkey_add`. It never authorizes `passkey_remove` or unrelated sensitive operations. Client `bootstrap=true` / `step_up=true` is ignored.

| Account state | First `passkey_add` |
|---|---|
| Zero passkeys + active password | `POST /v1/auth/passkey/register/password-reauth` then register. Argon2id verify of the current account; AUTH-A rate limits; generic 401. Not primary auth. |
| Zero passkeys + no password, this signup session | Server grants bootstrap on passwordless `signup/complete` for that issued session only. |
| ≥1 active passkey | Normal passkey Step-Up. Bootstrap Grant/Allow refused. |

Passwordless signup Grant failure (Valkey down) revokes the just-created session and returns `503` (account row remains; no cookies). Password signup does not receive signup bootstrap; that account uses password re-auth.

Successful first passkey `register/finish` consumes/deletes the bootstrap key. Replay, another session, revoked/expired session, or a later account that already has a passkey cannot reuse it. Password login never copies bootstrap onto a new session.

## Last-passkey safety

Remove is allowed if another **active** passkey exists **or** an active password fallback exists. Otherwise HTTP **409** `conflict`. Duplicate remove of an already-revoked credential is success (no extra logout). AUTH-B does not silently create a password and does not invent recovery.

Successful removal of a live credential performs logout-all (ADR-002 §12).

## Security events

AUTH-B does **not** add durable auth audit event types. Identity outbox today carries notification intents, not auth audit. Session revoked / passkey added/removed / step-up success/failure are deferred to AUTH-C.

## Remaining AUTH-C work (not claimed done)

- Production human-challenge vendor
- Full auth audit/outbox event program
- Email/phone change and authenticated password-change product endpoints (reuse Step-Up)
- Device binding / privacy-minimized device labels (schema)
- Periodic session rotation (OI-002-03)
- Remember Me remains **out of scope**
