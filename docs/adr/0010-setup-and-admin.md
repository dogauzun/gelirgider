# ADR 0010: First-time setup and admin

Status: accepted (GLG-317, 2026-09-23); amended 2026-09-25 (GLG-352,
register and login refused until setup); amended 2026-09-28 (GLG-580,
`claude.yml` and `claude-code-review.yml` deleted); amended 2026-10-07
(GLG-1115, the `tefas` source is off by default; GLG-1117, registration
closed by default)

## Context

A fresh install has no setup step and no admin. Everything an operator
controls lives in env vars, CLI flags or host cron:

- Finnhub and Tiingo keys come from `FINNHUB_API_KEY` and `TIINGO_API_KEY`,
  read by `us.NewFetcher` (`backend/internal/us/us.go`).
- The schedule is one global `PRICE_FETCH_AT` plus `PRICE_FETCH_ENABLED`
  (`backend/internal/config/config.go`).
- The sources are hardcoded in `backend/cmd/server/main.go`: `bist`,
  `bist-splits`, `tcmb`, `us`, `crypto`.
- TEFAS runs outside the backend as `tools/tefas` on a host cron
  (ADR 0004, GLG-87, GLG-109).
- There are no roles. `/api/auth/register` is open to anyone.
- Operator tasks are CLI flags: `-set-password`, `-backfill-prices`.

Changing a key or the fetch time means editing `.env` and restarting.

This ADR records the decisions of epic GLG-296 (Phase 11), approved by the
user on 2026-09-23.

## Decision

### Bootstrap: a one-time setup token in the log

While no user has `is_admin = 1`, the server makes a setup token at startup
(32 random bytes, hex) and logs it once at WARN with the field
`setup_token`. The token lives in memory only. A restart prints a new one.

`/setup` asks for the token plus the admin email and password. Only someone
who can read the server log can create the first admin. After that the token
is cleared and `/api/setup/admin` answers 409.

If the email is already registered (a dev DB with users), that user is
promoted only when the password matches.

Until setup is done, `/api/auth/register` and `/api/auth/login` answer 403
`setup_required`, so no account is created or signed in before the admin.

### Admin is a flag on a normal user

`users.is_admin`, default 0. An admin keeps portfolios like any other user.
There is no separate admin account type and no role table.

- `RequireAdmin` reads the flag from the DB on every request, so a demotion
  applies at once.
- The last admin cannot be demoted or deleted.
- An admin sees account fields of other users (email, created date, admin
  flag), never their portfolios or transactions.

### Global tables with no `user_id`: `settings` and `source_runs`

```
settings(key, value, updated_at)                                  key is the primary key
source_runs(id, source, kind, started_at, finished_at, rows, error)
```

The rule "every table that holds user data has `user_id`" (PRINCIPLES #2) is
about user data. These tables hold none:

- `settings` is install-wide config: provider keys, per-source schedule, the
  registration toggle. One install has one value per key, whoever reads it.
- `source_runs` is a log of market-data fetches, one row per source per run.

They are global for the same reason `prices`, `fx_rates`, `splits` and
`dividends` are. No query on them is scoped to a user, and deleting a user
(GLG-310) leaves them untouched. A table that would hold anything a user owns
still gets `user_id`.

### Secrets: AES-GCM with `SECRET_KEY`

Provider keys are stored AES-256-GCM encrypted: `base64(nonce || ciphertext)`
with a fresh random nonce per write, stdlib `crypto/aes` and `crypto/cipher`,
no new dependency.

- The key is the `SECRET_KEY` env var: base64 of 32 random bytes. The server
  refuses to start without it. `-healthcheck` and `-backup` still work
  without it.
- `make dev` writes one into `.env` if it is missing.
- The API never returns a key, only `configured` and the last 4 characters.
- A wrong `SECRET_KEY` gives a clear error naming the setting, never a panic.

Losing `SECRET_KEY` means the stored provider keys can no longer be
decrypted. Nothing else is lost: portfolios, prices and plain settings are
not encrypted. The fix is to set a new `SECRET_KEY` and enter the provider
keys again in the admin area. A backup holds the ciphertext, so restoring it
needs the same `SECRET_KEY` it was taken with.

### Env vars removed

| Removed               | Replaced by                        |
| --------------------- | ---------------------------------- |
| `FINNHUB_API_KEY`     | secret `provider.finnhub.api_key`  |
| `TIINGO_API_KEY`      | secret `provider.tiingo.api_key`   |
| `PRICE_FETCH_ENABLED` | `source.<name>.enabled` per source |
| `PRICE_FETCH_AT`      | `source.<name>.at` per source      |

The DB is the only source of truth for these. There is no migration from the
old env vars: there is no prod instance. Offline dev means disabling the
sources in the admin area. `SECRET_KEY`, `DOMAIN` and the other deployment
settings stay in env. PRINCIPLES #7 ("configuration through environment
variables only") is reworded to match in GLG-318.

> Resolved 2026-09-23 (GLG-318): PRINCIPLES #7 now says that deployment
> configuration comes from environment variables and that install-wide
> settings an admin changes at runtime live in the `settings` table.

### Registration toggle

`auth.registration_open`, default open. Closed means `/api/auth/register`
returns 403 and only an admin creates users. The login page hides the
register link while it is closed.

> Amended 2026-10-07 (GLG-1117): default closed. An unset toggle means
> closed, so an internet-exposed self-host takes no sign-ups from strangers
> until the admin opens it. An existing instance that never set the toggle
> flips to closed on upgrade.

### Sources and schedule, live

Provider selection is enable or disable per source: `bist`, `bist-splits`,
`tcmb`, `us`, `crypto`, `tefas`. No new providers.

> Amended 2026-09-29 by [ADR 0012](0012-benchmarks.md) and
> [ADR 0017](0017-gold.md) (GLG-515, GLG-518, GLG-555): there are nine
> sources now. `tufe` (TÜİK, 2026-09-28) and `xu100` (TCMB EVDS,
> 2026-09-29) feed the benchmarks of ADR 0012, and `gold` (BtcTurk
> PAXG-TRY, 2026-09-29) prices the `GOLD` market of ADR 0017.

Per source: `source.<name>.enabled` (default on, `tefas` off, see
ADR 0004) and `source.<name>.at`, a daily `HH:MM` in Europe/Istanbul
(default 19:00, `tefas` 14:00 because TEFAS publishes around 13:30). Global: startup catch-up on/off, startup
backfill on/off, and a backfill start date override (`schedule.backfill_from`,
empty means the earliest transaction).

> Amended 2026-09-29 (GLG-515, GLG-518): `tufe` defaults to 11:00, after
> TÜİK's 10:00 release, and `xu100` defaults to 20:00. `gold` keeps the
> 19:00 default.

Changes apply live, with no restart. The scheduler ticks once a minute,
re-reads the settings, and runs each enabled source whose time has passed
today and that has not succeeded today. Weekday, `EveryDay` and retry rules
stay as they are. A malformed value logs once and falls back to its default.
A disabled source is skipped by the schedule, catch-up and backfill, but an
admin can still run it by hand.

### Admin area and wizard

`/admin` (Yönetim / Administration) has three sections:

- Data providers: Finnhub and Tiingo keys, with Save, Test and Clear.
- Sources: enabled, time, last success, last error, rows, and Run now and
  Backfill buttons. Status comes from `source_runs`.
- Users: list, create, reset password, promote/demote, delete, and the
  registration toggle.

> Amended 2026-09-29 (GLG-562, GLG-517): `/admin` has four sections. An
> Instruments section, where an admin adds an instrument by market, symbol,
> name and currency, sits between Sources and Users (GLG-562, 2026-09-28).
> Data providers holds a third key, TCMB EVDS, which the `xu100` source
> needs (GLG-517).

The setup wizard is two steps. Step 1 creates the admin account. Step 2 is
optional and reuses the providers and sources sections and the registration
toggle as they are.

### TEFAS moves into the backend

The `tools/tefas` crawler becomes `backend/internal/tefas`, a normal
scheduler source named `tefas` with a `-fetch-tefas` flag, backfill and
on-demand fetch like the other markets. Then `tools/tefas`, `deploy/tefas.sh`,
`deploy/tefas.Dockerfile`, the compose `tefas` service and the Makefile
`dev-tefas` target are deleted.

> Superseded 2026-10-06 (GLG-1087): the per-source `-fetch-*` flags are one
> `-fetch <source>` flag with `-date` (#1050), so TEFAS runs as
> `-fetch tefas`; see `deploy/RUNBOOK.md` §5.

The request shape, the honest User-Agent, tracked funds only and the
one-month re-fetch stay as in ADR 0004, and so does its robots.txt decision
([ADR 0004](0004-tefas-data-source.md)): the `tefas` source is off by default
and the operator who turns it on decides. Only where the crawler runs
changes: the backend scheduler instead of host cron.

### `ci.yml` deleted

`.github/workflows/ci.yml` has only the tefas job. With the crawler gone it is
deleted. Backend and frontend checks stay local before merge, as CLAUDE.md
says. `claude.yml` and `claude-code-review.yml` were untouched here and
deleted on 2026-09-28 (GLG-580, ADR 0015).

### Recorded simplifications

- A dev DB with users but no admin also goes through setup.
- `make dev` no longer fills US prices until the keys are entered in the
  wizard or the admin area.
- The setup token is not persisted. A restart prints a new one.
- Live config is read per tick, with no cache. The in-memory "ran today" and
  "running" flags assume one backend instance, like the scheduler already
  does.

### Not in this epic

- Invite links.
- Alternative providers per market.
- Per-source intervals (intraday).
- CI for backend and frontend.
- An audit log of admin actions.

## Known risks

- Anyone who reads the backend log before setup is done can create the admin.
  Run setup right after the first start. A restart rotates the token.
- Losing `SECRET_KEY` means entering the provider keys again, and a backup can
  only be restored with the key it was taken with. Keep `SECRET_KEY` next to
  the backups, not only on the host.
- Registration is open by default, as today. The admin closes it in the
  wizard or the admin area.

  > Superseded 2026-10-07 (GLG-1117): registration is closed by default; an
  > unset toggle means closed, and the admin opens it in the wizard or the
  > admin area.

- `-set-password` stays the escape hatch if every admin is locked out. It can
  reset any account, so it stays an operator-only tool on the host.

## Consequences

- GLG-297 adds `users.is_admin`, `RequireAdmin` and `is_admin` in `/api/me`.
- GLG-298 adds the `settings` table and repository with AES-GCM secrets and
  `SECRET_KEY`.
- GLG-299 ports TEFAS into `backend/internal/tefas` as a scheduler source.
- GLG-300 adds the setup token and the first-admin API.
- GLG-301 adds the registration toggle.
- GLG-302 moves the Finnhub and Tiingo keys into encrypted settings and drops
  their env vars.
- GLG-303 makes the scheduler read per-source enabled and time from settings,
  live, and drops `PRICE_FETCH_ENABLED` and `PRICE_FETCH_AT`.
- GLG-304 adds the user management API.
- GLG-305 deletes `tools/tefas`, its deploy files and `ci.yml`.
- GLG-306 adds the `/admin` shell and nav entry.
- GLG-307 adds the source and schedule settings API.
- GLG-308 adds the test API key endpoint.
- GLG-309 adds `source_runs` and status fields on the sources API.
- GLG-310 adds delete user with all of their data.
- GLG-311 adds wizard step 1.
- GLG-312 adds the data providers section.
- GLG-313 adds the users section and registration toggle UI.
- GLG-314 adds run now and backfill now.
- GLG-315 adds the sources section.
- GLG-316 adds wizard step 2.
- GLG-317 writes this ADR, the Phase 11 roadmap section and the RUNBOOK
  section.
- GLG-318 rewords PRINCIPLES #7.
