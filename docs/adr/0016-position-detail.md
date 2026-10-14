# ADR 0016: Position detail page

Status: accepted (2026-09-28).

## Context

Özet lists every position in one row. The portfolio history API answers the
whole portfolio in the reporting currency, so it cannot draw one holding: how
its value and cost moved, where the buys and sells happened on the price, and
its transactions next to its figures. This ADR records the read-only page per
position, built on one endpoint and on data the app already loads.

## Decision

### Endpoint: the position history

`GET /api/portfolios/{id}/instruments/{instrument_id}/history?from=&to=`, in
`backend/internal/server/position_history.go`. The response is in the
instrument's own currency, one point per day from `from` to `to`, with
`close`, `quantity`, `avg_cost`, `cost_basis` and `market_value`.

- `close` is the latest close on or before the date, manual merged over
  fetched as in the portfolio history, and `null` before the first price.
- The other four come from one `holdings.State` fed only this instrument's
  transactions and split rows of this portfolio, so they agree with the Özet
  row. They are zero on a day the position is not held. Before its first price
  the position is valued at cost.
- `to` defaults to today in Europe/Istanbul, and a later `to` is clamped to
  today. Without `from`, the range starts at the instrument's first
  transaction in the portfolio, but no earlier than 3660 days before `to`. An
  explicit `from` is kept, except that it moves up to the instrument's first
  transaction when that falls after it and on or before `to`.
- `from` after `to`, a malformed date, or an explicit range over 3660 days is
  a 400. A non-numeric (including `all`) or foreign portfolio id is a 404, and
  so is an unknown instrument id.
- A known instrument with no transactions in the portfolio answers 200 with
  zero quantity and amounts. Every read filters by the user.

### Stats, markers and transactions come from what the app already loads

The page's figures come from the `Position` of
`GET /api/portfolios/{id}/summary` with this `instrument_id`, the same object
the Özet row renders; an instrument id with no `Position` shows the not-found
state. The KPI strip is in the reporting currency, led by "Toplam sonuç",
unrealized plus realized P/L. The summary's `realized_pl` already holds each
dividend's net, so the total counts them once; the Temettü cell shows the
instrument's net from the dividends timeline and says it is inside
Gerçekleşmiş. The two are never subtracted: realized converts at today's rate
and the timeline at each dividend's own date's, so for a foreign position
they differ by the rate's move. The average cost and the two charts stay in
the instrument's own currency.

The trade markers and the position's transactions list come from the
transactions API filtered by instrument, the infinite query İşlemler already
uses. It never returns split rows, so split markers come from
`Position.splits`.

### Entry: the positions row, a dialog, then the page

A click or Enter on a positions row on Özet opens `PositionDialog`: the
position's figures, price chart over the summary's range and last
transactions, flipping through the table's rows. Its title links to the page
`/portfolios/:id/positions/:instrumentId`. On a desktop row the symbol links
to the page directly; on a phone the row opens the dialog. The page keeps a
"Tüm işlemler" link to İşlemler filtered by the instrument.

### Closed positions

A closed position (quantity 0, still listed in the summary for its realized
P/L) opens too. The page drops the days after the last held one and before the
first, rather than drawing a flat zero line. A day between a close and a
reopen is a gap in the value, cost and average-cost lines; the price line
runs through it.

### Not on `all`

There is no page in the `all` scope ([ADR 0014](0014-all-portfolios-view.md)),
and the endpoint answers 404. There `PositionDialog` shows the title and the
summary row's figures alone, with no chart, transactions or dividends.

### Charts, ranges and privacy

The page reuses the line chart and the Özet range picker; "all" sends no
`from`, which the endpoint defaults to the first transaction. The price chart
draws `close` and the average cost at full price precision. Tooltips and axis
labels go through the chart's own formatters, so privacy mode masks them.

## Recorded simplifications

- No per-position returns: the page shows P/L over cost, as Özet does.
- Markers come only from the transaction pages already loaded; an old trade on
  an unfetched page has no marker yet.
- Prices are not adjusted for splits ([ADR 0008](0008-splits.md)): the close
  line jumps on a split date while the rest stays continuous; the split marker
  names the jump.

### Out of scope

Per-position IRR or TWR, a benchmark on a position.
