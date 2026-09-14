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

**Temporary policy actions:** Tighten `IDENTITY_AUTH_*` windows via config/restart; enable HumanChallenge operations **only** with Turnstile (`IDENTITY_HUMAN_CHALLENGE_PROVIDER=turnstile`) or leave `unconfigured` (fail-closed). `fake` is rejected in staging/production. Scale Valkey/API as needed.

**What NOT to do:** Do not disable Valkey abuse limits. Do not fail-open the limiter. Do not add Remember Me, JWT sessions, or browser fingerprinting. Do not treat these events as Trust.

## HumanChallenge provider outage

**Expected fail policy:** When an operation is in `IDENTITY_HUMAN_CHALLENGE_OPERATIONS`, missing token → `403` `challenge_required`. Invalid/timeout/wrong-action/wrong-host/replay → request fails closed (`403` `forbidden` or `503` `unavailable`). Token omission is not success. Siteverify is one bounded attempt; do not retry spent tokens. V1 does not send `remoteip`. Generic `forbidden` (CSRF, origin, Step-Up) is not a challenge signal.

**Temporary response:** Leave provider `unconfigured` (fail-closed) or remove operations if challenge must not block login **only** with an explicit operator decision. `fake` is rejected in staging/production.

**Recovery:** Restore Turnstile Siteverify (official endpoint); confirm hostname allowlist and server-owned action binding; hashed replay keys in Valkey may require extra solves after flush (never privilege gain). Production sitekey/secret live in the hosting secret store, not git.

Turnstile production widget credentials remain **launch blockers**. The consumer widget exists (`NEXT_PUBLIC_TURNSTILE_SITEKEY` public-only; explicit render; memory-only `challengeToken`). Netgsm SMS adapter exists; production Netgsm credentials, approved sender header, SMS credit, and OTP package remain operator steps (`LIVE_NETGSM_TEST_PENDING`). This is not the Türkiye Compliance Gateway.

## Email/SMS outage

**User impact:** Signup/reset HTTP still returns a generic `challengeId` (anti-enumeration). Mail is sent only after SES accepts the message; HTTP never claims `sent`. Users will not receive a code while SES is down, in sandbox against an unverified recipient, or while email mode is `disabled`.

**Retry/degraded behavior:** `NOTIFICATIONS_EMAIL_MODE` / `NOTIFICATIONS_SMS_MODE` = `disabled` leaves senders nil; worker retries and never completes a successful no-op. `external` email requires `EMAIL_PROVIDER=ses` plus `EMAIL_SES_REGION` and `EMAIL_SES_FROM` at config load, and a constructed SES adapter at worker start. `external` SMS requires `SMS_PROVIDER=netgsm` plus `NETGSM_USERNAME`, `NETGSM_PASSWORD`, and `NETGSM_MSGHEADER` at config load, and a constructed Netgsm adapter at worker start.

**SES (transactional email):** API v2 `SendEmail`. Server-owned From only. AWS default credential chain (environment / task role / instance role / workload identity). Access keys are never committed or logged. SES sandbox is region-specific; the sending domain/identity must be verified in that region; production access out of sandbox is an AWS account request, not application truth. DKIM/domain authentication is configured in SES/DNS, not in this repo. SendEmail success means SES accepted/queued the message — it is **not** end-user delivery. SES does not provide exactly-once on this path; the existing dispatcher/outbox remain at-least-once on ambiguous failures and own retry/backoff. Permanent SES errors are not retried.

**Netgsm (transactional SMS + Identity OTP):** REST v2. HTTP Basic Auth. Stdlib `net/http` (no SDK). Transactional path `POST /sms/rest/v2/send`. OTP path `POST /sms/rest/v2/otp` (Identity already generated the code; adapter only transports). Server-owned `msgheader`. Username/password never logged or committed. Successful `code=00` means Netgsm accepted/queued the message — **not** handset delivery. `jobid` is an opaque string on internal `ProviderRef` only. No adapter retry loop; dispatcher/outbox own backoff. Ambiguous timeout is at-least-once. OTP package missing (`60`) is permanent/config, not a fallback to `/send`. Do not invent `iysfilter`/marketing policy here.

**Truthful messaging:** API bodies must not include `sent` / `delivered`. Client copy should say the message will arrive if the channel is available — not that it was sent.

SES production credentials, verified domain, DKIM, and sandbox exit remain operator steps (`LIVE_SES_TEST_PENDING` until a verified test destination is used). Netgsm production credentials, approved header, credit, OTP package, and a safe test destination remain operator steps (`LIVE_NETGSM_TEST_PENDING`).

## Valkey outage

**Session behavior:** Cookie resolve rehydrates from PostgreSQL (fail-open cache).

**Abuse behavior:** Auth rate limits and verification issuance fail closed (`unavailable`).

**Step-Up / first-passkey bootstrap:** Elevation and bootstrap grants fail closed. Loss/restart requires stepping up / re-auth again. Never grants extra privilege.

## Account takeover report

1. Revoke the current session and/or `POST /v1/auth/sessions/revoke-others`; password reset completion revokes **all** sessions and bumps `session_epoch`.
2. User completes password reset (no auto-login) and reviews passkeys (`GET /v1/auth/passkeys`). Last-factor remove is `409`.
3. Collect audit evidence from `platform.outbox_events` (`identity.auth.security` v1) and `auth_security_event` logs: login success/fail, logout, revoke, passkey add/remove, step-up, rate-limit, challenge. Payloads have user/session management ids only — no cookies, passwords, OTP, or identifiers.

## Secret compromise

**Provider secret rotation:** Rotate Turnstile secret / email / SMS vendor secrets in the hosting secret store. Restart API/worker. Never log or commit production Turnstile secrets. The public sitekey may be placed in frontend config (`NEXT_PUBLIC_TURNSTILE_SITEKEY`). Align `NEXT_PUBLIC_TURNSTILE_OPERATIONS` with `IDENTITY_HUMAN_CHALLENGE_OPERATIONS`.

**Session/key considerations:** Rotate verification material keyring with overlap (existing sealed challenges). Session cookies remain hashed at rest; mass revoke via password reset or epoch bump if a session-signing/hash assumption is broken (tokens are random opaque secrets, not JWTs).

**Incident notes:** Record `request_id` / `trace_id` from events. Do not paste cookies, OTP, or destination addresses into tickets.
