# ADR 0034: Realized trades report

Status: accepted (2026-10-19).

## Context

The app keeps one realized figure per position
([ADR 0016](0016-position-detail.md)): the sum of every sell's gain and every
dividend's net since the position was first opened. It never shows one sale.
A user who sold a share in three lots cannot see which lot made money, what
each cost, or how long the shares were held, and a closed position is a single
row. Comparable products list closed trades, each with its cost, its gain and
its holding period, and this is the question a saver asks after a sale: "was
that a good sale".

The holdings engine ([ADR 0019](0019-core-engine-and-auth.md)) already works
out each sell's realized gain at the average cost, to add it to the position.
It only throws the per-sell figure away.

## Decision

### The engine records one `Trade` per sell

`holdings.Trade` holds, all in the instrument's currency:

- `Date`: the sell's date.
- `InstrumentID` and `Currency`.
- `Quantity`: the quantity sold.
- `Proceeds`: the sell's `amount − fee`, the cash it brought in.
- `Fee`: the sell's fee, already inside `Proceeds`.
- `Cost`: `Quantity` times the average cost, the `sold` that `Apply`
  subtracts from the cost basis.
- `Realized`: `Proceeds − Cost`, the figure the sell adds to the position's
  realized P/L.
- `OpenedAt`: the date the position was opened, see below.

`State.Trades` gets one entry for every `sell` row `Apply` folds in, in apply
order, beside the realized P/L it already adds to the position. `Compute`
throws the list away with the state. `Merge` and `SummarizeAll` ignore trades
and `OpenedAt`: trades are per portfolio, so a sale realizes against the
average cost of its own portfolio, never a pooled one
([ADR 0014](0014-all-portfolios-view.md)).

Worked example: buy 10 at 100 on 2026-09-01, sell 4 at 120 with a fee of 2 on
2026-09-02, sell 6 at 90 on 2026-09-03. The average cost stays 100 through the
partial sell, so there are two trades:

| sell       | proceeds      | cost    | realized | opened     | held   |
| ---------- | ------------- | ------- | -------- | ---------- | ------ |
| 2026-09-02 | 480 − 2 = 478 | 4 × 100 | 78       | 2026-09-01 | 1 day  |
| 2026-09-03 | 540 − 0 = 540 | 6 × 100 | −60      | 2026-09-01 | 2 days |

Their realized sum, 78 − 60 = 18, is the position's realized P/L.

### `OpenedAt` and the short rule

`Position.OpenedAt` is the date of the buy that took the position's quantity
from zero or below to above zero. Later buys and partial sells leave it alone,
so every sell from one opening carries the same date. `resetIfClosed` clears
it when the quantity returns to zero, where it already resets the cost, and
the next opening sets a new date.

A sell made while the quantity was zero or negative (a short) sells shares the
position did not hold, so no buy opened it: its `OpenedAt` is empty. The
average cost has no lots, so `OpenedAt` is the age of the position, not of the
shares sold: a position built over a year and sold in one day reads as a year
old.

### What is not a trade

Only a `sell` row is a trade, and not a term deposit's:

- A buy that covers an oversold quantity realizes P/L in the engine but is
  not a trade.
- A dividend is not a trade. It is return, not a flow
  ([ADR 0011](0011-portfolio-returns.md)), and it has no sold quantity or
  cost.
- A deposit instrument's maturity `sell` at price 1
  ([ADR 0024](0024-term-deposits.md)) is bookkeeping with a realized of zero;
  the gain is the interest row. The engine records every sell, since `State`
  does not know an instrument's market, and the route leaves out the sells of
  instruments in market `DEPOSIT`.

The report's total is the realized P/L of the listed sells alone. It differs
from Özet's realized P/L, which also holds dividends, interest and cover buys,
and, for a foreign trade, the rate it converts at (below). The card's hint
line says so.

### `GET /api/portfolios/{id}/trades?instrument_id=`

`{id}` is a numeric portfolio id or `all`, resolved by `scopeTransactions`
like the other `all` read routes, which makes this a read listed in ADR 0014.
Each portfolio is applied in its own `holdings.State`, in
`transaction.Repo.List` order. Another user's portfolio and any other id are
404, and every read filters by the user. `instrument_id` is optional and keeps
the trades of one instrument; a malformed value is a 400. The route returns
the whole list, with no paging and no date range, and carries swag
annotations ([ADR 0013](0013-api-docs.md)).

The response is `{currency, trades, missing_rates}`, with no total: `currency`
is the reporting currency and `missing_rates` the currencies a trade could not
be converted from. `trades` is newest first, by date descending and then
transaction id descending. Each trade is:

```
{portfolio_id, date, instrument_id, symbol, name, market, currency,
 quantity, proceeds, cost, fee, realized, realized_pct,
 opened_at, days, realized_reporting}
```

- `proceeds`, `cost`, `fee` and `realized` are 2-decimal strings in the
  instrument's currency; `quantity` keeps its stored scale.
- `realized_pct` is `realized / cost` as a fraction string with 4 decimals,
  `null` when `cost` is not positive.
- `opened_at` is the `OpenedAt` date, and `days` is the sell date minus
  `opened_at` in days. Both are `null` for a short.
- `realized_reporting` is `realized` in the reporting currency, a 2-decimal
  string, see below.

### Realized is converted at the sell date

`realized_reporting` converts `realized` at the latest rate on or before the
sell date, the walk of the dividends timeline (`loadFXWalk`). A trade in the
reporting currency needs no rate. When a currency has no rate by a trade's
sell date, that trade's `realized_reporting` is `null` and the currency is in
`missing_rates`; the other trades are unaffected, and a total over the list
sums the rows that have a value.

This differs from Özet, which converts realized P/L at today's rate
([ADR 0003](0003-multi-currency-and-market-data-sources.md)). The report
shows the cash realized on the day of each sale, so a dollar stock sold at a
gain when USD/TRY was 30 reads as that gain in lira, not as a gain revalued at
today's rate. [ADR 0027](0027-return-attribution.md) already lists the
sell-date conversion as out of the attribution's scope, and nothing changes
there: the summary's `realized_pl`, `fx_pl` and the attribution keep today's
rate, and the trades report feeds none of them.

### Frontend

A "Gerçekleşen satışlar" card sits on Performans under the drawdown card.

- A KPI row with the number of sells; the win rate, the sells with a positive
  `realized` over all sells; the total `realized_reporting` over the rows that
  have one, with a note when some rows have none; and the average `days` over
  the rows that have one.
- A year select: "Tümü" plus every year that has a sell.
- The table: date, instrument, quantity, proceeds, cost, realized with its
  percent, and days. It sorts by date, realized and days, newest first by
  default. On `all` it has a portfolio column, named through `usePortfolios`.
- The hint line that says why the total is not Özet's realized P/L.

The position page ([ADR 0016](0016-position-detail.md)) gets a "Satışlar" card
above its transactions card, with the same table for the one instrument
(`?instrument_id=`), shown only when the instrument has a sell. The `all`
scope has no position page.

Strings go through i18n keys ([ADR 0021](0021-product-and-ui-decisions.md)).

### Out of scope

This is not a tax report: no stopaj or tax calculation, no tax year, nothing
in it is a filing figure. Also out of the first version:

- FIFO or lot matching.
- A trades column on Özet's closed positions table.
- Holding days on an open position's row.
- A CSV export of trades.
- The trades in the position dialog.
- A chart of realized P/L over time.
