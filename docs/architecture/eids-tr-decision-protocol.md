# EİDS signed verification decision protocol (0C-EIDS-A)

Implements ADR-015’s Germany-facing **minimal signed verification decision**. Official EİDS/e-Devlet adapters are **not** in this package.

## Boundary

KONUMLU product (`cmd/server`) stays in Germany. Sensitive government I/O stays on the Türkiye Compliance Gateway (`cmd/trgateway`).

Germany **must not** receive: TCKN, birth date, address, raw EİDS/e-Devlet responses, government/provider tokens, raw identity payloads, or full provider transaction payloads.

## Protocol

Package: `backend/internal/eids/trdecision`

V1 claims (typed; never client JSON authority):

- `schema_version` = `tr-decision-v1`
- `verification_type` = `property` | `vehicle` (server-controlled)
- `status` = `approved` | `rejected`
- `decision_id` cryptographically random opaque id
- `subject_ref` 32-byte hex (Germany-owned mapping; not TCKN/email/phone/user UUID)
- `issued_at` / `valid_until` UTC
- `audience` = `konumlu-germany-eids-v1`

Signing: **Ed25519** (`crypto/ed25519` only). Canonical bytes are `ClaimsV1.SigningBytes(key_id)` (fixed field order, `key_id` bound). Algorithm is not negotiable. No `alg` field.

Envelope: `{ key_id, claims, signature(base64url) }`.

## Keys

- **TR:** one active signing private key (`TR_COMPLIANCE_SIGNING_KEY_ID`, `TR_COMPLIANCE_SIGNING_PRIVATE_KEY`). Runtime/file only. Never in Germany config, git, logs, or DB.
- **Germany:** trusted public-key ring `TR_COMPLIANCE_TRUSTED_PUBLIC_KEYS` (`key_id:base64url,...`) so rotation can overlap old+new public keys.
- HSM/KMS and key ceremony are operator tasks, not this package.

## Germany ingress

`POST /internal/tr-compliance/v1/verification-decisions`

Not a consumer route. App-level `Authorization: Bearer <TR_COMPLIANCE_INGRESS_TOKEN>` (constant-time compare of SHA-256 digests). Consumer cookies and Staff IAM bearers cannot authorize this route.

Production hardening: TLS at the load balancer; **mTLS preferred at the reverse-proxy/service boundary**. This process does not fake mTLS after TLS is terminated.

Fail-closed verification order: service auth → body bounds → schema → trusted `key_id` → Ed25519 → audience → type/status → ids → subject mapping → time policy → durable replay → then apply EİDS state.

## Time policy

- Clock skew default **2 minutes** (`TR_COMPLIANCE_MAX_CLOCK_SKEW`)
- Maximum **ingress** age default **10 minutes** (`TR_COMPLIANCE_MAX_INGRESS_AGE`)

Ingress freshness is **not** business `valid_until`. A listing verification may remain product-valid longer than the signed envelope’s transport window. An envelope past `valid_until` at verify time is still rejected.

## Replay / idempotency

PostgreSQL tables (`000054_eids_tr_signed_decisions`):

- `eids.subject_refs` — opaque `subject_ref` → `verification_id` + listing + type
- `eids.tr_signed_decisions` — `decision_id` PK, claims hash, coarse status, timestamps, `key_id`

Same `decision_id` + same canonical hash → idempotent success (no second apply).  
Same `decision_id` + different claims → **409** conflict; original row is never overwritten.  
Do not use Valkey as replay truth.

`subject_ref` is unique per Germany verification attempt (`eids.verifications` row). It is not a reusable user id. A later attempt gets a new verification row and a new `subject_ref`.

Multiple distinct `decision_id` values may still target the same `subject_ref`. Application is **monotonic on signed `issued_at`**, not arrival time:

- newer `issued_at` may supersede older applied verification state on that attempt
- older `issued_at` arriving later is inserted into `eids.tr_signed_decisions` for replay/audit and **must not** overwrite newer applied state
- equal `issued_at` with a different `decision_id` is **409**; no verification change and no extra replay row (no protocol tie-break)

Application of a state-changing replay row + `eids.verifications` status is one PostgreSQL transaction. A stale (older) envelope inserts the replay row only.

## TR issuer and delivery

Issuer (`trdecision.Issuer`) accepts only an internal `ProviderResult` (subject_ref, type, approved/rejected, valid_until). No HTTP route mints this from a browser.

Delivery client (`internal/infrastructure/eids.DeliveryClient`) POSTs the envelope to Germany with the service token. Timeouts are not treated as Germany approval. **Durable TR outbound outbox is not implemented** — at-least-once network retries are safe because Germany is idempotent. Exactly-once network delivery is not claimed.

Official EİDS provider adapter remains a later package.

## Degraded mode (ADR-015)

TR outage: Germany `/healthz` and `/readyz` stay up (they do not probe TR). New official verification is unavailable. Unconfigured gateway still never returns verified. No fail-open.

## Logging

Safe: `request_id`, `decision_id`, `verification_type`, decision status, `key_id`, result class, latency, HTTP status.

Never: `subject_ref`, signature, service token, key material, raw body, TCKN/provider data.
