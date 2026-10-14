# ADR 0002: Production datastore and schema migrations

Status: accepted (GLG-41, 2026-09-13); amended 2026-09-13 (GLG-50, one VM
became one Docker host); superseded in part by
[ADR 0015](0015-pr-review-routine.md) (GLG-580, 2026-09-28): no GitHub
Actions, `go test ./...` runs against SQLite only as a local check before
every merge; amended 2026-10-05 (GLG-888, a table rebuild may start with
`PRAGMA defer_foreign_keys = ON`); amended 2026-10-14 (GLG-1367,
correction: backups are not copied off the host automatically)

## Decision

### SQLite stays as the production datastore

Production runs **one backend instance on one Docker host** against a single
SQLite database file. WAL mode is already enabled in `internal/db` at startup. The
database file lives on a **bind-mounted host directory** (not a named Docker
volume) so that backups, restores, and inspection are plain file operations on
the host.

CI already runs `go test ./...` against SQLite, so the Phase 4 roadmap item
"run CI against the production datastore" is satisfied with no change.

> Superseded 2026-09-23 (GLG-257): the backend CI job was removed, and
> [ADR 0015](0015-pr-review-routine.md) (GLG-580) deleted the last GitHub
> Actions workflows on 2026-09-28; `go test ./...` runs against SQLite only
> as a local check before every merge.

Why this holds at the current scale:

- Single-digit users for the foreseeable future, a few hundred transactions
  per user, one price row per instrument per trading day. This is megabytes,
  not gigabytes.
- One process owns the file. SQLite's single-writer model is a limitation only
  when there is more than one writer.
- No second backend replica is planned. There is nothing to share the database
  with.
- The portable-SQL rule in `docs/PRINCIPLES.md` (section 4) keeps the exit
  open: nothing outside the repository layer knows which database is in use,
  so a later move to PostgreSQL is a repository-layer and deploy change, not a
  rewrite.

What would flip this decision, any one of:

- A second backend replica is needed (for availability or for load).
- The database file grows past a few GB, at which point backups by file copy
  and `VACUUM INTO` stop being cheap.
- Observed write contention: `SQLITE_BUSY` errors in the backend logs under
  normal use.

### Schema migrations stay as ordered `.sql` files, no tool

Schema changes are plain `.sql` files in `backend/internal/db/schema`, embedded
into the binary with `go:embed`, applied in filename order at startup, and
tracked by filename in the `schema_version` table. This is what `internal/db`
already does; the decision is to keep it and write down the rules.

Rules:

- **Forward-only.** No down migrations. Undoing a schema change is a new
  forward file, or a restore from backup.
- **Applied files are immutable.** A file that has been merged to `main` is
  never edited. If it was wrong, add a new file that corrects it. GLG-42 adds a
  checksum per applied file so an edited file fails startup instead of silently
  diverging between environments.

  > Resolved 2026-09-14 (GLG-42): `schema_version` stores a checksum per
  > applied file, and startup fails when an applied file has changed since
  > (`backend/internal/db/db.go`).

- **One file, one transaction.** Each file is applied inside a single
  transaction, so a failed statement leaves the database at the previous
  version instead of half-applied. GLG-42 makes this true; today statements in
  a file are executed as one `Exec` call outside an explicit transaction.

  > Resolved 2026-09-14 (GLG-42): each schema file is now applied inside its
  > own transaction together with its `schema_version` row, so a failed
  > statement rolls the whole file back (`backend/internal/db/db.go`).

- **File names are `NNN_short_name.sql`** with a zero-padded three-digit
  prefix (`007_add_fx_rates.sql`). Filename order is application order. Two
  PRs that both add `007_` conflict on merge, which is the intended signal to
  renumber one of them.
- Files must be portable SQL per `docs/PRINCIPLES.md`: no SQLite-only
  pragmas, triggers, or stored procedures inside migration files.

  > Amended 2026-10-05 (GLG-888): one exception for table rebuilds.
  > `027_portfolios_autoincrement.sql` rebuilds `portfolios` (copy, drop,
  > create, copy back) to add `AUTOINCREMENT`, and starts with
  > `PRAGMA defer_foreign_keys = ON` so dropping a table that other tables
  > reference is checked at `COMMIT`, when the same ids are back, instead of
  > failing at once. SQLite switches it off at `COMMIT` or `ROLLBACK`, and
  > foreign keys stay enforced. A rebuild must not rename the old table
  > aside: SQLite repoints references to the new name. It works only while
  > no reference to the table is `ON DELETE CASCADE`, since the drop deletes
  > its rows first.

## Alternatives considered

| Option                                              | Verdict    | Why                                                                                                                              |
| --------------------------------------------------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------- |
| SQLite on a bind mount, WAL mode                    | **chosen** | One process, one file, zero extra services. Backup is a file copy.                                                               |
| PostgreSQL in a container on the same host          | rejected   | A second stateful service to back up, upgrade, and monitor. No benefit at single-digit users on one instance.                    |
| Managed PostgreSQL (cloud provider)                 | rejected   | Monthly cost plus a network hop on every query, for no benefit at this scale. Revisit if a replica or a multi-GB file appears.   |
| Plain `.sql` files, `schema_version` table, no tool | **chosen** | Already implemented in about 60 lines of `internal/db`. Forward-only, transaction-per-file, checksum guard cover the real risks. |
| `golang-migrate`                                    | rejected   | New dependency and a CLI to install on the host for features (down migrations, many drivers) we have decided not to use.         |
| `goose`                                             | rejected   | Same as above; Go-function migrations are not needed while business logic stays out of the schema layer.                         |
| `atlas`                                             | rejected   | Separate binary and declarative schema diffing. Far more machinery than six files justify.                                       |

## Known risks

- A single host is a single point of failure. Mitigation: backups copied off
  the host (GLG-44, GLG-46) and a rehearsed restore, not a replica.

  > Correction 2026-10-14 (GLG-1367): GLG-46 did not copy backups off the
  > host. `deploy/backup.sh` keeps gzipped local copies in
  > `deploy/data/backups/` for 14 days; copying them off the host is the
  > operator's job (`deploy/RUNBOOK.md` §4).

- SQLite's one-writer-at-a-time model means a long write transaction blocks
  every other write. Mitigation: keep write transactions short; the scheduled
  price fetch (GLG-35) writes one day of rows per run. `SQLITE_BUSY` in the
  logs is the flip signal above.
- WAL mode leaves `-wal` and `-shm` files next to the database. A naive
  `cp` of only the `.db` file can miss committed data. Mitigation: backups use
  `VACUUM INTO` (GLG-44), which writes a consistent single-file copy.
- Without a tool there is no `migrate down` for local experiments. Mitigation:
  delete the local database file and restart; it is a development database.
- Renumbering a migration file after a merge conflict is manual. Acceptable at
  the current PR rate. A file can only be renamed before it reaches `main`, so
  the only database that may have applied it under the old name is a local
  development one, which is deleted and recreated.

## Consequences

- `deploy/compose.yml` for production (GLG-45) mounts a host directory at
  `/data` instead of the `db-data` named volume used for local runs.

  > Superseded 2026-09-14 (GLG-45): GLG-45 replaced the `db-data` volume in
  > the one `deploy/compose.yml` with the `./data` host directory, so no
  > named database volume exists any more, and `make dev` runs the backend
  > outside Docker.

- GLG-42 changes `internal/db` to wrap each schema file in a transaction and
  to store and verify a checksum in `schema_version`. No other code changes.
- GLG-44 adds a `-backup <path>` flag to the backend binary that runs
  `VACUUM INTO` against the configured `DB_PATH`. GLG-46 documents the host
  cron that calls it and copies the result off the host.

  > Correction 2026-10-14 (GLG-1367): GLG-46 did not copy backups off the
  > host. `deploy/backup.sh` keeps gzipped local copies in
  > `deploy/data/backups/` for 14 days; copying them off the host is the
  > operator's job (`deploy/RUNBOOK.md` §4).

- `docs/PRINCIPLES.md` section 4 ("Datastore is undecided") stays as written:
  the datastore is decided for now, the portability rule is what makes the
  decision cheap to revisit.

  > Amended 2026-10-01 (GLG-711): `docs/PRINCIPLES.md` section 4 is now
  > titled "SQLite now, portable SQL always" and points to this ADR; the
  > portable-SQL rule is kept.
