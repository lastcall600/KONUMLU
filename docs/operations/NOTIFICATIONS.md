# Notification operations (NOTIFY-A / NOTIFY-B)

This is not legal advice. Transactional email uses Amazon SES when `NOTIFICATIONS_EMAIL_MODE=external` and `EMAIL_PROVIDER=ses`. SMS/push vendors are **not selected**. Do not treat SMS/push dispatch as production-ready.

## Processes

`cmd/worker` runs:

1. Platform outbox `RunWorkers` (domain events, AUTH-C, verification intents, moderation warnings, marketplace producers → `Materialize`)
2. Notification `Dispatcher.Run` (claims `notifications.channel_deliveries` for configured senders only)

Shutdown cancels both loops. Do not add another daemon.

## Unconfigured providers

When email mode is `disabled`, dispatcher email sender is **nil**. The dispatcher does **not** claim pending email rows and does **not** mark `accepted`. It logs `notification_dispatch_unconfigured` once per process and waits on the outbox poll interval. No hot loop. No fake success.

When email mode is `external` with SES wired, the dispatcher claims email `channel_deliveries` only after preference/consent/account-state/JIT destination/suppression re-check. SES is transport only.

Verification OTP still uses the legacy `notifications.intent` + `DeliveryService` path (`disabled` never sent; `external` uses the same SES transport). Do not mix OTP into `notifications.intents.variables`.

## SES email

- Provider: Amazon SES, API v2 `SendEmail`
- Accept means queued by SES, not mailbox delivery
- No provider-level exactly-once claim; existing dispatcher backoff is authoritative
- Credentials: AWS default chain; never committed or logged
- Operator prerequisites: verified sending domain/identity in the SES region; region-specific sandbox; production access request; DKIM/domain authentication in SES/DNS

## Claim / retry

- PostgreSQL `FOR UPDATE SKIP LOCKED`, bounded batch
- Claim `pending` / `retryable_failed` when `next_attempt_at` is due
- Reclaim `processing` when `updated_at` is older than the processing hold (`OUTBOX_LEASE`)
- Adapter classes: retryable / timeout / permanent / unconfigured
- Backoff capped (8 attempts / 300s)

## Destinations

Resolved just-in-time from Identity. Never stored on notification rows. Never logged.

## Push

No registration HTTP. No endpoint table. Eligible push channels are suppressed `channel_unavailable`.

## What not to do

- Do not add Firebase/APNs/WebPush/Twilio SDKs without an approved dependency and vendor decision
- Do not create `000053` without a reviewed schema proposal
- Do not expose `POST /send-notification`
- Do not cut over moderation warnings without a double-notify review
