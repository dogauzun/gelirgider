# ADR 0035: Cash flow timeline and the invested line

Status: accepted (2026-10-19).

## Context

The Özet chart draws the portfolio's value and its cost basis. Neither says how
much money the user put in: the cost basis is what the open positions cost, so
it ignores cash, and it falls when a position is sold. Özet's KPI card shows one
all-time deposits total, and the history carries `net_flow` for the selected
range ([ADR 0011](0011-portfolio-returns.md)), but nothing shows the money in
and out month by month or how it moved over time. A saver asks "how much did I
put in, and am I still doing it": the gap between the value and the money put in
is the gain, and the month-by-month record is the habit.

The history sweep already counts every external flow for the returns
([ADR 0011](0011-portfolio-returns.md)), and the dividends timeline already
buckets rows by month and converts each at its own date's rate
([ADR 0009](0009-dividends.md)). Both are reused; no new table or setting is
needed.

## Decision

### `invested` on every history point

Each point of `GET /api/portfolios/{id}/history` (and `all`,
[ADR 0014](0014-all-portfolios-view.md)) carries `invested`, a 2-decimal string
in the reporting currency: the money put in, net of money taken out, up to the
end of that day. It is built from the same flows as `net_flow`, so it is a
running total of them:

- The first point's `invested` is its rounded `total_value`. Everything held
  when the range opens counts as put in, whatever the range, so the two lines
  meet on the first point and the gap after it is what the range earned.
- Each later point adds the day's step: `round(total) − round(total − flow)`,
  the step the flow makes in the rounded total, which is how `net_flow` sums
  them. The last point's `invested` minus the first's therefore equals
  `net_flow` exactly, never a cent off.

What is a flow is ADR 0011's rule, unchanged: the gross amount of a deposit
minus a withdrawal, each at its own day's rate (a flow with no rate yet is
carried to the day it converts), and the first-price jump of a holding the
first point had no price for. Fees, buys, sells, dividends, interest and splits
are not flows. Flows are end-of-day, so a deposit raises `invested` and the
value on the same day. `invested` is never `null`: unlike a return it needs no
positive base.

Worked example with no positions, in the reporting currency: deposits of 100
on day 1 and 1000 on day 2, and a withdrawal of 300 on day 3.

| day | event          | total | step | invested |
| --- | -------------- | ----- | ---- | -------- |
| 1   | deposit 100    | 100   | none | 100      |
| 2   | deposit 1000   | 1100  | 1000 | 1100     |
| 3   | withdrawal 300 | 800   | −300 | 800      |

`net_flow` is 1000 − 300 = 700, and the last `invested` minus the first is
800 − 100 = 700. With no positions `invested` equals the total. A buy changes
neither: deposit 1000 and buy shares for 1000 on day 1, and the shares are worth
1200 on day 2. The value is 1200, `invested` stays 1000, and the 200 between
them is the gain.

`total_value − invested` is the range's gain, the figure the chart header shows.
The position history ([ADR 0016](0016-position-detail.md)) is unchanged and has
no `invested`.

### The chart

The Özet chart draws `invested` as a dashed line, "Yatırılan", in the money
mode only, toggled from the legend like the cost line, which stays. The percent
mode plots the time-weighted return, which is already net of the flows
([ADR 0011](0011-portfolio-returns.md)), so it has no money line. The line is
drawn by the existing line chart on Recharts
([ADR 0023](0023-ui-design-system-v2.md)).

### `GET /api/portfolios/{id}/cashflows`

`{id}` is a numeric portfolio id or `all`, resolved by `scopeTransactions` like
the other `all` read routes, which makes this a read listed in ADR 0014.
Another user's portfolio and any other id are 404, and every read filters by
the user. The route takes no parameters and carries swag annotations
([ADR 0013](0013-api-docs.md)).

The response is `{currency, months, totals, missing_rates}`, with `currency` the
reporting currency. `months` is one bucket per calendar month from the month of
the scope's first transaction to the current month in `Europe/Istanbul`, oldest
first and zero-filled, so a month with nothing in it is a row of zeros; a scope
with no transaction has no months. A month is:

```
{"period": "2026-09", "deposits": "5000.00", "withdrawals": "500.00",
 "net": "4500.00", "dividends": "85.00", "interest": "12.40", "fees": "7.50"}
```

All fields are 2-decimal strings in the reporting currency, and `totals` has the
same fields over every month, each summed from the exact amounts and rounded
once, so it can differ from the sum of the rounded months by a cent. What each
counts:

- `deposits` and `withdrawals` are the gross amounts of the month's `deposit`
  and `withdrawal` rows, as `holdings.Stats` counts them: a fee on either is
  not part of the flow, as in `net_flow`.
- `net` is `deposits − withdrawals`.
- `dividends` is the net of stopaj of the month's `dividend` rows with an
  instrument, and `interest` the net of those without one
  ([ADR 0009](0009-dividends.md)).
- `fees` is every fee as `Stats.Fees` counts it: the fee of a trade, the fee of
  a deposit or withdrawal, and `amount + fee` of a `fee` row.
- Buys and sells are internal to the portfolio and are not listed.

Example, for a TRY reporting currency and a scope whose only rows in 2026-09
are these:

| date       | row                                          | counts in               |
| ---------- | -------------------------------------------- | ----------------------- |
| 2026-09-02 | deposit 1000 TRY                             | deposits 1000           |
| 2026-09-10 | dividend 100 TRY on an instrument, stopaj 15 | dividends 100 − 15 = 85 |
| 2026-09-12 | dividend 12.40 TRY with no instrument        | interest 12.40          |
| 2026-09-15 | deposit 100 USD, USD rate 40                 | deposits 4000           |
| 2026-09-20 | withdrawal 500 TRY                           | withdrawals 500         |
| 2026-09-25 | fee row 7.50 TRY                             | fees 7.50               |

They make the month above: deposits 1000 + 4000 = 5000, `net` 5000 − 500 = 4500.
The stopaj is not a fee here, since `Stats.Fees` does not count it. With this as
the only month, `totals` equals the row.

Each row converts at the latest rate on or before its own date, the walk of the
dividends timeline (`loadFXWalk`,
[ADR 0003](0003-multi-currency-and-market-data-sources.md)). A row in the
reporting currency needs no rate. A row whose currency has no rate on or before
its date is left out of every field and its currency is in `missing_rates`; the
other rows are unaffected. This differs from `invested`, which carries such a
flow to the first day with a rate and also holds the first-price jumps, so the
months' `net` need not sum to `net_flow`.

There is no 3660-day cut like the history's: the route reads transaction rows,
not prices, and returns a row per month.

### The card and the streak

A "Nakit akışı" card sits on Performans under the realized trades card (ADR 0034). It draws the
months as grouped bars on a zero line: deposits up in the gain color and
withdrawals down in the loss color. A window switch, "12 ay | 24 ay | Tümü",
cuts the bars to the last 12 or 24 months or shows all of them; it opens on
24 ay and is not stored. A month's tooltip shows the six fields, and a totals
line sums the shown window.

The header shows the saving streak: the number of consecutive months with
`net > 0` ending in the current month, or in the previous month when the current
one is not above zero yet, as "Üst üste {n} ay" ("Streak {n} months"), with
the longest such run beside it as "En uzun {n} ay". The current month is still
open, so one without a deposit yet does not reset the streak; a previous month
with `net` at or below zero does. The streak and the longest run read the whole
record, whatever the window.

The bars are drawn by one grouped-bars chart extracted from the frame of
`ReturnBars`, so the period returns bars and this card do not duplicate it:
the returns bars pass percent values and the card passes money. Strings go
through i18n keys ([ADR 0021](0021-product-and-ui-decisions.md)).

### Out of scope

- A savings-rate percentage.
- Buys and sells per month.
- A per-instrument or per-portfolio cash flow on `all`.
- Exporting the months.
- The invested line on the position page, whose cost line is the same idea per
  holding.
