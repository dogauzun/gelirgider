# ADR 0003: Multi-currency model, TCMB FX source, US price source

Status: accepted (GLG-70, 2026-09-14); amended 2026-09-14 (GLG-82, Finnhub
chosen for US prices after the robots.txt check); amended 2026-09-14
(GLG-135, `missing_rates` on the value history); amended 2026-09-15
(GLG-152, no backward FX carry); amended 2026-09-23 (GLG-236, Tiingo US
price history); amended 2026-09-23 (GLG-254, holdings with no price yet are
valued at cost); amended 2026-09-23 (GLG-259, the Finnhub close is dated by
the quote's timestamp); amended 2026-09-23 (GLG-267, portfolio `fx_pl`
residual); superseded in part by [ADR 0010](0010-setup-and-admin.md)
(GLG-302, GLG-303, 2026-09-23): the Finnhub and Tiingo keys are encrypted
settings entered in the admin area, and each source runs at its own daily
time; amended 2026-09-26 (GLG-393, `returns` replaces
`net_flow_weighted`); amended 2026-09-28 (GLG-524, Phase 16 trade-date cost
for open positions)

## Decision

### Currency model

Every transaction row carries a `currency` column (already present, always
`TRY` today). Buy, sell and dividend rows take the **instrument's** currency,
set by the server from `instruments.currency`, never accepted from the
client. Deposit, withdrawal and fee rows take the currency the user chooses
on the request. Allowed currencies: `TRY`, `USD`, `EUR`.

Cash is kept **per currency**: a portfolio can hold TRY and USD cash at the
same time. A USD buy draws on USD cash; there is no implicit conversion
between cash balances. Negative cash per currency stays allowed, per the
GLG-25/27 decision — this ADR does not change that.

Positions are valued in the instrument's currency. Each user has a
**reporting currency** (`users.reporting_currency`, default `TRY`). The
summary and value-history APIs convert positions and cash into the reporting
currency using the FX rate on the valuation date; storage stays in the
instrument's and the cash row's own currency.

Simplification, recorded with its ceiling: realized P/L and cost basis are
converted at the **valuation-date** rate, not the trade-date rate, so FX
gain/loss is not separated from price gain/loss. Upgrade path: convert each
transaction at its own date's rate and report FX P/L as its own line, if a
user ever needs that breakdown.

Narrowed in Phase 16 (GLG-520) for open positions:

1. Each open position also keeps a cost basis in the reporting currency at
   **trade-date** rates. Every buy is converted at its own date's rate, the
   latest `fx_rates` row on or before that date (the same rule as
   `fxWalk`). A sell reduces it in the same proportion as the native
   `CostBasis`; a close resets it (GLG-527).
2. The summary API reports each position's currency gain as
   `fx_pl_reporting` = `CostBasis` × today's factor − trade-date cost.
   `unrealized_pl_reporting` stays the price part at today's rate, so
   `market_value_reporting` − trade-date cost = `unrealized_pl_reporting` +
   `fx_pl_reporting` (GLG-532).
3. Realized P/L and closed positions stay at the valuation-date rate. That
   is the remaining, narrowed ceiling. Their FX part stays in the portfolio
   `fx_pl` residual (GLG-267) together with the FX on cash; that residual is
   unchanged and still includes the open positions' currency part too, so
   the per-position figures are a breakdown of it, not an addition to it.
   Upgrade path: convert realized P/L at the sell-date rate.
4. A buy dated before its currency's first `fx_rates` row makes that
   position's `fx_pl_reporting` null. There is no backward carry and no
   invented rate, the same rule as for the totals below. The remedy is the
   existing FX backfill.

No new provider interface. Each market gets its own package (`bist` today,
`us`, `tefas`, `tcmb` from Phase 5) that writes the shared `prices` /
`fx_rates` tables with its own `source` value. Provider quirks stay inside
the package, per `docs/PRINCIPLES.md` #9 — the same shape ADR 0001 already
chose for BIST.

### FX storage

Table `fx_rates(currency, date, rate, source)`, primary key `(currency,
date)`. `rate` is **TRY per 1 unit of `currency`**, stored as decimal text.
`TRY` has no rows (rate is implicitly 1). Converting from currency A to
reporting currency B uses `rate(A) / rate(B)`. A missing day carries the last
known rate forward, the same rule already used for `prices`.

There is no backward carry. On days before a currency's first `fx_rates`
row — a fresh install before the first TCMB fetch, a backfill started later
than the earliest trade, or a currency TCMB never published for that
period — the positions and cash in that currency are left out of the totals
entirely rather than converted at a guessed rate, and the currency is
reported back (`holdings.Totals.Missing`, surfaced as `missing_rates` on the
value-history response, GLG-135) so the UI can mark those points as
incomplete. Summary and history share the same conversion, so both behave
this way. The remedy is to backfill further, not to invent a rate (GLG-152).

Carrying the _first_ known rate backward, the mirror of the forward carry,
was rejected: it prices a 2024 holding at a 2026 rate and reports that as
2024 value, which distorts exactly the long ranges the chart exists for.
Dropping the affected points from the series was rejected too: the data is
incomplete, not absent, and the user can fix it by backfilling, so a visible
point plus the `missing_rates` hint says more than a chart that silently
starts later.

### Holdings with no price yet (GLG-254)

A holding can predate its instrument's first `prices` row: a US symbol
bought before the first Finnhub quote, a TEFAS fund before the crawler
window, a BIST gap. On those days the value history keeps valuing it **at
cost**, unlike a missing FX rate, and marks the point instead: each such
point carries `price_unknown: true`, and the response lists the symbols in
`missing_prices`, the union over the range like `missing_rates`.

> Amended 2026-09-23 (GLG-299): the TEFAS crawler is now the backend's
> `tefas` source in `backend/internal/tefas` (ADR 0004); "the crawler
> window" means the range of NAVs that source has fetched so far.

Leaving the holding out, as the FX rule does, was rejected. A missing rate
drops a currency's cash and positions together, so a buy inside that
currency moves nothing. A missing price drops only the position while the
cash that paid for it is still counted, so the total would fall by the buy
amount on the buy day and jump by the full market value on the first price
day: a bigger fake move in both directions than at-cost gives. At cost, the
buy day is neutral and the only jump left is the first price day's move from
cost to market.

That jump is gain earned before the range as much as inside it, so the
period delta must not count it when the range's first point was incomplete.
For each instrument the first point had no price for, the jump (its
unrealized P/L on its first priced day, at that day's rate) is added to
`net_flow`, which the header already subtracts, and counted as that day's
flow in the `returns` calculation (ADR 0011). An instrument bought and first priced inside
the range keeps its jump as gain: all of it happened in the range. The
remedy for the flat stretch is the same as for FX: backfill prices further.

## FX source: TCMB daily exchange rate XML

Verified 2026-09-14 with curl:

```
GET https://www.tcmb.gov.tr/kurlar/today.xml                 -> 200, ~9 KB
GET https://www.tcmb.gov.tr/kurlar/202609/11092026.xml       -> 200 (archive: /kurlar/{YYYYMM}/{DDMMYYYY}.xml)
GET https://www.tcmb.gov.tr/kurlar/202609/12092026.xml       -> 404 (Saturday; also holidays and days not yet published)
```

One `<Currency CurrencyCode="USD">` element per currency:

```xml
<Currency CurrencyCode="USD"><Unit>1</Unit><ForexBuying>48.4305</ForexBuying><ForexSelling>48.5178</ForexSelling>...
```

Store `ForexBuying / Unit` for `USD` and `EUR`, `source = 'tcmb'`. TCMB
publishes around 15:30 Europe/Istanbul; the existing 19:00 scheduler run is
late enough to pick it up same-day.

> Amended 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-303): 19:00 is now only
> the default of the `tcmb` source's own `source.tcmb.at` setting, which an
> admin can change.

## US instruments: Nasdaq Trader symbol directory

Verified 2026-09-14:

```
GET https://www.nasdaqtrader.com/dynamic/symdir/nasdaqlisted.txt   -> pipe-separated, header "Symbol|Security Name|Market Category|Test Issue|Financial Status|Round Lot Size|ETF|NextShares"
GET https://www.nasdaqtrader.com/dynamic/symdir/otherlisted.txt    -> ~7,600 lines, header "ACT Symbol|Security Name|Exchange|CQS Symbol|ETF|Round Lot Size|Test Issue|NASDAQ Symbol"
```

Import both, drop `Test Issue = Y`, keep ETFs, market `US`, currency `USD`.
Snapshot is committed as `us_symbols.csv` next to `bist_symbols.csv` and
refreshed by hand with the importer in `tools/`.

> Superseded 2026-10-06 (GLG-1093): the symbol importers in `tools/` are one
> binary, `backend/cmd/symbols` (#1045); refresh the list with
> `go run ./cmd/symbols -market us` from `backend/`.

## US prices: Finnhub quote endpoint, tracked symbols only

Both originally recorded options are ruled out by `robots.txt` on their
actual API host (checked 2026-09-14, GLG-82): `query1.finance.yahoo.com`
(Yahoo v8 chart) and `api.twelvedata.com` (Twelve Data) both serve a blanket
`User-agent: * / Disallow: /` on the host the fetcher would call; Yahoo's
marketing site additionally lists `anthropic-ai`/`ClaudeBot` by name.
Finnhub's `finnhub.io` (the same host `/api/v1/...` is served from) is
`Allow: /` with only `/terms-of-service` and `/faq` excluded, so it is the
implemented source (GLG-82, `internal/us`):

```
GET https://finnhub.io/api/v1/quote?symbol=AAPL
    header: X-Finnhub-Token: <API key>
    -> {"c":<current price>,"d":<change>,"dp":<percent change>,
        "h":<day high>,"l":<day low>,"o":<day open>,"pc":<previous close>,
        "t":<unix time of c>}
    unknown symbol -> 200 with every field zeroed (no 404)
```

One request per symbol, so the fetcher only fetches US instruments that
appear in at least one transaction (tracked instruments), not the full
directory. The close is dated by the quote's own `t`, not by the run time
(GLG-259): once 16:00 New York on `t`'s date has passed, `c` is that
session's close and is stored under that date (this includes a run before
the next open, when `c` is still the last session's close and `pc` the one
before it); while the session is open, `pc` is stored under the weekday
before. No holiday calendar: the day after a holiday, mid-session, the
pre-holiday close is stored under the holiday, the same value the forward
carry gives that day. Prices are converted from Finnhub's JSON
float to decimal by formatting with 4 decimals before parsing, so no float
binary noise (e.g. `128.89999389648438`) is ever stored. API key is read
from the `FINNHUB_API_KEY` environment variable. Free tier is 60
requests/minute, comfortably above a once-daily, one-lookup-per-tracked-
symbol run; a small delay between symbols is kept anyway. Fetch runs in the
same daily 19:00 Europe/Istanbul job (US market close is 23:00 or 00:00
Istanbul, depending on DST, so that run stores the previous session).

> Superseded 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-302, GLG-303): the
> Finnhub key is the encrypted secret setting `provider.finnhub.api_key`,
> entered in the admin area, and `FINNHUB_API_KEY` is no longer read; the US
> fetch runs at the `us` source's own `source.us.at` time (default 19:00),
> not in one shared daily job.

Because Finnhub's quote endpoint only returns the current previous-close,
not a historical range, backfill re-fetches "the previous close as of now"
per date it is run for rather than replaying each historical day
individually. Finnhub's daily candles (`/stock/candle`) are not in our plan
(`{"error":"You don't have access to this resource."}`, 2026-09-23), so
history comes from Tiingo, below.

## US price history: Tiingo daily prices (GLG-236)

Without history, a US holding was valued at cost until its first Finnhub
quote, then jumped (QA 2026-09-23: +109K TRY in one day for 20 AAPL bought
in February). robots.txt on each candidate's API host, checked 2026-09-23:

- Key-less sources are all blocked: `query1`/`query2.finance.yahoo.com`,
  `stooq.com` (only Googlebot/Bingbot allowed) and `api.nasdaq.com` serve
  `User-agent: * / Disallow: /`; `eodhd.com` disallows `/api/`.
- `api.tiingo.com` redirects its robots.txt to `www.tiingo.com`, which
  disallows only `/admin`. **Chosen.**
- `api.massive.com` (formerly Polygon) and `www.alphavantage.co` have no
  robots.txt; Massive's free tier keeps only 2 years of daily bars, Alpha
  Vantage's free daily series only the last 100 days.

```
GET https://api.tiingo.com/tiingo/daily/AAPL/prices?startDate=2026-09-15
    header: Authorization: Token <API key>
    -> [{"date":"2026-09-15T00:00:00.000Z","close":331.34,"adjClose":331.34,...}, ...]
    unknown ticker -> 404 {"detail":"Error: Ticker 'X' not found"}
```

One request per symbol covers any range. `close` (unadjusted) is stored, the
same basis as Finnhub's previous close and the prices users enter, with
`source = 'tiingo'`; parsed as `json.Number`, so no float noise. Tiingo
spells share classes with a dash (`BRK-B`), the Nasdaq directory with a dot
(`BRK.B`); the fetcher converts. API key from `TIINGO_API_KEY`; unset means
US history is skipped with one log line per process, and the latest close
still comes from Finnhub.

> Superseded 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-302): the Tiingo key is
> the encrypted secret setting `provider.tiingo.api_key`, entered in the
> admin area, and `TIINGO_API_KEY` is no longer read; an unset key still
> skips US history with one log line.

`us.SyncHistory` fetches from the requested start date minus a week (so a
weekend or holiday start still gets the close before it) to Tiingo's
latest, and does nothing when the instrument already has a price on or
before the start date. That makes it cheap to call on every save and every
startup. It runs:

- after a US transaction is saved (on-demand fetch), from the transaction's
  date, synchronously, one request, like the crypto fetch;
- in the startup backfill and `-backfill-prices`, per tracked US instrument
  from its own earliest transaction date (or `-backfill-from`).

The free tier's hourly request limit is the ceiling: a statement import
with many new US symbols can hit it. Failures are logged and the next save
or startup retries the symbols that still have no history.

## TEFAS

Not decided here. `POST https://www.tefas.gov.tr/api/DB/BindHistoryInfo` (the
endpoint most scrapers use) returned 404 `Method not found or disabled!` on
2026-09-14. GLG-71, a spike running in parallel with this ticket, picks the
source and writes ADR 0004.

> Resolved 2026-09-14 by [ADR 0004](0004-tefas-data-source.md) (GLG-71):
> fund list and daily NAV both come from TEFAS's own `api/funds/` JSON
> endpoints.

## Scheduler

The scheduler becomes multi-source (`[]Source`). One daily run at
`PRICE_FETCH_AT` executes every source in sequence (BIST, TCMB, US, later
TEFAS). Each source keeps its own "not yet available" retry; one source
failing does not block the others.

> Superseded 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-303): `PRICE_FETCH_AT`
> is gone. Each source has its own enabled flag and daily time in settings
> (`source.<name>.at`, default 19:00, `tefas` 14:00); the scheduler ticks
> once a minute and runs every due source in sequence, each with its own
> retry.

## Alternatives considered

| Decision           | Option                             | Verdict    | Why                                                                                                                                                               |
| ------------------ | ---------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| FX source          | TCMB daily XML                     | **chosen** | Official rate authority, free, no key, archive since 1996 in the same format.                                                                                     |
| FX source          | ECB reference rates                | rejected   | EUR-based cross rates only; no TRY authority.                                                                                                                     |
| FX source          | Yahoo `USDTRY=X`                   | rejected   | Unofficial, same instability already seen with Yahoo elsewhere (ADR 0001).                                                                                        |
| US instrument list | Nasdaq Trader symbol directory     | **chosen** | Official listing files, free, no key, covers both Nasdaq and other US exchanges.                                                                                  |
| US prices          | Yahoo v8 chart                     | rejected   | `query1.finance.yahoo.com/robots.txt` is a blanket `Disallow: /`; `finance.yahoo.com` additionally names `anthropic-ai`/`ClaudeBot` (GLG-82).                     |
| US prices          | Stooq daily CSV                    | rejected   | Now behind a JavaScript proof-of-work challenge (checked 2026-09-14).                                                                                             |
| US prices          | Twelve Data                        | rejected   | `api.twelvedata.com/robots.txt` is a blanket `Disallow: /` on the actual API host (GLG-82), despite being free-tier viable (800 req/day).                         |
| US prices          | Finnhub quote endpoint             | **chosen** | `finnhub.io/robots.txt` is `Allow: /`; free tier (60 req/min) is well above tracked-instrument volume; API key required, read from env (GLG-82) (see note below). |
| US prices          | Alpha Vantage                      | rejected   | 25 requests/day, too low even for tracked-only volume.                                                                                                            |
| US price history   | Tiingo daily prices                | **chosen** | `www.tiingo.com/robots.txt` disallows only `/admin`; any range in one request; free key (GLG-236).                                                                |
| US price history   | Finnhub `/stock/candle`            | rejected   | Not in our Finnhub plan (GLG-236).                                                                                                                                |
| US price history   | Massive (Polygon) aggregates       | rejected   | Free tier keeps only 2 years of daily bars, 5 req/min.                                                                                                            |
| US price history   | Nasdaq `api.nasdaq.com` historical | rejected   | robots.txt `Disallow: /` (GLG-236).                                                                                                                               |

> Superseded 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-302):
> the Finnhub row's API key is the encrypted setting
> `provider.finnhub.api_key`, entered in the admin area, not an environment
> variable.

## Known risks

- TCMB's URL pattern and XML schema are not a documented API, the same risk
  ADR 0001 already accepted for the BIST bulletin. Mitigation: the fetcher
  matches the `CurrencyCode` attribute and child element names, not position.
- TCMB 404s on non-business days and does not distinguish "holiday" from "not
  yet published". Mitigation: same retry-until-a-cutoff approach as GLG-35,
  reusing the existing scheduler retry rather than inventing a second one.
- Finnhub's quote endpoint gives only the latest previous-close, not a
  historical range. Resolved by Tiingo for history (GLG-236). Finnhub still
  writes the latest close, dated by the quote's own timestamp (GLG-259).
- Tiingo requires a second API key (`TIINGO_API_KEY`) and its free tier has
  an hourly request limit. Mitigation: history is fetched once per symbol
  and range, never re-fetched once stored; unset key degrades to no history.

  > Superseded 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-302): the key is the
  > encrypted setting `provider.tiingo.api_key`, not `TIINGO_API_KEY`.

- Finnhub requires an API key (`FINNHUB_API_KEY`), unlike Yahoo/TCMB/BIST.
  Mitigation: read from environment, never hardcoded; the fetcher fails
  clearly (not a panic) when unset, since it is not set in CI.

  > Superseded 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-302): the key is the
  > encrypted setting `provider.finnhub.api_key`, not `FINNHUB_API_KEY`, and
  > an unset key still fails the fetch with an error. There has been no CI
  > since the same day (GLG-257, [ADR 0015](0015-pr-review-routine.md)).

- Converting realized P/L at the valuation-date rate instead of the
  trade-date rate understates or overstates FX-driven gains on old trades in
  a currency that has moved a lot against TRY. Accepted for Phase 5 and
  narrowed in Phase 16: open positions now split their gain into a price
  part and `fx_pl_reporting` at trade-date rates, so the ceiling is left on
  realized P/L and closed positions only, whose FX part stays in the
  portfolio `fx_pl`. The upgrade path is recorded above.
- `fx_rates` has no row for `TRY`; every call site that looks up a rate must
  special-case `TRY -> 1` rather than expecting a row for every currency in
  `users.reporting_currency` or `instruments.currency`.

## Consequences

- GLG-72 adds the `fx_rates` table and repository.
- GLG-73 makes the transactions API set currency from the instrument for
  trades and accept it on the request for cash rows.
- GLG-74 changes the holdings engine to keep cash per currency and convert
  positions to the reporting currency.
- GLG-75 adds `users.reporting_currency` and the account-page control for it.
- GLG-76 imports the Nasdaq Trader symbol directory as US instruments.
- GLG-77 adds the TCMB fetcher (`internal/tcmb`) that writes `fx_rates`.
- GLG-78 makes the scheduler multi-source and wires TCMB into the daily job.
- GLG-79 and GLG-80 convert the summary and value-history APIs to the
  reporting currency.
- GLG-81 and GLG-84 are the frontend changes for currency selection and
  reporting-currency display.
- GLG-82 adds the US price fetcher (`internal/us`); GLG-85 wires it into the
  daily job and backfill.
- GLG-236 adds Tiingo daily-close history to `internal/us`, wired into the
  on-demand fetch and the startup backfill.
- GLG-71 (parallel spike) and GLG-83, GLG-87 cover TEFAS under ADR 0004, out
  of scope for this ADR.
- GLG-527 keeps each open position's cost basis at trade-date rates in the
  reporting currency in the holdings engine (Phase 16).
- GLG-532 adds per-position `fx_pl_reporting` to the summary API, null when
  a buy of the position has no rate on its date.
- GLG-537 shows a foreign position's currency gain under its unrealized P/L
  on the summary page.
