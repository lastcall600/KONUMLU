# PostgreSQL backup and restore

This runbook is for KONUMLU’s canonical store: PostgreSQL + PostGIS (D-004). It does not select a cloud vendor and does not put backup logic in application code.

Local compose (`docker-compose.yml`) is development-only. Production hosting implements the same PostgreSQL model with off-host storage.

## Two models (do not conflate)

| Model | What it proves | What it is not |
|---|---|---|
| **A. Logical backup** (`pg_dump` / `pg_restore`) | Schema + data can be restored into a **fresh** instance and used by `cmd/server` | Point-in-time recovery. Continuous WAL. Sub-dump-window RPO. |
| **B. Physical PITR** (`pg_basebackup` + WAL archive) | Restore to a recovery target time, then verify and cut over | Not executed against the KONUMLU application database in the local drill (WAL is not enabled on `docker-compose.yml`) |

The local application drill executed **A**. A small **B** capability proof used an isolated lab container on the same `postgis/postgis:16-3.5` image. Production must implement **B** on the real cluster.

## What is backed up (PostgreSQL)

- All KONUMLU domain schemas and tables in the target database
- PostGIS extension and geography/geometry data
- `schema_migrations` (golang-migrate version + dirty flag)
- Sequences, constraints, indexes owned by the dump

## What is NOT backed up by PostgreSQL backup

- S3 / MinIO / any object-storage media bytes
- External provider data (Staff IdP, EİDS, email/SMS, PSP)
- Secrets and config (`DATABASE_URL`, keyrings, object-storage keys)
- Valkey (cache / session hot cache / rate limits — derived only; D-005)
- Application binaries, images, and Git history

Media backup is a separate object-storage lifecycle. Do not treat a successful DB restore as a complete platform restore.

## Production backup strategy

Assumptions: single primary PostgreSQL/PostGIS (modular monolith; no invented replica topology). Hosting may add replicas later without changing application code.

1. **Periodic base backup** — `pg_basebackup` (or hosting equivalent) at least daily, stored **off-host**.
2. **WAL archiving** — `wal_level=replica`, `archive_mode=on`, `archive_command` (or a hosting WAL shipper) to **off-host** archive. `archive_timeout` sized so RPO is achievable (initial target 5–15 minutes).
3. **Logical dump (optional complement)** — `pg_dump -Fc` for portable restore tests and schema transport. Complements PITR; does not replace it.
4. **Offsite copy** — second location / account; backups are as sensitive as the live database.
5. **Encryption and access** — encrypt at rest in backup storage; restrict credentials to restore operators; never embed passwords in Git or this document.
6. **Retention (initial proposal, hosting decision)** — WAL + base backups: 7–14 days; monthly logical dump retained longer if required by policy. Confirm with business/legal.
7. **Restore testing** — restore into an **isolated** instance on a defined cadence (at least before production launch, then periodically). Never restore over the live primary as the first test.

## Proposed RPO / RTO (not guaranteed)

| Target | Initial proposal | Status |
|---|---|---|
| RPO | 5–15 minutes | Requires production WAL archive + verified restore; **business approval required** |
| RTO | ≤ 60 minutes | Isolated restore + migrate check + `/healthz` `/readyz` + smoke reads; **business approval required** |

Local drill timings (logical dump of development data) are recorded below. They are not production RTO.

## Restore prerequisites

- Compatible PostgreSQL **major** 16 and PostGIS **3.5** (same family as `postgis/postgis:16-3.5`)
- Isolated instance, **new volume**, not the live data directory
- Backup artifact + (for PITR) WAL archive covering the target time
- Operator credentials via environment or secret manager — not files in Git
- `cmd/migrate version` available from the same release as the restored schema
- Application config pointing at the **restored** DSN only for the drill process

## Step-by-step: local logical restore drill

Use development credentials from the local environment. Substitute user/database names from your env (`POSTGRES_USER`, `POSTGRES_DB`). Do not paste passwords into tickets.

### 1. Source

```text
docker compose up -d postgres
# wait until healthy
```

Confirm reachability, PostGIS, and migrate version:

```text
docker exec konumlu-postgres pg_isready -U konumlu -d konumlu
go -C backend run ./cmd/migrate version
```

`DATABASE_URL` must point at the **source** for this command.

Optional local-only dataset (safe to re-run):

```text
Get-Content scripts/ops/backup-restore/seed-local-drill.sql | docker exec -i konumlu-postgres psql -U konumlu -d konumlu -v ON_ERROR_STOP=1
```

### 2. Backup (logical)

```text
New-Item -ItemType Directory -Force var/backups | Out-Null
docker exec konumlu-postgres pg_dump -U konumlu -d konumlu -Fc --no-owner -f /tmp/konumlu.dump
docker cp konumlu-postgres:/tmp/konumlu.dump var/backups/konumlu-logical.dump
docker exec konumlu-postgres rm -f /tmp/konumlu.dump
```

`var/backups/` is gitignored. Treat dump files as sensitive.

### 3. Fresh target

```text
docker compose -f docker-compose.restore-drill.yml up -d
# host port 55432, volume konumlu_postgres_restore_drill — not konumlu_postgres_data
```

Target starts empty (initdb only). Do not restore onto `konumlu-postgres`.

### 4. Restore

The `postgis/postgis:16-3.5` image preloads PostGIS/`tiger` into `POSTGRES_DB`. Restoring a full dump **into that preloaded database** fails (`schema "tiger" already exists`). Create a truly empty database from `template0` and restore there. Do not drop the source database.

```text
docker exec konumlu-postgres-restore-target psql -U konumlu -d postgres -c "CREATE DATABASE konumlu_restored TEMPLATE template0 OWNER konumlu;"
docker cp var/backups/konumlu-logical.dump konumlu-postgres-restore-target:/tmp/konumlu.dump
docker exec konumlu-postgres-restore-target pg_restore -U konumlu -d konumlu_restored --no-owner --no-acl --exit-on-error /tmp/konumlu.dump
```

`--exit-on-error` is required. A `pg_restore` without it can hide failures. Then run SQL verification — **exit code of pg_restore alone is not PASS**. Application `DATABASE_URL` for the drill uses database `konumlu_restored` on port 55432.

### 5. Verify restored data

```text
Get-Content scripts/ops/backup-restore/verify-restored.sql | docker exec -i konumlu-postgres-restore-target psql -U konumlu -d konumlu_restored -v ON_ERROR_STOP=1
```

Must fail on empty/unrestored databases (missing `schema_migrations` or drill rows).

### 6. Application against restored DB

Start **one local** `cmd/server` with `DATABASE_URL` aimed at port `55432` (or the restore container network). Do not change production config files.

Required local env names are listed in `.env.example`. Provide values from your shell only.

```text
# HTTP_ADDR e.g. 127.0.0.1:18080 so the source-stack server is not overwritten
GET http://127.0.0.1:18080/healthz
GET http://127.0.0.1:18080/readyz
GET http://127.0.0.1:18080/v1/public/profiles/a1000001-0000-4000-8000-000000000011
GET http://127.0.0.1:18080/v1/public/listings/a1000002-0000-4000-8000-000000000001
```

Leave `VALKEY_URL` unset for a DB-only readiness proof, or point it at local Valkey if you need cache checks.

### 7. Migration compatibility

```text
# DATABASE_URL -> restored instance
go -C backend run ./cmd/migrate version
go -C backend run ./cmd/migrate up
go -C backend run ./cmd/migrate version
```

Expect clean version `51` (current repo head at the time of this drill) and `up` = no change / still clean.

## Step-by-step: production PITR (hosting)

Not wired in `docker-compose.yml`. Implement on the production cluster:

1. Enable WAL archive to off-host storage (`archive_mode`, `archive_command` or vendor WAL shipper).
2. Take periodic `pg_basebackup`; retain with WAL per policy.
3. To restore: provision isolated instance → restore base backup → configure `restore_command` and `recovery_target_time` (or LSN) → start → wait for consistent recovery → `promote`.
4. Run `cmd/migrate version` (must be clean, expected version).
5. Point a staging `cmd/server` at the recovered DSN; `/healthz` `/readyz`; smoke reads.
6. Cutover: pause writes if needed, catch remaining WAL, promote, switch application `DATABASE_URL` / DNS, confirm. Do not dual-write.
7. Keep the old primary offline until verification succeeds; rollback = switch DNS/DSN back to previous primary if it is still intact.

## Verification checklist

- [ ] Isolated target (different container, volume, port)
- [ ] `pg_restore --exit-on-error` completed
- [ ] PostGIS extension + `PostGIS_Version()`
- [ ] `schema_migrations` version expected and **not dirty**
- [ ] Domain schemas present
- [ ] Deterministic rows/counts/FKs match source
- [ ] `cmd/migrate version` clean; `up` idempotent
- [ ] `GET /healthz` = ok
- [ ] `GET /readyz` = ok against restored DB
- [ ] Application reads of restored rows succeed
- [ ] At least one negative check failed as designed (empty DB, missing row, or bad DSN)
- [ ] Backup files not in Git

## Rollback / cutover notes

- Logical restore is for a **new** cluster. It is not an in-place undo of live writes after the dump.
- PITR cutover is DNS/DSN switch after promote. Rollback is switching back if the old primary was not destroyed.
- Never restore over the only copy of production data as an experiment.

## Credentials / secrets

- Do not commit dump files, WAL, or `.env`.
- Do not put passwords in this document or in scripts.
- Production backup storage IAM/keys must be narrower than general deploy keys.
- Verification material keyring is **not** in PostgreSQL backup as operable secrets if you only stored ciphertext; the process still needs env-injected keys to boot.

## Local drill result

Recorded 2026-09-12 against **local development data only** (source `konumlu-postgres` was not overwritten).

| Item | Result |
|---|---|
| Source image | `postgis/postgis:16-3.5` container `konumlu-postgres` |
| Source DB | `konumlu` on host port 5432 |
| PostgreSQL | 16.9 (Debian 16.9-1.pgdg110+1) |
| PostGIS | 3.5.2 (`PostGIS_Version()` = `3.5 USE_GEOS=1 USE_PROJ=1 USE_STATS=1`) |
| Source WAL | `wal_level=replica`, `archive_mode=off` (no PITR on the app DB) |
| Migration version | **51**, not dirty (source and restored) |
| Backup method (app data) | `pg_dump -Fc --no-owner` (logical). **Not PITR.** |
| Backup artifact | `var/backups/konumlu-logical.dump` (~185 KB, gitignored) |
| Restore target | `konumlu-postgres-restore-target` port 55432, volume `konumlu_postgres_restore_drill`, database **`konumlu_restored`** created from `template0` |
| Backup duration | **734 ms** |
| Restore duration | **3878 ms** (`pg_restore --exit-on-error`) |
| Application verification duration | **191 ms** (`/healthz`, `/readyz`, public profile, public listing) |
| Source vs target | listings=5, users=3, cases=6, reports=30, listing_locations=5; drill listing checksum `de5e22f8b6690df0b2dd963ce634e361` matched |
| Failure tests | empty target verify failed (`schema_migrations` missing); unrestored `konumlu` on target failed the same check; missing title `THIS-RECORD-MUST-NOT-EXIST` failed; invalid DSN `GET /readyz` = 503 while `/healthz` = 200 |
| PITR lab (not app data) | Isolated `docker-compose.pitr-lab.yml`: base backup + WAL archive; restore stopped before post-backup insert; recovered table had id=1 only |

## Production gaps

- WAL archiving is **not** enabled on the development `postgres` service
- No off-host / offsite backup storage or encryption product selected (hosting decision)
- Retention numbers not legally approved
- Object-storage (media) backup/versioning not implemented in this sprint
- No automated production restore cadence or paging
- RPO 5–15m / RTO ≤ 60m are proposals until WAL + isolated restore is run on the production-shaped cluster
- Replica / failover topology not part of V1 runtime docs

## Local WAL/PITR lab (capability only)

`docker-compose.pitr-lab.yml` starts `postgis/postgis:16-3.5` with `archive_mode=on`. It uses database `pitr_lab`, not `konumlu`. On 2026-09-12 this lab restored a base backup plus archived WAL to a recovery target time and promoted; the post-backup row was absent after recovery. Do not treat the lab as a KONUMLU application backup. Production still must enable WAL archive on the real primary and store base backups off-host.
