# ADR 0009: Dividends from provider data

Status: accepted (2026-09-23).

## Context

Dividends are entered by hand, or come from the Midas statement import
(ADR 0006). Without provider data, a user who does not type each dividend in
sees it nowhere.

The `dividend` transaction type holds the gross in `amount` and the stopaj
(withholding tax) in `fee`, and the holdings engine nets the fee out
(ADR 0006). Midas dividend rows carry the payment date.

## Decision

### Dividends are market data, not user data

One global `dividends` table keyed by instrument and ex-date, like `prices`
and `splits` (ADR 0008). It has no `user_id`: it holds no user data.

- `date` is the ex-date, a market `DATE`.
- `amount` is the gross cash per share, an exact decimal, greater than 0, in
  the instrument's currency.
- Last writer wins for the same instrument and ex-date.

### Suggest, do not write

Providers never create user transactions. The app suggests expected dividends
per portfolio, `GET /api/portfolios/{id}/dividends/expected`: the quantity
held before the ex-date times the gross per share. The user adds each one
with one click, which creates a normal `dividend` transaction through the
existing create-transaction API.

Rejected: synthetic rows in the transaction list, like splits, and
transactions created automatically from provider data. Both double count
against dividends entered by hand or imported from Midas.

### Gross in `amount`, stopaj in `fee`

The same convention as the Midas import. Stopaj is pre-filled at a rate per
market, read from the `stopaj_rates` table (`market`, `rate` as an exact
decimal fraction in text) on every request. A market with no row gets 0. The
table is global, like `dividends`. It is seeded with:

| Market    | Stopaj                      |
| --------- | --------------------------- |
| `BIST`    | 15%                         |
| `US`      | 20% (US-Turkey treaty rate) |
| any other | 0                           |

A change in law is an `UPDATE` of one row by the operator, with no release and
no admin UI.

The user can edit it before or after adding. For dividends the fee field is
labelled Stopaj / Withholding tax instead of Komisyon / Commission.

### Sources

- **BIST:** the cash dividend columns of the İş Yatırım capital increase table
  that the bonus-issue fetch already reads (ADR 0008). Gross TRY per share is
  `SHHE_NAKIT_TM_ORAN` / 100, source `isyatirim`, no second request. The same
  robots.txt limit applies: nothing under `/_layouts/`, so the current
  calendar year only. Rows dated after today are stored too: a planned
  dividend lands in `dividends` ahead of its ex-date (the bonus part of the
  same row is still skipped until its ex-date, ADR 0008). `dividend.Expected`
  stops at today, so nothing suggests a planned dividend before its ex-date.
- **US:** Tiingo `divCash` on the existing daily-history call, source
  `tiingo`, currency `USD`. The backfill and the daily recent pass both go
  through it, so no new HTTP calls.
- **TEFAS, CRYPTO and GOLD:** no source.

robots.txt of `www.isyatirim.com.tr`, `api.tiingo.com` and `finnhub.io` allows
these paths (checked 2026-09-23).

### Hiding and dismissing

- A suggestion is hidden when the portfolio already has a `dividend`
  transaction for that instrument dated from ex-date −7 to +45 days. This
  covers dividends entered by hand and Midas imports, which use the payment
  date. Each recorded dividend hides one ex-date only: the latest one on or
  before its date, else the earliest one after it.
- The user can dismiss a suggestion. Dismissals are per user and portfolio.
  There is no undo.

### Timeline

`GET /api/portfolios/{id}/dividends/timeline` is a read-only view of the
dividends already recorded: monthly and yearly buckets with `gross`, `tax`,
`net` and `interest`, and totals per instrument, in the reporting currency.
Each `dividend` transaction converts at the latest `fx_rates` rate on or
before its own date. A row with no such rate is left out and its currency is
listed in `missing_rates`. `last_12_months` is the window the tab's "Son 12
ay net" shows, summed before rounding: the monthly buckets are each rounded,
so adding them can differ from it by a cent. `total` is one bucket over every
recorded dividend, summed before rounding; the tab's "Toplam net", "Toplam
stopaj" and "Faiz toplam" show it, because the yearly buckets are each
rounded too. The route takes a numeric portfolio id only; the
`all` scope is a 404 (ADR 0014).

The Temettü tab shows the timeline. Its first card, "Onay bekleyen
temettüler", lists the expected dividends and is always open.

### Recorded simplifications

- **The transaction date is the ex-date, not the payment date.** Neither
  source gives a payment date.
- **Stopaj rates are per market only.** No per-instrument rate for ETFs or
  funds: a BIST fund or a US holding under a different treaty rate is
  pre-filled with its market's rate and the user corrects it. A nullable
  `instruments.stopaj_rate` would cover it if it matters.
- **BIST history covers the current calendar year only.**
- **A cancelled planned dividend stays in the table.** The daily sync
  upserts what the page lists and deletes nothing; a changed announcement
  (same instrument and ex-date) is overwritten in place, a withdrawn one is
  not removed.
- **The quantity counts every row dated before the ex-date**, including
  splits.
- **Interest is not in the dividend bars.** The timeline reports it in its
  own `interest` field per bucket.
- **The timeline converts at the transaction-date rate, the Özet stats at
  today's rate**, so the two dividend totals in the reporting currency differ
  when the rate moved.

### Out of scope

Stock dividends (bonus issues are splits, ADR 0008), dividend reinvestment,
fund distributions, crypto staking rewards, payment dates, an undo for
dismissals.
