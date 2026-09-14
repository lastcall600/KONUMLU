# Türkiye Compliance Gateway (operator)

Germany main platform and this gateway are **separate runtimes** ([ADR-015](../ADR/ADR-015-tr-compliance-gateway-data-residency.md)). This runbook does not place official EİDS credentials in git.

## Processes

| Process | Role |
|---|---|
| `cmd/server` (Germany) | Product API. Verifies Ed25519 decisions. Stores opaque `subject_ref` + replay metadata only. |
| `cmd/trgateway` (Türkiye) | Signing runtime + future official adapter host. **Does not** mint decisions from a public HTTP body. |
| `cmd/worker` | Unchanged marketplace/outbox work. Does not call official government APIs. |

Image: `docker build -f backend/Dockerfile --build-arg COMMAND=trgateway backend`

## Germany env (names only)

```
TR_COMPLIANCE_INGRESS_ENABLED
TR_COMPLIANCE_INGRESS_TOKEN
TR_COMPLIANCE_TRUSTED_PUBLIC_KEYS
TR_COMPLIANCE_AUDIENCE
TR_COMPLIANCE_MAX_CLOCK_SKEW
TR_COMPLIANCE_MAX_INGRESS_AGE
```

Never set `TR_COMPLIANCE_SIGNING_PRIVATE_KEY` on Germany. Process start fails closed if it is present.

When ingress is enabled in staging/production, token, trusted public keys, audience, and time policy are required.

## Türkiye env (names only)

```
TR_COMPLIANCE_SIGNER_ENABLED
TR_COMPLIANCE_SIGNING_KEY_ID
TR_COMPLIANCE_SIGNING_PRIVATE_KEY
TR_COMPLIANCE_GERMANY_INGRESS_URL
TR_COMPLIANCE_INGRESS_TOKEN
```

Private key: runtime secret or file injected at deploy. Production custody / key ceremony is an operator-security task (no HSM/KMS in this package). Prefer mTLS at the proxy in front of `POST /internal/tr-compliance/v1/verification-decisions`.

HTTPS is required for the Germany ingress URL except local loopback.

## Health

Germany `/healthz` and `/readyz` do **not** depend on the TR gateway. TR `/healthz` does not depend on Germany PostgreSQL.

If TR is down: marketplace stays up; new EİDS verification stays unavailable; never auto-approved.

## Durable delivery

Germany idempotency allows TR to retry POSTs. This package does **not** implement a TR outbox. Operators must not assume exactly-once network delivery.

## Backups

TR database backups stay in Türkiye. See [BACKUP-RESTORE.md](./BACKUP-RESTORE.md) and ADR-015. Do not dump TR sensitive tables into Germany object storage.

## Next

Wire the official EİDS/e-Devlet adapter on TR only. Do not add client `approved=true` APIs.
