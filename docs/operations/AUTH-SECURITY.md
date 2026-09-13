# Auth security operations runbook

Identity-owned consumer auth operations. This is **not** Trust / Güven Pasaportu and **not** Staff IAM.

No production credentials belong in this file.

AUTH-A (abuse) and AUTH-B (session/step-up) remain in force. AUTH-C adds durable `identity.auth.security` outbox events and explicit provider launch blockers.

## Fail-open / fail-closed

| Dependency | Session cache | Abuse / issuance limits | Step-Up / bootstrap | Required HumanChallenge | Email/SMS delivery |
|---|---|---|---|---|---|
| Valkey down | Fail **open** to PostgreSQL | Fail **closed** (`503` / `unavailable`) | Fail **closed** (no elevation) | Fail **closed** | n/a |
| PostgreSQL down | Requests fail | n/a (durable truth) | n/a | n/a | Intents not written |
| HumanChallenge vendor down / unconfigured | n/a | Unrelated auth continues if challenge not required | n/a | Fail **closed** | n/a |
| Email/SMS adapter missing | n/a | n/a | n/a | n/a | Retryable failure; **never** marked sent |

## Credential stuffing spike

**Symptoms:** Elevated `auth.rate_limit.triggered` events; HTTP `429` `rate_limited` on `/v1/auth/password/login` and passkey begin/finish; `Retry-After` present; identifier resolve / Argon2id verify volume drops after limits.

**Metrics/logs:** `auth_abuse` (`reason_code` `velocity_ip` / `velocity_account` / `velocity_target`); `auth_security_event` with `event_type=auth.rate_limit.triggered`; Valkey `identity:auth:rl:*` counters. No raw email/phone in keys or logs.

**Temporary policy actions:** Tighten `IDENTITY_AUTH_*` windows via config/restart; enable HumanChallenge operations **only** with a real server-side verifier (not `fake` in staging/production). Scale Valkey/API as needed.

**What NOT to do:** Do not disable Valkey abuse limits. Do not fail-open the limiter. Do not add Remember Me, JWT sessions, or browser fingerprinting. Do not treat these events as Trust.

## HumanChallenge provider outage

**Expected fail policy:** When an operation is in `IDENTITY_HUMAN_CHALLENGE_OPERATIONS`, missing/invalid/timeout/wrong-action/wrong-host/replay → request fails closed (`403`/`503`). Token omission is not success.

**Temporary response:** Leave provider `unconfigured` (fail-closed) or remove operations if challenge must not block login **only** with an explicit operator decision. `fake` is rejected in staging/production.

**Recovery:** Restore vendor adapter (not selected in AUTH-C); confirm hostname/action binding; hashed replay keys in Valkey may require extra solves after flush (never privilege gain).

Vendor onboarding remains a **launch blocker**.

## Email/SMS outage

**User impact:** Signup/reset HTTP still returns a generic `challengeId` (anti-enumeration). Mail/SMS is **not** claimed as sent. Users will not receive a code until a vendor adapter is wired and healthy.

**Retry/degraded behavior:** `NOTIFICATIONS_EMAIL_MODE` / `NOTIFICATIONS_SMS_MODE` = `disabled` leaves senders nil; worker retries and never completes a successful no-op. `external` without an adapter **fails worker start**.

**Truthful messaging:** API bodies must not include `sent` / `delivered`. Client copy should say the message will arrive if the channel is available — not that it was sent.

Email and SMS vendors remain **launch blockers**.

## Valkey outage

**Session behavior:** Cookie resolve rehydrates from PostgreSQL (fail-open cache).

**Abuse behavior:** Auth rate limits and verification issuance fail closed (`unavailable`).

**Step-Up / first-passkey bootstrap:** Elevation and bootstrap grants fail closed. Loss/restart requires stepping up / re-auth again. Never grants extra privilege.

## Account takeover report

1. Revoke the current session and/or `POST /v1/auth/sessions/revoke-others`; password reset completion revokes **all** sessions and bumps `session_epoch`.
2. User completes password reset (no auto-login) and reviews passkeys (`GET /v1/auth/passkeys`). Last-factor remove is `409`.
3. Collect audit evidence from `platform.outbox_events` (`identity.auth.security` v1) and `auth_security_event` logs: login success/fail, logout, revoke, passkey add/remove, step-up, rate-limit, challenge. Payloads have user/session management ids only — no cookies, passwords, OTP, or identifiers.

## Secret compromise

**Provider secret rotation:** Rotate HumanChallenge / email / SMS vendor secrets in the hosting secret store. Restart API/worker. AUTH-C does not store those secrets in Identity config.

**Session/key considerations:** Rotate verification material keyring with overlap (existing sealed challenges). Session cookies remain hashed at rest; mass revoke via password reset or epoch bump if a session-signing/hash assumption is broken (tokens are random opaque secrets, not JWTs).

**Incident notes:** Record `request_id` / `trace_id` from events. Do not paste cookies, OTP, or destination addresses into tickets.
