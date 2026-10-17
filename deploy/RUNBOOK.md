# Operations runbook

Running gelirgider with Docker Compose on any Docker host: a laptop, a home
server, or a cloud machine reachable on ports 80/443. No VM provisioning, no
cloud-provider steps, no Kubernetes — see `deploy/README.md` for the
local production-like run and image/config overview.

All commands below are run from the repo root unless noted. `deploy/data`
is owned by the backend's uid 65532 with mode 700 (§1), so a command that
touches it from the host runs as root, shown with `sudo`.

## 1. Prerequisites

1. Install Docker Engine with the compose plugin (`docker compose version`
   must print something).
2. Clone/checkout the repo on the host.
3. Copy the env file and set `DOMAIN`:
   ```
   cp deploy/.env.example deploy/.env
   ```
   Edit `deploy/.env` and set `DOMAIN`:
   - `localhost` if the host isn't reachable from the internet — Caddy
     issues a local self-signed certificate.
   - A real hostname (DNS already pointing at this host) when the host is
     reachable on ports 80 and 443 — Caddy gets a Let's Encrypt certificate
     automatically.
4. Generate a `SECRET_KEY` once:
   ```
   openssl rand -base64 32
   ```
   Paste the output into the existing `SECRET_KEY=` line of `deploy/.env`
   (the copy of `.env.example` already has that line, empty). Do not append
   a second `SECRET_KEY` line, and do not generate a new key on a host that
   already has one: a new key works like a lost one, below. Compose and the
   backend refuse to start without it. It encrypts the provider API keys
   (Finnhub, Tiingo and TCMB EVDS) and the users' two-step login secrets in the
   database, and nothing else. Keep a
   copy off the host, next to the backups. To keep the key out of the
   environment, store it in a file instead, mount it as a Docker secret and
   set `SECRET_KEY_FILE` to its path; the commented example in
   `deploy/compose.yml` shows how, including the `SECRET_KEY` guard to
   remove there. `SECRET_KEY` wins when both are set.
5. Make the env file and the data directory private. The backend runs as
   the distroless `nonroot` user, uid and gid 65532, and must own
   `deploy/data`; the database and its backups hold password hashes and
   encrypted provider keys:
   ```
   chmod 600 deploy/.env
   sudo mkdir -p deploy/data
   sudo chown 65532:65532 deploy/data
   sudo chmod 700 deploy/data
   ```
   Without the `chown`, Docker creates `deploy/data` owned by root and the
   backend cannot create its database there.

**Lost or changed `SECRET_KEY`.** A backup can be restored with any valid
`SECRET_KEY`: users, portfolios, transactions and prices do not depend on
it. Only the stored provider API keys and the users' two-step login secrets
do (the last paragraph of this section says what that costs). With a different
key the backend starts normally, the provider rows in Yönetim / Administration show that the
stored key cannot be read, and the US and BIST 100 fetches fail until the
keys are entered again there. To recover, generate a new key as above, put
it in the `SECRET_KEY=` line (or the secret file `SECRET_KEY_FILE` names),
restart the backend, and enter the three
provider keys again.

The key also seals each user's two-step login (TOTP) secret
([ADR 0025](../docs/adr/0025-two-step-login.md)). With a different key those
secrets cannot be read, and every user who turned two-step login on is locked
out of login, with no fallback to the password alone, until an admin or the
operator resets that user's password (Yönetim / Administration or
`-set-password`, §9), which also turns two-step login off. Those users then
enrol again. Users without two-step login are not affected.

## 2. First deploy

1. Pull the published backend and frontend images
   (`ghcr.io/dogauzun/gelirgider-backend` and `-frontend`, at the tag
   `IMAGE_TAG` in `deploy/.env` names, `latest` by default) and start
   everything, detached:
   ```
   docker compose -f deploy/compose.yml pull
   docker compose -f deploy/compose.yml up -d
   ```
   The published images cover `linux/amd64` and `linux/arm64`. A checkout
   with local code changes builds both images from the checkout instead:
   ```
   docker compose -f deploy/compose.yml up -d --build
   ```
2. Check container health:
   ```
   docker compose -f deploy/compose.yml ps
   ```
   `backend` should show `healthy` (it takes a few seconds after start —
   see the compose `healthcheck`, `start_period: 5s`). `frontend` and
   `caddy` should show `running`/`Up`.
3. Open `https://DOMAIN` in a browser (`https://localhost` for a local run
   — accept the self-signed certificate warning).
4. Read the one-time setup token from the backend log:
   ```
   docker compose -f deploy/compose.yml logs backend | grep setup_token
   ```
   The token is printed while no admin exists. It is kept in memory only, so
   a restart prints a new one; use the latest.
5. Open `https://DOMAIN/setup` (every page redirects there until setup is
   done), enter the token and the admin email and password. An email that
   is already registered becomes the admin only if its password is right.
6. The optional second step sets the Finnhub, Tiingo and TCMB EVDS keys,
   the sources and whether registration is open. Registration is closed
   until an admin opens it. Everything there can be changed later.

After setup, operator settings live in Yönetim / Administration (`/admin`),
not in env vars or cron:

- Finnhub, Tiingo and TCMB EVDS API keys, with a Test button.
  `FINNHUB_API_KEY` and `TIINGO_API_KEY` are not read. The EVDS key feeds
  the BIST 100 benchmark (`xu100`); see §5.
- Per source (the nine sources listed in §5): enabled and daily time in
  Europe/Istanbul, applied without a restart, plus startup catch-up, startup
  backfill and backfill start date.
- Last success, last error and rows per source, with Run now and Backfill
  buttons.
- Enstrüman ekle / Add instrument: adds a BIST, US, TEFAS, crypto or gold
  instrument that the seed lists do not have.
- Users: create, reset password, make or remove admin, delete, and open or
  close registration.

## 3. Update

1. Pull the new code, for changes to `compose.yml`, the Caddyfile and these
   docs:
   ```
   git pull
   ```
2. Pull the new images and restart:
   ```
   docker compose -f deploy/compose.yml pull
   docker compose -f deploy/compose.yml up -d
   ```
   The images for a commit on `main` are published a few minutes after it
   merges (`.github/workflows/publish.yml`); a `pull` before that run
   finishes gets the previous `latest`. A checkout with local code changes
   rebuilds instead, with
   `docker compose -f deploy/compose.yml up -d --build`.

Since GLG-1118 the backend runs as uid 65532 instead of root. On a host
set up before it, hand the existing database files to that uid once, before
step 2, and make the env file and the directory private:

```
sudo chown -R 65532:65532 deploy/data
sudo chmod 700 deploy/data
chmod 600 deploy/.env
```

Without it the new backend cannot write the root-owned database and does
not start.

Since GLG-1117 (2026-10-07) an unset registration toggle means closed: an
instance that never set it stops taking sign-ups after the update. Open it
in Yönetim / Administration → Users if anyone should sign up.

The backend applies any new schema files under
`backend/internal/db/schema/` on startup, and records a checksum for each
applied file. If a previously-applied schema file was edited after being
merged, the backend refuses to start with an error like:

```
apply schema: schema file 00N_xxx.sql has changed since it was applied
```

This means the code checked out doesn't match what's actually in the
database. Do not edit already-applied schema files to work around it —
schema files are forward-only (see Rollback below). Either:

- **Fix forward**: add a new schema file that makes the corrected change,
  commit it, and update again; or
- **Restore**: if the deploy went out with a broken schema file, roll back
  the code (Rollback, below) and restore the last known-good backup (Restore
  drill, below) if the broken file already ran against the live database.

Check `docker compose -f deploy/compose.yml logs backend` for the exact
error before deciding which.

## 4. Backup

`deploy/backup.sh` runs the backend's `-backup` flag (`VACUUM INTO`, added
in GLG-44) for a consistent snapshot, gzips it into `deploy/data/backups/`,
and deletes local backups older than 14 days.

Plain `cp` of the live `gelirgider.db` file is not used because the backend
runs SQLite in WAL mode: the database file on disk can be missing recently
committed pages that still sit in the `-wal` file, so a `cp` taken mid-write
can copy a torn, inconsistent snapshot. The distroless backend image also
has no shell or `sqlite3` CLI to script around it. `VACUUM INTO` asks
SQLite itself for a consistent copy instead.

1. Run a backup manually, as root (`deploy/data` is mode 700, §1). The
   backend container must be running, because the script runs the backup
   inside it with `docker compose exec`:
   ```
   sudo deploy/backup.sh
   ```
   It prints two lines: the snapshot the backend wrote inside the container
   and its size in bytes, then the path of the gzipped backup, relative to
   `deploy/`:
   ```
   /data/backup-20260914T033000.db 1048576
   backup ok: ./data/backups/gelirgider-20260914T033000.db.gz
   ```
   The backup is mode 600 in a mode 700 `deploy/data/backups/`.
2. Schedule it with cron in root's crontab (`sudo crontab -e`), daily at
   03:30 host time, appending output to a log file (use absolute paths for
   your checkout):
   ```
   30 3 * * * /path/to/gelirgider/deploy/backup.sh >> /path/to/gelirgider/deploy/data/backup.log 2>&1
   ```

Copying `deploy/data/backups/` somewhere off this host (another disk,
object storage, whatever sync tool is already in use) is the operator's
job — this script only keeps local copies.

The export in the account menu is not a backup. It holds transactions only,
one CSV per portfolio. Users, password hashes, provider keys, manual prices,
dividend dismissals, learned import names and price history live only in the
database, so `backup.sh` stays the backup. §6 restores one.

## 5. Daily price fetch

The backend has a built-in scheduler (no cron needed) that fetches each
source (`bist`, `bist-splits`, `tcmb`, `us`, `crypto`, `tefas`, `tufe`,
`xu100`, `gold`) once a day.
It checks the settings once a minute, so a change applies without a restart
(GLG-303):

- `source.<name>.enabled` — on by default, except `tefas`, which stays off
  until an admin turns it on (tefas.gov.tr's robots.txt disallows its
  endpoint, GLG-1114). Disabling a source also skips it in the startup
  catch-up, the startup backfill and the fetch after a transaction is saved.
  Offline dev means disabling the sources.
- `source.<name>.at` — daily time, Europe/Istanbul, default `19:00`
  (`tefas` `14:00`, TEFAS publishes around 13:30; `tufe` `11:00`, TÜİK
  releases the CPI on the 3rd at 10:00; `xu100` `20:00`). A source that isn't ready
  yet (e.g. BIST hasn't published) is retried hourly until 23:00.
- `schedule.catchup_enabled`, `schedule.backfill_enabled` — the startup
  catch-up and history backfill below, both on by default.
  `schedule.backfill_from` (`YYYY-MM-DD`) overrides the backfill start date,
  like `-backfill-from`.

These live in the `settings` table; the admin area edits them (GLG-307,
GLG-315). A malformed value logs a warning once and uses its default.

The Finnhub (latest US close), Tiingo (US daily-close history) and TCMB
EVDS (BIST 100 index closes) API keys are set in Yönetim / Administration,
not in env vars. Without the Finnhub key US price fetches fail; without the
Tiingo key US history is skipped with one log line. Without the EVDS key the
`xu100` source fails with
`xu100: EVDS API key not configured, set it in the admin area` and the BIST
100 benchmark stays empty; the backfill skips it with one log line. The EVDS
key comes with a free account at <https://evds3.tcmb.gov.tr>: sign up, then
copy the API key from the account's profile page into the TCMB EVDS row.
BIST, TCMB, BtcTurk crypto and gold, TEFAS and TÜİK need no key.

At startup, before it waits for the scheduled time, the backend runs each
source once (the catch-up), walking back up to 7 days for the latest
published data and skipping a source that already has it — so a backend
started after 19:00, or after downtime, has today's data instead of waiting
for tomorrow. It logs `catch-up: fetch complete` / `catch-up: already
stored, skipping fetch` per source, and a `fetch complete` (or `fetch:
already stored, skipping`) line after each scheduled run, naming the
source. The catch-up runs
in the background: the HTTP server starts listening immediately.

`make dev` gets the same behaviour, so a fresh local database fills itself
within a minute of the first start. The backend binary also has one-shot
flags that do a single job and exit without starting a server:

| Flag                                        | What it does before it exits                                                                                                                                | Needs `SECRET_KEY` or `SECRET_KEY_FILE`      |
| ------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------- |
| `-fetch SOURCE`                             | Runs one scheduler source for `-date` as if the scheduler fired it: `bist`, `bist-splits`, `tcmb`, `us`, `crypto`, `tefas`, `tufe`, `xu100` or `gold`.      | Only `us` and `xu100`                        |
| `-date YYYY-MM-DD`                          | The date `-fetch` runs for; default today in Europe/Istanbul. `us` and `tufe` ignore it (Finnhub serves only the latest close, TÜİK one whole history).     | No                                           |
| `-backfill-prices`                          | Fetches the price history back to the earliest transaction date, see below.                                                                                 | No; without it, `us` and `xu100` are skipped |
| `-backfill-from YYYY-MM-DD`                 | Used with `-backfill-prices`: starts the backfill at this date instead of the earliest transaction date, e.g. `-backfill-prices -backfill-from 2025-01-01`. | Same as `-backfill-prices`                   |
| `-add-split MARKET:SYMBOL,YYYY-MM-DD,RATIO` | Records a split that no provider covered, see below.                                                                                                        | No                                           |
| `-set-password EMAIL`                       | Sets a user's password, read from stdin, see §9.                                                                                                            | No                                           |
| `-backup PATH`                              | Writes a consistent copy of the database to `PATH` with `VACUUM INTO` and prints the path and its size in bytes. `deploy/backup.sh` runs it, see §4.        | No                                           |

"Needs `SECRET_KEY` or `SECRET_KEY_FILE`" means the flag exits with an error
unless `SECRET_KEY`, or the file `SECRET_KEY_FILE` names, holds base64 of 32
bytes; the server itself needs it too. A `SECRET_KEY_FILE` that cannot be
read is reported with its path. The difference matters for a local
`go run`: under Docker Compose, every `docker compose` command already
refuses to run while `SECRET_KEY` is empty in `deploy/.env`, unless the
compose file uses `SECRET_KEY_FILE` instead.

`-fetch` runs the same code as the source's schedule and its Run now button
in Yönetim / Administration, with the same outcomes: `us` logs a failed
Tiingo recent-window sync and still exits 0 after the Finnhub closes are
stored; `us`, `crypto` and `tefas` print `nothing to fetch for <source>` and
exit 0 when no instrument of that market is tracked, as does `bist-splits`
when the capital events page yields no rows. An unknown source name exits
2, a malformed `-date` or a failed fetch exits 1. `-date` without a
`-fetch` source, `-backfill-from` without `-backfill-prices`, or
`-fetch`, `-backup`, `-set-password` or `-add-split` given an empty value
(`-fetch "$SRC"` with `$SRC` unset), prints what is missing and exits 2
without starting a server.

Locally, run the flags from the repo root (the Go module lives in
`backend/`):

```
cd backend && go run ./cmd/server -fetch bist -date 2026-09-14
cd backend && go run ./cmd/server -backfill-prices
```

`-backfill-prices` fetches US prices only with a valid `SECRET_KEY` or
`SECRET_KEY_FILE` (the provider keys are encrypted with it); without one, it logs
that it skipped US and XU100 and backfills every other source. It also skips
XU100 when no EVDS key is set, and TEFAS unless the `tefas` source is turned
on in Yönetim / Administration (it is off by default). TÜFE is not part of it: the `tufe` source's
first run, the startup catch-up, downloads the whole CPI history.

On the server, pass the flags straight to `run --rm backend`; the image's
entrypoint is already `/server`, so don't repeat it (`backend /server
-backfill-prices` would run `/server /server -backfill-prices`, ignore the
flag and start a second full server against the live database):

```
docker compose -f deploy/compose.yml run --rm backend -fetch bist -date 2026-09-14
```

`-backfill-prices` fetches historical BIST bulletin prices, TCMB FX
rates, BtcTurk crypto closes, TEFAS fund NAVs, Tiingo US closes, the
BtcTurk gram gold series and the EVDS BIST 100 closes back to the earliest
transaction date, plus the latest US prices, then exits — this
is also the one-time step to run once after first deploy, so existing
transactions get valued instead of waiting for the next scheduled run:

```
docker compose -f deploy/compose.yml run --rm backend -backfill-prices
```

`-add-split MARKET:SYMBOL,YYYY-MM-DD,RATIO` records a split no provider covered (ADR 0008) and exits: date is the ex-date, ratio is new shares per old share (100% bedelsiz `2`, 50% bedelsiz `1.5`, 1-for-10 reverse `0.1`), e.g. `docker compose -f deploy/compose.yml run --rm backend -add-split BIST:THYAO,2026-05-12,2`; re-running overwrites, ratio `1` neutralises a wrong row.

## 6. Restore drill

Run this once after the first deploy, so a real restore isn't the first
time it's tried.

The drill replaces the live database with a backup, so everything written
after that backup was taken is lost. Step 1 takes a fresh backup first, and
the drill restores that fresh file: only a change saved between step 1 and
step 3 is lost. In a real restore from an older backup, step 1 still keeps a
copy of the current state, and step 2 picks the older file instead.

1. Take a fresh backup while the backend is running (see §4):
   ```
   sudo deploy/backup.sh
   ```
2. Decompress a copy of the backup to restore, keeping the original `.gz`.
   For the drill, that is the file step 1 printed:
   ```
   sudo gunzip -k deploy/data/backups/gelirgider-<timestamp>.db.gz
   ```
3. Stop the backend:
   ```
   docker compose -f deploy/compose.yml stop backend
   ```
4. Move the live database files aside:
   ```
   sudo mv deploy/data/gelirgider.db deploy/data/gelirgider.db.bak
   sudo mv deploy/data/gelirgider.db-wal deploy/data/gelirgider.db-wal.bak 2>/dev/null || true
   sudo mv deploy/data/gelirgider.db-shm deploy/data/gelirgider.db-shm.bak 2>/dev/null || true
   ```
5. Put the backup in place, owned by the backend's uid:
   ```
   sudo mv deploy/data/backups/gelirgider-<timestamp>.db deploy/data/gelirgider.db
   sudo chown 65532:65532 deploy/data/gelirgider.db
   ```
6. Start the backend again:
   ```
   docker compose -f deploy/compose.yml start backend
   ```
7. Log in at `https://DOMAIN` and confirm the restored data looks right.
   With a different `SECRET_KEY` than the backup was taken with, the data
   is all there, but the stored provider keys cannot be decrypted and must
   be entered again, and users with two-step login are locked out until
   an admin resets their password (see Lost or changed `SECRET_KEY` in §1).
8. Once confirmed, delete the `.bak` files moved aside in step 4 (`sudo rm`). The fresh
   backup from step 1 stays in `deploy/data/backups/`.

## 7. Logs

1. Follow backend logs:
   ```
   docker compose -f deploy/compose.yml logs -f backend
   ```
   The backend logs structured JSON (one object per line). A request line
   from a signed-in user looks like:
   <!-- prettier-ignore -->
   ```json
   {"time":"2026-09-14T10:00:00Z","level":"INFO","msg":"request","method":"GET","path":"/api/portfolios","status":200,"duration_ms":1,"remote_addr":"203.0.113.7","user_id":1}
   ```
   `remote_addr` is the client's address, taken from the `X-Forwarded-For`
   header that the proxies in front of the backend set: the entry
   `TRUSTED_PROXY_HOPS` places from the right (2 in `compose.yml`, for Caddy
   then nginx, each appending one address). Anything the client sent itself
   sits further left and is ignored. If another proxy or CDN is put in
   front of Caddy, make Caddy trust it (`trusted_proxies`) and raise
   `TRUSTED_PROXY_HOPS` to 3, or every request logs and rate-limits under
   that proxy's address. `user_id` is there
   only for a signed-in request. The container health check's own
   `GET /api/health` requests are logged too, with `remote_addr` set to the
   container's loopback address. `status` 499 means the client closed the
   request before it finished (e.g. the user left the page while it
   loaded); it is not an error.
   An error line (e.g. a recovered panic) looks like:
   <!-- prettier-ignore -->
   ```json
   {"time":"2026-09-14T10:00:01Z","level":"ERROR","msg":"panic recovered","path":"/api/transactions","method":"POST","error":"...","stack":"..."}
   ```
2. Follow Caddy logs — certificate issuance/renewal events show up here:
   ```
   docker compose -f deploy/compose.yml logs -f caddy
   ```
3. Follow everything at once:
   ```
   docker compose -f deploy/compose.yml logs -f
   ```

## 8. Rollback

1. Pin the images of the previous known-good commit in `deploy/.env`, by
   the first 7 characters of its SHA (`git log --oneline` lists the
   commits), or by a release:
   ```
   IMAGE_TAG=sha-<7>
   ```
   `IMAGE_TAG=v1` follows the major version (minor and patch releases arrive
   on `docker compose pull`); `IMAGE_TAG=v1.4.2` pins one release. Releases
   are cut by the `release` workflow in Actions and listed on the GitHub
   Releases page (<https://github.com/dogauzun/gelirgider/releases>).
   A merge that landed while another publish run was queued may have no
   `sha-<7>` tag: GitHub keeps one pending run per concurrency group and
   drops the older one, so that commit's images were never built. Pick the
   latest `sha-` tag that exists on the package page
   (<https://github.com/dogauzun/gelirgider/pkgs/container/gelirgider-backend>),
   or `latest`. A weekly prune keeps only the newest 30 versions of each
   image, so a commit older than that has no image: roll back by rebuilding
   from the checkout, as in the last paragraph below.
2. Pull and restart:
   ```
   docker compose -f deploy/compose.yml pull
   docker compose -f deploy/compose.yml up -d
   ```
3. Once a fixed version is published, empty the `IMAGE_TAG=` line to follow
   `latest` again, and pull and restart as in step 2.

A checkout with local code changes skips the steps above and rolls back by
checking out the previous commit (`git checkout <previous-sha>`) and rebuilding with
`docker compose -f deploy/compose.yml up -d --build`.

Tags published before multi-arch support are `linux/amd64` only, so an arm64
host rolls back to one of them the same way, by rebuilding from the checkout at
that commit with `up --build`, not by pinning `IMAGE_TAG`.

Schema files are forward-only and are never edited or removed once merged
(see Update, above). Rolling back the code does not undo a schema file that
already ran against the live database. An older backend binary refuses to
start against it: it exits at startup with `schema file <name> was applied
by a newer version of gelirgider; run that version or restore an older
backup`, instead of running against a schema it does not know. If the
rollback needs to cross a schema file, restore the backup taken before that
schema file was applied instead (Restore drill, above), then pin the
matching images.

## 9. Reset a forgotten password

There is no self-service reset: the app cannot send email yet, so a
"forgot password" link would have nowhere to send a token. The backend
binary has an operator escape hatch instead (GLG-124). It reads the new
password from stdin — never an argument — so it stays out of the process
list, and it exits without starting a server.

1. Set the password. The leading space keeps the command out of shell
   history where the shell is configured for it (bash `HISTCONTROL=ignorespace`,
   zsh `setopt histignorespace`):
   ```
    echo 'the-new-password' | docker compose -f deploy/compose.yml exec -T backend /server -set-password alice@example.com
   ```
   On success it prints:
   ```
   password set for alice@example.com, all its sessions deleted
   ```
   The minimum length is 8 characters, the same as registration. An unknown
   email or a too-short password prints the reason and exits 1, changing
   nothing.
2. Every session of that user is deleted, so a browser still logged in as
   them is signed out. Other users are untouched.
3. Pass the new password to the user over a channel they control and have
   them change it themselves from the Hesap (Account) page.

Anyone who can run this command can take over any account — it is the same
level of trust as having the database file, so it stays an operator-only
tool on the host.

It is also the escape hatch if every admin is locked out: reset an admin's
password from the host and log in again. It does not change the admin flag.
