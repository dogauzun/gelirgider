# ADR 0033: Per-position returns with a benchmark

Status: accepted (2026-10-18).

## Context

The portfolio's returns ([ADR 0011](0011-portfolio-returns.md)) answer how the
portfolio did. For one holding the app shows only P/L over cost, in the
reporting currency, and [ADR 0016](0016-position-detail.md) left per-position
returns and a benchmark out. For a Turkish investor the question is sharper
than a gain over cost: a BIST share up 40% in a year of 60% inflation lost
money, and a US share's lira gain is mostly the dollar's. "Did THYAO beat
BIST 100" and "did my Apple shares beat the dollar" need a number.

The pieces exist. The position history already walks one holding's daily
value in its currency, the return functions of ADR 0011 take a values series
and a flows series and nothing else, and the shadow portfolio of
[ADR 0012](0012-benchmarks.md) runs on any flows.

## Decision

### On the position history, in the instrument's currency

`GET /api/portfolios/{id}/instruments/{instrument_id}/history`
([ADR 0016](0016-position-detail.md)) returns the three returns of ADR 0011
(simple, time-weighted, money-weighted) of the position over the requested
range, in the instrument's own currency, so a rate's move is not in them:

- `returns: {simple, twr, mwr}`: fractions as strings with 6 decimals, or
  `null` when undefined, as on the portfolio history.
- `returns_from`: the first day the returns count from, `null` when there is
  none.
- `twr` on every point: the running time-weighted return from `returns_from`,
  `null` on the days before it, and `null` on every point when the range's
  `twr` is `null`.

The reporting-currency view of a holding's return, with the currency's part
split out, stays the attribution's job ([ADR 0027](0027-return-attribution.md)).

### Flows are the position's own trades and income

`V_i` is the day's `market_value`. `F_i` is the money the position takes in
(positive) or gives out (negative) on day `i`:

- a buy: `amount + fee`, in;
- a sell: `amount − fee`, out;
- a dividend of the instrument: `amount − fee`, out. The cash leaves the
  position, so a dividend counts as return;
- a split row or a cash row (deposit, withdrawal, interest): nothing. A split
  changes the quantity and the price together, so the value does not move.

The fee of a trade is inside its flow, so a buy's fee is a cost the position
has to earn back and a sell's fee is a loss. Flows are end-of-day, as in
ADR 0011. The return math is float64 and the amounts stay decimal until the
one conversion, as there.

Worked example: buy 10 at 100 on day 1 (no price yet), closes of 110 on
day 2 and 60 on day 4 (day 3 carries 110), a 2-for-1 split on day 4, a sell
of 5 at 60 on day 5. The series starts on day 2. Values are 1100, 1100, 1200,
900 and flows 0, 0, 0, −300. The time-weighted and money-weighted returns are
0.090909 (1200 / 1100 − 1; the day-5 factor is (900 + 300) / 1200, so the
sell does not read as a loss). Simple is (900 − 1100 + 300) / (1100 − 300) =
0.125000, because it divides by what is left after the money taken out.

### Where the series starts

The series starts at the first point of the range that has a close. Before
it the position is valued at cost, and the jump to the first price would read
as a gain, so those points are left out: their flows sit inside `V_0` (the
first-day rule of ADR 0011), `returns_from` is the first point with a close,
and their `twr` is `null`. A range with no priced point, or one whose `V_0`
is not positive (a short position), answers `null` for all three returns.
The other `null` rules of ADR 0011 hold.

The series ends at the last point with a value or a flow. A closed position
has value 0 and no flow after its last sell, and the period rate of ADR 0011
would compound over those days, so they are left out of the returns and of
the benchmark's: their `twr` and `benchmark_twr` are `null`, like those before
`returns_from`. An open position ends on `to`. A closed stretch inside the
range stays in the series.

### The benchmark is a shadow position

`?benchmark=` takes `USD`, `EUR`, `XU100`, `TUFE` or `GOLD` and nothing else:
`instrument:<id>` is not accepted on a position, and an unknown code is a 400.
The benchmark's level is converted into the instrument's currency with the FX
walk of the history sweep (a level in the instrument's own currency needs no
rate), then the shadow portfolio of ADR 0012 runs on the position's own flows,
from the same start point as the returns:

```
u_0 = V_0 / P_0    u_i = u_{i-1} + F_i / P_i    S_i = u_i * P_i
```

The response carries `benchmark: {code, returns, estimated_from}`, with
`returns` computed from `S` and the flows by the same functions as the
position's, and `benchmark_twr` on each point from the start, by the rule of
`twr`. `estimated_from` is TÜFE's first estimated day, as in ADR 0012.
Without `?benchmark=`, `benchmark` is `null`. A level of 0, or a missing rate,
on any day from the start makes the benchmark's returns `null` and leaves
`benchmark_twr` out, never a number over part of the range.

### Frontend

A "Getiri" card sits above the chart card on the position page, and above the
chart in the position dialog. It shows the range's return by the method the
user stored for Özet, annualized under it for a gap of 364 days or more by the
rule of ADR 0011, a method select, a benchmark select, the benchmark pill with
the excess return (`(1 + p) / (1 + b) − 1`, the real return against TÜFE) and a
hint naming the currency and that dividends are counted. The benchmark select
offers the fixed codes only, and its choice is the one Özet remembers. The
chart card gets a third view, "Getiri (%)": the position's running `twr` from
0% with the benchmark's `benchmark_twr` as the dotted line, and a notice in
place of the chart when the range's `twr` is undefined.

### Recorded simplifications

- The positions table and the summary route carry no per-position return:
  that would run one history sweep per position on every summary call. The
  page and the dialog load one position's history already.
- The `all` scope has no position page ([ADR 0014](0014-all-portfolios-view.md)),
  so it has no per-position returns.
- Not offered: the reporting-currency return per position, `instrument:<id>`
  benchmarks on a position, realized-only or since-inception returns, a
  drawdown per position.
