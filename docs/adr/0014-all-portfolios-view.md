# ADR 0014: All-portfolios view

Status: accepted (GLG-525, 2026-09-28); superseded in part by
[ADR 0023](0023-ui-design-system-v2.md) (GLG-875, 2026-10-04): "Tüm
portföyler" is a sidebar row, not a switcher entry; amended 2026-10-05
(GLG-935, the view gains the Portföyler table and replaces the home page);
amended 2026-10-06 (GLG-955, a Performans tab)

## Context

A user can keep several portfolios, for example one per broker. Each has its
own summary page and chart, and the home page adds their values up into one
total-assets card. There is no page that shows the positions, P/L and chart
of all of them together, so "what do I hold in total, and how did it do" has
to be added up by hand.

This ADR records the decisions of epic GLG-521 (Phase 17): one read-only
view of all of a user's portfolios, where every figure equals the sum of the
per-portfolio figures.

## Decision

### URL and API: the reserved id `all`

The view reuses the existing portfolio routes with the reserved id `all`:

- `GET /api/portfolios/all/summary`
- `GET /api/portfolios/all/history`, with `from`, `to` and `benchmark`
  unchanged.
- The page `/portfolios/all`.

Portfolio ids are numeric, so `all` never clashes with a real one. One scope
resolver, `scopeTransactions` in `backend/internal/server/scope.go`
(GLG-528), serves both handlers. It turns the `{id}` path value into one
transaction list per portfolio in scope: the one portfolio for a numeric id,
every portfolio of the user for `all`. A non-numeric id other than `all`, or
another user's portfolio, is `portfolio.ErrNotFound`, the 404 the routes
return today.

Every other `/api/portfolios/{id}/*` route keeps parsing `{id}` as a number
and keeps returning 404 for `all`: transactions, expected dividends, rename,
delete, and the position history
(`/api/portfolios/{id}/instruments/{instrument_id}/history`). The view is
read-only by construction, not by a check that could be forgotten. Statement
imports live under `/api/imports` with the portfolio id in the body, which
must stay a real portfolio.

> Amended 2026-10-05 (GLG-935): the page's "Dikkat" card books or dismisses
> an expected dividend and enters a manual price from `/portfolios/all`, each
> through the owning portfolio's own route (`/api/portfolios/{n}/...`) or the
> user's price route; the `all` routes stay read-only.

### Merge rule: one holdings pass per portfolio, then sum

The summary runs one holdings pass per portfolio, exactly as the single
portfolio summary does, and `holdings.Merge` sums the resulting summaries
per instrument:

- quantity, cost basis, realized and unrealized P/L, market value;
- the Phase 16 trade-date cost `CostBasisReporting`, and `CostRateMissing`
  as an OR;
- cash and realized P/L per currency.

Average cost is the summed cost basis over the summed quantity. An
instrument closed in one portfolio and open in another is one open row
carrying both realized P/Ls. A merged row is closed only when it is closed
in every portfolio: long in one and oversold by as much in another sums to
quantity zero but stays open, so its value and cost stay in the totals
(GLG-827).
Splits are unioned by date.

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
- the same-day ordering of GLG-411 (id order) and the sell-first fix of
  GLG-372 (a sell that would go below zero has the same day's later buys
  moved ahead of it) stay inside each portfolio, so whether a sale in A is
  reordered never depends on shares held in B;
- a split is applied once per portfolio that holds the instrument, and its
  rows need no de-duplication across portfolios.

The history follows the same rule: no `holdings.State` ever holds the
transactions of two portfolios. Each day's value and external flow on `all`
equal the sum of that day's per-portfolio values and flows, so the returns
and benchmarks of ADR 0011 and ADR 0012 are computed on the summed series.

### No new SQL

`all` runs the existing per-portfolio queries in a loop: one
`transactions.List` per portfolio of the user, an N+1 loop, with a
`ponytail:` note in `scopeTransactions` (one UNION query if portfolio
counts grow). Every query keeps filtering by `user_id`. Prices, manual
prices, instruments and FX rates are looked up once for the union of the
instruments.

### Per-position FX split and daily change

The Phase 16 per-position FX split (`fx_pl_reporting`, GLG-532) and the
Phase 15 daily change (`day_change`, GLG-542) are summed per instrument
after `Merge`, like the other amounts. The FX split needs the trade-date
cost that `holdings.State` only keeps when its `FactorOn` is set, so each
portfolio's `State` gets `FactorOn`. That is why the summary for `all` goes
through the same per-portfolio summary computation (`computeSummary`) that a
single portfolio uses, and merges its results, rather than calling
`holdings.Compute` on the merged list.

### Read-only

On `/portfolios/all` there is:

- no manual-price editing;
- no Phase 15 buy and sell buttons;
- no links to transactions or imports;
- no position page: the Phase 19 position page is not available for `all`,
  and its API returns 404 for `all` as above.

### What stays per portfolio

- Transactions: listing, adding, editing, deleting.
- Statement imports.
- Expected dividends and their dismissal.
- Manual prices: they are stored per user and instrument, so `all` values
  an instrument at the same price as every portfolio, but they are edited
  from a portfolio's summary page only.

### Label

The view is labelled "Tüm portföyler" / "All portfolios", i18n key
`portfolio.all`. "Tümü" alone was rejected: it is already
`chart.range.all` and `transactions.filter.all`, and would be ambiguous in
the portfolio switcher next to the chart range buttons.

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-875):
> there is no portfolio switcher any more. "Tüm portföyler" is the first row
> of the sidebar, under the brand mark and above the portfolio list, with the
> combined total; the sidebar shows from `lg`. The label and the key are
> unchanged.

### Benchmarks

Benchmarks work unchanged on `all`: the shadow portfolio of ADR 0012 takes
the summed `V_0` and the summed daily flows.

## Recorded simplifications

- One transactions query per portfolio (N+1), see "No new SQL".

### Not in this epic

- Editing anything from the all-portfolios view.
- A user-chosen subset of portfolios.
- A position page across portfolios.

## Consequences

- GLG-525 writes this ADR and the Phase 17 roadmap section.
- GLG-528 adds `holdings.Merge` and the scope resolver `scopeTransactions`.
- GLG-530 accepts `all` on the summary API.
- GLG-535 accepts `all` on the history API, returns and benchmarks included.
- GLG-540 adds "Tüm portföyler" to the portfolio switcher and a read-only
  header for `all`.

  > Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md)
  > (GLG-875): the switcher is gone and "Tüm portföyler" is a sidebar row.
  > The read-only header for `all` stays: `PortfolioHeader` shows the name
  > and the Özet tab, with no actions.

- GLG-545 renders `/portfolios/all` read-only on the summary page.
- GLG-547 links the home page total-assets card to `/portfolios/all`.

  > Superseded 2026-10-05 (GLG-935): the home page is gone. `/` redirects to
  > `/portfolios/all` and the brand links go there. The view has the Özet
  > layout of [ADR 0023](0023-ui-design-system-v2.md) summed over every
  > portfolio, its "Dikkat" items each naming their portfolio, then a
  > Portföyler table (`frontend/src/components/PortfoliosTable.tsx`): one row
  > per portfolio with its value, share, return over the page's range by the
  > page's method with a sparkline, and day change, each row from its own
  > summary and history request. The header has no tab row and a "Yeni
  > portföy" button; rename and delete stay in each portfolio's own header.

  > Amended 2026-10-06 (GLG-955): the header has a tab row again, Özet and
  > Performans. Performans is the periodic returns heatmap of
  > [ADR 0023](0023-ui-design-system-v2.md), from
  > `GET /api/portfolios/all/returns/periods`; the sidebar's Tüm portföyler
  > row keeps the Performans tab when a portfolio's is open.
