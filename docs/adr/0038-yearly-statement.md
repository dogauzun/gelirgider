# ADR 0038: Yearly statement

Status: accepted (2026-10-19).

## Context

Every yearly figure exists on some card for some range. The periods heatmap has
a year's return ([ADR 0011](0011-portfolio-returns.md)), the Özet return split
has the four buckets for any range ([ADR 0027](0027-return-attribution.md)),
the chart header has the change in value and `net_flow` for a year preset, and
the cash flow card has the months
([ADR 0035](0035-cash-flow-timeline.md)). None of them puts a year on one
line, lets two years be read side by side, or shows the opening and closing
values that make a statement add up. Comparable products do: a performance
timeline grouped by year, or a report that sets several periods side by side.
For a saver in TRY the year is the unit of thought and the figure that matters
is the real one, and the app already has the TÜFE series
([ADR 0012](0012-benchmarks.md)).

The history sweep already computes everything per day: it is cut into calendar
years for the periods route, it sums the attribution buckets over any span of
days, and it gives the benchmark and the TÜFE level for the same span. The
statement is one more reading of that sweep.

## Decision

### `GET /api/portfolios/{id}/years`

`{id}` is a numeric portfolio id or `all`, resolved by `scopeTransactions` like
the other `all` read routes, which makes this a read listed in
[ADR 0014](0014-all-portfolios-view.md). Another user's portfolio and any other
id are 404, and every read filters by the user. The one parameter is
`benchmark`, accepted as the periods route accepts it (ADR 0012), with 400
`unknown benchmark` otherwise. The route carries swag annotations
([ADR 0013](0013-api-docs.md)).

One sweep runs from the history's default start to today with the benchmark and
the attribution switched on, and is cut into calendar years exactly as the
periods route cuts it. A year's `from`, `to`, `partial`, `returns` and
`benchmark` are therefore the periods route's year bucket, and its returns are
the chart header's for that year's preset (ADR 0011). A year starts on its
first point, as every range in the app does: the first day is the base, its own
move and its own flow are inside `opening`, and the return of a year's first
day is in neither that year nor the one before, so years do not chain exactly.

### The response

`{currency, years, missing_rates, inflation_estimated_from}`. `currency` is the
reporting currency, `years` is oldest first (the card reverses it), and
`inflation_estimated_from` is the first estimated TÜFE day of ADR 0012, or
`null`. A year is:

```
{"year": "2025", "from": "2025-01-01", "to": "2025-12-31", "partial": false,
 "opening": "1100.00", "closing": "1720.00", "net_flow": "500.00",
 "gain": "120.00", "price": "120.00", "income": "0.00", "fx": "0.00",
 "cash_fx": "0.00",
 "returns": {"simple": "0.075000", "twr": "0.075000", "mwr": ...},
 "benchmark": null, "inflation": "0.300000",
 "best": {"instrument_id": 7, "symbol": "ABC", "market": "BIST",
          "currency": "TRY", "total": "120.00"},
 "worst": {"instrument_id": 7, "symbol": "ABC", "market": "BIST",
           "currency": "TRY", "total": "120.00"}}
```

Money is a 2-decimal string in the reporting currency and a return is a
6-decimal fraction string, as everywhere else. What each field is:

- `opening` and `closing`: the `total_value` of the year's first and last
  point, the values the chart draws.
- `net_flow`: the history's `net_flow` for the same span, the rounded flow
  steps after the first day, first-price jumps included
  ([ADR 0035](0035-cash-flow-timeline.md)). It makes `opening + net_flow`
  reconcile with the points.
- `gain`: `closing − opening − net_flow`, computed on those rounded strings, so
  a row adds up to the cent.
- `price`, `income`, `fx`, `cash_fx`: the four buckets of ADR 0027 over the
  span, folded as the attribution route folds its `total`. A bucket is `null`
  when a day of the span had no rate for it, and the currency is then in
  `missing_rates`.
- `returns` and `benchmark`: the periods bucket's, so `benchmark` is `null`
  without the parameter or when the benchmark has no level on some day of the
  year.
- `inflation`: the TÜFE change over the span, `level[to] / level[from] − 1`
  with 6 decimals, from the TÜFE daily level of ADR 0012, loaded once over the
  whole sweep. It is `null` unless the reporting currency is TRY and the level
  at `from` is positive. It does not depend on the benchmark parameter.
- `best` and `worst`: the instruments with the largest and the smallest
  `price + income + fx` over the span, among those with every rate, as above;
  `null` when the year has none. With one instrument both name it.

### Worked example

A deposit of 1000 and a buy of 10 shares at 50 on 2024-11-15, with a close of
50 that day. Closes of 60 on 2024-12-31, 66 on 2025-06-30 and 72 on
2025-12-31, and a deposit of 500 on 2025-06-15. Days without a close carry the
last one. Reporting currency TRY, no fees.

| year | opening | net_flow | closing | gain | price | returns.twr |
| ---- | ------- | -------- | ------- | ---- | ----- | ----------- |
| 2024 | 1000    | 0        | 1100    | 100  | 100   | 0.100000    |
| 2025 | 1100    | 500      | 1720    | 120  | 120   | 0.075000    |

- 2024 starts on its first point, 2024-11-15. The deposit and the buy are that
  day's own, so they are inside `opening`: 500 cash and 10 × 50 in shares make 1000. On 2024-12-31 the value is 500 + 10 × 60 = 1100, `price` is
  10 × (60 − 50) = 100, and the time-weighted return is 1100 / 1000 − 1.
- The close on the buy day matters. Without it the shares are valued at cost
  until 2024-12-31, their first price is a first-price jump, which is a flow
  (ADR 0011), `net_flow` is 100, and the 2024 gain is 0.
- 2025 opens on 2025-01-01 at 1100, the close of 60 carried. The deposit on
  06-15 is the year's `net_flow`, and it lifts the value to 1600: 1000 cash and
  10 × 60 in shares. The value is 1660 on 06-30 and 1720 on 12-31, the gain is
  1720 − 1100 − 500 = 120, and `price` is 10 × (72 − 60) = 120.
- The time-weighted return of 2025 chains the days: the deposit day is
  (1600 − 500) / 1100 = 1, 06-30 is 1660 / 1600, and 12-31 is 1720 / 1660. The
  product is 1720 / 1600 − 1 = 0.075000, the periods heatmap's 2025 cell.

### The identity is the statement

`gain` equals the sum of the four buckets within a bound that adds up the
roundings:

- the rounded buckets are within 0.02 of the exact change in value less the
  flows (ADR 0027);
- `gain` is computed on the rounded `opening`, `closing` and `net_flow`, so
  against the exact figure it can be up to 0.01 further off for the two rounded
  values, plus one cent per flow day, since `net_flow` adds each flow day as a
  difference of rounded values (ADR 0027, ADR 0035).

The card shows `gain` as the figure and the buckets as its split, and never a
fifth "rounding" column. The split that includes the first day's own move is
not offered, because a year starts on its first point.

### Inflation and the real return

`inflation` is the year's TÜFE change over the same days as the row. A year
that reaches past the last published month uses the estimated levels of
ADR 0012, and the card marks it estimated when `inflation_estimated_from` falls
inside the year. The real return is the excess return of ADR 0012 against
TÜFE, computed by the card and not a field:

```
real = (1 + twr) / (1 + inflation) − 1
```

It uses the year's time-weighted return, the return that is net of the flows,
whatever method the page picked, and is a dash when either figure is `null`.
For the 2025 row above, with a TÜFE change of 30%, it is
1.075 / 1.3 − 1 = −0.173077.

### The card

A "Yıllık özet" card sits on Performans directly under the periods heatmap
card, reading the page's benchmark and return method selects. A table has one
row per year, newest first:

- the year, with the partial marker and the from-to title of the heatmap;
- opening, net flow, gain (signed, in the gain or the loss tone), closing, and
  the return by the page's method;
- a segmented control "Özet | Dağılım": the second view replaces the opening,
  net flow and closing columns with price, income, currency and cash currency;
- with a benchmark, a column with its return and the difference as a pill
  (`excessReturn`);
- in TRY, a "TÜFE" column and a "Reel" column;
- an "En iyi / en kötü" column naming `best` and `worst` with their totals.

Below `md` the table scrolls sideways with the year column fixed, as the
heatmap does. A scope with no transaction shows the transactions-empty text.
Strings go through i18n keys ([ADR 0021](0021-product-and-ui-decisions.md)).

### Out of scope

- A bar chart of the buckets per year.
- Quarters and months, which the heatmap has.
- A per-portfolio breakdown on `all`.
- A CSV export of the rows.
- A link from a row to Özet with the year's range.
- The fees of the year as a column: they are inside `price` (ADR 0027), and the
  cash flow card lists them per month.
