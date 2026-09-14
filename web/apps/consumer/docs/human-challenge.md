# Consumer HumanChallenge (Turnstile)

Cloudflare Turnstile widget for `web/apps/consumer` auth only. This is **not** Step-Up, EİDS, Trust / Güven Pasaportu, or staff authentication.

## Public config

Frontend may receive only the public sitekey:

`NEXT_PUBLIC_TURNSTILE_SITEKEY`

Never put `IDENTITY_HUMAN_CHALLENGE_TURNSTILE_SECRET` (or any Siteverify secret) in the consumer app or in `NEXT_PUBLIC_*`.

Operator-owned challenged operations (must match backend `IDENTITY_HUMAN_CHALLENGE_OPERATIONS` values that this app actually calls):

`NEXT_PUBLIC_TURNSTILE_OPERATIONS`

Unknown tokens are ignored. The user cannot type a Turnstile action.

## Rendering

The official explicit-render script is loaded once:

`https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit`

`turnstile.render` / `reset` / `remove` wrap a local React component. No third-party Turnstile package.

Widget `action` equals the Identity `AuthOperation` (`password_login`, `signup_start`, …).

## Token lifecycle

The token lives in React/memory for **one** operation. It is sent as existing auth JSON `challengeToken`, then cleared. It is never written to `localStorage`, `sessionStorage`, the URL, analytics, or logs.

Expired / timeout / error callbacks clear the token and require a fresh solve. Replay needs a new widget token.

## Challenge activation

Show the widget when a public sitekey is set **and** either:

- the operation is listed in `NEXT_PUBLIC_TURNSTILE_OPERATIONS`, or
- the backend returns `{ "error": "challenge_required" }` for **that** attempted operation.

A generic HTTP `403` / `forbidden` must not activate Turnstile (CSRF, origin, Step-Up, and failed/invalid tokens stay `forbidden`).

Empty sitekey: current auth UX, no widget.

## Test sitekey

Cloudflare visible always-pass test sitekey (public/test-only): `1x00000000000000000000AA`.

Production sitekey/secret provisioning remains operator-owned. Matching backend test secret is required for live Siteverify; without it, live E2E is pending.
