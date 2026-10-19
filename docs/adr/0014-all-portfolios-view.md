# ADR 0014: All-portfolios view

Status: accepted (2026-09-28).

## Context

A user can keep several portfolios, for example one per broker. Each has its
own summary page and chart. "What do I hold in total, and how did it do"
needs one view of all of a user's portfolios, where every figure equals the
sum of the per-portfolio figures. The view is for reading; the few writes it
accepts are listed under "URL and API".

## Decision

### URL and API: the reserved id `all`

The view reuses the existing portfolio routes with the reserved id `all`:

- `GET /api/portfolios/all/summary`
- `GET /api/portfolios/all/history`, with `from`, `to` and `benchmark`
  unchanged.
- `GET /api/portfolios/all/returns/periods`
- `GET /api/portfolios/all/attribution`
  ([ADR 0027](0027-return-attribution.md))
- `GET /api/portfolios/all/trades`
  ([ADR 0034](0034-realized-trades.md)), the sales of every portfolio, each
  realized against its own portfolio's average cost.
- `GET /api/portfolios/all/cashflows`
  ([ADR 0035](0035-cash-flow-timeline.md)), the deposits and withdrawals of
  every portfolio summed per month.
- `GET /api/portfolios/all/cash-gaps` (ADR 0011)
- `GET /api/portfolios/all/projection` (the real-terms projection of
  [ADR 0029](0029-goals-and-projection.md)), a read like the others.
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
(`/api/portfolios/{id}/instruments/{instrument_id}/history`). Three route paths
accept `all` for writing, and they are the only ones that do:

- `GET` and `PUT /api/portfolios/{id}/allocation-targets`
  ([ADR 0028](0028-target-allocation.md)), the target allocation set.
- `GET`, `PUT` and `DELETE /api/portfolios/{id}/goal`
  ([ADR 0029](0029-goals-and-projection.md)), the savings goal.
- `POST /api/portfolios/{id}/cash-gaps` (ADR 0011), which records the deposits
  for the cash gaps the `GET` lists.

The first two store one setting for the whole view. They resolve `{id}` with
`scopePortfolioID` in the same `scope.go`: `all` becomes a NULL portfolio id,
so the all-portfolios set or goal is stored apart from every portfolio's own.
Both are settings stored beside the portfolios, not a change to any portfolio's
transactions. The cash-gaps `POST` does change transactions. It resolves `{id}`
with `scopeTransactions` like its `GET`, recomputes the gaps of every portfolio
in scope and inserts each deposit into the portfolio whose cash had the gap, in
one database transaction. The client sends only a note, never an amount or a
portfolio. Statement imports live under `/api/imports` with the portfolio id in
the body, which must stay a real portfolio.

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
amounts. `day_change_base`, the value `day_change` is a percentage of, is worked out the same way, from the merged quantity and every portfolio's trades, so it is the sum of the portfolios' bases. The FX split needs the trade-date cost that `holdings.State` only
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

### Read-mostly, with what stays per portfolio

On `/portfolios/all` there is no manual-price editing, no buy and sell
buttons, no links to transactions or imports, and no position page (ADR 0016).
The "Dikkat" card still books or dismisses an expected dividend through the
owning portfolio's route (`/api/portfolios/{n}/...`), enters a manual price
through the user's price route, closes a due term deposit (ADR 0024) and books
or skips a due saving plan (ADR 0030) through their own `/api/deposits/{id}` and
`/api/plans/{id}` routes; it reads each portfolio's due deposits and due plans.
The `all` routes read,
except the three writes listed under "URL and API": the allocation targets
routes of ADR 0028, the goal routes of ADR 0029 and the cash-gaps `POST`, which
the performance page offers on `all` as well. The projection is a normal read
`all` route.

Per portfolio: transactions (listing, adding, editing, deleting), statement
imports, expected dividends and their dismissal, term deposits (creating,
deleting, renewing) and saving plans (creating, editing, deleting), and the due
lists the "Dikkat" card reads. Manual prices are stored per user and instrument,
so `all` values an instrument at the same price as every portfolio.

## Recorded simplifications

- One transactions query per portfolio (N+1), see "No new SQL".

### Out of scope

Entering or editing transactions by hand from the all-portfolios view, a user-chosen subset of portfolios, a
position page across portfolios.
