# ADR 0002: Production datastore and schema migrations

Status: accepted (2026-09-13).

## Decision

### SQLite stays as the production datastore

Production runs **one backend instance on one Docker host** against a single
SQLite database file. WAL mode is enabled in `internal/db` at startup. The
database file lives on a **bind-mounted host directory** (`deploy/data`, not a
named Docker volume) so that backups, restores, and inspection are plain file
operations on the host.

`go test ./...` runs against SQLite, the production datastore, as part of the
required `Go tests` check on every pull request and on `main`.

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
tracked by filename in the `schema_version` table.

Rules:

- **Forward-only.** No down migrations. Undoing a schema change is a new
  forward file, or a restore from backup.
- **Applied files are immutable.** A file that has been merged to `main` is
  never edited. If it was wrong, add a new file that corrects it.
  `schema_version` stores a SHA-256 checksum per applied file, and startup
  fails when an applied file has changed since, so an edited file cannot
  silently diverge between environments.
- **One file, one transaction.** Each file is applied inside a single
  transaction together with its `schema_version` row, so a failed statement
  rolls the whole file back and leaves the database at the previous version.
- **File names are `NNN_short_name.sql`** with a zero-padded three-digit
  prefix (`007_add_fx_rates.sql`). Filename order is application order. Two
  PRs that both add `007_` conflict on merge, which is the intended signal to
  renumber one of them.
- Files must be portable SQL per `docs/PRINCIPLES.md`: no SQLite-only
  pragmas, triggers, or stored procedures inside migration files. One
  exception: a table rebuild (copy, drop, create, copy back, as in
  `027_portfolios_autoincrement.sql`) may start with
  `PRAGMA defer_foreign_keys = ON`, so dropping a table that other tables
  reference is checked at `COMMIT`, when the same ids are back, instead of
  failing at once. SQLite switches it off at `COMMIT` or `ROLLBACK`, and
  foreign keys stay enforced. A rebuild must not rename the old table aside:
  SQLite repoints references to the new name. It works only while no
  reference to the table is `ON DELETE CASCADE`, since the drop deletes its
  rows first.

## Backups

The backend binary has a `-backup <path>` flag that runs `VACUUM INTO` against
the configured `DB_PATH`. `deploy/backup.sh`, run from host cron, calls it and
keeps gzipped local copies in `deploy/data/backups/` for 14 days. Copying
those backups off the host is the operator's job, not the script's
(`deploy/RUNBOOK.md` section 4).

## Alternatives considered

| Option                                              | Verdict    | Why                                                                                                                              |
| --------------------------------------------------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------- |
| SQLite on a bind mount, WAL mode                    | **chosen** | One process, one file, zero extra services. Backup is a file copy.                                                               |
| PostgreSQL in a container on the same host          | rejected   | A second stateful service to back up, upgrade, and monitor. No benefit at single-digit users on one instance.                    |
| Managed PostgreSQL (cloud provider)                 | rejected   | Monthly cost plus a network hop on every query, for no benefit at this scale. Revisit if a replica or a multi-GB file appears.   |
| Plain `.sql` files, `schema_version` table, no tool | **chosen** | Already implemented in about 60 lines of `internal/db`. Forward-only, transaction-per-file, checksum guard cover the real risks. |
| `golang-migrate`                                    | rejected   | New dependency and a CLI to install on the host for features (down migrations, many drivers) we have decided not to use.         |
| `goose`                                             | rejected   | Same as above; Go-function migrations are not needed while business logic stays out of the schema layer.                         |
| `atlas`                                             | rejected   | Separate binary and declarative schema diffing. Far more machinery than a plain list of `.sql` files justifies.                  |

## Known risks

- A single host is a single point of failure. Mitigation: backups the operator
  copies off the host and a rehearsed restore, not a replica.
- SQLite's one-writer-at-a-time model means a long write transaction blocks
  every other write. Mitigation: keep write transactions short; the scheduled
  price fetch writes one day of rows per run. `SQLITE_BUSY` in the logs is the
  flip signal above.
- WAL mode leaves `-wal` and `-shm` files next to the database. A naive
  `cp` of only the `.db` file can miss committed data. Mitigation: backups use
  `VACUUM INTO`, which writes a consistent single-file copy.
- Without a tool there is no `migrate down` for local experiments. Mitigation:
  delete the local database file and restart; it is a development database.
- Renumbering a migration file after a merge conflict is manual. Acceptable at
  the current PR rate. A file can only be renamed before it reaches `main`, so
  the only database that may have applied it under the old name is a local
  development one, which is deleted and recreated.
