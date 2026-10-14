# ADR 0019: Core engine and auth

Status: accepted (GLG-712, 2026-10-01). A retro ADR: it records decisions
taken in Phases 1-5, and their later fixes, that had no ADR.

## Context

These decisions lived only in code, old tickets and `docs/HISTORY.md`, so
agents and QA runs keep rediscovering them, and some have been filed as bugs
although they are deliberate, for example negative positions and cash
overdraft. This ADR records what the code does today, why where a ticket or
commit says so ("reason not recorded" where none does), and where it lives.
A bare file name is in the section's package:
`backend/internal/auth` for auth, `backend/internal/holdings` for
`holdings.go`, and `backend/internal/server` for `server.go`, `summary.go`
and `history.go`.

## Decision

### Auth and sessions

- **Passwords are bcrypt hashes** at `bcrypt.DefaultCost`
  (`backend/internal/auth/handlers.go:194`, `setpassword.go:30`; GLG-14,
  commit 8ced1c2, `docs/HISTORY.md`, Phase 1). A password is 8 characters to 72
  bytes, bcrypt's own input limit, may not be only whitespace and is never
  trimmed (`handlers.go:21-26`, `handlers.go:437-453`). A login with an
  unknown email compares against a dummy hash, so response time does not
  reveal whether an email is registered (`handlers.go:70-80`,
  `handlers.go:243-246`; GLG-335, commit 0487d5a).
- **Sessions are server-side rows behind a cookie.** A session id is 32
  random bytes, base64url encoded (`auth.go:133-140`), valid for 30 days from
  creation with no renewal (`auth.go:19-20`). The cookie is `session`,
  `HttpOnly`, `SameSite=Lax`, and `Secure` when `COOKIE_SECURE=true`
  (`handlers.go:425-435`, `backend/internal/config/config.go:31`). The table
  stores only the token's sha256, so a database copy or a `-backup` file
  carries no usable login (`auth.go:124-131`; GLG-408, commit 11c2544).
  Expired rows are deleted on every login and once at startup
  (`auth.go:41-50`, `backend/internal/server/server.go:86`; GLG-120, commit
  ce86ae4), because they otherwise ended up in every backup.
- **One `RequireUser` wrap** guards every `/api/*` route except the public
  auth, setup and health routes registered before it (`server.go:101-110`,
  `server.go:153`, `middleware.go:15-36`). `RequireAdmin` reads `is_admin`
  on every request, so a demotion applies at once (`middleware.go:38-41`).
- **Auth rate limit: 10 requests per minute per client IP**, hardcoded
  (`handlers.go:28-32`), on register, login (`handlers.go:46-47`), the
  password change (`handlers.go:59`) and first-time setup (`setup.go:48`).
  The reason is to slow down credential guessing (GLG-18, commit 13d9454);
  the password change is included because a stolen session could otherwise
  guess the current password (`handlers.go:53-54`). The limiter is an
  in-memory fixed window, and each route has its own counter
  (`backend/internal/ratelimit/ratelimit.go:27-37`). The client IP is the
  first `X-Forwarded-For` entry only when `TRUST_PROXY=true`, else the
  connection's address (`ratelimit.go:16-20`, `ratelimit.go:81-95`).
- **Cross-origin check on mutating requests.** The whole handler is wrapped
  in the stdlib `http.CrossOriginProtection`, so a POST, PUT, PATCH or DELETE
  whose `Sec-Fetch-Site` or `Origin` shows another origin gets 403
  (`server.go:155`, `server.go:158-171`; GLG-407, commit 2065a3d).
  `SameSite=Lax` stops cross-site pages but not another origin of the same
  site, such as another port or a sibling subdomain posting a `text/plain`
  form without a preflight.
- **A password change signs out every other session** and keeps the one
  that made the request (`handlers.go:409-420`; GLG-17, commit a5190e8). If
  the cookie is missing, every session of the user is deleted, since the
  password has already changed (GLG-133, commit 417c8da).
- **No self-service reset.** Sending mail is not part of the product
  (`setpassword.go:15-19`). A forgotten password is reset by the operator
  with `-set-password <email>`, which reads the new password from stdin so
  it stays out of the process list, then exits without serving
  (`backend/cmd/server/main.go:66`, `main.go:136`; GLG-124, commit 51e8991),
  or by an admin with `POST /api/admin/users/{id}/password`
  (`admin.go:22`, `admin.go:117-155`; GLG-304, see
  [ADR 0010](0010-setup-and-admin.md)). Both go through `auth.SetPassword`,
  which applies the same password rule as the HTTP endpoints and deletes
  every session of that user (`setpassword.go:20-40`).

### Holdings engine

- **Holdings are derived, never stored.** Every portfolio event is a
  transaction row, and positions and cash are computed from the rows on each
  request (GLG-25, commit c3f57d5). `backend/internal/holdings` is pure
  computation with no database and no HTTP (GLG-27, commit 7179066).
- **Weighted average cost.** A buy adds `amount + fee` to the cost basis. A
  sell realizes `amount - fee - quantity × avg_cost`, shrinks the cost basis
  by `quantity × avg_cost` and leaves the average cost unchanged
  (`holdings.go:196-235`). GLG-27 chose it over FIFO because the roadmap asks
  for average cost (`docs/HISTORY.md`, Phase 2); FIFO could be added as a second
  method if tax reporting ever lands. A position whose quantity reaches 0
  resets its cost basis and average cost, so a remainder is not folded into
  the next buy (`holdings.go:287-300`; GLG-372, commit f25e42c).
- **Negative positions are allowed on purpose.** No layer checks that a sell
  is covered by holdings. GLG-25 left the check out so the transactions API
  stays independent of the engine, and GLG-27 specified that a negative
  quantity is returned as it is and the UI shows a warning. The test is
  `holdings_test.go:99-101`, and the warning is the "Negatif" / "Negative"
  pill on the summary page (`frontend/src/pages/SummaryPage.tsx:437`). A
  sell with no position realizes its whole net proceeds, since the average
  cost is 0 (`holdings.go:221-222`).
- **Cash overdraft is allowed on purpose.** `addCash` has no floor
  (`holdings.go:183-185`), and cash is kept per currency with no implicit
  conversion ([ADR 0003](0003-multi-currency-and-market-data-sources.md),
  lines 15-18, citing the GLG-25/27 decision). Reason not recorded beyond
  that citation: neither ticket discusses a negative cash balance. A buy
  with no deposit behind it leaves negative cash, which the value history
  flags, since it leaves no base to measure a return on
  (`history.go:405-409`).
- **Same-day ordering.** `transaction.SortForApply` orders rows by date,
  then splits first, then id, which is typing or statement order
  (`backend/internal/transaction/transaction.go:93-150`). A sell that would
  take its instrument below zero has the same day's later buys of that
  instrument moved ahead of it, one at a time, because a newest-first broker
  history enters a day trade sell-first (GLG-372). Any other day keeps id
  order, so a sell followed by a same-day rebuy realizes against the old lot
  (GLG-411, commit e728517). The ordering stays inside each portfolio
  ([ADR 0014](0014-all-portfolios-view.md), lines 77-80).

### Value history is computed on request

There is no snapshot table and no snapshot job: the daily value series is
computed on each request from the transactions and the price history
(`docs/HISTORY.md`, Phase 3; GLG-37, commit 03341c8). Reason not recorded. The
handler keeps one `holdings.State` per portfolio for the whole sweep and
applies each transaction once (`history.go:389-394`), since replaying the
prefix every day had cost about 6.4 s of CPU at 5001 transactions (GLG-131,
commit 4a3995a). A range is at most 3660 days (`history.go:34`).

### Manual prices

A manual price is per user, stored in `manual_prices` keyed by user,
instrument and date (`backend/internal/db/schema/009_manual_prices.sql`), and
every query filters by `user_id` (`backend/internal/price/manual.go:15-18`).
Before GLG-103 the manual entry wrote into the shared `prices` table and
clobbered fetched prices and other users' entries (commit 0a382e3). It is
set and deleted with `PUT` and `DELETE /api/instruments/{id}/prices/{date}`
(`backend/internal/price/routes.go:34-35`), so it applies to the instrument in
every portfolio of the user.

A manual price wins over a fetched one **on the same date** only: the user is
correcting that date's value (`holdings.MergeSeries`, `holdings.go:18-46`).
Otherwise the newer row wins, so a fetched price dated after a manual one
replaces it as the last price (`summary.go:266-268`, `history.go:312-313`).
The summary reports `price_source` `manual` so the page can tag the price and
offer its deletion (`summary.go:77-82`; GLG-134).

### On-demand fetch, backfill queue and startup catch-up

- **On-demand fetch after a save.** Every create or update of a transaction
  that has an instrument, or is cash in a currency other than TRY, calls the
  `OnDemandFetch` callback with a 5 second timeout; errors are logged and
  ignored, since the transaction is already saved
  (`backend/internal/transaction/handlers.go:26-47`, `handlers.go:137-147`).
  Before this, a new symbol showed no P/L until the next scheduler run
  (GLG-182, commit 3716313). For BIST and US the synchronous fetch runs only
  when the instrument has no price on or before today, so it costs one fetch
  per new symbol (`server.go:251-282`). A source disabled in settings is not
  fetched (`server.go:232-238`). The rest
  of the per-market behaviour is described at `server.go:189-215`.

  > Amended 2026-10-01 (GLG-743): a save of a `GOLD/GRAM` transaction now
  > fetches the gram gold series (ADR 0017) from the transaction date, but
  > not before 2022-06-30, in one BtcTurk request via
  > `btcturk.SyncGoldHistory`, the range rule the startup gold backfill also
  > uses, and skips it when stored prices already cover the range. Before,
  > a first gold buy got no price until the next `gold` run and no older
  > history until a restart. The gold coins still fetch nothing.

  > Amended 2026-10-02 (GLG-749): the gold fetch now runs on every save that
  > reaches the callback, in any market and for cash, while the `gold`
  > source is enabled, so the GOLD benchmark is filled like USD and EUR.

- **Background backfill.** The callback then queues a walk from the
  transaction date, clamped to `bist.BackfillMinDate`, to yesterday
  (`server.go:479-495`), so the history chart has a close for every trading
  day instead of the cost basis (GLG-189, commit bbf3db9). TCMB FX rates are
  walked for every market while the `tcmb` source is enabled
  (`server.go:291-293`; GLG-192, commit 391f062).
- **One queue, one worker.** `backfillQueue` runs one walk at a time and
  keeps requests that arrive meanwhile as pending, merged per source to the
  earliest start date (`server.go:338-357`). It replaced a `TryLock` that
  dropped them and left gaps after an import (GLG-217, commit 4ad7964). A
  walk has a 2 hour bound (`server.go:183-187`, `server.go:434-471`).
- **Startup catch-up, then history backfill, then the scheduler**, in one
  goroutine off the HTTP server, so startup never blocks on the network and
  only one network walker runs at a time (`backend/cmd/server/main.go:272-294`).
  The catch-up walks back up to 7 days for each source's latest published
  data (`backend/internal/scheduler/scheduler.go:67-79`), so a backend started
  after the fetch time or after downtime has today's data (GLG-190, commit
  4a39e99). The history backfill fills gaps back to the earliest transaction,
  so a dev or restored database needs no manual `-backfill-prices` (GLG-191,
  commit 9c07a42). Both follow the schedule settings of ADR 0010.
- **`make dev` behaves like production**: the Makefile loads a gitignored
  `.env` (`Makefile:1-2`; GLG-194, commit 12ab303). GLG-193 (commit 00c92bf)
  ran the TEFAS crawler, since removed (`docs/HISTORY.md`, Phase 0).
- **Holiday memory.** A weekday that returned no data is written to
  `empty_fetches` once a later date in the same walk is synced, and
  `HasSource` treats it as synced, so holidays are not requested on every
  restart (`backend/internal/db/schema/012_empty_fetches.sql`,
  `backend/internal/bist/backfill.go:38-43`, `price/price.go:124-155`,
  `fx/fx.go:63-94` under `backend/internal`; GLG-277, commit c997bf4). Empty
  dates at the end of a walk may not be published yet and stay unmarked. The
  table is shared market data, not user data.

## Recorded simplifications

- The rate limiter is in memory and per process, so it only holds for a
  single backend instance (`ratelimit.go:32-33`).
- A session lasts 30 days from login and is never extended.
- One backfill worker and one pending entry per source (`server.go:347-348`).
- The startup backfill and an on-demand backfill can run at once and fetch
  the same date; the ceiling is a few duplicate downloads, since upserts are
  idempotent (`backend/cmd/server/backfill.go:482-487`).
- A run of more than 5 empty weekdays is not remembered as holidays, since
  it looks more like a source outage (`bist/backfill.go:15-19`).

> Superseded 2026-10-07 (GLG-1116): the rate limiter's client IP (Auth and
> sessions) is no longer the first `X-Forwarded-For` entry. nginx appends to
> the header Caddy set, and other proxies keep what the client sent, so the
> first entry could be client-chosen. With `TRUST_PROXY=true` the client IP
> is the entry `TRUSTED_PROXY_HOPS` places from the right (default 1); compose
> sets it to 2 for Caddy then nginx. The `ratelimit.go:16-20` and
> `ratelimit.go:81-95` references above now point at other lines; see
> `TrustedProxyHops` and `ClientIP` in `backend/internal/ratelimit/ratelimit.go`
> and the startup check in `backend/cmd/server/main.go`. See
> [ADR 0020](0020-deployment-topology.md).

> Superseded 2026-10-08 (GLG-1132): the `ratelimit.go:27-37` (Auth and
> sessions) and `ratelimit.go:32-33` (Recorded simplifications) references
> above now point at other lines; the claims still hold. The fixed-window
> limiter is the `Limiter` type and its constructor `New` in
> `backend/internal/ratelimit/ratelimit.go`, and the single-instance note is
> the "ponytail: in-memory limiter" comment on `Limiter`.

> Superseded 2026-10-08 (GLG-1135): most other code line references above
> have drifted too and now point at other lines; the claims still hold.
> Current locations, by name rather than line (a bare file name is in the
> section's package, as above):
>
> - Auth and sessions: the bcrypt cost is in `createUser` in `handlers.go`
>   and `SetPassword`; the length limits are `minPasswordLen` and
>   `maxPasswordLen`, the rule is `validatePassword`; the dummy hash is
>   `dummyHash`, compared in `login`. Session ids are `newSessionID`, the
>   30 days are `sessionTTL`, the stored sha256 is `hashToken`, and expired
>   rows are purged in `CreateSession` and, at startup, by the
>   `DeleteExpiredSessions` call in `newHandler` in `server.go`. The cookie is the `cookie` method of
>   `handler`; `COOKIE_SECURE` is read in `Load` in
>   `backend/internal/config/config.go`. The public routes are
>   `RegisterRoutes` and `RegisterSetupRoutes`, the `protected` mux is
>   mounted with `sessions.RequireUser` at the end of `newHandler` in
>   `server.go`; the middlewares are `RequireUser` and `RequireAdmin` in
>   `middleware.go`. The rate limit is `authRateLimit` and `authRateWindow`
>   in `handlers.go`, applied with `ratelimit.Limit` in `RegisterRoutes`,
>   `RegisterProtectedRoutes` (whose doc comment gives the password-change
>   reason) and `RegisterSetupRoutes` in `setup.go`. The cross-origin check
>   is `withCrossOriginProtection` in `server.go`, in the `return` chain at
>   the end of `newHandler`. Other sessions are signed out at the end of
>   `changePassword`. The `-set-password` flag is defined and handled in
>   `main` in `backend/cmd/server/main.go`; the admin reset is
>   `adminSetPassword` in `admin.go`.
> - Holdings engine: the buy and sell rules are the `TypeBuy` and
>   `TypeSell` cases of `State.Apply` in `holdings.go`, the reset at zero is
>   `resetIfClosed`, and the cash floor's absence is `addCash`. The test is
>   `TestComputeNegativeQuantity` in `holdings_test.go`; the "Negatif" /
>   "Negative" pill (`summary.negativeQuantity`) is now in
>   `frontend/src/components/PositionsTable.tsx`. The negative-cash flag is
>   the `negativeCash` field of `historySweep` in `history.go`. Same-day
>   ordering is `SortForApply` in `transaction.go`.
> - Value history: one `holdings.State` per portfolio is kept in
>   `historyHandler.sweep`; the range cap is `maxHistoryDays`.
> - Manual prices: the per-user queries are the `ManualRepo` methods in
>   `backend/internal/price/manual.go`. Merging is `holdings.MergeSeries`
>   and, for the latest price in the summary, `holdings.LatestClose`; the
>   source tag is the `PriceSource` field of `positionResponse` in
>   `summary.go`.
> - On-demand fetch and backfill: `OnDemandFetchTimeout`, `OnDemandFetch`
>   and `EnsurePrice` in `backend/internal/transaction/handlers.go`;
>   `newOnDemandFetch` in `server.go`, whose doc comment describes the
>   per-market behaviour and whose body checks `scheduler.Enabled`, runs
>   the BIST and US fetch only without a `prices.Latest` row and appends
>   `tcmbSource` while `tcmb` is enabled. The walk is queued by
>   `startBackfill`; the queue is `backfillQueue` with `add`, `next` and
>   `drain`, its "ponytail: one worker and one pending entry per source
>   name" comment is the single-worker note, and the 2 hour bound is
>   `onDemandBackfillTimeout`. The startup sequence is the goroutine in
>   `main` that calls `scheduler.CatchUp`, `backfillStartupHistory` and
>   `scheduler.Run`; the duplicate-download note is the doc comment of
>   `backfillStartupHistory` in `backend/cmd/server/backfill.go`.
> - Holiday memory: the `MarkEmpty` field of `BackfillSource` and the
>   holiday paragraph of `BackfillRange`'s doc comment in
>   `backend/internal/bist/backfill.go`, and `Repo.HasSource` and
>   `Repo.MarkEmpty` in `price/price.go` and `fx/fx.go`. The 5-day run is
>   `maxHolidayRun` in the same `bist/backfill.go`.

> Superseded 2026-10-08 (GLG-1136): the cross-ADR line references in Holdings
> engine have drifted. The ADR 0003 "lines 15-18" citation is the second
> paragraph of "Currency model" in
> [ADR 0003](0003-multi-currency-and-market-data-sources.md), and the ADR 0014
> "lines 77-80" citation is the same-day ordering bullet under "Merge rule:
> one holdings pass per portfolio, then sum" in
> [ADR 0014](0014-all-portfolios-view.md). The claims still hold.
>
> Since GLG-1106 (commit a7c4e8f) a buy no longer always adds `amount + fee`
> to the cost basis. A buy into a negative position first covers the
> oversold quantity like the sell in reverse: for the covered part it books,
> as realized P/L, the short's share of the negative cost basis less what
> covering cost, fee share included, and only the rest of the buy becomes
> cost basis. So a sell with no position still realizes its whole net
> proceeds, but the covering buy books its cost against them. See the
> `TypeBuy` case of `State.Apply` in `holdings.go`.
