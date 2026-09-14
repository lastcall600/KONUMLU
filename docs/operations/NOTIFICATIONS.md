# Notification operations (NOTIFY-A / NOTIFY-B / NOTIFY-C / NOTIFY-D)

This is not legal advice. Transactional email uses Amazon SES when `NOTIFICATIONS_EMAIL_MODE=external` and `EMAIL_PROVIDER=ses`. Transactional SMS and Identity OTP SMS use Netgsm when `NOTIFICATIONS_SMS_MODE=external` and `SMS_PROVIDER=netgsm`. Push transports are optional per environment: Web Push (VAPID), FCM HTTP v1, and/or APNs token auth. Durable push **endpoint registration** exists (`000053`). Accept ≠ displayed. Live provider credentials are not committed (`LIVE_WEBPUSH_TEST_PENDING`, `LIVE_FCM_TEST_PENDING`, `LIVE_APNS_TEST_PENDING`, `LIVE_NETGSM_TEST_PENDING`).

## Processes

`cmd/worker` runs:

1. Platform outbox `RunWorkers` (domain events, AUTH-C, verification intents, moderation warnings, marketplace producers → `Materialize`)
2. Notification `Dispatcher.Run` (claims `notifications.channel_deliveries` for configured senders only)

Shutdown cancels both loops. Do not add another daemon.

## Unconfigured providers

When email mode is `disabled`, dispatcher email sender is **nil**. The dispatcher does **not** claim pending email rows and does **not** mark `accepted`. It logs `notification_dispatch_unconfigured` once per process and waits on the outbox poll interval. No hot loop. No fake success.

When email mode is `external` with SES wired, the dispatcher claims email `channel_deliveries` only after preference/consent/account-state/JIT destination/suppression re-check. SES is transport only.

When SMS mode is `external` with Netgsm wired, the dispatcher claims SMS `channel_deliveries` on the same re-check path and calls REST v2 `/sms/rest/v2/send`. Identity verification OTP uses the legacy `notifications.intent` + `DeliveryService` path against REST v2 `/sms/rest/v2/otp`. Do not mix OTP into `notifications.intents.variables`. Do not fall back from `/otp` to `/send`.

Verification OTP still uses the legacy `notifications.intent` + `DeliveryService` path (`disabled` never sent; `external` uses the same Netgsm transport for phone and SES for email).

## SES email

- Provider: Amazon SES, API v2 `SendEmail`
- Accept means queued by SES, not mailbox delivery
- No provider-level exactly-once claim; existing dispatcher backoff is authoritative
- Credentials: AWS default chain; never committed or logged
- Operator prerequisites: verified sending domain/identity in the SES region; region-specific sandbox; production access request; DKIM/domain authentication in SES/DNS

## Netgsm SMS

- Provider: Netgsm. REST v2. HTTP Basic Authentication. No SDK.
- Transactional notifications: `POST https://api.netgsm.com.tr/sms/rest/v2/send`
- Identity OTP: `POST https://api.netgsm.com.tr/sms/rest/v2/otp` (requires the Netgsm OTP package on the account)
- `msgheader` is server-owned (`NETGSM_MSGHEADER`). Callers cannot supply a sender header.
- Credentials (`NETGSM_USERNAME` / `NETGSM_PASSWORD`) are server-only: never logged, never returned, never committed.
- Accept means Netgsm queued the message, **not** handset delivery. Delivery reporting is not in this package.
- `jobid` is an opaque string on internal `ProviderRef` only. Do not parse as int/int64. Do not expose publicly.
- One HTTP call per adapter invocation. No provider-internal retry loop. Dispatcher/outbox own retry/backoff.
- Ambiguous network timeout after the provider may have accepted is retryable: external SMS is **at-least-once**. `jobid` is not an idempotency key. Provider code `85` is a rate/duplicate threshold, not application exactly-once.
- This package is not marketing SMS. `iysfilter` is omitted; commercial/İYS policy is not invented here.
- Operator prerequisites: Netgsm account, API user/password, API permission, approved sender header, SMS credit, OTP package for Identity OTP, optional API IP restriction, safe test destination.

## Claim / retry

- PostgreSQL `FOR UPDATE SKIP LOCKED`, bounded batch
- Claim `pending` / `retryable_failed` when `next_attempt_at` is due
- Reclaim `processing` when `updated_at` is older than the processing hold (`OUTBOX_LEASE`)
- Adapter classes: retryable / timeout / permanent / unconfigured
- Backoff capped (8 attempts / 300s)

## Destinations

Resolved just-in-time from Identity. Never stored on notification rows. Never logged.

## Push

Durable table `notifications.push_endpoints` (000053). Encrypted at rest (AES-256-GCM). HMAC-SHA256 uniqueness. Consumer HTTP:

- `POST /v1/push-endpoints` (CSRF + Origin)
- `GET /v1/push-endpoints`
- `DELETE /v1/push-endpoints/{endpointId}` (CSRF + Origin; soft `revoked_at`)

Keys: `PUSH_ENDPOINT_ENCRYPTION_KEY`, `PUSH_ENDPOINT_HASH_KEY` (required in staging/production). Development: omit both to leave registration HTTP unwired; set both to enable. V1 has one encryption key (`endpoint_key_id` = `v1`). Key rotation / previous-key decrypt is **not** implemented.

HMAC uniqueness is `v1|channel|platform|provider|canonical_endpoint_identity` (web = endpoint URL; mobile = opaque token). p256dh/auth live only in ciphertext.

Logout / session revoke does **not** revoke endpoints (V1; no session↔device binding).

Worker decrypts material JIT per send and never persists plaintext. Dispatcher is the only retry authority. Adapters perform one HTTP call.

### Web Push (VAPID)

Enable by setting all of `WEBPUSH_VAPID_PUBLIC_KEY`, `WEBPUSH_VAPID_PRIVATE_KEY`, `WEBPUSH_VAPID_SUBJECT` (`mailto:` or `https:`). Optional `WEBPUSH_TIMEOUT` (default 5s, max 30s). Library: `github.com/SherClockHolmes/webpush-go`. 201/200 = accepted. 404/410 = revoke endpoint. 429/5xx/timeout = retryable. Do not log private key, endpoint URL, p256dh, or auth.

### FCM HTTP v1

Enable with `FCM_PROJECT_ID`. Optional `FCM_CREDENTIALS_FILE` (service-account JSON path; never committed). Otherwise Google application default / workload identity. Optional `FCM_TIMEOUT`. POST `https://fcm.googleapis.com/v1/projects/{id}/messages:send` with OAuth2 Bearer. Access tokens and device tokens are never logged. `UNREGISTERED` and token-authoritative `INVALID_ARGUMENT` revoke the endpoint. 429/5xx/UNAVAILABLE/QUOTA_EXCEEDED = retryable. Auth/config failures are permanent.

### APNs token authentication

Enable with `APNS_TEAM_ID`, `APNS_KEY_ID`, `APNS_TOPIC`, `APNS_ENVIRONMENT` (`sandbox` or `production`; never inferred from the device token), and `APNS_PRIVATE_KEY` or `APNS_PRIVATE_KEY_FILE` (.p8). Optional `APNS_TIMEOUT`. ES256 JWT is cached ~50 minutes. HTTP/2 POST `/3/device/{token}` with server-owned `apns-topic`. 200 = accepted. `BadDeviceToken` / `Unregistered` / `DeviceTokenNotForTopic` / HTTP 410 revoke. 429/5xx/Shutdown = retryable. Do not log device token, signing key, JWT, or Authorization.

### Fanout

One `channel_deliveries` row per channel. Independent endpoint attempts. Channel accepted if any configured endpoint is accepted. Invalid endpoints revoked. Retryable failures are not hidden when nothing succeeded. No per-endpoint delivery migration.

Never log tokens, Web Push URLs, p256dh, auth, ciphertext, keys, or provider JWTs.

Frontend: `WEB_PUSH_FRONTEND_PENDING` (no consumer service worker in this package). Mobile: `MOBILE_PUSH_CLIENT_PENDING`.

## What not to do

- Do not expose `POST /send-notification`
- Do not cut over moderation warnings without a double-notify review
- Do not invent İYS consent inside the Netgsm adapter
- Do not generate OTP inside the Netgsm adapter
- Do not store push tokens plaintext
- Do not treat endpoint registration as a successful send
