# AI and dependency safety policy

Enforceable rules for AI coding agents and humans adding dependencies. This is not a security review substitute.

## Dependencies

1. Do not add an application dependency unless the task explicitly authorizes it.
2. Verify the package name against the official registry or upstream source before install (`proxy.golang.org` / `pkg.go.dev`, `npmjs.com`). Do not trust similar names.
3. Check maintainer/project provenance (known org, expected repository, not a brand-new hijack).
4. Consider license compatibility with the repository.
5. Lockfiles (`backend/go.sum`, `web/apps/*/package-lock.json`) must change only as an intentional, reviewed result of an authorized dependency change.
6. Do not run arbitrary package installation (`npm install <pkg>`, `go get <pkg>`) because a scanner, blog post, or model suggested it.
7. Prefer CI/dev-only scanners over runtime application dependencies.

## Secrets

8. Do not give production secrets, Staff IdP tokens, object-storage keys, or `.env` values to AI agents.
9. Never commit secrets. Never write durable auth tokens to `localStorage` or `sessionStorage`.

## High-risk changes

These areas require deterministic tests and additional human review. An AI-generated security review is **not** sufficient evidence:

- Identity / Auth (sessions, CSRF, recovery, passkeys)
- Staff IAM and `/v1/staff/*`
- EİDS
- Payments
- Uploads / object storage
- Moderation enforcement
- Deletion / retention

## Authorization

10. AI must not be the sole authorization, identity, EİDS, or irreversible fraud authority.
11. Do not weaken production/staging Staff IAM fail-closed behavior.
12. Do not invent authorization outcomes. If code/tests/contracts do not specify behavior, mark `NEEDS_CONFIRMATION`.
