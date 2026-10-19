# ADR 0003: Multi-currency model, TCMB FX source, US price source

Status: accepted (2026-09-14).

## Context

Phase 5 adds US stocks and USD or EUR cash, so it needs a currency model, an
FX source and US prices, each source passing robots.txt on its real API host.

## Decision

### Currency model

- Every transaction row carries a `currency`. Buy, sell and dividend rows take
  the **instrument's** currency, set by the server, never accepted from the
  client. Deposit, withdrawal and fee rows take the currency the user picks.
  Allowed: `TRY`, `USD`, `EUR`.
- Cash is kept **per currency**. A USD buy draws on USD cash, nothing converts
  between balances implicitly, and negative cash stays allowed.
- Each **user** has a reporting currency (`users.reporting_currency`, default
  `TRY`). The summary and value-history APIs convert positions and cash into it
  at the rate on the valuation date. Storage stays in each row's own currency.
- Open positions also keep a cost basis in the reporting currency at
  **trade-date** rates, so the summary splits their gain into a price part and
  a currency part (`fx_pl_reporting`), null when a buy predates the currency's
  first rate. Realized P/L and closed positions are converted at the
  valuation-date rate, so their currency effect stays in the portfolio-level
  `fx_pl` residual. That is a known ceiling, upgraded by converting realized
  P/L at the sell-date rate.
  [ADR 0027](0027-return-attribution.md) adds a daily split of a range's
  change, with the currency's share as `fx`; `fx_pl` and `fx_pl_reporting`
  stay and mean "since cost".
- No provider interface: each market is its own package writing the shared
  `prices` or `fx_rates` table (`docs/PRINCIPLES.md` #9).

### FX storage and gaps

`fx_rates(currency, date, rate, source)` holds **TRY per 1 unit of currency**
as decimal text. `TRY` has no rows and is implicitly 1, so every lookup
special-cases it. Converting A to B uses `rate(A) / rate(B)`. A day with no
rate carries the last known rate forward.

There is **no backward carry**. Before a currency's first rate (a fresh
install, a backfill that starts after the earliest trade) its positions and
cash are left out of the totals, and the currency is reported as
`missing_rates` on the value history so the UI can mark the point incomplete.
Carrying the first rate backward would price a 2024 holding at a 2026 rate. The
remedy is to backfill further, never to invent a rate.

### Holdings with no price yet

A holding can predate its instrument's first stored price. The value history
values it **at cost** on those days, marks the point `price_unknown`, and lists
the symbols in `missing_prices`. Dropping the position, as the FX rule does,
would fall by the buy amount on the buy day and jump by its full market value
on the first price day. When the range's first point was incomplete, that
first-price jump counts as that day's flow in the returns calculation (ADR
0011), since the gain was earned before the range.

### FX source: TCMB

TCMB's daily exchange-rate bulletin (`tcmb.gov.tr/kurlar`, one XML file per
date, archive since 1996) is the official rate authority, free, keyless and
allowed by robots.txt. The fetcher stores `ForexBuying / Unit` for `USD` and
`EUR` as `source = 'tcmb'` under the bulletin's own date, matching elements by
name. TCMB publishes around 15:30 Europe/Istanbul. A 404 means a weekend, a
holiday or not yet published, and TCMB does not tell those apart, so it reuses
the scheduler's retry.

### US sources

- **Instruments**: the Nasdaq Trader symbol directory, test issues dropped,
  ETFs kept, as a committed snapshot refreshed with
  `go run ./cmd/symbols -market us` from `backend/`.
- **Latest close, primary for the daily run**: Finnhub `/quote`, one request
  per tracked US instrument only. Its robots.txt is `Allow: /`. The key is an
  encrypted admin setting (ADR 0010), and an unset key fails the fetch. The
  close is dated by the quote's own timestamp: after 16:00 New York on that
  date it is that session's close, otherwise the previous close is stored under
  the earlier session (weekends and the NYSE holidays are skipped).
- **Sector**: after the quotes, the daily run requests Finnhub
  `/stock/profile2` for each tracked US instrument whose `instruments.sector`
  is unset and stores `finnhubIndustry` raw, `''` for an empty profile so it is
  not asked again (ADR 0026). No key skips it.
- **History, splits, dividends**: Tiingo daily prices, one request per symbol
  for any range, unadjusted `close` stored with `source = 'tiingo'`. Its
  robots.txt blocks only `/admin`. The key is an admin setting too, and an unset
  key skips US history with one log line while Finnhub still delivers the
  latest close. The daily run also re-reads a recent window for splits and
  dividends (ADRs 0008, 0009). The free hourly limit is the ceiling, and the
  next save or startup retries what it cut off.
- **Ruled out**: Yahoo, Twelve Data, Stooq, `api.nasdaq.com` and EODHD by
  robots.txt, Alpha Vantage and Massive by small free tiers.

### Manual prices and on-demand fetch

Manual prices are per user. On the same instrument and date a manual price wins
over a fetched one, as a correction. Otherwise the newer date wins.

Saving a transaction fetches what it needs when its market's source is enabled.
A crypto or TEFAS instrument fetches its prices from the transaction date in one
request. A US instrument fetches one Finnhub quote only when it has no price
yet, plus one Tiingo history request. A BIST instrument syncs the latest
bulletin and queues a background bulletin backfill. A background walk also
backfills TCMB rates over the same range for every market. A fetch is skipped
when stored data already covers the range, and a failure is retried next time.
A fetch that has to go to the network takes one process-wide slot first, so
fetches run one at a time: each writes, and SQLite has one writer. A fetch whose
data is already stored (or that has no key to fetch with) never takes the slot.
A caller waits for it at most three fetch timeouts and then skips the fetch; the
fetch's own timeout starts once it has the slot, and the slot is let go when that
timeout ends. A save thus takes at most 15 s of waiting plus 5 s of fetching, a
parse at most 15 s of lookups, 15 s of waiting and 5 s of fetching, under a
proxy's 60 s. The import page parses at most two files at once, so the queue
stays short enough for that wait.
