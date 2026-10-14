# ADR 0016: Position detail page

Status: accepted (GLG-526, 2026-09-28); amended 2026-09-29 (GLG-589, a day
not held is a gap in the position charts, see "Closed positions")

## Context

Özet lists every position of a portfolio in one row: quantity, average
cost, last price, market value, unrealized and realized P/L. The symbol
opens İşlemler filtered to the instrument (GLG-344). There is no page for
one position: how its value and cost moved over time, where the buys and
sells happened on the price, and its transactions next to its figures.

The portfolio history API (`GET /api/portfolios/{id}/history`) answers the
whole portfolio in the reporting currency, so it cannot draw one holding.

This ADR records the decisions of epic GLG-523 (Phase 19): one read-only
page per position of one portfolio, built on one new endpoint and on data
the app already loads.

## Decision

### Endpoint: the position history

```
GET /api/portfolios/{id}/instruments/{instrument_id}/history?from=&to=
```

In `backend/internal/server/position_history.go` (GLG-531). The response is
in the instrument's own currency, one point per day from `from` to `to`:

```
{
  "currency": "TRY",
  "points": [
    {"date": "2026-09-01", "close": "41.20", "quantity": "100",
     "avg_cost": "38.5", "cost_basis": "3850.00", "market_value": "4120.00"}
  ]
}
```

- `close` is the latest close on or before the date, manual merged over
  fetched as in the portfolio history, and `null` before the first price.
- `quantity`, `avg_cost`, `cost_basis` and `market_value` come from one
  `holdings.State` fed only this instrument's transactions and split rows of
  this portfolio, so they agree with the Özet row. They are zero on a day the
  position is not held, such as after it is closed. Before its first price
  the position is valued at cost.
- `cost_basis` and `market_value` have 2 decimals, `avg_cost` up to 9.
- `to` defaults to today in Europe/Istanbul, and a later `to` is clamped to
  today.
- Without `from`, the range starts at the instrument's first transaction in
  the portfolio, but no earlier than 3660 days before `to`. A `to` before
  that first transaction is then a 400 "from must not be after to".
- An explicit `from` is kept as given, except that when the instrument's
  first transaction falls after it and on or before `to`, `from` moves up
  to that transaction.
- `from` after `to`, a malformed date, or an explicit range over 3660 days
  is a 400.
- A non-numeric or foreign portfolio id is a 404 "portfolio not found"; an
  unknown instrument id is a 404 "instrument not found".
- A known instrument with no transactions in the portfolio answers 200.
  Without `from` that is one point for `to`; with an explicit `from` (the
  1m, 3m and 1y ranges) it is one point per day from `from` to `to`. Either
  way quantity and amounts are zero, and `close` still follows the prices.
- Every read filters by the user: the portfolio lookup, the transactions
  list and the manual prices.

This is the contract the page tickets build against.

### Stats: the existing summary

The page's figures (quantity, average cost, last price, market value,
unrealized and realized P/L, the split badge) come from the `Position` of
`GET /api/portfolios/{id}/summary` that has this `instrument_id`, the same
object the Özet row renders. No per-position summary endpoint. An
instrument id with no `Position` in the summary shows the not-found state.

### Markers and transactions list: the transactions API

The trade markers and the position's transactions list come from
`useTransactions(portfolioId, [], instrumentId)` in
`frontend/src/api/transactions.ts`, the infinite query İşlemler already uses
for its instrument filter (GLG-333). The transactions API never returns
split rows, so split markers come from `Position.splits`.

Markers use the `ChartMarker` kinds `buy`, `sell`, `dividend` and `split`
of `LineChart` (GLG-536), and are placed on the dates that match a point of
the series.

### Entry: the Özet symbol and back to İşlemler

The symbol link in `InstrumentLabel` in `frontend/src/pages/SummaryPage.tsx`
opens the position page instead of İşlemler (GLG-549). The page keeps a
"Tüm işlemler" link to İşlemler filtered by the instrument, the URL the
symbol opens today (`?instrument=<id>&symbol=<symbol>`, GLG-344), so that
path is not lost.

> Superseded 2026-10-07 (GLG-933, GLG-1104, GLG-1105): the Özet symbol link is
> `SymbolLink` in `frontend/src/components/PositionsTable.tsx` (GLG-933). A
> click or Enter on a positions row opens `PositionDialog` (GLG-1104,
> GLG-1105), whose title links to the position page outside the `all` scope.
> On a desktop row the symbol still links to the page; on a phone row it is
> plain text and the row opens the dialog.

The page route is `/portfolios/:id/positions/:instrumentId`, next to
`/portfolios/:id/transactions` in `frontend/src/App.tsx`.

### Closed positions

A closed position (quantity 0, still listed in the summary for its realized
P/L, GLG-218) opens too. It shows its realized P/L and the history up to the
close: the page drops the points after the last one with a non-zero
quantity, rather than drawing a flat zero line up to today.
The same goes for the days before the first held one in the range. A day
between a close and a reopen is a gap in the value and cost lines, and in
the price chart's average-cost line; the price line runs through it
(GLG-589).

### Not on `all`

The page is not available in the Phase 17 `all` scope
([ADR 0014](0014-all-portfolios-view.md)). The endpoint parses `{id}` as a
number and answers 404 for `all`, and GLG-545 renders the `all` summary
without the symbol link to this page.

### Charts, ranges and privacy

The page reuses `LineChart` and the Özet range buttons. The `RANGES` and
`fromForRange` of `SummaryPage.tsx` move to `frontend/src/lib/ranges.ts`
(GLG-539) so both pages share them; "all" sends no `from`, which the
endpoint defaults to the first transaction.

> Superseded 2026-09-29 (GLG-606): `fromForRange` moved as planned (GLG-539)
> and was then replaced by `resolveRange`, which `frontend/src/lib/ranges.ts`
> exports next to `RANGES` and which gives the API both `from` and `to` for
> the shared `RangePicker`.

Chart tooltips and axis labels go through LineChart's own formatters, so
the Phase 15 privacy mode (GLG-564), which masks the LineChart tooltip and
axis, covers the position charts with no extra work.

### Open points for the implementing tickets

Checked against the code on 2026-09-28. These are not new APIs; each is a
choice left to the ticket named:

- `formatValue` in `frontend/src/components/LineChart.tsx` is a module
  function, not a prop, and rounds to 0 decimals. That suits market value
  and cost (GLG-546) but hides the decimals of a price like 41.20. GLG-551
  decides whether the price chart takes a digits option on `LineChart` or
  accepts whole-number price labels; either way the formatting stays inside
  `LineChart`, so privacy mode still covers it.

  > Superseded 2026-10-05 (GLG-912, GLG-918): the file is
  > `frontend/src/components/charts/LineChart.tsx` (GLG-912), and
  > `formatValue` is an optional prop (GLG-918); see the GLG-918 note below.

- `LineChart` takes `HistoryEntry[]` (`total_value`, `cost_basis`) and
  always draws a cost line. GLG-546 maps `market_value` to `total_value`
  and `cost_basis` to `cost_basis`. GLG-551 maps `close` to `total_value`,
  drops the `null` closes before the first price, and decides what the
  second line shows (`avg_cost` is the natural fit) or how to hide it.
- There are two `InstrumentLabel` components, in `SummaryPage.tsx` and
  `TransactionsPage.tsx`. GLG-549 changes only the Özet one.

> Resolved 2026-09-29 (GLG-546, GLG-549, GLG-551): `formatValue` stayed a
> module function with 0 decimals, so the price chart accepts whole-number
> price labels; `LineChart` now takes `ChartPoint[]`, whose `total_value`
> and `cost_basis` may be `null` for a gap, and a `labels` prop that renames
> the two lines; the price chart maps `close` to `total_value`, drops the
> `null` closes and draws `avg_cost` as its second line; and both
> `InstrumentLabel` components still exist, with only the Özet one linking
> to the position page.

> Superseded 2026-10-07 (GLG-933, GLG-1105): only the `InstrumentLabel` in
> `TransactionsPage.tsx` is left. The Özet symbol is `SymbolLink` in
> `frontend/src/components/PositionsTable.tsx`, and a positions row opens
> `PositionDialog`, whose title links to the position page; see the note
> under "Entry: the Özet symbol and back to İşlemler".

> Superseded 2026-10-05 (GLG-918): `LineChart`, now
> `frontend/src/components/charts/LineChart.tsx` (GLG-912), takes an optional
> `formatValue` for its tooltip amounts; the price chart passes
> `formatUnitPrice(v, currency, 6)`, so the price and the average cost keep
> their decimals and stay masked in privacy mode. The value charts keep whole
> amounts.

## Recorded simplifications

- No FX conversion: the page is in the instrument's own currency. The
  reporting-currency figures stay on Özet.

  > Superseded 2026-10-05 (GLG-938): the KPI strip is in the reporting
  > currency, led by "Toplam sonuç", unrealized plus realized P/L. The
  > summary's `realized_pl` already holds each dividend's net, so the total
  > counts them once, and the Gerçekleşmiş cell shows `realized_pl_reporting`
  > as Özet does, marked as including dividends. The Temettü cell shows the
  > instrument's net from the dividends timeline, as the Temettü tab does, and
  > says it is inside Gerçekleşmiş. The two are never subtracted: realized
  > converts at today's rate and the timeline at each dividend's own date's,
  > so for a foreign position they differ by the rate's move. The average cost
  > and the two charts stay in the instrument's own currency.

- No per-position returns: the page shows P/L over cost, as Özet does.
- Markers come only from the transaction pages already loaded. An old trade
  on a page not yet fetched has no marker until the list loads that page.
- Prices are not adjusted for splits
  ([ADR 0008](0008-splits.md)): the close line jumps on a split date while
  quantity, cost and market value stay continuous. The split marker names
  the jump.

### Not in this epic

- Per-position IRR or TWR.
- A benchmark on a position.
- A reporting-currency view of the position.
- A position page across portfolios (ADR 0014).

## Consequences

- GLG-526 writes this ADR and the Phase 19 roadmap section.
- GLG-531 adds the position history endpoint.
- GLG-536 adds optional trade markers and series labels to `LineChart`.
- GLG-539 moves the chart `RANGES` and `fromForRange` into
  `frontend/src/lib/ranges.ts`.
- GLG-546 adds the page route, the stats and the value vs cost chart.
- GLG-549 makes the Özet symbol open the position page.
- GLG-551 adds the price chart with trade markers and the position's
  transactions list.

> Superseded 2026-09-29 (GLG-606): `resolveRange` in
> `frontend/src/lib/ranges.ts` has replaced the `fromForRange` that GLG-539
> moved there.
