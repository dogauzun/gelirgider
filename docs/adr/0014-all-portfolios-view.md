# ADR 0014: All-portfolios view

Status: accepted (2026-09-28).

## Context

A user can keep several portfolios, for example one per broker. Each has its
own summary page and chart. "What do I hold in total, and how did it do"
needs one read-only view of all of a user's portfolios, where every figure
equals the sum of the per-portfolio figures.

## Decision

### URL and API: the reserved id `all`

The view reuses the existing portfolio routes with the reserved id `all`:

- `GET /api/portfolios/all/summary`
- `GET /api/portfolios/all/history`, with `from`, `to` and `benchmark`
  unchanged.
- `GET /api/portfolios/all/returns/periods`
- The page `/portfolios/all`.

Portfolio ids are numeric, so `all` never clashes with a real one. One scope
resolver, `scopeTransactions` in `backend/internal/server/scope.go`, serves
these handlers. It turns the `{id}` path value into one transaction list per
portfolio in scope: the one portfolio for a numeric id, every portfolio of the
user for `all`. A non-numeric id other than `all`, or another user's
portfolio, is `portfolio.ErrNotFound`, the 404 the routes return today.

Every other `/api/portfolios/{id}/*` route keeps parsing `{id}` as a number
and keeps returning 404 for `all`: transactions, expected dividends, rename,
delete, and the position history
(`/api/portfolios/{id}/instruments/{instrument_id}/history`). The view is
read-only by construction, not by a check that could be forgotten. Statement
imports live under `/api/imports` with the portfolio id in the body, which
must stay a real portfolio.

### Merge rule: one holdings pass per portfolio, then sum

The summary runs one holdings pass per portfolio, exactly as the single
portfolio summary does, and `holdings.Merge` sums the resulting summaries per
instrument:

- quantity, cost basis, realized and unrealized P/L, market value;
- the trade-date cost `CostBasisReporting`, and `CostRateMissing` as an OR;
- cash and realized P/L per currency.

Average cost is the summed cost basis over the summed quantity. An instrument
closed in one portfolio and open in another is one open row carrying both
realized P/Ls. A merged row is closed only when it is closed in every
portfolio: long in one and oversold by as much in another sums to quantity
zero but stays open, so its value and cost stay in the totals. Splits are
unioned by date.

One combined pass over all transactions was rejected. It would pool the
average cost across portfolios, so a sale in one portfolio would realize
against the other portfolio's buys. Counter-example:

| step               | portfolio A   | portfolio B | one combined pass |
| ------------------ | ------------- | ----------- | ----------------- |
| buy 10 @ 100 in A  | 10 @ 100      |             | 10 @ 100          |
| buy 10 @ 200 in B  | 10 @ 100      | 10 @ 200    | 20 @ 150          |
| sell 10 @ 150 in A | realized +500 |             | realized 0        |

A's card shows +500 realized, B's card 0, so the combined figure must be
+500. The combined pass would show 0, and the unrealized P/L would be off by
the same 500 the other way.

Summing per-portfolio passes also means:

- the combined realized P/L equals the sum of the portfolio cards;
- the same-day ordering (id order) and the sell-first rule (a sell that would
  go below zero has the same day's later buys moved ahead of it) stay inside
  each portfolio, so whether a sale in A is reordered never depends on shares
  held in B;
- a split is applied once per portfolio that holds the instrument, and its
  rows need no de-duplication across portfolios.

The history follows the same rule: no `holdings.State` ever holds the
transactions of two portfolios. Each day's value and external flow on `all`
equal the sum of that day's per-portfolio values and flows, so the returns
and benchmarks of ADR 0011 and ADR 0012 are computed on the summed series.

The per-position FX split (`fx_pl_reporting`) and the daily change
(`day_change`) are summed per instrument after `Merge`, like the other
amounts. The FX split needs the trade-date cost that `holdings.State` only
keeps when its `FactorOn` is set, so the summary for `all` goes through the
same per-portfolio `computeSummary` that a single portfolio uses and merges
its results, rather than calling `holdings.Compute` on the merged list.

### No new SQL

`all` runs the existing per-portfolio queries in a loop: one transaction list
per portfolio of the user, an N+1 loop that is fine for a handful of
portfolios (a `shortcut:` note in `scopeTransactions` names the upgrade, one
UNION query). Every query keeps filtering by `user_id`. Prices, manual
prices, instruments and FX rates are looked up once for the union of the
instruments.

### The page

`/portfolios/all` is the app's landing page: `/` redirects to it. It has the
Özet layout of ADR 0023 summed over every portfolio, a "Dikkat" card whose
items each name their portfolio, and a Portföyler table
(`frontend/src/components/PortfoliosTable.tsx`): one row per portfolio with
its value, share, return over the page's range by the page's method with a
sparkline, and day change, each row from its own summary and history request.
The header has a "Yeni portföy" button and two tabs, Özet and Performans;
Performans is the periodic returns heatmap, from
`GET /api/portfolios/all/returns/periods`. Rename and delete stay in each
portfolio's own header.

The view is labelled "Tüm portföyler" / "All portfolios", i18n key
`portfolio.all`. It is the first row of the sidebar, above the portfolio list,
with the combined total. "Tümü" alone was rejected: it is already
`chart.range.all` and `transactions.filter.all`, and would be ambiguous next
to the chart range buttons.

Benchmarks work unchanged: the shadow portfolio of ADR 0012 takes the summed
`V_0` and the summed daily flows.

### Read-only, with what stays per portfolio

On `/portfolios/all` there is no manual-price editing, no buy and sell
buttons, no links to transactions or imports, and no position page (ADR 0016).
The "Dikkat" card still books or dismisses an expected dividend and enters a
manual price, each through the owning portfolio's own route
(`/api/portfolios/{n}/...`) or the user's price route; the `all` routes stay
read-only. The one exception is the goal routes of ADR 0029, which accept `all`
for writing too.

Per portfolio: transactions (listing, adding, editing, deleting), statement
imports, expected dividends and their dismissal. Manual prices are stored per
user and instrument, so `all` values an instrument at the same price as every
portfolio.

## Recorded simplifications

- One transactions query per portfolio (N+1), see "No new SQL".

### Out of scope

Editing from the all-portfolios view, a user-chosen subset of portfolios, a
position page across portfolios.
