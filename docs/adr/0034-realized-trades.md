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

`holdings.State` appends a `holdings.Trade` for every `sell` row it applies,
beside the realized P/L it already adds to the position. The fields, all in
the instrument's currency:

- `Date`: the sell's date.
- `InstrumentID` and `Currency`.
- `Quantity`: the quantity sold.
- `Proceeds`: the sell's `amount − fee`, the cash it brought in.
- `Fee`: the sell's fee, already inside `Proceeds`.
- `Cost`: the average cost of the position before the sell, times `Quantity`.
- `Realized`: `Proceeds − Cost`, the figure the sell adds to the position's
  realized P/L.
- `OpenedAt`: the date the position was opened, see below.

The holding period is `Date − OpenedAt` in days. It is derived by the reader,
not stored.

Worked example: buy 10 at 100 on 2026-09-01, sell 4 at 120 with a fee of 2 on
2026-09-02, sell 6 at 90 on 2026-09-03. The average cost stays 100 through the
partial sell, so there are two trades:

| sell       | proceeds      | cost    | realized | opened     | held   |
| ---------- | ------------- | ------- | -------- | ---------- | ------ |
| 2026-09-02 | 480 − 2 = 478 | 4 × 100 | 78       | 2026-09-01 | 1 day  |
| 2026-09-03 | 540 − 0 = 540 | 6 × 100 | −60      | 2026-09-01 | 2 days |

Their realized sum, 78 − 60 = 18, is the position's realized P/L.

### `OpenedAt` and the short rule

`OpenedAt` is the date of the buy that took the position's quantity from zero
or below to above zero. Later buys and partial sells leave it alone, so every
sell from one opening carries the same date. When the quantity returns to
zero the position is closed, and the next opening sets a new date.

A sell made while the quantity was zero or negative sells shares the position
did not hold, so no buy opened it: its `OpenedAt` is empty, and it has no
holding period. The average cost has no lots, so `OpenedAt` is the age of the
position, not of the shares sold: a position built over a year and sold in
one day reads as a year old.

### What is not a trade

Only a `sell` row is a trade. These realize or return money in the position
but are not trades:

- A buy that covers an oversold quantity. The engine books its realized gain
  in the position, but the sale that opened the short is the trade.
- A dividend of the instrument. It is return, not a flow
  ([ADR 0011](0011-portfolio-returns.md)), and it has no sold quantity or cost.
- The `sell` that books a term deposit's maturity
  ([ADR 0024](0024-term-deposits.md)). It returns the principal at price 1, so
  every such trade would read realized 0; the gain is the interest row, a
  dividend with no instrument. The engine records no trade for an instrument
  in market `DEPOSIT`.

So the sum of a position's trades differs from the position's `realized_pl`
by its dividends and its cover buys, and the report's total differs from
Özet's realized P/L by those and by the rate in the next section. The report
says so in a hint; the two figures are not meant to match.

### `GET /api/portfolios/{id}/trades?instrument_id=`

`{id}` is a numeric portfolio id or `all`, resolved as the other `all` read
routes are ([ADR 0014](0014-all-portfolios-view.md)); another user's portfolio
and any other id are 404. `all` runs one holdings pass per portfolio and
concatenates their trades, so a sale in one portfolio realizes against that
portfolio's buys alone and the total equals the sum of the portfolios'.
`instrument_id` is optional and keeps the trades of one instrument; a
malformed value is a 400, and an instrument with no trades is an empty list.
Every read filters by the user. The route carries swag annotations
([ADR 0013](0013-api-docs.md)).

The response lists the trades newest first, ties in the engine's order. Each
carries the fields of the record in snake_case, amounts and quantities as
decimal strings and dates as `YYYY-MM-DD`, with `opened_at` `null` when empty.
Each also carries `realized_reporting`, its `Realized` in the reporting
currency, and the response carries the reporting currency and the total of
those figures.

### Realized is converted at the sell date

`realized_reporting` converts `Realized` at the rate of the sell date (the
latest rate on or before it, as the trade-date cost of the summary is). It is
`null` for a trade whose currency has no rate by then, and the total is `null`
when any trade is, never a number over part of the list. A trade in the
reporting currency needs no rate.

This differs from Özet, which converts realized P/L at today's rate
([ADR 0003](0003-multi-currency-and-market-data-sources.md)). The report asks
what each sale made when it happened, so a dollar stock sold at a gain when
USD/TRY was 30 reads as that gain in lira, not as a gain revalued at today's
rate. For a foreign trade the two totals differ by the rate's move since the
sale. The summary's `realized_pl`, `fx_pl` and the attribution keep today's
rate: the identity of [ADR 0027](0027-return-attribution.md) sums the buckets
to the change in total value, which the sell-date figure would break. The
trades report is a separate view and feeds none of them.

### Frontend

- Performans gets a card with the trades, the total and the hint about why it
  is not Özet's realized P/L. It works on a numeric portfolio and on `all`.
- The position page ([ADR 0016](0016-position-detail.md)) gets a "Satışlar"
  card with that instrument's trades, from `?instrument_id=`, in the
  instrument's own currency. The `all` scope has no position page, so it has
  no such card.

Strings go through i18n keys ([ADR 0021](0021-product-and-ui-decisions.md)).

### Out of scope

This is not a tax report. It does no lot matching (FIFO or otherwise), no tax
year, no stopaj or tax calculation, and nothing in it is a filing figure. The
first version also has no date range, no export, and no return percentage or
benchmark per trade.
