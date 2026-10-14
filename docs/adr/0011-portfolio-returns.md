# ADR 0011: Portfolio returns

Status: accepted (GLG-394, 2026-09-25); superseded in part by
[ADR 0012](0012-benchmarks.md) (GLG-487, 2026-09-27): the chart header
compares the portfolio's return against one benchmark, a shadow portfolio
that receives the same flows; amended 2026-09-28 (GLG-529,
GLG-533, interest is a dividend with no instrument); amended 2026-09-29
(GLG-594, GLG-607, annualized return on the chart header); amended
2026-09-29 (GLG-641, no return without a positive base); amended 2026-09-30
(GLG-661, every calendar year annualizes); amended 2026-10-01 (GLG-675, the
1Y preset annualizes to its own period return); amended 2026-10-05
(GLG-952, a daily `twr` on every history point); amended 2026-10-05
(GLG-953, returns per calendar month and year); amended 2026-10-06
(GLG-957, drawdown from the daily `twr`); amended 2026-10-07 (GLG-1109,
proposed deposits for the cash gaps)

## Context

The chart header shows one percentage for the selected range. It is a
modified Dietz return computed in the frontend by `rangeDelta` in
`frontend/src/components/LineChart.tsx`, from two fields of
`GET /api/portfolios/{id}/history`:

- `net_flow`: the external cash that entered inside the range, so a deposit
  is not read as a gain (GLG-219).
- `net_flow_weighted`: the same flows, each weighted by the fraction of the
  range left after its day, so a large deposit on the last day does not drag
  the percentage toward 0% (GLG-234).

> Superseded 2026-10-05 (GLG-911, GLG-912): `rangeDelta` is in
> `frontend/src/lib/chart.ts` (GLG-911); `frontend/src/components/LineChart.tsx`
> is deleted (GLG-912).

The range starts at the first transaction when the requested range opens
before it (GLG-266), so the first deposit is not weighed as a mid-range flow.

There is no time-weighted return (TWR) and no true money-weighted return
(MWR). Dietz is only a first-order approximation of the MWR. Every other
percentage in the app is P/L over cost.

This ADR records the decisions of epic GLG-388 (Phase 12), taken with the
user on 2026-09-25.

## Decision

### Three period returns, chosen on the chart header

For the selected range the app shows one of three returns:

- **simple**: the gain over the money put in.
- **time-weighted (TWR)**: the return of the portfolio itself, with the
  timing and size of deposits and withdrawals taken out.
- **money-weighted (MWR)**: the true internal rate of return of the range,
  not the Dietz approximation.

All three are period returns for the range as shown. There is no
annualization. The user picks the method with a selector on the chart header
only, and the choice is remembered in `localStorage`, like the theme and the
locale. The home portfolio cards and the KPI card are unchanged.

### Computed in the backend, returned on the history API

The returns are computed in the backend, inside the existing history sweep
(GLG-391), and returned on `GET /api/portfolios/{id}/history` as:

```
"returns": {"simple": "0.250000", "twr": "0.250000", "mwr": "0.250000"}
```

Each value is a fraction (0.25 is 25%) as a string with 6 decimals, or
`null` when it is not defined.

Computing them in the frontend was rejected. What counts as a flow (the
carry of flows with no FX rate yet, the first-price jumps, gross fees, the
first-day rule) lives in the sweep, and the response is the documented API
shape (PRINCIPLES #8). A second copy of those rules in TypeScript would drift.

> Amended 2026-10-05 (GLG-953): `GET /api/portfolios/{id}/returns/periods`
> returns the three returns, and the benchmark's, for every calendar month
> and year from the first transaction to today, oldest first; the first and
> the current bucket are `partial`. A bucket's returns are the history
> endpoint's for the range the chart header's calendar preset sends:
> `YYYY-01-01` to `YYYY-12-31` for a year (`resolveRange` in
> `frontend/src/lib/ranges.ts`), the first to the last day for a month,
> clamped to the first transaction (GLG-266) and to today. The history
> takes that first day's value as the base, with its flows inside it
> (`F_0 = 0`), so a year's bucket equals the header for that year's preset,
> and the return of each period's first day, from the previous period's
> last value to its own, is in neither bucket: twelve months do not chain
> exactly to their year. One sweep over the whole span is cut per bucket by
> `historySweep.rangeInputs` in `backend/internal/server/history.go`, which
> the history endpoint uses for its own range too: day `i`'s flows are
> dropped, and a first-price jump counts when day `i` held the instrument
> unpriced. A bucket's `benchmark` is `null` without `?benchmark=` or when
> the benchmark has no level on some day of the bucket, never a number over
> part of it.

### Ratios in float64

The inputs are exact decimals, converted once to float64 with
`InexactFloat64()`, and the return math runs in float64. A return is a
ratio, not money: PRINCIPLES #3 covers price, quantity, cost and value, and
those stay decimal. Chaining exact decimal factors was rejected: a ten-year
range has about 3660 daily factors, and their exact product grows to a
mantissa of tens of thousands of digits. The IRR needs fractional powers,
which decimal does not do exactly anyway.

### Inputs: a value and a flow per day

For each day `i` of the range, `0` to `n−1`:

- `V_i` is the exact `totals.TotalValue` of that day in the reporting
  currency, the same value the chart draws.
- `F_i` is the external flow that entered on day `i`, in the reporting
  currency.

`F_0 = 0`: a flow on the first day is already inside `V_0`.

An external flow is a converted deposit minus a converted withdrawal, gross,
as `net_flow` already counts it, plus the first-price jump pseudo-flows of
GLG-254. Fees, buys, sells, dividends and splits are not flows: they move
value inside the portfolio or are part of its performance.

Flows are end-of-day. A flow on day `i` does not earn anything on day `i`.
This is the same convention the Dietz weight `(to − day)/(days − 1)` already
used, where a flow on the last day weighs 0.

A carried flow, one in a currency with no FX rate yet (GLG-233), counts on
the day it converts and takes that day's position. This is a change from
GLG-234, which kept the weight of the day the flow was made. The cash is not
in `V` before it converts, so treating it as present earlier would count a
return on money the values never held.

### No return without a positive base

All three returns are `null` when `V_0 <= 0`, or when the day's cash,
converted to the reporting currency (the point's `cash`), is negative on
any day of the range (GLG-641). A portfolio that records buys but not the
deposits that paid for them has negative cash, so `V` is only the P/L of
positions bought with money never put in, and any ratio over it is
meaningless: GLG-632 read +9,783,701% on a range whose `V_0` was 0 because
unpriced positions at cost cancelled the negative cash. The chart header
then shows a dash for the percentage, as for any other `null` method
(GLG-410).

Treating the part of a buy that takes cash below zero as an implicit
deposit was rejected: it would invent flows the user never recorded.

> Amended 2026-10-07 (GLG-1109): implicit deposits are still rejected. The
> app may propose recorded deposits for the cash gaps, one per day a
> currency's cash reaches a new low below zero, and writes them only when
> the user confirms (`GET` and `POST /api/portfolios/{id}/cash-gaps`).

### simple

```
simple = (V_last − V_0 − ΣF) / (V_0 + ΣF)
```

The gain over everything put in: the start value plus the net flows. It is
`null` when the base `V_0 + ΣF` is not positive.

Known weakness: a huge deposit on the last day pulls it toward 0, the case
GLG-234 fixed for Dietz. That is what "gain over money put in" means. TWR and
MWR are the answer to it, and the user can switch to them.

### twr

```
twr = Π_{i≥1} (V_i − F_i) / V_{i−1}  −  1
```

Each day's factor is the day's value without the flow that just came in,
over the previous day's value. A day with `V_{i−1} <= 0` is skipped: there
was nothing to earn a return on. The result is `null` when any factor is
negative, or when no day produced a usable factor. A factor of exactly 0 is
kept: it is a real −100%.

> Amended 2026-10-05 (GLG-952): each point of the history response also
> carries `twr`, the running product up to that day minus one, from
> `timeWeightedSeries` in `backend/internal/server/returns.go`: `"0.000000"`
> on the first point, a skipped day repeats the previous day's value, and
> the last point equals `returns.twr` character for character. When
> `returns.twr` is `null` (no positive base, cash below zero on any day, a
> negative factor), `twr` is `null` on every point, never on some.

> Amended 2026-10-06 (GLG-957): the Performans tab's drawdown is, for each
> point of the all-time history, how far the return index `1 + twr` is below
> its highest value up to that point: `(I_i − max_{j≤i} I_j) / max_{j≤i} I_j`,
> zero at a new high, never positive. The largest drawdown is the lowest of
> these, with its peak (the last day at the high it fell from) and its trough
> (the first day at that low; of two lows equal within 1e-9, so within
> float noise, the first); the current one is the last point's. It is
> `drawdown` in `frontend/src/lib/drawdown.ts`, a
> transform of the series the API returned, not a second copy of the return
> rules: it is `null` when `twr` is `null`, where the card gives the
> undefined-return explanation, or when the history has fewer than two
> points, where it says there is not enough data.

### mwr

The period rate `R` that solves

```
V_0·(1+R) + Σ F_i·(1+R)^{w_i} = V_last,   w_i = (n−1−i)/(n−1)
```

`w_i` is the fraction of the range left after day `i`, the same weight Dietz
uses. Modified Dietz is the first-order approximation of this equation,
which is why `net_flow_weighted` is removed once the frontend reads
`returns` (GLG-393).

It is solved by bisection on `[−1, hi]`. `hi` starts at 1 and doubles while
`f(hi) <= 0`, up to `1e6`. The result is `null` when there is no sign change
on the interval, or with fewer than two points. Multiple roots, which
alternating large flows can produce, are not handled.

`V_0 == 0` was allowed until GLG-641. It happens when the first point is
missing its FX rate, and the IRR then spread the return over the whole
range although the money was only in for part of it. It is now `null`, see
"No return without a positive base".

### One-point ranges

On a one-point range there is nothing to compare: all three returns are
`null` and `net_flow` is zero.

### Worked examples

The test table of GLG-390. `values` are `V_0 … V_last`, `flows` are
`F_0 … F_last`.

| values             | flows        | simple    | twr       | mwr       |
| ------------------ | ------------ | --------- | --------- | --------- |
| [1000, 1250]       | [0, 0]       | 0.250000  | 0.250000  | 0.250000  |
| [1000, 1800, 1710] | [0, 1000, 0] | -0.145000 | -0.240000 | -0.190000 |
| [10000, 1011000]   | [0, 1000000] | 0.000990  | 0.100000  | 0.100000  |
| [1000, 700, 577.5] | [0, -500, 0] | 0.155000  | -0.010000 | 0.102500  |
| [0, 1200]          | [0, 1200]    | null      | null      | null      |
| [0, 1000, 1100]    | [0, 1000, 0] | null      | null      | null      |
| [-100, 1000, 1100] | [0, 1000, 0] | null      | null      | null      |
| [1000, 0]          | [0, 0]       | -1.000000 | -1.000000 | -1.000000 |
| [1000, -100]       | [0, 0]       | -1.100000 | null      | null      |
| [1000]             | [0]          | null      | null      | null      |

- Row 2: TWR is `0.8 × 0.95 − 1 = −0.24`. MWR solves
  `1000x² + 1000x = 1710` with `x = (1+R)^0.5 = 0.9`, so `R = −0.19`.
- Row 3: the last-day deposit of a million pulls simple toward 0, while TWR
  and MWR give the 10% the portfolio made.
- Row 4: MWR solves `1000x² − 500x = 577.5` with `x = 1.05`, so
  `R = 0.1025`.
- Rows 5 to 7: `V_0` is not positive, so all three are `null` (GLG-641).
  Before, row 5's simple read 0%, row 6's MWR spread the gain over the
  whole range although the money was only in for half of it (21% where TWR
  read 10%), and row 7's simple and TWR read 22% and 10%.
- Row 9: the only TWR factor is negative, and the MWR root `−1.1` lies
  below `−1`.
- Row 1 with cash below zero on any day of the range: all three `null`.

## Recorded simplifications

- Interest is a `dividend` with no instrument (Phase 15, GLG-533). The
  transactions API accepts one with its currency validated like a
  deposit's, and Midas "Diğer Gelir" (nema) is imported as one; until then
  it was a `deposit`, an external flow that made all three returns
  understate. The history sweep counts it as return, not as a flow
  (`dayFlow` in `backend/internal/server/history.go` sums deposits and
  withdrawals only), so simple, TWR and MWR include it.
  `holdings.State.Apply` books it, net of its fee, to cash only, since there
  is no position to carry realized P/L, and `ownDateFlows` in
  `backend/internal/server/summary.go` counts it with the deposits, so it
  stays out of `fx_pl`. On the KPI card it therefore shows in cash and total
  value but not in `realized_pl`. Upgrade path: an income-per-currency map
  on `holdings.Summary`, added to `RealizedPL`, with the interest then
  dropped from `ownDateFlows` so it is not taken out of `fx_pl` twice.
- Multiple IRR roots are not detected.
- A range whose first point has no FX rate (`V_0 == 0`), or with cash below
  zero on any day (a buy recorded before the deposit that paid for it),
  shows no return at all, even when a shorter range inside it would have
  one.

### Not in this epic

- Annualized returns in the API. The chart header annualizes the period
  return in the frontend, `(1 + r)^(365 / days) − 1`, only for ranges a
  year or more long (Phase 21, GLG-607). `days` is the gap between the
  first and last point; a gap of 364 to 366 days is one year, so every
  calendar year (2025 as well as the leap year 2024, GLG-661) and the 1Y
  preset (GLG-675) qualify and annualize to their own period return. MWR
  is a period IRR, so the same formula applies to all three methods.
- Per-position returns.
- Returns on the home portfolio cards.
- Benchmark comparison.

> Superseded 2026-09-27 by [ADR 0012](0012-benchmarks.md) (GLG-487): the
> chart header compares the portfolio's return against one benchmark, a
> shadow portfolio that receives the same flows.

## Consequences

- GLG-390 adds the pure return functions, simple, TWR and MWR, over a value
  and flow series, in `returns.go`.
- GLG-391 computes the three returns in the history sweep and adds `returns`
  to the response.
- GLG-392 adds the return method selector on the chart header.
- GLG-393 removes `net_flow_weighted` once the frontend reads `returns`.
- GLG-394 writes this ADR, the Phase 12 roadmap section and the ADR 0005 fix.
- GLG-533 (Phase 15) books interest as a dividend with no instrument, and
  GLG-529 records it under Recorded simplifications.
- GLG-641 makes all three returns, and the benchmark's (ADR 0012), `null`
  without a positive base, which also fixes GLG-632.
