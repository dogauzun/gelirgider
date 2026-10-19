# ADR 0036: Risk and volatility figures

Status: accepted (2026-10-19).

## Context

The app shows three returns ([ADR 0011](0011-portfolio-returns.md)), one
benchmark to set against them ([ADR 0012](0012-benchmarks.md)) and the
drawdown. None of them says how rough the road was: two portfolios with the
same return can differ in how far and how fast they swung. Comparable products
show volatility, beta against an index and risk-adjusted figures, and a user
who asks whether a return was worth its swings has nothing to look at.

The inputs exist. Each history point carries the running time-weighted return
`twr` and, with a benchmark asked for, `benchmark_twr`
([ADR 0011](0011-portfolio-returns.md), [ADR 0012](0012-benchmarks.md)), and
the position history carries both in the instrument's currency
([ADR 0033](0033-position-returns.md)). The drawdown card already turns that
series into a statistic in the frontend.

## Decision

### Computed in the frontend, from the points already loaded

The figures are functions of the history points' `twr` and `benchmark_twr`,
like the drawdown (`frontend/src/lib/drawdown.ts`), and the API gets no new
field and no new route. The rules that make a flow, the first-day rule and the
`null` rules all live in the two running series, so a second copy of them
cannot drift, which is why ADR 0011 kept the returns in the backend and why
this does not need to. The math is float64, as there: these are ratios, not
money (PRINCIPLES #3). `twr` has 6 decimals, so each daily return carries a
rounding error of about `1e-6`, far below the one or two decimals of a percent
the card shows.

### The daily return

For consecutive points `i−1` and `i`:

```
r_i = (1 + twr_i) / (1 + twr_{i-1}) − 1
```

The points are calendar days: a day the markets are shut repeats the previous
value ([ADR 0011](0011-portfolio-returns.md)), so its return is 0 and it is
still a return. A return exists only when `twr` is not `null` on both points
and `1 + twr_{i-1}` is above 0 (a portfolio that fell 100% has nothing to earn
a return on); otherwise that day is left out. `benchmark_twr` gives `b_i` the
same way. When `twr` is `null` on every point, every figure is `null`.

Worked example: the running `twr` on four consecutive days is 0.000000,
0.020000, 0.009800 and 0.040094.

| day | twr      | r_i   |
| --- | -------- | ----- |
| 1   | 0.000000 |       |
| 2   | 0.020000 | +0.02 |
| 3   | 0.009800 | −0.01 |
| 4   | 0.040094 | +0.03 |

`1.0098 / 1.02 − 1 = −0.01` and `1.040094 / 1.0098 − 1 = 0.03`. The mean is
0.013333 and the sample standard deviation of 0.02, −0.01 and 0.03 is
√(0.000867 / 2) ≈ 0.020817 (0.000867 is the sum of the squared deviations from
the mean).

### Annualized with √365

The figures that are a spread are annualized by `√365`, whatever the markets
in the portfolio. The history has a point for every calendar day, and the
portfolio holds BIST, US, TEFAS, crypto and gold, which trade on different
days, so there is no one trading calendar to count. The sum of squared moves
over a year is the same whichever days carry them: a move that lands on a
Monday after a flat weekend is one larger day, not three, and the per-day
variance times 365 gives the same annual variance as the variance of trading
days times 252. `√365` over calendar-day returns therefore reads the same for
a stock-only and for a mixed portfolio, without choosing a calendar.

### The figures

Over the window's returns (windows below), with `n` the number of returns:

- **volatility**: the sample standard deviation of `r_i` (divisor `n − 1`),
  times `√365`.
- **downside deviation**: the target is 0, `√(Σ min(r_i, 0)² / n) × √365`.
  Every return counts in the divisor, not only the negative ones, and the mean
  is not subtracted.
- **best day** and **worst day**: the largest and the smallest `r_i`, with
  their dates.

For the example: the volatility is 0.020817 × √365 ≈ 39.8%, and the downside
deviation is √(0.01² / 3) × √365 ≈ 11.0%, since only day 3 is below 0. The best
day is day 4 at +3% and the worst day is day 3 at −1%.

### Against the benchmark

The benchmark figures use only the days on which both `r_i` and `b_i` exist,
`m` of them, so each pair is the same day and a benchmark that starts later
does not pair with the days before it:

- **benchmark volatility**: the sample standard deviation of `b_i` over those
  days, times `√365`, so it compares with a portfolio's volatility over the
  same days.
- **beta**: `cov(r, b) / var(b)`, both with the divisor `m − 1`.
- **correlation**: `cov(r, b) / (σ_r × σ_b)`.

They are `null` when no `benchmark_twr` is on the points (no benchmark asked
for, or none with a level on the first point, ADR 0012), when fewer than 30
shared returns remain, and for beta and correlation when `var(b)` is 0 (a flat
benchmark has no beta); the correlation is `null` too when `σ_r` is 0. A
`null` benchmark figure never hides the portfolio's own.

### Windows and the minimum

The Performans card computes the figures over the all-time history, the one
the drawdown card reads, and cuts it to a window: **1Y** (the returns dated
within the last 365 days of the last point), **3Y** (1095 days) and **Tümü**
(all of them). A window longer than the history equals Tümü and is not
offered. A figure needs at least **30 returns** in its window, 30 shared ones
for the benchmark figures; with fewer it is `null` and shown as a dash, since
a month of days says more about that month than about the portfolio.

The all-time history is the history API's own range, no further back than 3660
days ([ADR 0016](0016-position-detail.md)); a longer record is cut at its
start.

### Drawdown recovery

The drawdown ([ADR 0011](0011-portfolio-returns.md)) gains the day it was won
back. For the largest drawdown, `recovered` is the first date after its trough
on which `1 + twr` is at or above the index of its peak, and `null` while the
portfolio is still below it. The card shows the days from peak to trough, and
from trough to recovery, as the calendar-day gap of the two dates; while
`recovered` is `null` it shows the days since the trough instead, marked as
not yet recovered. This changes `drawdown()` and its return value, and no API.

### Rolling 365-day return

For each point `t`, the rolling return is

```
(1 + twr_t) / (1 + twr_k) − 1
```

where `k` is the latest point dated on or before the date of `t` minus 365
days. A point with no such `k` (the first year), a `null` `twr` on either point
or `1 + twr_k` at 0 has no value. It is the return of the last 365 days as
they ran, not annualized, and the card draws it as a line from the first day
that has one, with its latest, best and worst value.

### The position page

The position page ([ADR 0016](0016-position-detail.md)) computes the same
figures with the same functions, over the page's range and not a window, from
the points of the position history, in the instrument's currency
([ADR 0033](0033-position-returns.md)): a rate's move is not in them and the
benchmark is the one converted into that currency. Points whose `twr` is
`null`, those before `returns_from` and after `returns_to`, are not in the
series, and a return needs two neighbors that are. The minimum of 30 returns
and every `null` rule hold; a short range simply shows dashes. The drawdown
and its recovery come with them, so ADR 0033 no longer lists a drawdown per
position as not offered.

### Out of scope

- **Sharpe and Sortino.** They need a risk-free rate, and the app has no
  risk-free series: TÜFE is inflation and not a rate, and a rate of 0 would
  flatter every ratio in a currency whose deposits pay tens of percent a year.
  They wait for a risk-free series, and the downside deviation above is
  already the Sortino denominator when that day comes.
- Alpha, tracking error, R², value at risk and a rolling volatility line.
- A computation in the backend or new fields on the history API.
- Risk figures on the positions table, the Portföyler table and the home card,
  and against more than one benchmark at a time.
