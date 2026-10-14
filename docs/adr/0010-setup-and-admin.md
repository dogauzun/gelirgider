# ADR 0010: First-time setup and admin

Status: accepted (2026-09-23).

## Context

An install needs an operator: someone who sets the provider keys, chooses
which market-data sources run and when, and manages users. Doing that through
env vars, CLI flags and host cron means editing `.env` and restarting for
every change, and gives no way to tell who may do what. Roles, the first
admin and install-wide settings therefore live in the app and the database.

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

The rule "every table that holds user data has `user_id`" (PRINCIPLES #2) is
about user data. These tables hold none:

- `settings` is install-wide config as key and value: provider keys,
  per-source schedule, the registration toggle. One install has one value per
  key, whoever reads it.
- `source_runs` is a log of market-data fetches, one row per source per run.

They are global for the same reason `prices`, `fx_rates`, `splits` and
`dividends` are. No query on them is scoped to a user, and deleting a user
leaves them untouched. A table that would hold anything a user owns still
gets `user_id`.

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

### Settings, not env vars

Provider keys (`provider.<name>.api_key`, for Finnhub, Tiingo and TCMB EVDS)
and the per-source `source.<name>.enabled` and `source.<name>.at` live in
`settings`, which is the only source of truth for them. `SECRET_KEY`,
`DOMAIN` and the other deployment settings stay in env (PRINCIPLES #7).
Offline dev means disabling the sources in the admin area.

### Registration toggle

`auth.registration_open`: an unset toggle means closed. Closed means
`/api/auth/register` returns 403 and only an admin creates users, so an
internet-exposed self-host takes no sign-ups from strangers until the admin
opens it. The login page hides the register link while it is closed.

### Sources and schedule, live

Each source can be enabled or disabled: `bist`, `bist-splits`, `tcmb`, `us`,
`crypto`, `tefas`, `tufe`, `xu100` and `gold`. There are no alternative
providers per market.

Per source: `source.<name>.enabled` (default on, `tefas` off, see ADR 0004)
and `source.<name>.at`, a daily `HH:MM` in Europe/Istanbul. The default time
is 19:00, with `tefas` at 14:00 because TEFAS publishes around 13:30, `tufe`
at 11:00 after TÜİK's 10:00 release, and `xu100` at 20:00. Global: startup
catch-up on/off, startup backfill on/off, and a backfill start date override
(`schedule.backfill_from`, empty means the earliest transaction).

Changes apply live, with no restart. The scheduler ticks once a minute,
re-reads the settings, and runs each enabled source whose time has passed
today and that has not succeeded today. A malformed value logs once and falls
back to its default. A disabled source is skipped by the schedule, catch-up
and backfill, but an admin can still run it by hand.

### Admin area and wizard

`/admin` (Yönetim / Administration) has four sections:

- Data providers: the Finnhub, Tiingo and TCMB EVDS keys, with Save, Test and
  Clear.
- Sources: enabled, time, last success, last error, rows, and Run now and
  Backfill buttons. Status comes from `source_runs`.
- Instruments: an admin adds an instrument by market, symbol, name and
  currency.
- Users: list, create, reset password, promote/demote, delete (with all of
  the user's data), and the registration toggle.

The setup wizard is two steps. Step 1 creates the admin account. Step 2 is
optional and reuses the providers and sources sections and the registration
toggle as they are.

### TEFAS runs in the backend

TEFAS is a normal scheduler source named `tefas`, with backfill and on-demand
fetch like the other markets. The request shape, the honest User-Agent,
tracked funds only and the one-month re-fetch are as in ADR 0004, and so is
its robots.txt decision: the `tefas` source is off by default and the
operator who turns it on decides.

### Recorded simplifications

- A dev DB with users but no admin also goes through setup.
- `make dev` fills no US prices until the keys are entered in the wizard or
  the admin area.
- The setup token is not persisted. A restart prints a new one.
- Live config is read per tick, with no cache. The in-memory "ran today" and
  "running" flags assume one backend instance, like the scheduler already
  does.

### Out of scope

Invite links, per-source intervals (intraday), an audit log of admin actions.

## Known risks

- Anyone who reads the backend log before setup is done can create the admin.
  Run setup right after the first start. A restart rotates the token.
- Losing `SECRET_KEY` means entering the provider keys again, and a backup can
  only be restored with the key it was taken with. Keep `SECRET_KEY` next to
  the backups, not only on the host.
- `-set-password` stays the escape hatch if every admin is locked out. It can
  reset any account, so it stays an operator-only tool on the host.
