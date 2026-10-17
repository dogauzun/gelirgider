# ADR 0008: Stock splits and bonus issues

Status: accepted (2026-09-23).

## Context

A stock split or a BIST bonus issue (bedelsiz sermaye artırımı) corrupts
holdings unless it is modelled. Every stored close is unadjusted, so on the
ex-date the price drops by the split ratio while the quantity stays the same:
the position loses value, the average cost stays at the pre-split level and
value history shows a step. Every holdings calculation runs through
`holdings.State.Apply`, fed by `transaction.Repo.List`.

## Decision

### Splits are market data, not user data

One global table `splits(instrument_id, date, ratio, source)`, unique on
`(instrument_id, date)`, with no `user_id`, like `prices`. Users do not enter
splits. They come from providers, and the `-add-split` operator flag is the
fallback. Last writer wins between sources for the same key.

### Ratio and date

The ratio is new shares per old share: 2 for a 2-for-1 split, 1.5 for a 50%
bonus issue, 0.1 for a 1-for-10 reverse split. It is an exact decimal stored
as TEXT and must be greater than 0. The date is the ex-date, a market `DATE`.
A split applies at the start of that date, before any transaction on the same
date, because trades on the ex-date are already at post-split prices.

### Engine effect

Quantity is multiplied by the ratio and average cost divided by it. Cost
basis, realized P/L and stored prices do not change.

### One choke point: `transaction.Repo.List`

`List` returns synthetic `split` rows for the instruments the portfolio has
transactions in, ordered by date with the split first. Every reader of
holdings gets splits without a change of its own. `split` is a holdings-engine
type only: the transactions API rejects it and the paged transaction list
never shows it.

### Sources

- **US:** Tiingo's `splitFactor` on the daily-history call. Finnhub's
  latest-close call carries none, so a daily pass also reads a recent Tiingo
  window per tracked US instrument.
- **BIST:** the capital increase table on İş Yatırım's "Temel Değerler ve
  Oranlar" page, read once a day as server-rendered HTML, source name
  `isyatirim`. The user accepted a broker's HTML page as the source because
  no keyless, robots.txt-clean API with ratios exists. The ratio is
  `1 + (bonus from internal resources + bonus from dividend) / 100`, in
  Turkish number format. Rows with no bonus part are skipped, and so are
  bonus issues dated after today, because a split must not apply before its
  ex-date. The same fetch also feeds dividends, planned ones included (ADR
  0009).
- **CRYPTO, TEFAS and GOLD:** never split, no source (GOLD per ADR 0017).

The page keeps only the current calendar year. Its year filter lives under
`/_layouts/`, which robots.txt disallows, so the fetcher never calls that path
and older bonus issues go through `-add-split`. The page is meant for a
browser: the fetcher matches cells by class name and fails loudly when the
table or a class is missing, and if the page disappears BIST splits fall back
to `-add-split`.

Rejected: the Borsa Istanbul bulletin (ex-date flag but no ratio), KAP and
Fintables (bot walls), Yahoo (robots.txt) and Borsa Istanbul DataStore (paid).

### Bonus shares booked by an import

A broker statement's bonus row is booked as a `buy` at price 0, and the same
event is also a global split, so both would scale the position twice. A split of instrument I with ex-date D is therefore left out for a
portfolio that has a `buy` of I at price 0 within 10 calendar days of D,
before or after. The statement row is the broker's record of what the account
received, fractions included, so it wins for that portfolio. Other portfolios
still get the split, and price scaling stays: prices are market data. The
statement preview's reconciliation applies the same rule.

Each such buy is also moved to the ex-date of the split it books, so the
position is held at the scaled price with the scaled quantity from the
ex-date. The statement import's duplicate check runs on `List`'s rows but pairs
a moved bonus buy on the date it is stored on (`Transaction.StoredDate`), so a
re-imported bonus row still pairs with the booked one. The CSV export reads
`transaction.Repo.ListStored` for the same reason.

Only a buy whose quantity matches what the split would add books it: the
zero-price buys in the window must add up to the quantity held before the
ex-date times (ratio - 1), give or take one share for the fraction paid in
cash. Shares gifted by hand next to a split do not match, so the split stays.

Ceiling: with no position held before the ex-date there is nothing to match
against, so the zero-price buy books the split. The upgrade path is an explicit
marker on imported bonus rows.

### Not modelled

Fractional shares are kept after a split, although BIST pays the fraction
(küsurat) in cash. Rights issues (bedelli), spin-offs, user-entered splits and
split rows in the statement import are out of scope.

## Known risks

A missed split leaves a step in value history and a stale average cost. A
wrong provider ratio or date corrupts every portfolio holding the instrument,
because the table is global; the operator flag or a later fetch replaces it.
