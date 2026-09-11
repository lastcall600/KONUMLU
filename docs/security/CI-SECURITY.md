# CI security controls

Scanner jobs, failure policy, and supply-chain notes for the production security baseline. Jobs live in `.github/workflows/ci.yml`.

## Jobs

| Job | What runs | CI failure |
|---|---|---|
| `dependency-security` | Official `govulncheck` v1.1.4 on `backend/` (reachable vulns). `npm audit --package-lock-only` on consumer and admin lockfiles | **Go:** any *new* reachable finding not listed in `.github/security/govulncheck-known.txt`. Known 0C-07 findings require authorized upgrades (not auto-patched). **npm:** `high` or `critical` (`--audit-level=high`). `moderate`/`low` are reported, not failed |
| `secret-scan` | Official Gitleaks CLI (`v8.30.1`), git history (`fetch-depth: 0`), `--redact` | Any finding. Ephemeral probe (not committed) must detect; repository scan must be clean |
| `authorization-security` | Targeted Go authorization / Staff IAM / fail-closed tests | Any test failure |
| `container-security` | Builds the same server/worker images as `backend-container`, scans with official Trivy CLI (`v0.74.0`) | **New CRITICAL** with an available fix (`--severity CRITICAL --ignore-unfixed --exit-code 1`). CVEs in `.trivyignore` are deferred 0C-07 findings (pgx, same as GO-2026-5004). HIGH/MEDIUM/LOW and unfixed issues are reported as artifacts, not failed |
| `sbom` | Official Syft CLI (`v1.51.1`) CycloneDX JSON for backend, consumer web, admin web | Generation failure only. Artifacts are uploaded, not committed |

Existing jobs (`backend`, `consumer-web`, `admin-web`, `migration-smoke`, `backend-container`) are unchanged in purpose.

## Tooling provenance

| Tool | Upstream | How it is pinned |
|---|---|---|
| govulncheck | `golang.org/x/vuln/cmd/govulncheck` (Go project) | module version `v1.1.4` via `go install` |
| npm audit | Node.js / npm CLI | runner Node 20; lockfiles only |
| Gitleaks | `gitleaks/gitleaks` GitHub release | version + SHA-256 of `linux_x64` archive |
| Trivy | `aquasecurity/trivy` GitHub release | version + SHA-256 of `Linux-64bit` archive |
| Syft | `anchore/syft` GitHub release | version + SHA-256 of `linux_amd64` archive |
| GitHub Actions (`checkout`, `setup-go`, `setup-node`, `upload-artifact`) | `actions/*` | full commit SHAs |

No application `go.mod` / `package.json` dependency is added for these scanners.

## Findings handling

1. Classify: CRITICAL / HIGH / MEDIUM / LOW.
2. For CRITICAL/HIGH: identify component, reachable/relevant, whether a patched version exists.
3. Do **not** auto-upgrade dependencies in this baseline. Narrow, clearly safe patches require a separate authorized task.
4. Secret findings always fail CI. Do not allowlist real credentials.

## Known findings (0C-07 baseline)

Scanners executed. Dependency upgrades were **not** applied in this task.

| ID | Severity | Component | Reachable? | Patched version | Action |
|---|---|---|---|---|---|
| GO-2026-5970 | HIGH | `golang.org/x/text@v0.31.0` (via pgx pool unicode norm) | Yes (`db.OpenPool`) | `v0.39.0` | Follow-up upgrade (not auto-applied) |
| GO-2026-5004 | HIGH | `github.com/jackc/pgx/v5@v5.7.6` SQL placeholder / dollar-quote confusion | Yes (`db.Pool.Query`) | `v5.9.2` | Follow-up upgrade (not auto-applied) |
| CVE-2026-33815 | CRITICAL (Trivy) | `github.com/jackc/pgx/v5@v5.7.6` memory-safety | Yes (gobinary in server/worker images) | `5.9.0+` | Same pgx upgrade; listed in `.trivyignore` |
| CVE-2026-33816 | CRITICAL (Trivy) | `github.com/jackc/pgx/v5@v5.7.6` memory-safety | Yes (gobinary in server/worker images) | `5.9.0+` | Same pgx upgrade; listed in `.trivyignore` |

These Go IDs are listed in `.github/security/govulncheck-known.txt`. Matching Trivy CVEs are listed in `.trivyignore`. New reachable IDs / new fixed CRITICAL CVEs fail CI.

npm audit (consumer + admin lockfiles): 0 vulnerabilities at high/critical.

Container Alpine 3.21 OS layer: 0 CRITICAL/HIGH (fixed). Gobinary HIGH (stdlib / `golang.org/x/net` toolchain, ~30 findings) is reported as artifacts and does **not** fail this baseline; remediation is a Go toolchain upgrade follow-up, not an auto-bump.

Container HIGH/MEDIUM/LOW (including unfixed) are reported as artifacts; they do not fail this baseline.

## Supply-chain observations

Pinned in this baseline:

- Workflow actions that previously used floating tags (`@v4`, `@v5`) now use commit SHAs.
- Scanner CLIs are downloaded from official GitHub releases and checksum-verified.

Follow-up (not done here — would change runtime images or action major versions):

- Pin `golang:1.24-bookworm` and `alpine:3.21` in `backend/Dockerfile` to digests.
- Evaluate moving existing jobs from Actions v4/v5 SHAs to current v7 majors (behavioral change).
- GitHub Advanced Security / Dependabot alerts (org setting, not workflow).
- Official Staff IdP adapter still unwired; production staff routes remain unregistered until a real adapter is authorized.

## Local equivalents

```text
go -C backend run ./cmd/archcheck
go -C backend test ./internal/staffauth/... ./internal/infrastructure/staffidp/...
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
govulncheck -C backend ./...
npm audit --prefix web/apps/consumer --package-lock-only --audit-level=high
npm audit --prefix web/apps/admin --package-lock-only --audit-level=high
```

Gitleaks / Trivy / Syft versions match CI pins. Reports must be redacted; do not commit generated SBOM or scan JSON.
