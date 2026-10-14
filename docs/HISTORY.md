# History

The shipped phases, in order, each mapped to a Jira epic in project `GLG`.
The decisions behind them live in the ADRs under [`adr/`](adr/).

## Phase 0 — Foundation

Status: shipped 2026-09-13 · Epic GLG-1

Goal: `make dev` runs an empty app end to end, and the checks pass. Until
2026-10-14 the checks ran only locally, before merge
([ADR 0015](adr/0015-pr-review-routine.md)).

- Monorepo skeleton: `backend/`, `frontend/`, `tools/`, `deploy/`, `docs/`, root `Makefile` (GLG-8, GLG-9, GLG-13).
- Go module with `net/http` server, `/api/health`, structured logging, env config (GLG-8).
- SQLite via `modernc.org/sqlite`, repository layer, first schema file (`users`) (GLG-10).
- React + TypeScript + Vite app with Tailwind, TanStack Query, router; dev proxy to backend (GLG-9).
- GitHub Actions CI for backend and frontend (GLG-11), since removed: the backend and frontend jobs went in GLG-257, the `tools/tefas` job in GLG-305 and the last workflows in GLG-580, so all checks ran locally until GitHub Actions returned on 2026-10-14 (see "Public repository"), and review comes from a review agent the merger spawns ([ADR 0022](adr/0022-pr-review-agent.md)).
- Dockerfile for backend; static build for frontend (GLG-12).

## Phase 1 — Users and auth

Status: shipped 2026-09-13 · Epic GLG-2 · ADR 0019

Goal: multiple users can sign up and only see their own data.
The decisions behind this phase are recorded in
[ADR 0019](adr/0019-core-engine-and-auth.md).

- Register, login, logout with session cookies; passwords hashed with bcrypt (GLG-14, GLG-16).
- Auth middleware; `user_id` scoping helper used by every repository (GLG-15).
- Minimal account page (change password) (GLG-17).
- Rate limiting on auth endpoints (GLG-18).

## Phase 2 — Portfolio core (BIST, manual)

Status: shipped 2026-09-13 · Epic GLG-3 · ADR 0019

Goal: track a BIST portfolio by hand with correct cost basis and P/L.
The decisions behind this phase are recorded in
[ADR 0019](adr/0019-core-engine-and-auth.md).

- Instruments table seeded with BIST symbols (importer in `tools/`) (GLG-23).
- Portfolios per user (one default, more allowed) (GLG-24, GLG-29).
- Transactions: buy, sell, dividend, cash deposit, cash withdrawal, fee. CRUD API and UI (GLG-25, GLG-30).
- Holdings engine: quantity, average cost, realized and unrealized P/L, cash balance. Tested with fixtures (GLG-27).
- Manual price entry per instrument and date (GLG-26, GLG-32). Since GLG-103 a manual price belongs to the user who entered it, and on the same date it wins over the fetched close.
- Portfolio summary page and transaction list (GLG-28, GLG-30, GLG-31).

## Phase 3 — Prices and history

Status: shipped 2026-09-13 · Epic GLG-4 · ADR 0001, 0019

Goal: valuation updates itself.
Decisions taken when planning this epic, recorded since in
[ADR 0019](adr/0019-core-engine-and-auth.md): no price provider interface,
and none arrived with the later markets either, since each provider is one
package with a concrete fetcher
([ADR 0003](adr/0003-multi-currency-and-market-data-sources.md)); the
fetcher writes the shared `prices` table with its own `source`, while manual
entry, which first wrote the same table, writes the per-user `manual_prices`
table since GLG-103 (`backend/internal/price/manual.go`); no snapshot table or
job, value history is computed on request from transactions and price history.

- Spike: choose the BIST end-of-day price source, record it in `docs/adr/0001-bist-price-source.md` (GLG-33). Chosen: Borsa Istanbul daily bulletin CSV.
- BIST end-of-day fetcher: parse one bulletin for one date into `prices` (GLG-34).
- Daily scheduler that runs the fetcher after market close with retries (GLG-35). Since GLG-303 each source has its own daily time, set in the admin area.
- History backfill: run the fetcher over past trading days (GLG-36).
- Portfolio value history API, computed on request from transactions and prices (GLG-37).
- Value-over-time chart (GLG-38).
- Allocation bar list (GLG-39), since replaced by one stacked bar with a legend (GLG-161) that switches between symbol, market and currency views (GLG-553), and since Phase 27 by a donut beside a legend with amounts (GLG-947).

## Phase 4 — Production readiness

Status: shipped 2026-09-14 · Epic GLG-5 · ADR 0002, 0019, 0020

Goal: deployable with Docker Compose on any Docker host, with real users.
Decisions taken when planning this epic, recorded since in
[ADR 0020](adr/0020-deployment-topology.md): SQLite stays as the production
datastore and schema migrations stay as plain `.sql` files with no tool (both
in `docs/adr/0002-production-datastore-and-migrations.md`); no external error
reporting service, panic recovery plus structured logs to stdout only; Caddy in
front of the existing nginx image for TLS; images are built on the Docker host
from a git checkout, no container registry (the repo was private at the
time, a registry would have needed pull credentials on the Docker host for no
gain); backups are a `VACUUM INTO` copy triggered by a host cron; copying them
off the host is the operator's job.

- Planning docs: ADR 0002 and this roadmap section (GLG-41).
- Schema files applied one per transaction, with a checksum guard against editing an already applied file (GLG-42).
- Panic recovery middleware, database-aware `/api/health`, client IP in request logs (GLG-43).
- `-backup <path>` flag on the backend binary using `VACUUM INTO` (GLG-44).
- Production compose: Caddy TLS in front of nginx, restart policies, health checks, log rotation, secure cookies and trusted proxy, database on a bind mount (GLG-45).
- Runbook for the compose deployment: deploy and update, backup script and cron, restore drill, rollback (GLG-46).

## Phase 5 — More markets

Status: shipped 2026-09-14 · Epic GLG-6 · ADR 0003, 0004, 0019

Goal: US stocks and TEFAS funds alongside BIST.
Decisions taken when planning this epic, recorded in
`docs/adr/0003-multi-currency-and-market-data-sources.md` and, in part,
since in [ADR 0019](adr/0019-core-engine-and-auth.md): cash is kept per
currency per portfolio with no implicit conversion between balances;
positions are valued in the instrument's currency and converted to a
reporting currency per user (`users.reporting_currency`, default `TRY`) for
summary and value history; realized P/L is converted at the valuation-date
rate as a recorded simplification, not the trade-date rate (narrowed in
Phase 16); a new
`fx_rates(currency, date, rate, source)` table holds TRY-per-unit rates, no
row for `TRY`; FX rates come from the TCMB daily exchange rate XML; US
instruments are imported from the Nasdaq Trader symbol directory and priced
for tracked instruments only, the latest close from a Finnhub quote (GLG-82)
and the daily history from Tiingo (GLG-236); the scheduler runs several
sources, one daily run per source with its own retry; TEFAS was a separate
spike (GLG-71) with its own ADR 0004, not decided in ADR 0003.

- Planning docs: ADR 0003 and this roadmap section (GLG-70).
- Spike: TEFAS fund list and daily NAV source, recorded in ADR 0004 (GLG-71).
- `fx_rates` table and repository, `internal/fx` (GLG-72).
- Transactions API: currency from the instrument for trades, from the request for cash rows (GLG-73).
- Holdings engine: cash per currency, position currency, conversion to a reporting currency (GLG-74).
- Reporting currency per user: schema column, GET/PUT on `/api/me`, account page select (GLG-75).
- US instruments: importer for the Nasdaq Trader symbol directory, market `US` (GLG-76).
- TCMB FX fetcher: parse one day's exchange rate XML into `fx_rates`, `internal/tcmb` (GLG-77).
- Scheduler runs multiple sources; wire TCMB FX into the daily job, `-fetch-fx` flag, FX backfill (GLG-78).
- Summary API in the user's reporting currency: cash per currency, position currency, converted totals (GLG-79).
- Value history API in the user's reporting currency using per-day FX rates (GLG-80).
- Frontend: transaction form currency select for cash rows, currency column, market badge in instrument search (GLG-81).
- US price fetcher: Finnhub quote for the latest close of tracked US instruments, `internal/us` (GLG-82); daily history from Tiingo (GLG-236).
- TEFAS fund import: seed market `TEFAS` instruments per ADR 0004 (GLG-83).
- Frontend: summary page shows cash per currency, position currency, totals in the reporting currency (GLG-84).
- Wire the US price fetcher into the daily job, `-fetch-us` flag, per-symbol backfill (GLG-85).
- TEFAS NAVs for tracked funds, per ADR 0004: first fetched by a standalone crawler on a host cron (GLG-87, GLG-109), and since Phase 11 by the backend scheduler source `tefas` (GLG-299), with the crawler removed (GLG-305).

## Phase 6 — Convenience: provider dividends and English UI

Status: shipped 2026-09-23 · Epic GLG-7 · ADR 0009, 0021

Goal: less manual work. The phase shipped dividends from provider data and the
English UI; the email alerts and the public share link first planned here were
dropped (GLG-284).
Decisions taken when planning this epic, recorded in
`docs/adr/0009-dividends.md`: provider dividends are market data, not user
data, in one global `dividends(instrument_id, date, amount, currency, source)`
table with no `user_id`, like `splits`; `date` is the ex-date, `amount` is the
gross cash per share as exact decimal TEXT, last writer wins; providers never
create user transactions, `GET /api/portfolios/{id}/dividends/expected`
suggests quantity held before the ex-date times per share and the user adds
each one with a click as a normal `dividend` transaction, chosen over
synthetic rows like splits and over auto-created transactions because of
double counting against dividends entered by hand or imported from Midas;
`amount` holds the gross and `fee` the stopaj, the Midas import convention
(ADR 0006), with stopaj pre-filled at a fixed rate per market (BIST 15%, US
20% treaty rate, others 0), editable, and the fee field labelled Stopaj /
Withholding tax for dividends; BIST dividends come from the cash dividend
columns (`SHHE_NAKIT_TM_ORAN` / 100) of the İş Yatırım capital increase table
GLG-282 already fetches, same fetch and same `/_layouts/` robots.txt limit, so
current calendar year only; US dividends come from Tiingo `divCash` on the
existing history call, carried by the backfill and the daily recent pass;
robots.txt checked 2026-09-23 for `www.isyatirim.com.tr`, `api.tiingo.com` and
`finnhub.io`; TEFAS, CRYPTO and, since Phase 18, GOLD have no source; a
suggestion is hidden when the portfolio already has a dividend for that
instrument dated ex-date −7 to +45 days, and the user can dismiss one
(per-user `dividend_dismissals`, no undo). Recorded simplifications: the
transaction date is the ex-date, not the payment date, since neither source
gives one; stopaj rates are fixed per market, with no per-instrument rate for
ETFs or funds; the quantity counts every row dated before the ex-date,
including splits. Not in this epic: stock dividends (bonus issues are splits),
dividend reinvestment, fund distributions, crypto staking rewards, payment
dates, an undo for dismissals.
Two items first listed here shipped in phases of their own: broker statement
import in Phase 8 (GLG-198), read from the monthly PDF statement because the
broker offers no CSV export, with a generic CSV import added in Phase 21
(GLG-597), and split handling in Phase 10 (GLG-261).

- English UI alongside Turkish (GLG-146, GLG-147, GLG-149, GLG-150), recorded in [ADR 0021](adr/0021-product-and-ui-decisions.md).
- Planning docs: ADR 0009 and this roadmap section (GLG-291).
- Schema: global `dividends` table and `dividend.Repo` (GLG-285).
- BIST cash dividends from the İş Yatırım capital increase table (GLG-286).
- US dividends from Tiingo `divCash` on the existing history fetch (GLG-287).
- Label the dividend fee field Stopaj / Withholding tax instead of Komisyon (GLG-288).
- Expected dividends API per portfolio with stopaj pre-filled (GLG-289).
- Dismiss an expected dividend (GLG-290).
- Frontend: expected dividends panel with add, add all and dismiss (GLG-292). It was on İşlemler and lives on the Temettü tab since Phase 21 (GLG-605).

## Phase 7 — UI/UX (Defter)

Status: shipped 2026-09-15 · Epic GLG-155 · ADR 0005

Goal: one visual system for the whole app, in light and dark.
Decisions taken when planning this epic, recorded in
`docs/adr/0005-ui-design-system.md`: one set of semantic color tokens in
`frontend/src/index.css` for light and dark, and components use those tokens
only — no raw Tailwind palette classes and no `dark:` variants, so dark mode
holds by construction; the theme choice (system / light / dark) lives in
`localStorage` and on `<html data-theme>`, applied before first paint,
browser-only like the locale in GLG-146; IBM Plex Sans/Mono self-hosted as
woff2 under `frontend/public/fonts` with tabular numerals everywhere, chosen
over the system font stack; no component library and no charting library,
just thin primitives under `frontend/src/components/ui/`; dialogs use the
native `<dialog>` element with `showModal()` and become bottom sheets on
phones, replacing `window.confirm`; transaction amounts are shown with the
cash-flow sign as presentation only, the API amount stays unsigned;
`GET /api/portfolios` gains `unrealized_pl` and `cash` so the portfolio cards
can show them. Visual reference: a private design canvas, not published.

- Planning docs: ADR 0005 and this roadmap section (GLG-174).
- Design tokens for light and dark, IBM Plex fonts, base styles (GLG-156).
- Brand mark favicon and `Mark` component, delete the unused icon sprite (GLG-157).
- `GET /api/portfolios` returns `unrealized_pl` and `cash` (GLG-158).
- UI primitives: Button, Field, Notice, Pill, Badge, Skeleton, Icon (GLG-159).
- LineChart: area fill, dashed cost basis, tooltip, compact axis, theme colors (GLG-160).
- Allocation as one stacked bar with a legend (GLG-161).
- ConfirmDialog and Toast on the native dialog element (GLG-162).
- Auth pages: brand block, 44px fields, password reveal (GLG-163).
- Positions table restyle and phone position list (GLG-164).
- Account page settings layout with toast feedback (GLG-165).
- App shell: header, account menu, toaster mount (GLG-166).
- Transaction add/edit in a native dialog with a live cash total (GLG-167).
- Portföyler page: hero, cards, create dialog, empty state (GLG-168).
- Özet KPI card, chart header, notices, skeletons, empty state (GLG-169).
- Transactions table: month groups, pills, cash-flow signs, phone list (GLG-170).
- Theme switch: system, light, dark (GLG-171).
- Portfolio sub-header with switcher and tabs (GLG-172).
- Transactions type filter, server-side, with chips (GLG-173).

## Phase 8 — Broker statement import

Status: shipped 2026-09-21 · Epic GLG-198 · ADR 0006

Goal: a user uploads the monthly PDF account statement their broker emails
them and the app turns it into transactions, instead of typing every trade by
hand. Midas was the first broker, and each further one is one new parser
package: the import now takes four broker keys, `midas`, `csv` (Phase 21),
`garanti` and `garanti_yatirim` (Phase 22).
Decisions taken when planning this epic, recorded in
`docs/adr/0006-broker-statement-import.md`: PDF text is extracted with the
pure-Go `github.com/ledongthuc/pdf` (`GetTextByRow` returns one string per
table cell on both sample statements), so no poppler in the Docker image and
no OCR; two layers, a broker-neutral `pdftext` wrapper plus one pure parser
package per broker (rows in, `Statement` out, no DB and no HTTP); parsers are
partial, a row that cannot be read becomes a `ParseError` and everything else
still parses, only "not this broker's statement" is fatal, so the user
finishes the last rows by hand in the preview; the flow is two steps,
`POST /api/imports/parse` returns a preview and writes nothing while
`POST /api/imports` commits the reviewed rows in one DB transaction; every
commit is recorded in a `statement_imports` table keyed by the file sha256 so
the same file cannot be imported twice, and each created transaction carries
`import_id` so an import can be undone with one call; a different file of
the same statement (same broker, portfolio, statement currency and exact
period) is refused while the earlier import exists, because Midas
regenerates the PDF on every download (GLG-361), and an empty monthly
statement is recorded as an import with no transactions, so the month shows
in the history and a re-upload is caught (GLG-360); the Midas mapping with
its deliberate simplifications (a Döviz Alış/Satış row books only its
own-currency leg, as a deposit or a withdrawal, since Midas prints the
conversion on both the TRY and the USD statement, GLG-358, GLG-363;
`Ücretsiz İşlem` skipped until a real example shows its cash effect,
PORTFÖY ÖZETİ used for reconciliation only, non-executed orders skipped,
column header rows optional per section); instruments are matched by symbol
within a market derived from the row currency (USD → `US`, TRY → `BIST`)
with the bulletin suffix `.E`/`.F` stripped, and the import creates no
instruments, except since Phase 22 a TEFAS fund that TEFAS confirms
(GLG-699); BIST ETFs are a
prerequisite (GLG-208); and customer PII (name, TCKN, address, IBAN) is never
stored or logged, parser tests use anonymized row fixtures and no real
statement enters the repo.

- Planning docs: ADR 0006 and this roadmap section (GLG-199).
- `internal/statement` types and the `pdftext` row extractor over `ledongthuc/pdf`, plus `backend/cmd/statement_dump` (GLG-200).
- Schema: `statement_imports` table, `transactions.import_id`, import repository (GLG-201).
- Midas parser: `internal/statement/midas` turns rows into a `Statement` (GLG-202).
- `POST /api/imports/parse`: upload a statement PDF, get a preview of proposed transactions (GLG-203).
- `POST /api/imports`: commit the reviewed rows in one DB transaction, plus list and delete (GLG-204).
- Frontend: statement import page with upload, preview table and commit (GLG-205).
- Frontend: past imports list with undo, imported pill on transactions (GLG-206).
- Reconcile the statement's period-end holdings against the portfolio (GLG-207).
- BIST ETFs: bulletin `ETF` group (`.F`) in the symbol seed and the daily price fetcher (GLG-208).
- Extract `TransactionDialog` and `InstrumentSearchInput` from `TransactionsPage` (GLG-209).
- Frontend: editable preview — fix error rows, pick instruments, add missing operations (GLG-210).
- Frontend: pick several statements at once, reviewed and committed one by one in period order (GLG-577).

## Phase 9 — Crypto

Status: shipped 2026-09-23 · Epic GLG-245 · ADR 0007

Goal: track crypto holdings (BTC, ETH, ...) next to BIST, US and TEFAS,
priced in TRY or USD, with values that update every day including weekends.
Decisions taken when planning this epic, recorded in
`docs/adr/0007-crypto.md`: the source is the BtcTurk public API with no API
key, `api/v2/server/exchangeinfo` for the pair list and `graph-api`
`v1/klines/history` with `resolution=1D` for daily candles, full history in
one request per pair (robots.txt checked 2026-09-23: `api.btcturk.com` and
`graph-api.btcturk.com` 404, `www.btcturk.com` `Allow: /`); CoinGecko,
Binance, Binance TR, Coinpaprika, CryptoCompare, Kraken and Coinbase were
rejected; market `CRYPTO` with one instrument per pair, symbol `BASE-QUOTE`
(`BTC-TRY` in `TRY`, `BTC-USD` in `USD`), named after the base currency;
USD pairs are priced from BtcTurk's `USDT` pairs treating 1 USDT as 1 USD, a
recorded simplification whose ceiling is a USDT depeg; USD pairs convert
through the existing TCMB `fx_rates` path, no FX work; only the close of
finished daily candles is stored, under the candle's UTC date, with
`source = 'btcturk'`, decoded with `json.Number` and never through
`float64`, for tracked pairs only; the scheduler runs every day and only
sources marked for every day run on weekends, which are `crypto` and, since
Phases 13 and 18, `tufe` and `gold`; per-unit prices (last
price, average cost) are no longer rounded to 2 decimals, since 37 of 190
TRY pairs trade below 1 TRY. Not in this epic: other quote currencies,
crypto statement import, wallet transfers, fees paid in coin, staking or
airdrops as a new transaction type, intraday or live prices.

- Planning docs: ADR 0007 and this roadmap section (GLG-246).
- Crypto instruments: importer for BtcTurk TRY and USDT pairs, seed market `CRYPTO` (GLG-247).
- `internal/btcturk`: daily crypto close fetcher for tracked `CRYPTO` pairs (GLG-248).
- Scheduler: run every day, let a source opt in to weekends (GLG-249).
- Per-unit prices at full precision: last price and average cost below 1 are not 0.00 (GLG-250).
- Frontend: Kripto/Crypto market label in instrument search, tagline mentions crypto (GLG-251).
- Wire crypto prices into the daily job, `-fetch-crypto` flag, and backfill (GLG-252).
- On-demand crypto price fetch after a transaction is saved (GLG-253).

## Phase 10 — Splits and bonus issues

Status: shipped 2026-09-23 · Epic GLG-261 · ADR 0008

Goal: a stock split or a BIST bonus issue (bedelsiz sermaye artırımı) no
longer corrupts holdings: quantity, average cost and value history stay
right across the ex-date.
Decisions taken when planning this epic, recorded in
`docs/adr/0008-splits.md`: splits are market data, not user data, in one
global `splits(instrument_id, date, ratio, source)` table unique on
`(instrument_id, date)` with no `user_id`, like `prices`; users do not enter
splits, providers do, with an operator CLI flag as the fallback; the ratio is
new shares per old share as exact decimal TEXT, greater than 0 (2-for-1 = `2`,
50% bonus issue = `1.5`, 1-for-10 reverse = `0.1`); the date is the ex-date
and the split applies before any transaction on that date; the engine
multiplies quantity by the ratio and divides average cost by it, leaving cost
basis and realized P/L unchanged, while prices stay unadjusted;
`transaction.Repo.List` is the one choke point and returns synthetic `split`
rows for the portfolio's instruments, ordered date, split first, then id;
`split` is not in `validTypes`, so the transactions API rejects it and the
paged list never shows it; US splits come from Tiingo's `splitFactor` on the
existing history call plus a daily recent-window pass, since Finnhub's latest
close carries no split data; the BIST source was settled by the spike GLG-265:
bonus issues come from the capital increase table on İş Yatırım's "Temel
Değerler ve Oranlar" page, read as server-rendered HTML, current calendar year
only (ADR 0008); CRYPTO, TEFAS and, since Phase 18, GOLD never split. Recorded
simplifications: fractional shares are kept (BIST's cash payment for the
fraction is not modelled), a zero-price buy typed to mimic a bonus issue
double counts once the split lands, last writer wins between sources for the
same `(instrument_id, date)`. Not in this epic: dividends from provider data
(shipped in Phase 6), rights issues (bedelli), spin-offs, split rows in the
statement import, user-entered splits.

- Planning docs: ADR 0008 and this roadmap section (GLG-262).
- Schema: `splits` table and `split.Repo` (GLG-263).
- Holdings engine: apply a split row, quantity times ratio, average cost divided by ratio (GLG-264).
- Spike: BIST split and bonus-issue data source (GLG-265).
- Holdings read path: `transaction.Repo.List` returns split rows for the portfolio's instruments (GLG-268).
- US splits from Tiingo `splitFactor` on the existing history fetch (GLG-269).
- Operator flag `-add-split` to record a split by hand (GLG-271).
- Summary API: list the splits that changed each position (GLG-273).
- Frontend: split badge on positions with the split date and ratio (GLG-276).
- Daily US split pass: recent Tiingo window per tracked US instrument (GLG-279).
- BIST bonus-issue fetcher from the İş Yatırım capital-increase table, daily `bist-splits` source (GLG-282).

## Phase 11 — First-time setup and admin

Status: shipped 2026-09-24 · Epic GLG-296 · ADR 0010

Goal: a fresh install walks the operator through a first-time setup that
creates a system admin; the admin sets price provider API keys, which
sources run, when they refresh and backfill, all from the UI, with no env
vars or CLI flags for that provider configuration. Deployment configuration
such as `SECRET_KEY` and `PORT` still comes from environment variables
(PRINCIPLES #7), and the operator CLI flags such as `-fetch-tefas`,
`-add-split` and `-backup` remain.
Decisions taken when planning this epic, recorded in
`docs/adr/0010-setup-and-admin.md`: while no admin exists the server prints a
one-time setup token to its log at startup, kept in memory only, and `/setup`
takes the token plus the admin email and password; admin is an `is_admin`
flag on a normal user, read from the DB per request, and the last admin
cannot be demoted or deleted; install-wide config lives in one global
key/value `settings` table and fetch runs in a `source_runs` log, both with no
`user_id` because they hold no user data, like `prices`; provider keys,
Finnhub and Tiingo and since Phase 13 TCMB EVDS (GLG-517), are AES-GCM
encrypted with a `SECRET_KEY` env var the server needs to start, the
API returns only `configured` and the last 4 characters, and losing the key
means entering the provider keys again; `FINNHUB_API_KEY`, `TIINGO_API_KEY`,
`PRICE_FETCH_ENABLED` and `PRICE_FETCH_AT` are removed with no migration;
registration is an admin toggle, open by default; provider selection is
enable/disable per source, six in this epic (`bist`, `bist-splits`, `tcmb`,
`us`, `crypto`, `tefas`) and nine since `tufe` and `xu100` came in Phase 13
and `gold` in Phase 18 (`backend/cmd/server/main.go`), each with its own
daily time in Europe/Istanbul, plus global catch-up, backfill and
backfill-from settings, all applied live with no restart; the wizard is
admin account first, then optional keys, sources and registration reusing
the admin page sections; TEFAS moved into the backend as a normal scheduler
source, so `tools/tefas`, its deploy files, its host cron and
`.github/workflows/ci.yml` were deleted; the `tefas` source stayed on by
default until Phase 31 (GLG-1114), since when it is off by default because
tefas.gov.tr's robots.txt disallows its endpoint, and the operator who turns
it on decides (ADR 0004). Recorded simplifications: a
dev DB with users but no admin also goes through setup, `make dev` fills no US prices until keys are entered,
the setup token is not persisted. Not in this epic: invite links, alternative
providers per market, per-source intraday intervals, an audit log of admin
actions. CI for backend and frontend was decided against: the checks run
locally ([ADR 0015](adr/0015-pr-review-routine.md)).

- Admin flag on users, `RequireAdmin` middleware, `is_admin` in `/api/me` (GLG-297).
- `settings` table and repository with AES-GCM encrypted secrets, `SECRET_KEY` (GLG-298).
- TEFAS fund prices as a backend scheduler source, `internal/tefas`, `-fetch-tefas` (GLG-299).
- Setup API: one-time setup token in the log and first admin creation (GLG-300).
- Admin toggle for open or closed registration (GLG-301).
- Provider API keys in encrypted settings; drop `FINNHUB_API_KEY` and `TIINGO_API_KEY` (GLG-302).
- Scheduler reads per-source enabled and fetch time from settings, live; drop `PRICE_FETCH_ENABLED` and `PRICE_FETCH_AT` (GLG-303).
- User management API: list, create, reset password, promote and demote (GLG-304).
- Remove the `tools/tefas` crawler, its deploy files and the tefas CI workflow (GLG-305).
- Frontend: `/admin` area shell and nav entry for admins (GLG-306).
- Admin API for source and backfill schedule settings (GLG-307).
- Test API key endpoint for Finnhub and Tiingo (GLG-308); the TCMB EVDS key and its test followed in Phase 13 (GLG-517).
- Source run log, `source_runs`, and status fields in `GET /api/admin/sources` (GLG-309).
- Admin delete user with all of their data (GLG-310).
- Setup wizard step 1: `/setup` page with token and admin account (GLG-311).
- Admin UI: data provider keys section with test button (GLG-312).
- Admin UI: users section with registration toggle (GLG-313).
- Admin run now and backfill now endpoints (GLG-314).
- Admin UI: sources section with schedule, status panel and run buttons (GLG-315).
- Setup wizard step 2: optional provider keys, sources and registration (GLG-316).
- Planning docs: ADR 0010, this roadmap section and the RUNBOOK (GLG-317).
- PRINCIPLES #7: install-wide settings live in the DB, not env (GLG-318).

## Phase 12 — Portfolio returns

Status: shipped 2026-09-26 · Epic GLG-388 · ADR 0011

Goal: the chart header shows an honest return for the selected range, as
simple, time-weighted or money-weighted, instead of one modified Dietz
percentage computed in the frontend.
Decisions taken when planning this epic, recorded in
`docs/adr/0011-portfolio-returns.md`: three period returns for the selected
range, simple (gain over the start value plus net flows), time-weighted (TWR)
and money-weighted (MWR, the true internal rate of return, not Dietz); period
returns only, with no annualization in the API (since Phase 21 the chart
header annualizes in the frontend, ADR 0011); the method is picked with a
selector on the chart header only and remembered in `localStorage` like the
theme and the locale, and the home cards and the KPI card are unchanged; the
backend computes all three inside the existing history sweep and returns
`returns: {simple, twr, mwr}` on `GET /api/portfolios/{id}/history`, each a
fraction string with 6 decimals or `null`, chosen over the frontend because
the flow rules live in the sweep; the ratios run in float64 from exact
decimals converted once, since a return is a ratio, not money, and chaining
thousands of exact decimal factors grows the mantissa without bound; the
inputs are the exact total value per day and the external flow per day
(deposit minus withdrawal, gross, plus the GLG-254 first-price jumps; fees,
buys, sells, dividends and splits are not flows), flows are end-of-day, a
first-day flow is inside the start value, and a carried flow counts on the day
it converts; the MWR is solved by bisection and makes `net_flow_weighted`
redundant. Since GLG-641 all three returns are `null` without a positive base,
that is when the range's start value is not positive or the day's cash in the
reporting currency is negative on any day of the range. Recorded
simplifications: Midas "Diğer Gelir" interest was imported as a deposit and so
counted as a flow, understating all three returns, until Phase 15 made it
income (GLG-533); multiple IRR roots are not handled. Not in this epic:
annualized returns (shipped in Phase 21, in the frontend), per-position
returns, returns on the home portfolio cards, benchmark comparison (shipped in
Phase 13).

- Planning docs: ADR 0011, this roadmap section, ADR 0005 fix (GLG-394).
- Pure return functions: simple, TWR and MWR (IRR) over a value and flow series, `returns.go` (GLG-390).
- History API: compute the three returns in the sweep, `returns` on the response (GLG-391).
- Frontend: return method selector on the chart header (GLG-392).
- Remove `net_flow_weighted` once the frontend reads `returns` (GLG-393).

## Phase 13 — Benchmarks

Status: shipped 2026-09-29 · Epic GLG-486 · ADR 0012

Goal: on the Özet chart the user can compare the portfolio against one
benchmark, USD/TRY, EUR/TRY, BIST 100 (XU100) or Turkish CPI (TÜFE), to
answer "did I beat the dollar / the index / inflation with the money I
actually put in".
Decisions taken when planning this epic, recorded in
`docs/adr/0012-benchmarks.md`: the benchmark is a shadow portfolio that
receives the same external flows on the same days as the real one (the Phase
12 `dayExternal` flows), all invested in the benchmark, drawn as a dotted line
so it never reads as the dashed cost line; its return uses the same
`periodReturns` and the method the user already picked, so its TWR is the
benchmark's own price change; one benchmark at a time, picked with a selector
next to the return method and remembered in `localStorage` (key `benchmark`),
home cards and the KPI card unchanged; every benchmark is a currency plus a
daily level converted into the reporting currency with the sweep's FX rates,
and USD (or EUR) is hidden when it is the reporting currency; USD and EUR come
from `fx_rates`, XU100 and TÜFE from a global
`benchmark_values(code, date, value, source)` table with no `user_id`; the two
spikes picked their sources, XU100 from TCMB EVDS series `TP.MK.F.BILESIK`
with the user's EVDS API key (GLG-492, GLG-517, GLG-518, with the startup
backfill GLG-644 and GLG-648) and TÜFE from the TÜİK data portal's file
download with no key (GLG-493, GLG-515); TÜFE's monthly index is the
end-of-month level spread geometrically over the month, extrapolated at the
last monthly rate past the last published month and marked with
`estimated_from`; shadow values and CPI levels are float64, rounded to 2
decimals, real money stays decimal; a benchmark with no level on or before the
range's first point is not shown for the range, later gaps carry forward; the
header also shows the excess `(1 + p) / (1 + b) − 1`, the real return against
TÜFE. Recorded simplifications: monthly CPI as an end-of-month level,
extrapolation at the last monthly rate, a benchmark missing at the range start
hidden for the whole range, shadow units may go negative. Not in this epic:
several benchmarks at once, gold (shipped in Phase 18), US CPI or other
foreign indices, benchmarks on the home cards or per position, annualized
returns (shipped in Phase 21), custom user benchmarks (narrowed in Phase 22,
where any instrument with a transaction in the scope can be the benchmark).

- Planning docs: ADR 0012 and this roadmap section (GLG-487).
- Schema: `benchmark_values` table and `benchmark.Repo` (GLG-488).
- Pure shadow-portfolio function (GLG-489).
- Pure TÜFE daily levels from monthly CPI (GLG-490).
- LineChart: optional dotted benchmark line (GLG-491).
- Spike: BIST 100 (XU100) daily close source (GLG-492). Chosen: TCMB EVDS series `TP.MK.F.BILESIK`, which needs a free EVDS API key.
- Spike: TÜFE monthly CPI source (GLG-493). Chosen: the TÜİK data portal's file download, with no key.
- TÜFE fetcher as the scheduler source `tufe` (GLG-515).
- TCMB EVDS API key in the provider settings, with a test button (GLG-517).
- XU100 fetcher as the scheduler source `xu100` (GLG-518), with its history backfilled at startup (GLG-644) and stored closes skipped (GLG-648).
- History API: `?benchmark=` shadow portfolio for USD and EUR, `benchmark` on the response (GLG-494).
- History API: XU100 and TÜFE benchmarks from `benchmark_values` (GLG-495).
- Frontend: benchmark selector on the chart header with the benchmark's return and the excess (GLG-496).

## Phase 14 — API docs (Swagger)

Status: shipped 2026-09-28 · Epic GLG-509 · ADR 0013

Goal: every `/api/*` route has a machine-readable description, for
developers and agents, so request and response shapes and error strings can
be read without going through the Go handlers or `frontend/src/api.ts`.
Decisions taken when planning this epic, recorded in
`docs/adr/0013-api-docs.md`: the spec is generated from swaggo annotation
comments on the handlers by swag v1.16.6, run with `go run` so swag stays out
of `go.mod`, as Swagger 2.0 JSON and YAML into `backend/apidocs/`;
`make swagger` generates it, and `make lint`, `make dev` and
`make dev-backend` run it first so a broken annotation fails there; the spec
is gitignored and never committed, so parallel annotation tickets do not
conflict and no stale copy exists; the backend serves `/api/docs/` only when
`API_DOCS_DIR` is set, which `make dev` does and prod never does, with
Swagger UI loaded from the jsDelivr CDN so no UI asset ships in the binary;
the session cookie, the admin 403, the `{"error": ...}` envelope, the body
cap and the 429 are described once in the general info, with no security
definitions since Swagger 2.0 has no cookie scheme; one tag per resource,
everything under `/api/admin/` tagged `admin`; each `@Failure` lists the
exact error strings the frontend keys off; no code is refactored for docs; a
drift test compares route registrations with `@Router` annotations and never
reads the generated spec. Not in this epic: OpenAPI 3 (needs swag v2), a
committed spec, API docs in production.

- `make swagger`, dev-only `/api/docs/` UI, general info, health annotation and ADR 0013 (GLG-510).
- Annotate auth, setup and user-admin routes (GLG-511).
- Annotate portfolio, transaction, instrument, price and statement import routes (GLG-512).
- Annotate the `server` package routes, including the inline closures in `schedule.go` and `providers.go` (GLG-513).
- Drift test: every registered `/api/*` route has a matching `@Router` annotation (GLG-514).
- Planning docs: this roadmap section (GLG-516).

## Phase 15 — Quick wins

Status: shipped 2026-09-29 · Epic GLG-519 · ADR 0006, 0011, 0021

Goal: twelve small improvements picked from the 2026-09-28 feature
brainstorm, each shipping on its own: interest recorded as income instead of
as a deposit, daily change and a stale-price badge on Özet, allocation by
market and currency, a YTD chart range, buy and sell buttons on a position
row, smarter duplicate detection and symbol picking in the import preview,
adding an instrument from the admin page, a transactions CSV export, a
privacy mode and PWA installability. No new data sources and no new tables.
Decisions taken when planning this epic, recorded here and as amendments to
`docs/adr/0011-portfolio-returns.md` and
`docs/adr/0006-broker-statement-import.md`, with no new ADR at the time, and
since in [ADR 0021](adr/0021-product-and-ui-decisions.md): interest is a
`dividend` with no instrument, which the transactions API accepts with a valid
currency, and Midas "Diğer Gelir" (nema) is imported as one; the history sweep
treats it as return, not as a flow, so simple, TWR and MWR stop understating
(the Phase 12 simplification is lifted), and it shows in cash and total value
but not in the KPI realized P/L; nema rows already imported as deposits stay
as they are, the way to reclassify them is to undo that import and import the
statement again, and there is no data migration because import rows do not
keep the statement description; daily change is current quantity × (last close
− previous close); the allocation currency view splits cash by currency
(`cash_by_currency_reporting`), so USD cash counts as USD exposure; the
stale-price badge counts calendar days between `price_date` and today in
Europe/Istanbul, stale past 2 days for CRYPTO and past 7 days for everything
else, with manual prices exempt; the CSV export uses a comma separator, UTF-8
with no BOM, dot decimals and raw type codes, leaves split rows out, freezes
the columns `date,type,symbol,quantity,price,amount,fee,currency,note` and
escapes cells starting with `=`, `+`, `-` or `@` against formula
injection, note cells at first and the symbol and market cells too since
GLG-623 (Phase 21 appends a `market` column after `note`; the Phase 15
columns keep their positions); privacy mode masks money only (the format
helpers, the LineChart tooltip and axis), quantities and percentages stay
visible, and the flag lives in `localStorage` like the theme;
`POST /api/admin/instruments` is idempotent, returning 200 with the existing
instrument for a known market and symbol, and takes its allowed market and
currency pairs from `seedSources`; the PWA is a manifest, icons and
theme-color only, with no service worker. Recorded simplifications: interest
is left out of the KPI realized P/L (upgrade path in ADR 0011); daily change
ignores same-day trades; the stale-price badge has no holiday calendar. Not in
this epic: user tags or custom groupings, annualized returns (shipped in Phase
21), yield on cost, a business-day holiday calendar, an offline service
worker, masking quantities, migrating old nema deposits, BES funds, a
received-income report.

- Planning docs: this roadmap section and the interest-as-income amendments to ADR 0011 and 0006 (GLG-529).
- Transactions API: accept a dividend with no instrument as interest income, import Midas nema as one (GLG-533).
- Transaction dialog: a Faiz/Nema type, instrument-less dividends labelled as interest (GLG-538).
- Summary API: previous close and daily change per position (GLG-542).
- Summary API: `market` per position and `cash_by_currency_reporting` (GLG-543).
- Özet: daily change column and a dated KPI sub-line (GLG-552).
- Allocation: toggle between symbol, market and currency views (GLG-553).
- Özet chart: a YTD range (GLG-554).
- Özet: a stale-price pill on a position whose fetched price is old for its market (GLG-557).
- Özet: buy and sell buttons on a position row open a prefilled transaction dialog (GLG-558).
- Import preview: flag trades and cash rows that repeat a hand-entered transaction (GLG-559).
- Import preview: picking an instrument for an unknown symbol fixes every row with that symbol (GLG-560).
- Admin API: `POST /api/admin/instruments` adds an instrument by market, symbol, name and currency (GLG-561).
- Admin page: add an instrument by market, symbol, name and currency (GLG-562).
- Transactions API: export a portfolio's transactions as CSV (GLG-563).
- Privacy mode: an eye toggle in the header that masks money amounts (GLG-564).
- PWA: a manifest, icons and theme-color so the web app can be installed (GLG-565).

## Phase 16 — FX gain vs price gain per position

Status: shipped 2026-09-28 · Epic GLG-520 · ADR 0003

Goal: a foreign position's gain on the summary page splits into the part the
price earned and the part the currency earned, so a US stock that went
nowhere while USD/TRY rose reads as a currency gain, not a stock gain.
Decisions taken when planning this epic, recorded as an amendment to
`docs/adr/0003-multi-currency-and-market-data-sources.md`, no new ADR: each
open position also keeps a cost basis in the reporting currency with every
buy converted at its own date's rate (latest `fx_rates` row on or before the
date, the `fxWalk` rule), a sell reducing it in the same proportion as the
native `CostBasis` and a close resetting it; the summary API adds
per-position `fx_pl_reporting` = `CostBasis` × today's factor − trade-date
cost, while `unrealized_pl_reporting` stays the price part at today's rate,
so `market_value_reporting` − trade-date cost = `unrealized_pl_reporting` +
`fx_pl_reporting`; a buy dated before its currency's first `fx_rates` row
makes that position's `fx_pl_reporting` null, with no backward carry and no
invented rate, and the remedy is the FX backfill. Recorded simplification,
the narrowed Phase 5 ceiling: realized P/L and closed positions stay at the
valuation-date rate and their FX part stays in the portfolio `fx_pl`
(GLG-267) with the FX on cash; upgrade path is realized FX at the sell-date
rate.

- Planning docs: ADR 0003 amendment and this roadmap section (GLG-524).
- Holdings engine: keep each open position's cost basis at trade-date rates in the reporting currency (GLG-527).
- Summary API: per-position `fx_pl_reporting`, the currency part of a foreign position's gain (GLG-532).
- Summary page: show a foreign position's currency gain under its unrealized P/L (GLG-537).

## Phase 17 — All portfolios view

Status: shipped 2026-09-28 · Epic GLG-521 · ADR 0014

Goal: one read-only view of all of a user's portfolios, where every figure
equals the sum of the per-portfolio figures.
Decisions taken when planning this epic, recorded in
`docs/adr/0014-all-portfolios-view.md`: the reserved id `all` on
`GET /api/portfolios/{id}/summary` and `GET /api/portfolios/{id}/history`
(`from`, `to`, `benchmark` unchanged) and the page `/portfolios/all`, with one
scope resolver serving both handlers; every other `/api/portfolios/{id}/*`
route keeps returning 404 for `all`, so the view is read-only by
construction; one holdings pass per portfolio, then `holdings.Merge` sums per
instrument (quantity, cost basis, realized and unrealized P/L, market value,
cash per currency, `CostBasisReporting` / `CostRateMissing`) with average cost
= summed cost / summed quantity, so the combined realized P/L equals the sum
of the portfolio cards and the GLG-411 / GLG-372 same-day ordering stays
inside each portfolio (one combined pass would show 0 instead of +500 for buy
10 @ 100 in A, 10 @ 200 in B, sell 10 @ 150 in A); no new SQL, an N+1 loop
over the user's portfolios; the Phase 16 `fx_pl_reporting` and the Phase 15
`day_change` are summed per instrument after `Merge`, each portfolio's
`State` keeping `FactorOn`; no manual-price editing, buy and sell buttons,
transactions links or Phase 19 position page on `all`; transactions,
imports, expected dividends and manual-price editing stay per portfolio;
the label is "Tüm portföyler" / "All portfolios" (`portfolio.all`), since
"Tümü" is already taken; benchmarks work unchanged. Not in this epic: editing
from the view, a chosen subset of portfolios, a position page across
portfolios.

- Planning docs: ADR 0014 and this roadmap section (GLG-525).
- `holdings.Merge` and the all-portfolios scope resolver (GLG-528).
- Summary API: accept `all` as the portfolio id (GLG-530).
- History API: accept `all` as the portfolio id, returns and benchmarks included (GLG-535).
- Portfolio header: "Tüm portföyler" in the switcher and a read-only header for `all` (GLG-540).
- Summary page: render `/portfolios/all` read-only (GLG-545).
- Home page: link the total-assets card to `/portfolios/all` (GLG-547).

## Phase 18 — Gold

Status: shipped 2026-09-29 · Epic GLG-522 · ADR 0017

Goal: the user can hold gram gold and gold coins next to the rest of the
portfolio, and compare the portfolio against gold.
Decisions taken when planning this epic, recorded in
`docs/adr/0017-gold.md`: gram gold is the instrument `GOLD/GRAM` ("Gram
Altın", market `GOLD`, symbol `GRAM`) in TRY, quantity in grams, in a new
market so it stays out of BIST backfill, splits and dividends; its price is
the BtcTurk PAXG-TRY finished daily close ÷ 31.1034768 (grams per troy
ounce), in decimal, stored under the UTC candle date as in ADR 0007; each
price is written to `prices` (source `btcturk_paxg`) and to
`benchmark_values` code `GOLD`, so the benchmark reuses the XU100 code path;
a scheduler source `gold` runs every day whether or not anyone holds gold,
since the benchmark needs the series, backfills on startup and shows in the
admin sources list; GOLD is a fifth benchmark in TRY on the ADR 0012 shadow
portfolio; coins (çeyrek, yarım, tam, ata) are `GOLD` instruments with
manual prices only, not gram × weight, which ignores the mint premium; the
spike GLG-541 verified the source against BIST gold certificates and
`ALTIN.S1` and filled the ADR's Source section: from 2024-11-08, the first
certificate trade, the PAXG gram price is within 1% of the certificates on
most days and at most 3.23% apart, and neither the certificates nor
`ALTIN.S1` would give a better series. Recorded
simplifications: no fetched gold price before 2022-06-30 (earlier buys show
price unknown, manual prices can fill them, the GOLD benchmark is hidden for
ranges that start earlier), no bank spread or coin premium, the same series
stored twice. Not in this epic: other metals, gold in USD or per ounce,
fetched or derived coin prices, bank gold accounts with their spread, gold
statement import.

- Planning docs: ADR 0017 and this roadmap section (GLG-534).
- Spike: confirm BtcTurk PAXG-TRY against BIST gold certificates (GLG-541). Confirmed: the price decision stands, with the limits recorded in ADR 0017.
- `GOLD` market: seed the Gram Altın instrument, label the market in search (GLG-544).
- History API and chart header: `?benchmark=GOLD` (GLG-548).
- btcturk `SyncGold`: store the PAXG-TRY gram price in `prices` and `benchmark_values` (GLG-550).
- Scheduler: `gold` source, startup backfill, admin sources list (GLG-555).
- Seed gold coins as manually priced `GOLD` instruments (GLG-556).

## Phase 19 — Position detail page

Status: shipped 2026-09-29 · Epic GLG-523 · ADR 0016

Goal: one page per position of a portfolio, opened from its symbol on Özet,
with its figures, its value against its cost over time, its price with the
buys and sells on it, and its transactions.
Decisions taken when planning this epic, recorded in
`docs/adr/0016-position-detail.md`: a new endpoint
`GET /api/portfolios/{id}/instruments/{instrument_id}/history` answers
`{currency, points: [{date, close|null, quantity, avg_cost, cost_basis,
market_value}]}` in the instrument's own currency, one point per day, `from`
defaulting to the instrument's first transaction and `to` to today in
Istanbul; the stats come from the existing summary `Position`; the markers
and the transactions list come from
`useTransactions(portfolioId, [], instrumentId)`, and split markers from
`Position.splits`; the Özet symbol link (`InstrumentLabel`) opens the page at
`/portfolios/:id/positions/:instrumentId`, and the page keeps a "Tüm
işlemler" link to İşlemler filtered by the instrument (GLG-344); a closed
position opens too, with its realized P/L and the history up to the close;
the page is not available on the Phase 17 `all` scope, the endpoint needing
a numeric portfolio id (coordinated with GLG-545); the charts reuse
`LineChart` and the Özet ranges, through the shared `RangePicker` since
Phase 21 (GLG-606), so the Phase 15 privacy mode (GLG-564)
covers their tooltips. Recorded simplifications: no FX conversion, no
per-position returns, markers only from the transaction pages already
loaded, prices not adjusted for splits (ADR 0008). Not in this epic:
per-position IRR or TWR, a benchmark on a position, a reporting-currency
view.

- Planning docs: ADR 0016 and this roadmap section (GLG-526).
- Position history API endpoint (GLG-531).
- LineChart: optional trade markers and series labels (GLG-536).
- Move chart `RANGES` and `fromForRange` into `lib/ranges.ts` (GLG-539). In Phase 21 `fromForRange` gave way to `resolveRange` and the shared `RangePicker` (GLG-606).
- Position page: route, stats and value vs cost chart (GLG-546).
- Özet: symbol link opens the position page (GLG-549).
- Position page: price chart with trade markers and the position's transactions list (GLG-551).

## Phase 20 — UI/UX round 2

Status: shipped 2026-09-29 · Epic GLG-566 · ADR 0021

Goal: fix the layout, density and discoverability problems found by the
2026-09-28 audit of every page at desktop (1280 px) and phone (375 px) width,
in light and dark, inside the Defter system; each fix is one small frontend
ticket with no API change.
Decisions taken when planning this epic, with no ADR at the time since the
Defter design system (ADR 0005) stays as written, and since recorded in
[ADR 0021](adr/0021-product-and-ui-decisions.md): the expected dividends
panel on İşlemler is collapsed by default as a native `<details>` element
with the count and net total in its summary line, and the count badge leaves
the İşlemler tab, since it counted pending dividends and read as a
transaction count (superseded in Phase 21, when the panel moved to the
Temettü tab and opens by default, ADR 0009); on phone
the Özet KPI block is a 2-column grid with the total-value card and the last
card, realized P/L, spanning both columns, desktop breakpoints unchanged; on phone a position row shows the
last price in its right column under the P/L, and the price editor takes the
full row width only while editing; on phone the portfolio tab row sticks
under the app header, then three tabs, Özet, İşlemler and İçe aktar, and
four since Temettü joined them in Phase 21; the language switch leaves the
signed-in app header, the auth pages keep theirs and Hesap → Dil stays the
place to change it once signed in; a portfolio card with no value, cash or
P/L says "Henüz işlem yok" with an İşlem ekle link instead of three zeros; on
desktop, clicking a transaction row opens its edit dialog, as tapping a row
does on phone, and the hover icons stay for keyboard users; the keyboard
shortcut hint in the transaction dialog is hidden on touch-only devices; the
admin page gets in-page section links under its heading. Not in this epic:
new features, chart changes, a bottom navigation bar, persisting the
panel's open state, and anything already under Phases 15-19.

- Planning docs: this roadmap section (GLG-567).
- İşlemler: expected dividends panel collapsed by default, count badge off the tab (GLG-568). Superseded in Phase 21 by the Temettü tab (GLG-605).
- Özet on phone: KPI cards in a 2-column grid with the total-value card and the realized P/L card spanning both (GLG-569).
- Header: remove the language switch from the signed-in header (GLG-572).
- Portföyler: an empty portfolio card says "Henüz işlem yok" with an İşlem ekle link instead of three zeros (GLG-573).
- Transaction dialog: hide the keyboard shortcut hint on touch-only devices (GLG-575).
- Özet on phone: last price under the P/L in the position row, editor full width only while editing (GLG-570).
- Portfolio header: sticky tab row on phone, then Özet / İşlemler / İçe aktar, with Temettü as the fourth tab since Phase 21 (GLG-571).
- İşlemler on desktop: clicking a transaction row opens its edit dialog (GLG-574).
- Admin page: in-page section links under the heading (GLG-576).

## Phase 21 — Portfolio stats, CSV import, dividends tab, date ranges

Status: shipped 2026-09-29 · Epic GLG-593 · ADR 0006, 0009, 0011, 0021

Goal: nine features picked after Phase 20, each split into small tickets:
an İstatistikler card on Özet (net contributions, dividends, interest,
fees, first trade), a generic CSV import next to the Midas PDF, a `market`
column on the CSV export, a date range filter on İşlemler, a Temettü tab
with a dividend timeline, calendar-year and custom ranges on the charts, an
annualized return on the chart header, copying the holdings with an analysis
prompt for an AI assistant, and cloning a transaction.
Decisions taken when planning this epic, recorded as amendments to
`docs/adr/0006-broker-statement-import.md`, `docs/adr/0009-dividends.md` and
`docs/adr/0011-portfolio-returns.md`, with no new ADR at the time, and since
in [ADR 0021](adr/0021-product-and-ui-decisions.md): the summary API adds
per-currency `stats` totals (deposits, withdrawals, fees, `dividends_gross`,
`dividends_tax`, interest) and `first_transaction_date`, with
reporting-currency totals at today's rate, null when a rate is missing, and
the `all` scope is the merged sum; the CSV import is a parser package
(`internal/statement/csv`, broker key `csv`) fed `encoding/csv` records by the
existing parse handler, with Turkish and English header and type aliases, a
BOM strip, `;` or `,` separator sniffing, dates as `2006-01-02`, `02.01.2006`
or `02/01/2006` and `1.234,56` numbers accepted; `Trade` and `Dividend` gain
an optional `Market` hint that wins over the currency rule, `CashMovement`
gains `Fee` and the kind `fee`, and a CSV has no holdings, so no
reconciliation; the CSV export appends `market` after `note`, earlier columns
keeping their positions; the transactions list and CSV export take `from` and
`to`, inclusive `YYYY-MM-DD`; `GET /api/portfolios/{id}/dividends/timeline`
returns monthly, yearly and per-instrument totals, each row converted at the
latest `fx_rates` rate on or before its date (`fxWalk`), lists the currencies
without a rate in `missing_rates` and takes a numeric id only; the bars are an
SVG `BarChart` with theme colors, no chart library (ADR 0005); the expected
dividends panel moves from İşlemler to the Temettü tab, open by default; the
range picker adds calendar-year chips, up to three previous years back to the
first transaction, and a custom range, on Özet and the position page; the
chart header annualizes the period return in the frontend,
`(1 + r)^(365 / days) − 1`, only for ranges a year or more long, where a range
whose first and last points are 364 to 366 days apart counts as one year and
annualizes to its own period return, so every calendar year and the 1Y preset
qualify (GLG-661, GLG-675); the AI copy puts full numbers on the clipboard
even in privacy mode, since the clipboard is not the screen, and the prompt
asks for no buy or sell recommendations; cloning opens a prefilled
new-transaction dialog with today's date that POSTs a new row. Recorded
simplifications: the Özet stats convert at today's rate (the Phase 5
simplification, like `realized_pl`) while the dividend timeline uses the
transaction-date rate; interest is not in the dividend bars; CSV notes are not
imported. Not in this epic: annualized returns in the API, note import from
CSV, a per-row FX walk for the stats.

- Planning docs: this roadmap section and the amendments to ADR 0006, 0009 and 0011 (GLG-594).
- Holdings engine and summary API: per-currency `stats` totals and `first_transaction_date` (GLG-595).
- Özet: İstatistikler card with net contributions, dividends, interest, fees and first trade (GLG-596).
- CSV parser: `internal/statement/csv` turns CSV records into a `Statement` (GLG-597).
- Import API: accept a CSV upload for broker `csv` (GLG-598).
- Import page: CSV as a broker option with the accepted columns as help text (GLG-599).
- Transactions API: append a `market` column to the CSV export (GLG-600).
- Transactions API: `from` and `to` on the list and the CSV export (GLG-601).
- İşlemler: date range filter next to the type chips (GLG-602).
- Dividends API: `GET /api/portfolios/{id}/dividends/timeline` (GLG-603).
- BarChart: SVG bars with a tooltip and theme colors (GLG-604).
- Temettü tab: dividend timeline, per-instrument table and the expected dividends panel (GLG-605).
- RangePicker: calendar-year chips and a custom date range on Özet and the position page (GLG-606).
- Chart header: annualized return sub-line for ranges of a year or more (GLG-607).
- Özet: copy the holdings and an analysis prompt for an AI assistant (GLG-608).
- İşlemler: clone a transaction into a prefilled new-transaction dialog (GLG-609).

## Phase 22 — Garanti BBVA statement import and fund benchmarks

Status: shipped 2026-10-05 · Epic GLG-693 · ADR 0004, 0006, 0008, 0012, 0018

Goal: Garanti BBVA is a common custodian of Turkish fund holdings, and
Garanti offers no export, only two monthly PDFs by e-mail: the bank's fund account
statement and the Garanti BBVA Yatırım brokerage statement. Both import
like a Midas statement, an unknown fund code on a statement becomes a TEFAS
fund once TEFAS confirms it, a tracked fund keeps the name TEFAS gives it,
and the Özet chart compares the portfolio against any of its own positions.
Decisions taken when planning this epic, recorded in
`docs/adr/0018-garanti-statements.md` and as amendments to
`docs/adr/0004-tefas-data-source.md`,
`docs/adr/0006-broker-statement-import.md` and
`docs/adr/0012-benchmarks.md`: two parser packages,
`internal/statement/garanti` (broker key `garanti`) and
`internal/statement/garantiyatirim` (broker key `garanti_yatirim`), read the
one row per page `pdftext.Rows` returns as a flat cell stream, switching
state on section titles; the fund statement uses Turkish numbers, nets its
movements per (date, fund, price) into one buy or sell, books the day's net
cash effect as one deposit or withdrawal per date, since the bank account
is outside the portfolio, maps a truncated movement name to a fund code
only through the same statement's balances table, never across statements
or against `instruments.name`, because fund names change while codes stay
(the parser still reads one statement at a time, but since GLG-766 the
import preview resolves a name the parser leaves unmapped through the fund
names learned from the balance rows of committed statements), and sets
`Statement.Market = "TEFAS"`; the brokerage statement uses US numbers (a
copy in Turkish number format is read too since GLG-725), reads the Hesap Durumu holdings and cash, the Menkul Kıymet
Hareket Listesi period and the Hesap Ekstresi ledger, books `HA` rows as
buys dated on the trade date when it is inside the period, else on the
value date, splits a `Komisyon` row pro rata over the buys it covers
(since GLG-728 and GLG-747 over the buys or the sells of its value date
whose amounts sum to its `Matrah`, one side each, else over every trade of
that date), and books `Banka Takas` rows as deposits or withdrawals; `Statement.Market`,
when set, is the market of the holdings and of the positions the statement
is silent about in the reconciliation, empty keeps the currency rule; the
import creates a TEFAS fund for an unknown code only with market hint
`TEFAS`, a symbol matching `^[A-Z0-9]{3}$`, NAVs from TEFAS, at most 10
codes per request and only while the `tefas` source is on, inserted as
`TEFAS`/`TRY` with `fonUnvan` as its name; the TEFAS fetcher updates a
tracked fund's name from `fonUnvan` on every fetch, the seed CSV is not
rewritten; the history API takes `?benchmark=instrument:<id>` for any
instrument with a transaction in the scope, priced from the sweep's own
series in the instrument's currency, and the Özet selector lists the
portfolio's positions under the fixed benchmarks. Recorded simplifications:
the fund statement's stopaj goes into the fee of the day's net trade even
when the net is a buy; the time of day is dropped; the Menkul Kıymet
Hareket Listesi rows are not read, the ledger is the source of truth, until
GLG-739: since then the list's `Bedelsiz` bonus shares and priced
`HS-Virman Giriş` transfers in are booked from it, an IPO allotment among
them priced from the ledger's `Halka Arz Tahsil` and refund rows (GLG-768),
and a transfer out that matches a ledger fractional cash-out is skipped
(GLG-767), while its `HS Alış` and `HS Satış` rows stay with the ledger,
which carries their commission; one
portfolio per account is recommended, since the brokerage cash
reconciliation compares the whole portfolio's TRY cash; `HS` rows, the sell
code seen on real statements, are read as sells since GLG-728; a split
inside an instrument benchmark's range shows as a drop, no back-adjust. Not
in this epic: statement-sourced prices, the Garanti gold account, non-share
assets at Garanti BBVA Yatırım, other banks, e-mail ingestion; names are
remembered across statements since the follow-ups, the user's instrument
pick for a printed name (GLG-735, GLG-736) and the fund names learned from
the balance rows of committed statements (GLG-766).

- Planning docs: this roadmap section, ADR 0018 and the amendments to ADR 0004, 0006 and 0012 (GLG-694).
- Import API: `Statement.Market` hint for reconciliation and the `garanti` and `garanti_yatirim` broker keys (GLG-695).
- Garanti BBVA parser: `internal/statement/garanti` turns the bank's fund statement into a `Statement` (GLG-696).
- Garanti BBVA Yatırım parser: `internal/statement/garantiyatirim` turns the brokerage statement into a `Statement` (GLG-697).
- TEFAS fetcher: keep a tracked fund's name in step with `fonUnvan` (GLG-698).
- Import API: create a TEFAS fund that TEFAS confirms when a statement row names an unknown fund code (GLG-699).
- Import page: Garanti BBVA and Garanti BBVA Yatırım broker options with a hint per statement (GLG-700).
- History API: `?benchmark=instrument:<id>` for any instrument with a transaction in the scope (GLG-701).
- Özet: the benchmark selector lists the portfolio's positions under the fixed benchmarks (GLG-702).
- Docs: manual prices live in `manual_prices`, not `prices` (GLG-703).
- Import page: translate the Garanti parsers' `unknown_row` and `bad_row` error codes (GLG-704).
- Backend tests seed fetched `prices` rows with source `test`, not `manual` (GLG-706).
- Garanti BBVA fund statement: a negative stopaj is read as a refund, not a bad row (GLG-723).
- Garanti BBVA fund statement: a fund held in two accounts is one holding, its balance rows summed per code (GLG-724).
- Garanti BBVA Yatırım statement: Turkish-format number cells are read, and trade amounts are cross-checked (GLG-725).
- Garanti BBVA Yatırım statement: holdings rows printed without the name cell are read (GLG-726).
- Garanti BBVA Yatırım statement: statements from before the 2019 BBVA rebrand, with the old website cell, are accepted (GLG-727).
- Garanti BBVA Yatırım ledger: `HS` rows are read as sells, the `Komisyon Tutarı ve BSMV` commission text is read next to the older `Komisyon` one, and either commission goes to the buys or sells of its value date whose amounts sum to its `Matrah`, else pro rata over the day's trades (GLG-728).
- Garanti BBVA Yatırım ledger: MKK, account maintenance and order-cancel charges are booked as fees or refunds, MKK and balance transfers as deposits or withdrawals, and a description wrapped into a second cell is joined (GLG-729).
- Garanti BBVA Yatırım ledger: a cash dividend row is a dividend booked as printed with tax 0, its transfer row a deposit or withdrawal (GLG-730).
- Decision record: a `Bedelsiz` bonus is a buy at price 0 with a guard against a stored split, an IPO allotment a buy and a fractional cash-out a sell; implemented by GLG-739, GLG-740 and GLG-741 (GLG-731).
- Decision record: the import remembers the user's instrument pick for a printed name; implemented by GLG-735 and GLG-736 (GLG-732).
- Import picker: a typed fund code is looked up on TEFAS (GLG-733).
- Decision record: the preview proposes opening positions for the first statement of an account; implemented by GLG-737 and GLG-738 (GLG-734).
- Import API: the commit's `aliases` store the user's instrument pick per broker and printed symbol in `import_aliases`, and a later preview resolves that symbol from the pick when nothing else matches (GLG-735).
- Import page: the commit sends the instrument picks, and a row resolved from an earlier pick is marked and keeps the picker (GLG-736).
- Import preview: an opening buy dated the day before the period for each instrument the statement holds more of than the portfolio explains and that has no transaction in the portfolio dated before the period (one with earlier transactions stays an ordinary mismatch), priced from stored or fetched prices, else the statement's price marked approximate, and one opening deposit per currency (GLG-737).
- Import page: proposed opening positions show as their own group with an Açılış pill and an approximate-price note (GLG-738).
- Garanti BBVA Yatırım parser: the Menkul Kıymet Hareket Listesi is read for `Bedelsiz` bonus shares, booked as buys at price 0, and priced `HS-Virman Giriş` transfers in (GLG-739).
- Garanti BBVA Yatırım ledger: the `Halka Arz Tahsil` IPO payment and its refund are booked so cash ends where the account does, and a fractional share cash-out is a sell of the fraction (GLG-740).
- Holdings: a stored split is skipped in a portfolio that holds a zero-price buy of the instrument within 10 days of the ex-date, so bonus shares a statement booked are not doubled (GLG-741).
- Garanti BBVA Yatırım ledger: when a day's buys and sells have equal totals, each side gets one commission (GLG-747).
- Garanti BBVA Yatırım ledger: 2018 trade references with a letter, the `Temettü Transferi` row and an account fee with a double space are read (GLG-763).
- Import: `broker=auto` detects the broker of each PDF, so one batch can mix statements, and the import page defaults to Otomatik (PDF) (GLG-764).
- Import batch: a file already imported, the same file twice or an identical statement of the same period is skipped with a reason, not failed (GLG-765).
- Garanti BBVA fund import: fund names from the balance rows of committed statements are learned, so a later movement printed by name resolves without a pick; a pick always wins (GLG-766).
- Garanti BBVA Yatırım trade list: the `HS-Virman Çıkış` of a fractional share the ledger cashes out is skipped (GLG-767).
- Garanti BBVA Yatırım: an IPO allotment is priced from the ledger's `Halka Arz Tahsil` and refund rows, not the trade list's price cell, and a mismatch is a `bad_number` error (GLG-768).
- Garanti BBVA fund import: a learned fund name follows the statement period, not the commit order (GLG-773).
- Import preview: a BIST symbol that matches no instrument syncs that date's bulletin first, so a delisted share on a backdated statement resolves (GLG-782).

## Phase 23 — Documentation sweep

Status: shipped 2026-10-01 · Epic GLG-707 · ADR 0019, 0020, 0021

Goal: every doc in the repo matches the code on `main`, stale text is
removed or marked, and decisions that lived only in the roadmap or in the
code get an ADR. The epic came from a full audit of the docs on 2026-10-01.
Decisions taken when planning this epic: `docs/ROADMAP.md` is split, it
keeps the future work and the non-goals, and the shipped phases move to
`docs/HISTORY.md`, where their text is corrected; an ADR body is history and
is not rewritten, a stale place gets a dated `Superseded`, `Amended`,
`Resolved` or `Correction` note and the status paragraph lists every
amendment, a convention the ADR index records; three retro ADRs record the
decisions that had none: 0019 core engine and auth, 0020 deployment
topology, 0021 the product and UI decisions of Phases 15, 20 and 21 and the
i18n approach; PRINCIPLES #8 drops the promise of a version segment in the
API path, since no route has one. No code behavior changed.

- deploy/README and `.env.example`: every backend env var with its default, the `make build` and `SECRET_KEY` steps fixed (GLG-709).
- RUNBOOK: the restore drill takes a fresh backup first, a restore works with any `SECRET_KEY` and only the provider keys are entered again, one table of CLI flags, and the full admin list (GLG-708).
- ADR 0020: the deployment topology, Compose on any Docker host with Caddy in front of nginx, no container registry and no external error reporting (GLG-713).
- `CLAUDE.md` and PRINCIPLES: markets, datastore, repo layout and provider shape match the code; PRINCIPLES #6 holds the one pre-merge check list; #8 drops the path-version rule (GLG-711).
- README: features, markets, import sources and doc links listed; the datastore and Node lines refreshed (GLG-710).
- Stale code comments: Go package docs, the backfill flag help, the API docs general info and the `tools/` headers describe the current behavior (GLG-718).
- ADR 0011, 0012, 0015, 0016 and 0017: the status lines list every amendment and the answered open points get dated notes (GLG-717).
- ADR 0019: the core engine and auth decisions, sessions, holdings rules, value history, manual prices and the on-demand fetch (GLG-712).
- ADR 0021: the product and UI decisions of Phases 15, 20 and 21 and the i18n approach (GLG-714).
- ADR 0005, 0006, 0008, 0009 and 0010: dated notes for the changed decisions and the amendments listed on the status lines (GLG-716).
- ADR 0001 to 0004: dated notes for the superseded and amended decisions, such as the provider interface, CI, env-var keys and the TEFAS host cron (GLG-715).
- ADR index: `docs/adr/README.md` lists every ADR with its dates and tickets and states the amendment convention (GLG-721).
- Roadmap split: the shipped phases move to `docs/HISTORY.md` with a status line each, and `docs/ROADMAP.md` keeps the future work and one non-goals list (GLG-719).
- HISTORY: phase text the code or the ADRs contradict is corrected, and Phases 0 to 2 get their ticket keys (GLG-720).
- ADR 0007: a dated note says `tufe` and `gold` also run every day, not only crypto (GLG-746).

## Phase 24 — UI v2: design language and app shell

Status: shipped 2026-10-05 · Epic GLG-867 · ADR 0023

Goal: the owner found the Defter look (ADR 0005) plain, so the app gets a new
design language and app shell, the first two of the five sub-projects in the
spec
[`2026-10-04-ui-v2-design-language-and-shell-design.md`](superpowers/specs/2026-10-04-ui-v2-design-language-and-shell-design.md).
The work is token-first and in place, so `main` works after every ticket, and
it has no backend, API or schema change.
Decisions taken when planning this epic, recorded in
[ADR 0023](adr/0023-ui-design-system-v2.md), which supersedes parts of ADR
0005: the "soft depth" look keeps the token names and changes their values;
cards float on a shadow instead of a hairline border; three new color tokens,
`well` for a sunken track, `raised` for a selected item and a secondary
button, and `raised-hover` for its hover; the shadow tokens `shadow-card`,
`shadow-ctl` and `shadow-pop` take their values from `--elevation-*`, so the
dark blocks can override them; radii are `rounded-card` 12 px and
`rounded-ctl` 8 px; self-hosted Inter replaces IBM Plex and `font-mono` goes;
Base UI (`@base-ui/react`) is the one new runtime dependency, used for `Menu`
and `Select` only, inside `CSPProvider disableStyleElements` because the
production CSP blocks the `<style>` element it renders; Recharts is allowed
but not added yet; from `lg` a 232 px sidebar lists the portfolios, below
`lg` a top bar opens it as a drawer, and below `md` the primary add button
floats. `conventions.test.ts` scans the components for banned classes:
`font-mono`, the old card string, `shadow-sm` to `shadow-2xl` and native
`<select>`. Not in this epic, and listed in [ROADMAP.md](ROADMAP.md): charts
on Recharts, the core pages and the secondary pages (sub-projects 3 to 5).
Until they ship, pages keep their Defter layouts and hand-drawn charts inside
the new skin and shell.

- Planning docs: ADR 0023, dated notes in ADR 0005 and 0021, the ADR index, `CLAUDE.md`, `frontend/README.md` and the roadmap entry (GLG-868).
- Tokens: UI v2 color values, the shadow and radius tokens, Inter in place of IBM Plex, `font-mono` removed; the contrast test covers the new pairs and `conventions.test.ts` starts (GLG-869).
- `card` utility (`rounded-card bg-surface shadow-card`) in place of the inline card strings; menus, dialogs, tooltips and toasts take `shadow-pop`; the shadow values move to `--elevation-*`, so dark mode gets its own (GLG-870).
- Favicon and app icons in the UI v2 `ink` and `paper` colors (GLG-879).
- Segmented controls: `ui/segmented.ts` with a `well` track and a `raised` active item, used by the range picker, the allocation toggle, the İşlemler type filter and the Temettü grouping; the active portfolio tab gets an accent underline (GLG-872).
- `Button` and `Badge`: accent primary, borderless `raised` secondary with `shadow-ctl` and a `raised-hover` hover, pill badge; `buttonClass()` for links styled as buttons (GLG-871).
- Base UI and the `Menu` primitive (`ui/Menu.tsx`); the portfolio header and card menus use it (GLG-873).
- `Select` primitive (`ui/Select.tsx`) on Base UI, portaled into the open `<dialog>`, with `CSPProvider` in `main.tsx`; the transaction dialog's currency select uses it (GLG-874).
- App shell: `AppShell` with the portfolio `Sidebar` from `lg` and a top bar below it, the `AccountMenu` and compact portfolio values; `Header.tsx` is deleted and the portfolio name is the page's h1 (GLG-875).
- Docs: dated notes in ADR 0006, 0014 and 0021 for what UI v2 made stale (GLG-878).
- The remaining native selects move to `Select`, and `conventions.test.ts` bans `<select` (GLG-877).
- Small screens: the sidebar as a drawer in a modal `<dialog>` below `lg`, the page title in the top bar, and a floating `+` add button below `md` (GLG-876).
- A new page starts at the top instead of the previous page's scroll position (GLG-881).
- Hesap language and theme pickers and the auth pages' language switch on the segmented classes; the empty-portfolio delete confirm is red (GLG-883).
- Phone top bar stays inside the viewport with a long portfolio name, a signed-in not-found page renders inside the shell, and the top bar shows the page's own title (GLG-882).

## Phase 25 — UI v2: charts foundation

Status: shipped 2026-10-05 · Epic GLG-910 · ADR 0023

Goal: Phase 24 left the charts hand-drawn inside the new skin and shell, so
the line chart and the bar chart are rebuilt on Recharts in the new look.
This is sub-project 3a of the redesign; the owner split sub-project 3 of
ADR 0023 into 3a (this phase), 3b (allocation donut, profit and loss per
position, drag to zoom) and 3c (a return-index series from the backend), each
with its own spec, plan and epic. Spec:
[`2026-10-05-ui-v2-charts-foundation-design.md`](superpowers/specs/2026-10-05-ui-v2-charts-foundation-design.md).
Frontend only, no backend, API or schema change, and the chart props did not
change, so no page layout changed.
Decisions taken when planning this epic, recorded in dated notes in
[ADR 0023](adr/0023-ui-design-system-v2.md): `recharts` 3.10.1 is the one new
runtime dependency; the charts are thin wrappers in `components/charts/`, and
Recharts draws the scales, paths, area, grid, cursor, animation, resizing,
touch and keyboard layer, while the printed amounts from the exact decimal
strings, the y ticks from `axisTicks` and `formatTicks`, the date labels,
privacy masking, the marker shapes, gaps never drawn to zero and the legend
as our own HTML stay ours; style A, "precise": straight segments, a soft
gradient fill, a dashed horizontal grid and one shared tooltip card, no
smoothed curve; a 300 ms animation on data change that reduced motion turns
off; the cost and benchmark legend entries are toggle buttons; Home and End
no longer jump to the ends, Left and Right still move point by point, and the
chart's role is `application`, Recharts' default, instead of `img`. Pages
reach the charts only through `components/LazyChart.tsx`, so Recharts loads
as a separate chunk on pages with a chart. At GLG-915 the main chunk was
730.68 kB (218.69 kB gzip) and the charts chunk 386.81 kB (110.60 kB gzip),
against 739.97 kB (221.52 kB gzip) for the main chunk before Recharts. Not in
this epic, and listed in
[ROADMAP.md](ROADMAP.md): sub-projects 3b, 3c, 4 and 5.

- Pure chart helpers and chart types move to `lib/chart.ts`, out of the chart components; the charts spec, plan and mockup land on `main` and the roadmap gets Phase 25 (GLG-911).
- Line chart on Recharts in style A: `components/charts/LineChart.tsx`, a `ComposedChart` with a gradient fill, a dashed grid and the shared tooltip card `charts/ChartTooltip.tsx`; the old `components/LineChart.tsx` is deleted, Home and End are dropped and the role is `application` (GLG-912).
- Line chart legend: the cost and benchmark entries are buttons with `aria-pressed`; a hidden series is not drawn, leaves the tooltip, and the y axis refits; a new range keeps the state, a newly chosen benchmark is shown, and a reload shows every series (GLG-914).
- Bar chart on Recharts in the line chart's look: `components/charts/BarChart.tsx` with accent bars with rounded tops and the shared tooltip card; the 1 px hairline for a zero bar, negative values drawn as zero, the compact money ticks and privacy masking stay; Home and End are dropped, the role is `application`, and a `well` band marks the hovered bar (GLG-913).
- Lazy charts chunk: pages import the charts from `components/LazyChart.tsx`, which shows a placeholder of the chart's height while the chunk loads and the chart error in the card if it fails, while the rest of the page works; `conventions.test.ts` keeps `recharts` imports inside `components/charts/` and imports of `components/charts/` inside `LazyChart.tsx` (GLG-915).
- Line chart tooltip: the card is placed from the active point, 12 px below it in the upper half of the plot and above it otherwise, inside the plot, for mouse, touch and keyboard (`placeTooltip` in `lib/chart.ts`); a day with no values shows the card with its date; a hidden legend entry keeps the muted text color with a line through it (GLG-922).

## Phase 26 — UI v2: core pages

Status: shipped 2026-10-05 · Epic GLG-928 · ADR 0023

Goal: Phases 24 and 25 changed the skin, the shell and the charts and left
every page layout as it was, so Özet, Tüm portföyler, İşlemler, the position
page and Temettü get new layouts that lead with what each page is for. This
is sub-project 4 of ADR 0023. Spec:
[`2026-10-05-ui-v2-core-pages-design.md`](superpowers/specs/2026-10-05-ui-v2-core-pages-design.md).
Measured before the epic at 1280 px with 15 positions: Özet was 3,097 px
tall with its first position row at y=1015, a position row was 109 to 133 px
tall, and the İşlemler table was 1,121 px wide in a 1,000 px card.
Decisions taken by the owner when planning this epic, recorded in the spec
and in dated notes in [ADR 0023](adr/0023-ui-design-system-v2.md) and ADR
0014: Özet answers "how am I doing?" and "what needs my attention?" first,
in layout A: a return card with the chart beside an attention card, one KPI
strip, then a compact positions table, and each number is on the page once;
the attention card lists data gaps, dividends to confirm, today's two largest
movers and concentration, with the thresholds a manual price older than 7
days, one position above 25%, cash above 40% and one foreign currency above
50%; Tüm portföyler is the combined Özet with one row per portfolio and
replaces the home page; the chart range is kept in the URL; desktop first,
and a phone stacks the same cards with no layouts of its own; the one backend
change is a filter on the transactions list by import, with no schema
change. Not in this epic, and listed in [ROADMAP.md](ROADMAP.md):
sub-projects 3b, 3c and 5.

- Filter by import: `GET /api/portfolios/{id}/transactions` and `/transactions.csv` take `import_id`, an import's id or `none` for the rows entered by hand, and any other value is a 400; the list filters in SQL before the cursor and the limit; `fetchTransactions` and the new `transactionsCsvUrl` in `api/transactions.ts` pass it; dated note in ADR 0006 (GLG-931).
- Shared pieces: `FitFigure` and the KPI classes move from `SummaryPage.tsx` to `components/ui/Kpi.tsx`, with the new `KpiStrip` and `KpiCell`, and the position page uses them too; `components/ui/table.ts` holds the compact table classes (`TABLE`, `TH`, `TD`, `TD_NUM`, `ROW_40`, `ROW_44`, `GROUP_ROW`); the specs, plans and mockups of the four UI v2 epics land on `main` and the roadmap gets Phase 26 (GLG-929).
- Range in the URL: `lib/useRange.ts` reads and writes `?range=<key>` or `?from=…&to=…` with `replace`, falls back to 3A on a value that does not parse and drops the parameters for 3A; Özet and the position page use it, and the links between them and the sidebar's portfolio links carry it; below `sm` `RangePicker` is a `Select` instead of the chips (GLG-930).
- Attention rules: `lib/attention.ts` with the `ATTENTION` thresholds and a pure `attentionItems` in four groups, Veri (a currency with no rate, a position with no price, manual prices older than 7 days, a benchmark estimate), Temettü onayı (each expected dividend), Bugün (the two largest day movers) and Yoğunlaşma; shares are of the reporting total including cash and compared in exact units, so exactly 25% is not flagged; `components/AttentionCard.tsx` renders the groups, the "Her şey yolunda" state and the Ekle, Yoksay, Fiyat gir and Güncelle actions (GLG-932).
- Add-transaction dialog: an "Ekle ve yeni" button beside Ekle saves, keeps the type and the date, clears the rest and puts focus back on the first field, as ⌘↵ now does too; the ⌘↵ hint shows only with a fine pointer (GLG-937).
- Temettü: the dividends to confirm are the first card, always open, with "Tümünü ekle" in its header; a KPI strip of six cells with yield on cost, the last 12 months' net over the open positions' reporting cost; a non-zero bar is at least 3 px tall; the table on `ui/table.ts` with payment count, share and last payment; an empty state card; dated notes in ADR 0009 and 0021 (GLG-939).
- İşlemler: one toolbar row with the search, the type switch, a "Tarih" popover (`components/DateRangeFilter.tsx`), a "Kaynak" select and the CSV link, behind a "Filtrele (n)" sheet below `md`; the table on `ui/table.ts` with 44 px rows, a Kaynak column ("Elle" or an import pill from `importLabel` in `api/imports.ts`) and actions that are always visible; `?import=<id>|none` filters the list and the CSV through `import_id`, and a single-file import commit lands there with a banner (GLG-936).
- Positions table: `components/PositionsTable.tsx` out of `SummaryPage.tsx`, 40 px one-line rows with Ağırlık, header sorting with `aria-sort` and a row menu with Al, Sat, Fiyat gir and Elle fiyatı kaldır; the price editor is `components/PriceDialog.tsx`; an "Açık | Kapalı" switch; P/L in the reporting currency, and the footer shows the server's `positions_value`, `unrealized_pl` and `cost_basis`; dated notes in ADR 0021 (GLG-933).
- Position page: a title row with the symbol as the only h1, the last price, today's change and the Al, Sat and Fiyat gir actions; a KPI strip of six cells, where Toplam sonuç is unrealized plus realized P/L and `realized_pl` already holds the dividends; one chart card with a "Fiyat | Değer ve maliyet" switch and `useRange`; the transactions in the compact table; dated note in ADR 0016 (GLG-938).
- Özet layout A: `components/ReturnCard.tsx` with the return for the chosen method, the amount and a pill with the benchmark's return and the difference, beside the `AttentionCard` (2:1 from `lg`); one KPI strip of five cells whose former sub-lines are `hint` popovers on `KpiCell`; the positions table, then Dağılım beside İstatistikler; Ekle on an attention dividend books it in one step; dated notes in ADR 0021 and 0023 (GLG-934).
- Shell debts: the toasts centre on the content column from `lg` and sit above the floating add button below `md`, the rename input fills the title row, and below `lg` the page's h1 is for screen readers only because the top bar shows the title (GLG-940).
- Tüm portföyler replaces the home page: `/` redirects to `/portfolios/all` and `HomePage.tsx` is deleted; the page is the summed Özet, with an attention card that names each item's portfolio (`mergeAttention`) and the new `components/PortfoliosTable.tsx` of value, share, return with a sparkline and day change per portfolio; with no portfolio it shows the create-portfolio empty state; dated notes in ADR 0014, 0021 and 0023 and the 2026-10-04 spec (GLG-935).

## Phase 27 — UI v2: chart additions

Status: shipped 2026-10-06 · Epic GLG-945 · ADR 0023

Goal: Phase 25 rebuilt the line and bar charts on Recharts without changing
what they show, and Phase 26 put them in the new page layouts, so the charts
get the additions of sub-project 3b of ADR 0023: an allocation donut in place
of the stacked bar, profit and loss per position, drag to zoom on the line
chart, and two chart debts from the Phase 25 close-out, date labels by width
and a legend height that does not jump when the charts chunk arrives. Spec:
[`2026-10-05-ui-v2-chart-additions-design.md`](superpowers/specs/2026-10-05-ui-v2-chart-additions-design.md).
Frontend only, no backend, API or schema change.
Decisions taken by the owner when planning this epic, recorded in the spec
and in dated notes in [ADR 0023](adr/0023-ui-design-system-v2.md) and ADR
0021: a donut replaces the stacked bar, with the total in the middle and an
HTML legend with amounts and shares that is the accessible content; a
"Katkı" card of bars per position switches between Toplam (unrealized P/L)
and Bugün (day change); P/L over a chosen range needs the backend and waits
for 3c; a drag sets the chart's range as filling in Özel does, with a mouse
only. Decisions taken during the epic: one date label per about 120 px of
plot width, which gives five labels on Özet's 556 px plot at 1280 px
(GLG-946); Katkı shows the four best and the four worst (GLG-948); a zoom
needs a drag of at least 8 px (GLG-1032); the browser's Back does not undo a
zoom, and the ✕ shows on every Özel range, returning to the default range
when there is no range from before a zoom (GLG-950); the cash and Diğer
colours of GLG-1045 are accepted. The close-out browser check of combined
`main` filed GLG-1027 to GLG-1035, GLG-1044 and GLG-1045 in this epic. Not
in this epic, and listed in [ROADMAP.md](ROADMAP.md): sub-projects 3c and 5.

- Date labels by width: `dateTicks(dates, plotWidth)` in `lib/chart.ts` labels one date per about 120 px of plot width, at least three and at most seven, evenly spaced by index and always the first and the last, fed the width of Recharts' plot area by a small `DateAxis` inside `LineChart`; the year rule (GLG-809) stays; the legend reserves two lines below `sm` and one from `sm` up (`LEGEND_HEIGHT`), and the line placeholder in `LazyChart.tsx` the same, so the card no longer grows when the chunk arrives; the roadmap gets Phase 27 (GLG-946).
- Allocation donut: the grouping rules move unchanged from `Allocation.tsx` into `allocationRows` in `lib/allocation.ts` (the largest five on `cat-1` to `cat-5`, the rest in "Diğer (n)" on `cat-6`, cash last on `--color-cash`), summed in exact units; `components/charts/AllocationDonut.tsx` on Recharts' `PieChart`, 150 px with a 16 px ring, "Toplam" and the compact total in the middle and the shared tooltip card on hover, `aria-hidden` with no focus stop; it comes from the charts chunk through `LazyChart.tsx` behind a ring placeholder, and a failed chunk leaves the legend alone; `Allocation.tsx` lists swatch, name, amount and share beside it, and hovering a slice or a row highlights the pair; dated notes in ADR 0021 and 0023 (GLG-947).
- Drag to zoom: `LineChart` takes `onZoom(from, to)`; a mouse press, a sideways drag and a release on the plot shade the span (a `ReferenceArea`) and hand its first and last dates to `onZoom`; a drag within one point, a release outside the plot and Escape do nothing, and touch and pen never zoom (the `pointerType` of the `pointerdown`); `useZoom` in `lib/useRange.ts` turns the span into an Özel range through `useRange` and remembers the range from before the first zoom; `RangePicker` takes `onClear` and shows a ✕ in front of Özel's dates that returns to that range, or to the default range after a reload; `ReturnCard` (Özet, Tüm portföyler) and `PositionPage` wire it; dated note in ADR 0023 (GLG-949).
- Katkı: `components/ContributionCard.tsx` draws one bar per open position on a shared zero line, a gain right and a loss left, the longest half the track, with the signed reporting amount at the row's end, in plain HTML on a CSS subgrid, so it never waits for the charts chunk; a "Toplam | Bugün" switch picks unrealized P/L or day change; `contributionRows` in `lib/contribution.ts` gives the four best and the four worst, ties by symbol, and "Tümünü göster" expands to all; symbols link to the position page with the range, plain text on Tüm portföyler; privacy masks the amounts and keeps the bars; Özet's lower rows, on both scopes, are Dağılım beside Katkı at one height from `lg`, then İstatistikler as a full-width `KpiStrip` of five cells; dated note in ADR 0023 (GLG-948).
- Katkı leaves out a position without a price, which the API sends with `unrealized_pl_reporting` "0.00": in Toplam mode `contributionRows` takes the P/L from `unrealized()`, now exported from `PositionsTable.tsx`, the rule behind the table's "—" (GLG-1029).
- A drag zooms only when it spans at least 8 px sideways (`MIN_ZOOM_DRAG_PX`) and two or more points, so a wobbly click on a dense series no longer sets an Özel range (GLG-1032).
- The Dağılım legend goes beside the donut on the card's width, not the viewport's: the card body is a `@container` and the row switches at `@md` (28rem), so in the half-width card from `lg` the names no longer shrink to nothing (GLG-1027).
- Below `sm` the Özel ✕ sits beside the range `Select`, its wrapper `display: contents`, and the two date inputs get the next row to themselves, so a whole date shows at 320 px (GLG-1030).
- The donut's tooltip stays inside the donut's box: without `allowEscapeViewBox` Recharts flips the card to the slice's other side, so it no longer pushes a phone page sideways (GLG-1028).
- `--color-cash` reaches 3:1 on `surface` in both themes (`#8b939f` light, `#5f6879` dark), checked in `index.css.test.ts`; dated note in ADR 0023 (GLG-1033).
- A privacy toggle re-renders instead of remounting the app: App's route key is the locale alone, and App's `usePrivacy()` re-renders everything below it, so component state survives a toggle (the range from before a zoom, the Katkı choice, sorts, the import review, an open drawer, focus); the language switch still remounts (GLG-1031).
- A mouse press on the line or bar chart takes no focus: the chart's wrapper calls `pressWithoutFocus` from `lib/chart.ts` on `onMouseDown`, which prevents the default and blurs a field focused elsewhere by hand, so the keyboard tooltip no longer sticks on the first date and the rename field still saves; Tab and the arrow keys, touch scrubbing and drag to zoom are unchanged (GLG-1034).
- `dateTicks` makes room for the labels: from the GLG-946 count it drops labels until neighbours no longer overlap, the first start-anchored, the last end-anchored, a label 40 px wide without a year and 74 px with one; on a narrow plot labels with a year go down to the two ends; `DateAxis` passes `multiYear` (GLG-1035).
- A press or a tap on a donut slice takes no focus either: the donut's wrapper uses `pressWithoutFocus` too (GLG-1044).
- Diğer and Nakit part by lightness, 3:1 apart in both themes with cash the lighter: `cat-6` is `#344054` light and `#5f6879` dark, cash `#c0c7d2` dark; `index.css.test.ts` checks both at 3:1 on `surface` and from each other; dated note in ADR 0023 (GLG-1045).

## Phase 28 — UI v2: return series

Status: shipped 2026-10-06 · Epic GLG-951 · ADR 0011, 0012, 0014, 0023

Goal: the return card drew money, so a deposit bent the line upward although
nothing was earned and the benchmark was a shadow value, not a percentage;
there was no view of returns per month or year and none of the fall from the
peak. All three need the return as a daily series net of deposits and
withdrawals, which the backend already chained but only returned as the
range's end result. This is sub-project 3c of ADR 0023. Spec:
[`2026-10-05-ui-v2-return-series-design.md`](superpowers/specs/2026-10-05-ui-v2-return-series-design.md).
No schema change; nothing new is stored.
Decisions taken by the owner when planning this epic, recorded in the spec
and in dated notes in [ADR 0011](adr/0011-portfolio-returns.md), ADR 0012,
ADR 0014 and [ADR 0023](adr/0023-ui-design-system-v2.md): the return card's
chart gets a "% | ₺" switch, opens in percent and remembers the choice; the
headline keeps the chosen method while the percent line is always
time-weighted and says so in its legend when the method differs; periodic
returns and drawdown go on a new Performans tab, also on Tüm portföyler; the
periodic view is a heatmap of months by year with a switch to bars for the
last 24 months. Decisions taken during the epic: a period's bucket takes its
first day's value as the base, as the history does, so months do not chain
into their year and a year's cell is the year bucket, never a product of its
months (GLG-953); the heatmap's five steps mix `gain` or `loss` into
`surface` at 10, 18, 27, 36 and 45% with the text always `ink` (GLG-955);
the bar view is component state that opens on the heatmap and hides
"Portföy | Fark" (GLG-956); the largest drawdown keeps the first of two
equal lows and a flat peak ends on its last day (GLG-957). The close-out
browser check of combined `main` filed GLG-1047 to GLG-1050 in this epic.
Not in this epic: profit and loss per position over a chosen range, a
percent mode on the position page's charts and risk figures beyond drawdown
(the spec's out of scope), and, listed in [ROADMAP.md](ROADMAP.md),
sub-project 5.

- Daily return on history points: each point of `GET /api/portfolios/{id}/history` carries `twr` and `benchmark_twr`, the cumulative time-weighted return from the range's first day as 6-decimal fraction strings; the first point is `"0.000000"` and a day ADR 0011 skips repeats the previous value; `timeWeightedSeries` in `returns.go` holds the running product and `timeWeightedReturn` reads its last element, so the last point equals `returns.twr` character for character; when the range's `returns.twr` is `null` (no positive base, negative cash on any day, a negative factor) every point's `twr` is `null`, and `benchmark_twr` is `null` on every point without a benchmark, where the shadow has no value or when the benchmark's own `twr` is `null`; dated notes in ADR 0011 and 0012, and the roadmap gets Phase 28 (GLG-952).
- Returns per period: `GET /api/portfolios/{id}/returns/periods?benchmark=<code>`, where `{id}` is a numeric id or `all`, returns `currency`, `months[]` and `years[]`, each bucket `{period, from, to, partial, returns, benchmark}`, oldest first with no gaps, from the first transaction to today, the first and the current bucket `partial`; boundaries are the calendar presets' (`YYYY-01-01` to `YYYY-12-31`, a month's first to last day), clamped to the first transaction and to today; the history handler's day walk is `historySweep` in `history.go`, whose `rangeInputs` and `rangeBenchmark` give any day range's inputs the way a history request for that range counts them, so the endpoint sweeps once and a bucket equals the history endpoint for the same range by construction, held by `TestPeriodReturnsMatchHistory`; `benchmarkLevels` gives 0 on a day without a level, so a bucket the benchmark does not cover gets `benchmark: null`; `usePeriodReturns` in `api/history.ts`, keyed under the history key; dated note in ADR 0011 (GLG-953).
- Percent mode: `LineChart` takes `unit: 'money' | 'percent'` (default `'money'`); in percent it reads the same point fields as fractions, with ticks from `axisTicks` printed by `formatPercentTicks` in `lib/chart.ts`, a solid zero line, no area fill, `signedPercent` in the tooltip and the screen-reader summary, never masked in privacy mode, and with the benchmark shown a difference row, (1 + value) / (1 + benchmark) − 1 as on the card's pill; `ReturnCard` has a "% | ₺" switch on the range row, stored as `chartMode` in `localStorage` and opening in percent, which draws `twr` and `benchmark_twr` without the cost line; with a method other than time-weighted the legend reads "Portföy (zaman ağırlıklı)" with a `title` saying why the line can end at another number than the headline; when `twr` is `null` for the range the card shows the value chart, disables the switch and says "Bu aralık için tanımsız"; dated note in ADR 0023 (GLG-954).
- Performans tab: `/portfolios/:id/performance` between Özet and İşlemler, for a portfolio and for `all`; Tüm portföyler gets its tab row back, with Özet and Performans; the sidebar keeps the tab; `pages/PerformancePage.tsx` has the benchmark and method selects, whose stored choices move from `SummaryPage` to `lib/useReturnSettings.ts` (`useBenchmark`, `useReturnMethod`), over the "Dönemsel getiri" card; `components/PeriodReturnsTable.tsx` is a real table, a row header per year newest first, twelve month cells and the year bucket, each the chosen method's return with one decimal, `·` for a month without a bucket, `—` for a `null` return, `*` and a `title` with the dates and the exact percentage on a partial bucket; `heatStep` (steps at 1, 2, 4 and 8%), `periodGrid` and `cellValue` in `lib/periods.ts`; "Portföy | Fark" only with a benchmark, Fark reading "Reel" against TÜFE; below `md` the table scrolls inside its card with the year column sticky; `index.css.test.ts` checks `ink` at 4.5:1 on every step; dated notes in ADR 0014 and 0023 (GLG-955).
- Bars: `components/charts/ReturnBars.tsx` in the charts chunk draws the last 24 months as grouped bars, portfolio in accent and benchmark in plum, the benchmark only when one is chosen and some month has its value, on a percent axis with a solid zero line and the shared `ChartTooltip`; a `null` return draws no bar and reads "tanımsız", a zero return a 1 px line; `LazyChart.ReturnBars` reserves the chart's and the legend's height; the "Dönemsel getiri" card gets an "Isı haritası | Çubuk" switch over the same months, method and benchmark (GLG-956).
- Drawdown: `drawdown(points)` in `lib/drawdown.ts` turns the all-time history's `twr` into, for each day, how far the index 1 + `twr` sits below its highest value so far, with the largest drawdown, its peak and trough dates, and the current one, re-implementing no return rule; `components/charts/DrawdownChart.tsx` in the charts chunk draws a loss-coloured area under a zero line at the top; `components/DrawdownCard.tsx` below "Dönemsel getiri" fetches the all-time history without a benchmark, the same query as Özet's "Tümü", and shows the two figures in its header, the periodic card's explanation when `twr` is `null` and "Grafik için yeterli veri yok." with fewer than two days; dated note in ADR 0011 (GLG-957).
- The portfolio tab row keeps a 12 px gap below `sm` (`gap-3`, `gap-6` from `sm` up) and its labels do not wrap, so neighbouring labels no longer touch at 375 px (GLG-1047).
- The heatmap's scroller in `PeriodReturnsTable.tsx` is `relative`, so the cells' absolutely positioned `sr-only` labels no longer widen the page to 813 px at 375 px, the fix GLG-679 applied to İşlemler and İçe aktar (GLG-1048).
- The percent tooltip names its difference row as the pill does, "reel" against TÜFE and "fark" otherwise: `LineChart` takes `labels.excess` and `ReturnCard` passes it (GLG-1050).
- An empty portfolio's Performans tab says "Henüz işlem yok." instead of an undefined return: `DrawdownCard` checks the point count before `twr`, so one all-zero point is the empty state, one point with a value is "Grafik için yeterli veri yok." and two or more with a `null` `twr` still explain why; the periodic card hides its two switches once it has loaded with no bucket (GLG-1049).

## Phase 29 — UI v2: secondary pages

Status: shipped 2026-10-06 · Epic GLG-962 · ADR 0021, 0023

Goal: the pages the earlier phases left in their Defter-era layouts looked
like another app. İçe aktar opened on a narrow form with the past imports,
the only undo, at the bottom, and its review led with two warnings that
repeated the same rows, listed unreadable rows at the end and had a table
wider than its card; Hesap and Yönetim sat in a centred column with larger
titles than the portfolio pages; Yönetim was mostly nine identical source
cards; setup's step 2 kept its buttons at the bottom of a long page; the
signed-out not-found page had no brand; checkboxes were the browser's own.
This is sub-project 5 of ADR 0023, the last of the redesign. Spec:
[`2026-10-05-ui-v2-secondary-pages-design.md`](superpowers/specs/2026-10-05-ui-v2-secondary-pages-design.md).
Frontend only, no backend, API or schema change.
Decisions taken by the owner when planning this epic, recorded in the spec
and in dated notes in [ADR 0023](adr/0023-ui-design-system-v2.md) and
[ADR 0021](adr/0021-product-and-ui-decisions.md): İçe aktar gets a clearer
flow, an easier review and the import history in view; a single import
lands on İşlemler filtered to it and a batch ends on the import page; Hesap,
Yönetim and the signed-out pages get a consistency pass with no new
features; Yönetim's sources become one compact table. Decisions taken
during the epic: the review bar is `sticky`, not `fixed`, so it stays in the
page column beside the sidebar and rests after the last row (GLG-966); the
history also stays below the review, so an import a warning names can still
be undone there (GLG-965); below `md` a source is a three-line list item,
since its time and both actions do not fit a second line at 375 px
(GLG-969); setup's step 2 is `max-w-4xl` so the sources table fits its card
(GLG-970); a visitor's not-found page sends to login (GLG-1058). ADR 0006
describes the parse, review and commit flow, which did not change, so it
got no note. The close-out browser check of combined `main` filed GLG-1054
to GLG-1058 in this epic. Not in this epic (the spec's out of scope): any
backend change, such as storing an import's file name; new import features
or rules; new settings on Hesap or Yönetim; the native date and time inputs.
With this phase every sub-project of ADR 0023 has shipped.

- Shared pieces: `ui/Checkbox` on Base UI's checkbox (`checked`, `onChange`, `indeterminate`, `disabled`, `label`, `description`), `accent` fill when checked, `line-strong` edge when not, a minus for the mixed state; with a `label` the whole row is a 44 px target and `description` becomes its `aria-describedby`; it replaces the five native checkboxes (the import rows and "select all", `RegistrationToggle`, the source and schedule toggles), and `conventions.test.ts` bans `type="checkbox"` in components; `PageHeader` (`title`, `description`, `actions`) is the header of a page that is not a portfolio page, a 20 px h1 visually hidden below `lg`; the roadmap gets Phase 29 and ADR 0023 a dated note (GLG-963).
- Hesap: the portfolio pages' wrapper (`mx-auto max-w-6xl px-6`, left-aligned) under `PageHeader`; one Tercihler card with three rows (reporting currency, language, appearance) beside a Şifre card from `lg`, stacked below, titles at 14 px, the select and the password form at a readable width; saving, errors and toasts unchanged; dated note in ADR 0021 (GLG-968).
- The split: the inline components of `pages/ImportPage.tsx` move verbatim to `components/import/` (`ImportQueue`, `PreviewTable`, `InstrumentPicker`, `HoldingsPanel`, `FilePicker`, `PastImports`, and `shared.ts` with `Row`, `QueueItem`, `alreadyImported`, `NEEDS_ATTENTION`); the page goes from 2,094 to 1,097 lines, with the same 671 tests (GLG-964).
- Yönetim: the portfolio pages' wrapper under `PageHeader`, the section links one row that scrolls sideways with 44 px targets, card titles at 14 px in a header strip; Kaynaklar is one compact table, `components/admin/SourcesTable.tsx` on `ui/table.ts`: source, on or off, time (İstanbul), last success, "Şimdi çalıştır" and "Geçmişi doldur" (none for `bist-splits`), a three-line list item per source below `md`, the schedule options a row of controls above it; `isStale(source, now)` in `api/sources.ts` is true when a source is on and never succeeded or last succeeded more than 48 hours ago, and its last success is then a `warn` pill, never for a source that is off; the table takes no props, so setup can render it; the users table uses the table classes and wraps a long email; every control keeps its endpoint; dated notes in ADR 0021 and 0023 (GLG-969).
- Signed-out pages: login, register, both setup steps and a visitor's unknown URL share `SignedOutFrame` (the brand above, the card, the language switch below) and one 22 px title class, `signedOutTitle`; the signed-out not-found page gains the brand and the switch; setup's step 2 renders Yönetim's provider cards and sources table at `max-w-4xl` and keeps "Şimdilik atla" and "Bitir" in a 64 px bar fixed to the bottom, the page padded by the same height; `deploy/README.md` and `deploy/RUNBOOK.md` named no moved control (GLG-970).
- İçe aktar step 1: `ImportSteps` ("Dosya seç", "İncele", "Bitti", in a batch "İncele · Ekstre n / m") under the tabs; `DropZone` opens the file dialog on click, Enter and Space, takes dropped files, marks the drag-over and names a folder, an empty file or a type the broker refuses instead of sending it; choosing or dropping files starts the preview at once, so "Önizle" and `FilePicker` are gone; from `lg` the upload card (the broker select in its header, the drop zone, the recognised brokers or the Garanti hint, the CSV columns behind "CSV biçimi", parse errors) sits beside Geçmiş içe aktarmalar, stacked below; each past import shows a pill with broker and period, the row count, the currency, the import day in the user's time zone, "İşlemlerde gör" to İşlemler at `?import=<id>` and "Geri al" (GLG-965).
- İçe aktar step 2: one "Tümü N | Sorunlu N" switch (`ReviewFilter`) replaces the two notices; Sorunlu holds `problemRows(rows)` in `components/import/reviewRows.ts` (unknown instrument, unsupported currency and error rows, each once), is selected on entry when there are any and falls back to every row once the last problem is fixed; the same-period and overlap warnings stay a notice; rows are in date order (`inDateOrder`, stable, an undated row last) with an unreadable row's raw line behind "Satırı göster"; the table has nine columns on `ui/table.ts` and fits its card, the instrument picker at least 12rem wide; `ReviewBar` is `sticky bottom-0` at every width with the counts, "+ Satır ekle" (renamed from "İşlem ekle") and the page buttons; no review rule changed (GLG-966).
- A file dropped during the review no longer opens in the tab: the window guard moves from `DropZone` to `useStrayDropGuard` in `components/import/`, which `ImportPage` calls once, so a stray drop is ignored in every step (GLG-1052).
- Batch end: a multi-file import ends at "Bitti" on the import page, `components/import/ImportDone.tsx` listing each imported statement with its row count and "İşlemlerde gör" to `?import=<id>` (`QueueItem` carries the import `id`), each skipped or failed file with its reason through `ParseError`, "Başka ekstre yükle" back to step 1, and a heading without a success line when nothing was imported; during review the queue folds to "Ekstre n / m · <file> · Tüm dosyaları göster"; a single statement still lands on İşlemler (GLG-967).
- Below `md` the review list gives the instrument picker its own full-width line (`basis-full`) under the row, for an unknown symbol and for Değiştir on a resolved row, whose open state `InstrumentCell` takes as an optional `change` pair from the list item (GLG-1053).
- A visitor's not-found page titles "Sayfa bulunamadı" without a full stop and links "Giriş yap" to `/login`; the signed-in one is unchanged (GLG-1058).
- The import steps stay on one line at 375 px: below `sm` the connectors are 12 px (`w-3 sm:w-8`) and a batch's review step reads only "İncele 1 / 3" (`import.steps.batchCounter`) (GLG-1054).
- "+ Satır ekle" opens the dialog as an add, titled "İşlem ekle" with Ekle and "Ekle ve yeni": `ImportPage` passes `editing` only for an edited row and the add prefill as `initial`; "Ekle ve yeni" keeps the dialog open and the starting currency, so a USD statement's next row stays USD; the review bar orders Vazgeç before "Bu ekstreyi atla", its buttons `ml-auto justify-end` so they stay right when they wrap (GLG-1055).
- Yönetim polish: the admin toggle holds both its labels with one hidden, so the users' actions line up across rows, and below `md` delete sits next to the email; a stale source's pill shows its last success as an İstanbul date and time ("4 Eki 14:05", the year when not this year), on setup too; below `md` a provider's status sits under its description on every row (GLG-1057).
- "İşlemlerde gör" at Bitti and in the history share one secondary button class, `viewImportClass` in `components/import/shared.ts`, 44 px tall below `md` like "Geri al", the history row's buttons `gap-2` apart; in the review table Tür holds only the type pill and the "Açılış" and "elle" markers move to the fixed-width Durum column, while the phone list keeps every pill together and wrapping (GLG-1056).

## Code-quality sweep

Status: shipped 2026-10-06 · no epic, PRs #1037 to #1055

Goal: the duplicated helpers, long parameter lists and crossed layers that
a code-quality audit of `main` at 1b978df listed are gone, and a linter
holds each line the sweep drew. The owner ran the sweep without Jira
tickets, one PR per finding, all squash-merged the same evening. No
behavior changed, every existing test stayed green, and the audit's
suggestions the owner declined (`max-lines-per-function`, the
eslint-comments plugin) were not added.

- Backend: `writeError` and `writeJSON` are shared from the `auth` package (#1037); the Istanbul time zone is loaded once as `price.Istanbul` (#1044); the winning close for a date is picked through `holdings.LatestClose` (#1042); the portfolio list and the summary are valued through one pipeline, `summaryHandler.value` (#1048); the five `-fetch-*` flags become `-fetch <source>` with `-date` (#1050); the symbol importers in `tools/` move into one `backend/cmd/symbols` binary (#1045); a summary loads its splits and its instruments in one query each, `ListMany` and `ByIDs` (#1052); the statement preview's lookup state is passed through `statement/handlers.go` as one `lookupSession` (#1046).
- Frontend: every segmented radiogroup renders through one `Segmented` component (#1040); the transaction row cell helpers and `tradeForm` are shared as `TransactionCells` (#1047); remembered choices are read and written through `useStoredChoice` (#1043); the transaction list filter travels as one `TransactionFilter` object (#1049); URL search params are patched through `patchSearchParams` (#1051); portfolio queries are invalidated through `invalidatePortfolio` and `invalidatePortfolios` in `api/transactions` (#1041); the toast store moves to `lib/toast` and the positions table's weight and day-change rules to `lib/positions`, so lib and api no longer import `components/` (#1053); the line chart no longer re-derives its rows on every drag move (#1039); the import queue ordering and row resolution leave `ImportPage` for their own helpers (#1038).
- Linters: golangci-lint runs `dupl` and `forbidigo` on the backend (#1054); eslint gets a layer-boundary rule (`lib/`, `api/` and `i18n/` must not import `components/`), a `localStorage` ban outside the module stores that own it, a ban on invalidating portfolio-scoped query keys by hand, and `max-params` at four as a warning (#1055).

> Amended 2026-10-10 (GLG-1267): both rules this sweep left out or set as a
> warning are eslint errors now, `max-lines-per-function` at 300 since
> GLG-1162 and `max-params` at four since GLG-1230; the full linter list is
> under [Code-quality sweep 2 and import fixes](#code-quality-sweep-2-and-import-fixes)
> and in [PRINCIPLES #6](PRINCIPLES.md#6-verified-and-reviewed-before-merged).

## Phase 30 — Cash gap deposits

Status: shipped 2026-10-07 · Epic GLG-1108 · ADR 0011

Goal: when every return was `null` because the cash went below zero, the
Performans tab said so in one generic sentence that named no day, no
currency and no amount and offered no action, so the user had to find the
missing deposits alone. The usual cause is buys, often from a statement
import, recorded without the deposits that paid for them. Spec:
[`2026-10-07-cash-gap-deposits-design.md`](superpowers/specs/2026-10-07-cash-gap-deposits-design.md).
No schema change: the deposits are ordinary transactions and their note goes
in the existing `transactions.note` column.
Decisions taken by the owner when planning this epic, recorded in the spec
and in a dated note in [ADR 0011](adr/0011-portfolio-returns.md): implicit
deposits stay rejected, and the app only proposes recorded deposits, written
when the user confirms; one deposit per day a currency's cash reaches a new
low below zero, of the extra shortfall, in the gap's currency (an FX
exchange was rejected, it needs a rate per day); the notice shows only where
the returns are `null`; the `all` scope lists the gaps per portfolio and
writes each deposit into its own portfolio, since cash never moves between
portfolios; undo is by hand, deleting the noted deposits on İşlemler.
Decisions taken during the epic: each gap is rounded up to the 9 fraction
digits a deposit takes and the low moves down by the rounded gap (GLG-1109);
the confirm dialog is the app's native `dialog` through `useModal`, not a
Base UI dialog (GLG-1110). Not in this epic (the spec's out of scope):
detection in the statement import review, a hint on the chart header's dash,
one deposit per uncovered buy.

- Gap walk and routes: `holdings.CashGaps(txs)` walks one portfolio's cash per currency in date order with `State.Apply`, copying no cash rule, and at the end of every day whose balance is below the lowest covered so far (starting at 0) emits a `Gap{Date, Currency, Amount}` of `low − balance`, rounded up to 9 fraction digits because a trade's amount can carry more (GLG-476), then moves the low down by that gap, so a currency's gaps sum to its deepest shortfall and a recovery followed by a shallower dip adds none; ordered by date, then currency; `GET /api/portfolios/{id}/cash-gaps`, `{id}` a numeric id or `all` through `scopeTransactions`, runs the walk once per portfolio and returns `[{portfolio_id, date, currency, amount}]` ordered by portfolio, date, currency, `[]` without gaps; `POST` to the same path with `{"note"}` takes no amounts, recomputes the gaps on the server and inserts one deposit per gap, dated its day, in its currency and portfolio, each through `transaction.FromRequest` like the transaction form, so the note is validated as a transaction's, all in one database transaction (`transaction.Repo.CreateAll`), returning the new ids, `[]` with nothing inserted when there are no gaps; another user's portfolio is 404; swag annotations and `backend/apidocs/`; on the frontend `CashGap`, `useCashGaps(scope)` keyed under the history key so a transaction change refetches it, and `useFillCashGaps(scope)`, which refreshes every portfolio query; dated note in ADR 0011, and the roadmap gets Phase 30 (GLG-1109).
- Notice and dialog: `components/CashGapNotice.tsx` replaces the bare `performance.periods.undefined` text in the "Dönemsel getiri" card of `PerformancePage` and in `DrawdownCard`; with gaps it names, per portfolio (with its name in the `all` scope), the first day the cash went below zero and the shortfall per currency, the exact sum of that currency's gaps, says why and shows "Eksik para girişlerini ekle"; without gaps it says the period starts with no value, and a failed load keeps the old text; the button opens a confirm dialog listing each deposit (date, the portfolio in `all`, currency, amount) whose "Ekle" posts the localized note "Eksik para girişi (otomatik)" and closes, a failed post showing its error in the dialog; both cards share the gaps query, so one confirm fixes both; `performance.cashGaps.*` strings in `tr` and `en` (GLG-1110).

## Phase 31 — Open-source readiness

Status: shipped 2026-10-08 · Epic GLG-1112

Goal: the repository can go public under AGPL-3.0. The scope came from a
readiness review on 2026-10-07: six audits (git history, PII in the tree,
backend security, licenses and third-party data, deploy and frontend, docs
hygiene) plus `govulncheck` and `npm audit`. Found clean and left without a
ticket: no secrets in the tree or the history, no committed `.env`, `.db`
or `.pdf`, auth, sessions, CSRF, SQL, PDF parsing and secret storage,
`govulncheck`, and every dependency license AGPL-compatible.
Decisions taken by the owner on 2026-10-07: the real amounts, quantities
and dates in the Midas and Garanti Yatırım statement fixtures are perturbed
(identity fields were already placeholders); the TEFAS fetcher becomes
opt-in and off by default, and the docs stop framing tefas.gov.tr's
robots.txt as an accepted bypass; the git history is published as is, with
no rewrite or squash; the AI-driven workflow stays public, so `CLAUDE.md`
and `.claude/` remain and only stale infrastructure notes and personal
disclosures go; registration closes by default after setup; the license is
AGPL-3.0-only with the line "Copyright (C) 2026 Doğa Uzuncukoğlu" in the README and
no per-file SPDX headers; security reports go through GitHub private
vulnerability reporting.
Decisions taken during the epic: explicit by-name TEFAS runs (Run now,
`-fetch tefas`, `cmd/symbols -market tefas`) still fetch, as for any
disabled source (GLG-1114); the client IP is counted from the right of
`X-Forwarded-For` rather than trusting a proxy to replace the header
(GLG-1116); the Swagger UI stays on the CDN, pinned with SRI, not vendored
(GLG-1123); the owner-only database mode is set in `internal/db`, not by a
process-wide umask (GLG-1128).

- Statement fixtures: every amount, quantity, price, FX rate and date in the Midas USD/TRY and Garanti Yatırım samples is invented, including the account opening date, the statement periods and the trade timestamps, in the same formats; totals, profit and loss, dividend net and Garanti's pro-rata commission and running balances are recomputed so the assertions still reconcile; every copy of the old figures follows (the server import stub, the ImportPage preview, the transaction, summary and dividend tests, the parser comments), both fixture comments say the samples are synthetic, and ADR 0006's PII paragraph now requires synthetic figures (GLG-1113).
- TEFAS opt-in: `scheduler.Enabled` defaults `source.tefas.enabled` to off, which gates the scheduler, catch-up, startup backfill, on-demand fetch and the statement fund lookup, and `-backfill-prices` skips TEFAS unless the source is on; the admin sources table notes on the tefas row that the endpoint is disallowed by robots.txt and that enabling it is the operator's decision, in `tr` and `en`; README, RUNBOOK and the `GET /admin/sources` swag description name the default (GLG-1114).
- Docs scrub: personal disclosures, statement dates and the private artifact link leave the ADRs and HISTORY, ADR 0018 and Phase 22 get a neutral Garanti BBVA motivation, ADR 0004 says only that a third-party crawler was looked at and not used, ADR 0004, ADR 0010 and HISTORY state the robots.txt disallow as a fact with TEFAS off by default, the crawler robots lines go, and ADR 0015 loses the billing, owner, secret and routines-URL lines (GLG-1115).
- Client IP: with `TRUST_PROXY=true`, `ratelimit.ClientIP` takes the `X-Forwarded-For` entry `TRUSTED_PROXY_HOPS` places from the right (default 1, the backend refuses a non-integer or a value below 1) instead of the leftmost one a client can forge; compose sets 2 for Caddy then nginx; documented in both `.env.example` files, the deploy README and RUNBOOK and a superseded note on ADR 0020 (GLG-1116).
- Registration: an unset `auth.registration_open` now means closed, so a fresh install takes no sign-ups until an admin opens registration, and an instance that never set the toggle flips to closed on upgrade; RUNBOOK and ADR 0010 say so (GLG-1117).
- Backend container: a root `.dockerignore` keeps `.git`, `.claude`, `node_modules`, databases, `deploy/data`, `.env` files and `frontend/dist` out of the build context; the backend runs as uid 65532 on `distroless/static-debian12:nonroot` with `read_only`, `cap_drop: [ALL]`, `no-new-privileges` and a `/tmp` tmpfs; the frontend gets a healthcheck Caddy waits for; `backup.sh` writes owner-only files; README and RUNBOOK document the `chown 65532:65532` of `deploy/data` and `sudo` for backups and restores (GLG-1118).
- Server hardening: `http.Server` sets `ReadHeaderTimeout` 10s, `ReadTimeout` 60s and `IdleTimeout` 120s, `WriteTimeout` staying off for long statement previews; a new `HOST` env sets the bind address and `make dev` binds the backend to `127.0.0.1`; every fetcher in `internal/` caps its response read, 32 MB for the BIST bulletin zip and 8 MB elsewhere (GLG-1119).
- License and data sources: the README carries the copyright line and AGPL-3.0-only and links `LICENSE` and a new `THIRD_PARTY_NOTICES.md`, which `make notices` regenerates from go-licenses and license-checker with every license file of each module and the Go standard library's; a README "Data sources" table lists each provider, its use and its access terms; both images carry the notices (GLG-1120).
- Contributor docs: `SECURITY.md` (GitHub private vulnerability reporting, `main` only, a first reply within a week, no bounty) and `CONTRIBUTING.md` (commands, PR format, test-first bug fixes, swag, i18n, and the agent and Jira workflow); outside contributors open a GitHub issue instead of a GLG ticket and drop the `(GLG-<n>)` title suffix, and `CLAUDE.md` and the README point there (GLG-1121).
- Repo hygiene: `.gitignore` ignores `*.pdf` and `.claude/settings.local.json`, the Makefile runs `chmod 600 .env` after writing the generated `SECRET_KEY`, and the transitive `source-map-js` goes to 1.2.2 for GHSA-68fv-2mgg-jv7q (GLG-1122).
- API docs: the docs page pins `swagger-ui-dist@5.33.0` with sha384 `integrity` and `crossorigin`, and `mountDocs` leaves the docs unmounted, with one startup warning, when `COOKIE_SECURE=true`; dated amendment to ADR 0013 (GLG-1123).
- nginx: `server_tokens off`, a `Permissions-Policy` header, `immutable` caching for the hashed `/assets/*` and `no-cache` for everything else, and image tags pinned to their minor (`nginx:1.31-alpine`, `node:22.23-alpine`, `caddy:2.11`) (GLG-1124).
- Frontend container: `nginxinc/nginx-unprivileged:1.31-alpine` runs nginx as uid 101 on port 8080, with Caddy, the healthcheck and `EXPOSE` following; the service gets `read_only`, `cap_drop: [ALL]`, `no-new-privileges` and tmpfs mounts for `/tmp` and the rendered `conf.d`; superseded note on ADR 0020 (GLG-1127).
- Database file mode: `db.Open` creates or tightens the database file to 0600 before SQLite opens it, so `-wal` and `-shm` follow, and `db.Backup` creates its snapshot `O_EXCL` 0600 before `VACUUM INTO` (GLG-1128).
- License in the images: the backend image carries `/LICENSE` and the frontend serves `/LICENSE` as UTF-8 text next to the notices (GLG-1130).
- Symbol tool: `cmd/symbols` caps every upstream read with the same limits as the internal fetchers (GLG-1131).
- Windows build: the PDF worker's `RLIMIT_DATA` cap moves into `rlimit_linux.go` with a no-op `rlimit_other.go`, so `GOOS=windows go build ./...` succeeds and the cap stays Linux only (GLG-1133).

## Code-quality sweep 2 and import fixes

Status: shipped 2026-10-08 to 2026-10-14 · no epic, GLG-1134 to GLG-1366, PRs #1102 to #1324 and #1 of the public repository

Goal: the findings of two code-quality checks (2026-10-08 and 2026-10-09,
label `code-quality`), of a full import of real broker statements on a fresh
database (2026-10-08), of the doc checks and of the QA runs up to 2026-10-14
are fixed, each by its own ticket with no epic, and each line the code-quality
tickets drew is held by a linter `make lint` runs. Tickets closed without a
change are left out.

- Linters and lint steps: golangci-lint adds `usestdlibvars` with `time-layout`, so date layouts are `time.DateOnly` (GLG-1160), a `forbidigo` ban on `strings.Repeat` outside `db.InList` (GLG-1161), `revive` `argument-limit` at 6 (GLG-1172), `gocognit` at 75 (GLG-1233), `noctx`, with a context passed into the user and auth repositories (GLG-1236), `nestif` at 10 (GLG-1237), `funlen` at 150 lines (GLG-1274) and a `forbidigo` ban on `price.MinDate` outside `price.CheckDate` (GLG-1275), and `dupl` drops to a threshold of 60 (GLG-1238); eslint makes `max-lines-per-function` (300, tests exempt) an error (GLG-1162) and `max-params` at four an error (GLG-1230), and bans `useQuery` and the mutating `api/` calls in components and pages (GLG-1164), a hand-written `document.body` focus check (GLG-1163), a bare `dialog` element (GLG-1165), a `md:hidden` list beside a desktop one (GLG-1166), an `Intl` formatter built inside a function, with or without `new` (GLG-1170, GLG-1175), `aria-disabled` outside the `pending` prop (GLG-1245), a hand-rolled `radiogroup` (GLG-1248), a redeclared share format (GLG-1250), `role="combobox"` (GLG-1285), `.replace` on `inputClass` or a clashing class appended to it (GLG-1284, GLG-1295), a copied sheet panel class string (GLG-1283), a bare `table` (GLG-1289), `aria-sort` outside `ui/SortHeader` (GLG-1286), an inline excess return, decimal add or subtract and day count (GLG-1287, GLG-1288, GLG-1291) and `useQueryClient` in components and pages (GLG-1282); `make lint` adds `npm run lint:dupl` (`jscpd`, 10 lines, tests exempt) (GLG-1252) and `npm run knip` for unused exports and types (GLG-1246), and its prettier step checks `.claude/` too (GLG-1134). Later in the sweep `make lint` adds `deadcode`, failing on any function only tests call (GLG-1331), `dupl` drops to 45 (GLG-1343), `gocognit` drops to 45 (GLG-1345), a `forbidigo` ban on `price.Istanbul` outside the day helpers keeps the zone in one place (GLG-1340) and jscpd drops to 6 lines (GLG-1350); eslint bans copying a `Map` group per row (GLG-1337), a hand-rolled `.current = true` double-click guard (GLG-1341), `aria-busy` outside `ui/ChartFrame` (GLG-1342), a helper that takes `t` as an argument (GLG-1344), a division by `prev_close` or `market_value_reporting` (GLG-1346), `pages.flatMap` outside `api/transactions.ts` (GLG-1347), a `state as` cast of router state (GLG-1348) and an inline `String()` around a `t()` var (GLG-1349).
- Backend structure: the startup backfill walks the scheduler sources' `Backfill` (GLG-1152), each backfill source name is defined once in its provider package (GLG-1154) and the scheduler source names once in `internal/scheduler` (GLG-1179), with the on-demand fetch mapping its market to them explicitly (GLG-1210); transaction filters are parsed and applied in one place (GLG-1155); TEFAS, crypto and US share one tracked-instrument backfill loop (GLG-1156); latest closes and merged price series load through two `holdings` helpers (GLG-1157); one conversion rule and one FX currency-set routine (GLG-1158); statement parse errors are appended with named fields in every parser (GLG-1159, GLG-1199); every SQL `IN` list is built with `db.InList` (GLG-1161); long parameter lists become structs: the import repo's rows (GLG-1217), the import routes' deps (GLG-1218), the server's repos and fetchers (GLG-1219), `computeSummary`'s prices (GLG-1220), Garanti trades as named-field literals (GLG-1221), the merged series repos (GLG-1223), US, gold and TEFAS `Syncer`s (GLG-1224, GLG-1225, GLG-1226) and the backfill's run-wide values (GLG-1227), and `fillTransaction`'s eight parameters go by inlining it into its scan function (GLG-1222); `proposeOpenings` splits into row builders (GLG-1233); the on-demand fetch is flattened (GLG-1237); `main.go`'s scheduler source guards are deduplicated (GLG-1238); the market-data providers share one size-capped fetch helper (GLG-1241); `TestPortfolioHistoryBenchmark` splits into per-benchmark helpers (GLG-1258); the statement commit's database transaction moves into `ImportRepo.Commit` (GLG-1273); the seven functions over 150 lines split into helpers (GLG-1274); one user-date check, `price.CheckDate` (GLG-1275); `httpbody` owns the one default market-data client and base URLs fall back through `cmp.Or` (GLG-1276), and `cmd/symbols` uses that client (GLG-1294) and `internal/bist`'s bulletin parse (GLG-1277); one PDF number parse, `statement.ParseNumber` (GLG-1278); handlers read the user with `auth.MustUserID` and check portfolio ownership with `portfolio.Owned` (GLG-1280); the history sweep reads its conversion factor from `holdings.Factor` (GLG-1281); the five server handlers embed `deps` instead of copying its fields (GLG-1327); `internal/price` shares one row scan per repo (GLG-1329); the test-only `ImportRepo` methods are removed (GLG-1331); the backfill helpers take the scheduler sources' repos and syncers (GLG-1332); every per-instrument price sync runs through one `instrument.EachTracked` loop (GLG-1333); a path instrument id is looked up through `instrument.FromPath` (GLG-1334); the expected-dividends walk moves into `dividend.Expected` (GLG-1335); the Istanbul yesterday, market date parse and UTC candle day are shared helpers (GLG-1340); the six functions over `gocognit` 45 split into helpers (GLG-1345); the on-demand price tests build their dates with `btcturk.LastCandleDay` and `price.Midnight` (GLG-1351).
- Frontend structure: `ImportPage` splits into a review reducer, row transforms and review components (GLG-1162); focus recovery after a row leaves the page is shared (GLG-1163, GLG-1228); queries and mutations move into `api/` hooks (GLG-1164), the expected-dividend add and dismiss flow too (GLG-1243); every dialog renders through one modal shell with the nested cancel guard (GLG-1165); one layout per viewport in `UsersSection` and `ExpectedDividends` (GLG-1166); the import preview shares the type pill, quantity × price and fee cells (GLG-1167) and its instrument cell is controlled with one change picker (GLG-1251); the position trade and price dialogs share one hook (GLG-1171), and the dialog and the page share the position title, actions and transactions (GLG-1244); `TransactionsPage` splits into list, filter and banner components (GLG-1200) and the form helpers move to `transactionForm.ts` (GLG-1201); `LineChart`'s drag to zoom becomes `useDragZoom` (GLG-1202) and the chart wrappers share their frame and axis constants (GLG-1253); non-component exports leave component files (GLG-1229); busy controls use the `pending` prop (GLG-1245); theme, privacy, locale, pending deletes and the toast store build on one `createStore` (GLG-1247, GLG-1255); the transaction type picker renders `ui/Segmented` (GLG-1248); one `isCashIn` rule for cells, form and reconcile (GLG-1249); one `SHARE_FORMAT` and a shared weight bar (GLG-1250); the last page-level query cache calls move into `api/` hooks (GLG-1282); dialogs share their panel classes through `ui/sheetPanel` (GLG-1283); `inputClass` takes size, width, read-only and trailing-icon options instead of string surgery (GLG-1284, GLG-1295), and a `className` passed to `Select` is linted against it (GLG-1298); the instrument search is built on Base UI `Autocomplete` (GLG-1285); the sortable tables share `ui/SortHeader` and `nextSort` (GLG-1286) and the expected dividends table is built on `ui/table` (GLG-1289); one excess-over-benchmark return (GLG-1287), exact decimal add, subtract and compare in `lib/decimal` (GLG-1288) and `daysBetween` in `lib/date` (GLG-1291); `PortfolioHeader` takes one `add` prop (GLG-1290); the import preview's opening deposit and closing withdrawal helpers fold into one `withCashRows` (GLG-1338); the portfolio pages are keyed through one layout route and the named lazy imports share `lazyNamed` (GLG-1339); the expected-dividend add and dismiss are guarded by `useOneAtATime` (GLG-1341); the history charts render through one `ui/ChartFrame` (GLG-1342); `marketLabel` calls `t()` itself (GLG-1344); the Dikkat card's mover ratio, the AI prompt's weight and the realized percent use `lib/positions` (GLG-1346); `useTransactions` returns the flattened rows (GLG-1347); navigation state is typed through one `useNavState` hook (GLG-1348); `t()` takes numbers, so counts lose their `String()` wrappers (GLG-1349); the tables share `ui/ActionsTh` and the transaction header cells (GLG-1350).
- Money internals and lookups: price closes, FX rates and benchmark values are `decimal.Decimal`, scanned from their TEXT columns, with the JSON unchanged (GLG-1235); transaction, dividend and split money columns scan straight into decimals (GLG-1239); the statement preview computes on decimal fields, its JSON unchanged (GLG-1240); the summary's previous closes load in two queries per distinct date instead of two per instrument (GLG-1234), and the latest FX rates in one query (GLG-1256); the statement preview loads its instruments in one query and lists transactions once (GLG-1153, GLG-1173), and memoizes its instrument lookups, which the commit loads in one query (GLG-1272); instrument lookups by id are chunked below SQLite's variable limit (GLG-1293); a statement's alias and name instruments load in one query (GLG-1330); the statement preview reads and splits the TEFAS fund list once per request (GLG-1336); the unknown-name and cash-gap groups push into the existing group instead of copying it per row (GLG-1337); the value-history sweep converts each currency's sum once instead of each position, builds its positions in one sorted slice and reads the database through a 256 MiB `mmap_size`, with `BenchmarkHistorySweep` to measure it (GLG-1363).
- Frontend loading: the eight signed-in pages are lazy-loaded behind one `Suspense` with a `page.loading` status (GLG-1242), and the app shell and setup step 2 leave the login page's bundle (GLG-1259); the transactions page keeps the instrument filter's search text out of its state (GLG-1168) and the Istanbul day formatter is built once (GLG-1170).
- Statement import: the BIST gold certificate `ALTIN.S1` is an instrument, the bulletin's `EMS` group with suffix `.S1` (GLG-1174), and `.S1` is stripped in CSV symbols too (GLG-1176); Midas "Diğer Gider" stopaj rows are fees (GLG-1177), "Ücretsiz İşlem" rows deposits (GLG-1187), and its interest row dated the day after `period_end` is accepted (GLG-1180); a fund an earlier statement names by its old name resolves through the batch's `names` (GLG-1181), an unmatched TEFAS name gets a renamed-fund hint (GLG-1197), and cut or abbreviated Garanti fund names match TEFAS funds (GLG-1188); a file already imported into another portfolio is a 409 `already_imported` (GLG-1182); two different files of one period both stay queued with a same-period note (GLG-1183) and the queue header names the statement's currency (GLG-1194); opening rows are priced at the statement's average cost first (GLG-1185), BIST openings sync the bulletin for their price (GLG-1189), and an opening or a closing sell before an in-period split is sized in pre-split units (GLG-1198, GLG-1212); closing sells are proposed for positions the statement omits, with a matching withdrawal (GLG-1190); the reconciliation is recomputed live from the edited rows (GLG-1191); unknown names are resolved once in a block above the table (GLG-1193); account numbers and IBANs are masked in the preview (GLG-1196); the instrument cell wraps (GLG-1184); a preview with no rows returns `[]` (GLG-1205); focus moves into the banner after a commit (GLG-1208); a superseded parse request is aborted (GLG-1231); a CSV that is not UTF-8 is read as Windows-1254 (GLG-1261); every statement, not only a CSV, is capped at 5000 rows (GLG-1269); Garanti fund movements match their codes and Garanti Yatırım commissions split in linear time (GLG-1270, GLG-1271); a CSV's period is computed by one `csvPeriod` in parse and commit (GLG-1279); a signed Garanti fund cell is unreadable, not a buy (GLG-1304); Vazgeç is ignored while a commit runs, in a batch (GLG-1296) and outside one (GLG-1299), and the second click of a double click on a batch's Atla or İçe aktar does not reach the next statement (GLG-1297); picking an instrument for an unknown row keeps a currency the file gave, and a contradicting one makes the row an error (GLG-1320), which the row names with its currency and its edit dialog books in the instrument's currency (GLG-1361).
- Bug fixes elsewhere: the positions card search folds Turkish letters (GLG-1143); a long portfolio name wraps in the cash-gap dialog (GLG-1144); focus stays on the Dikkat card after a price is saved (GLG-1145), moves to a neighbouring user after a delete (GLG-1146), returns to the delete button when the delete-user confirm is cancelled (GLG-1207) and stays on the portfolio tab row after a keyboard tab change (GLG-1262); every P/L-over-cost percent uses one rule (GLG-1169); a money amount that rounds to zero loses its sign (GLG-1186); instrument search matches every typed word and says when results are cut (GLG-1192); the day change is totalled once on the server as `day_change` (GLG-1204); the chart load error text follows a locale switch (GLG-1254); a double click adds an expected dividend once (GLG-1260) and sends one request on create, register and admin saves (GLG-1268) and on the remaining admin and import actions (GLG-1292); the server unreachable screen offers a retry (GLG-1263); a picked instrument's label stays out of the instrument search (GLG-1305), the gold market word matches inside a multi-word search (GLG-1306) and the search's hidden dismiss buttons are named from i18n (GLG-1322); the phone transaction row's details line takes the row's full width (GLG-1307) and runs under the icon buttons (GLG-1321); focus stays in the provider row after a key save or clear (GLG-1308) and a key test result is announced to screen readers (GLG-1309); an admin-added instrument's symbol and name are length-capped (GLG-1310); each failed source's run disclosure is named after its source (GLG-1311); the allocation header claims cash only when the donut has cash (GLG-1323); creating a portfolio from Tüm portföyler focuses its add button (GLG-1324); `us.Sync` continues past a failing symbol, so one bad symbol no longer fails the US run (GLG-1333); dark `muted` and `loss` keep AA contrast on a hovered table row (GLG-1356); a pasted password over 72 characters is no longer cut in only the first of two fields, so identical pastes match and the server's 72-byte limit answers (GLG-1353); the summary sends `total_pl_reporting`, `net_contribution` and `dividends_net` rounded once, and the dividend yield on cost divides by `cost_basis` (GLG-1354); creating a portfolio from an empty state opens it with its add button focused (GLG-1355); the period returns heatmap's scroller is reachable by keyboard (GLG-1357); the positions row-menu and portfolios sparkline header cells are named (GLG-1358); English reads "1 unit" for a position of exactly 1 (GLG-1359); the second click of a double click on a confirm dialog's button no longer reaches the row under it (GLG-1360); a one-shot server flag with an empty value (`-fetch`, `-backup`, `-set-password`, `-add-split`) exits 2 with a usage error instead of starting the server (GLG-1362); the edit, clone and delete buttons of a transaction row, and the import preview's row actions, add the row's quantity × price and signed amount to their names, so a TRY and a USD deposit on one day no longer share one name (GLG-1364).
- Docs: drifted line references in ADR 0019 and 0020 name their code and sections, and ADR 0019 records the oversold buy rule (GLG-1135, GLG-1136); RUNBOOK names TEFAS off by default (GLG-1137); ADR 0010's open-registration risk is marked superseded (GLG-1138); ADR status lines and the index name their amendments (GLG-1139, GLG-1148), GLG-1086's ADR 0023 amendment gets its date (GLG-1150), GLG-749's and GLG-1123's notes take their Istanbul merge date (GLG-1151), ADR 0006's status line lists the tickets that edited its body (GLG-1232), and an inline edit inside another ticket's note is not an amendment (GLG-1149); Phase 31 enters this history (GLG-1140); ADR 0020 notes the images' license files (GLG-1141) and ADR 0016 the positions dialog entry (GLG-1142); ADR 0006 and 0018 note the import changes of GLG-1180 to GLG-1191 (GLG-1206), and ADR 0001 and 0006 the `.S1` suffix, "Diğer Gider" and preview masking (GLG-1265); README, PRINCIPLES and the code-quality skill list every golangci-lint linter (GLG-1209), and the `make help` text and CONTRIBUTING every `make lint` step (GLG-1264); ADR 0023 notes the type picker renders `ui/Segmented` (GLG-1266) and its status line and index row list the Autocomplete amendment (GLG-1302); the work after GLG-1133 enters this history (GLG-1267); README, PRINCIPLES and the code-quality skill list `funlen` (GLG-1300); ADR 0006 notes every statement is capped at 5000 rows (GLG-1301); CLAUDE.md's tech stack lists Base UI checkbox and autocomplete (GLG-1312); the status lines and index rows of ADR 0004, 0005, 0010, 0015 and 0018 name two tickets that edited their bodies, and Phase 11 says TEFAS has been off by default only since Phase 31 (GLG-1313); ADR 0009, 0020 and 0021 use the superseded note kind and Istanbul dates (GLG-1314); ADR 0023 names `ui/inputClass.ts` as the home of `inputClass` (GLG-1315); the cash-gap spec notes its confirm is the native dialog through `useModal` (GLG-1316); the swag annotations and the Midas parser's doc match the handlers (GLG-1317); the deploy README says `TRUSTED_PROXY_HOPS` is checked only with `TRUST_PROXY` on (GLG-1318); ADR 0005's status line and index row list GLG-794 (GLG-1319) and ADR 0023's index row lists GLG-1356 (GLG-1365); the `price.decimalPattern` comment names the right digit limits (GLG-1325); the work merged through GLG-1313, GLG-1325, GLG-1350 and GLG-1363 enters this history (GLG-1303, GLG-1326, GLG-1352, GLG-1366, #1 of the public repository); a pre-publication review drops the owner's holding claim from ADR 0004, dates the private-repo rationale in ADR 0020 and this history, and stops citing the unpublished design canvas in ADR 0005 and this history (#1309).

## Public repository

Status: 2026-10-14

The project moved to a new public repository, `dogauzun/gelirgider`, started
from one commit holding the former private repository's `main` at that date
(commit 91f7b7d3 there). The former repository is archived as
`dogauzun/gelirgider-private`. Pull request numbers cited above and in commit
subjects before this date refer to that former repository; the numbering
starts again at 1 here.

GitHub Actions runs `make lint`, `make test` and `make build-frontend` on every
pull request and on `main` (GLG-1372, #4) and checks pull request titles
(GLG-1374, #3); the `test` job then splits into `test-backend` and
`test-frontend` (GLG-1379, #9). The five checks are required on `main` and,
green on the PR head, are the merge bar in place of the merger's local run (GLG-1373, #7).
The owner set the repository up the same day: Actions allows GitHub-owned
actions only (`actions/*`, `github/*`); CodeQL default setup runs, and the
three alerts it raised were cleared by GLG-1377 (#6); Dependabot alerts and
security updates, secret scanning with push protection and private
vulnerability reporting are on; `main` requires `lint`, `test-backend`,
`test-frontend`, `build-frontend` and `pr-title` with branches up to date.

## Published images

Status: 2026-10-14

`.github/workflows/publish.yml` builds the backend and frontend images on
every push to `main` and every `v*` tag and publishes them, `linux/amd64`
only, as `ghcr.io/dogauzun/gelirgider-backend` and
`ghcr.io/dogauzun/gelirgider-frontend`: `latest` and `sha-<7>` from `main`,
the tag name from a tag (GLG-1382, #11). `deploy/compose.yml` pulls those
images instead of building them on the Docker host, at the tag `IMAGE_TAG`
names in `deploy/.env` (`latest` by default, `sha-<7>` or `vX.Y.Z` to pin a
version or roll back), and keeps `build:` for a modified checkout (GLG-1383).
