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

The benchmark figures use the days on which both `r_i` and `b_i` exist, `m` of
them, so each pair is the same day:

- **benchmark volatility**: the sample standard deviation of `b_i` over those
  days, times `√365`, so it compares with a portfolio's volatility over the
  same days.
- **beta**: `cov(r, b) / var(b)`, both with the divisor `m − 1`.
- **correlation**: `cov(r, b) / (σ_r × σ_b)`.

All of them are `null` when `benchmark_twr` is `null` on any point of the
window (no benchmark asked for, or one with no level on or before the
history's first point, which nulls it on every point, ADR 0012; on the position
page, a day from `returns_from` with no level or no rate, ADR 0033), and when `Σ(b_i − b̄)²` is 0, since a flat benchmark has no
beta; the correlation is `null` too when `σ_r` is 0. A `null` benchmark figure
never hides the portfolio's own.

### Windows and the minimum

The Performans card reads the all-time history, the one the drawdown card
reads, with the page's benchmark, and cuts it to a window with a segmented control, **1Y | 3Y | Tümü**. It opens on
1Y and the choice is not stored. A window is the points dated on or after the
last point's date minus 365 days (1Y) or 1095 days (3Y); Tümü is every point.
The first point of a window is its base, so the window's returns are the ones
of the points after it. All three options are always offered.

A window with fewer than **30 daily returns** shows "Yeterli veri yok" in place
of the figures, benchmark figures included, instead of a dash per figure: a
month of days says more about that month than about the portfolio. The
benchmark figures need 30 shared returns of their own and are `null` below
that.

The all-time history is the history API's own range, no more than 3660 days
([ADR 0019](0019-core-engine-and-auth.md)); a longer record is cut at its
start.

### Drawdown recovery

The drawdown ([ADR 0011](0011-portfolio-returns.md)) gains the day it was won
back. For the largest drawdown, `recovered` is the first date after its trough
on which `1 + twr` is at or above the index of its peak, and `null` while the
portfolio is still below it. The header adds the days from peak to trough, and
from trough to recovery, as the calendar-day gap of the two dates, or "henüz
toparlanmadı" while `recovered` is `null`. This changes `drawdown()` and its
return value, and no API.

### Rolling 365-day return

For each point `i`, the rolling return is

```
(1 + twr_i) / (1 + twr_k) − 1
```

where `k` is the last point dated on or before the date of `i` minus 365 days.
A point with no such `k` (the first year) is left out, and so is one with a
`null` `twr` on `i` or `k`, or with `1 + twr_k` at 0. The benchmark's line is
built the same way from `benchmark_twr`, `null` where it is missing on `i` or
`k`. It is the return of the last 365 days as they ran, not annualized. The
card draws both lines under the figures over the whole history, so each point
reads what the twelve months ending there returned against the benchmark, and
draws no chart when no point has a `k`.

### The position page

The position page ([ADR 0016](0016-position-detail.md)) shows the same risk
figures, computed with the same functions over the points of the position
history for the page's range and benchmark, in the instrument's currency
([ADR 0033](0033-position-returns.md)): a rate's move is not in them and the
benchmark is the one converted into that currency. The range picker is the
window and there is no window control. Points whose `twr` is `null`, those
before `returns_from` and after `returns_to`, are not in the series, and a
return needs two neighbors that are. A series with fewer than 30 returns shows
the same "Yeterli veri yok".

### Out of scope

- **Sharpe and Sortino.** They need a risk-free rate, and the app has no
  risk-free series: TÜFE is inflation and not a rate, and a rate of 0 would
  flatter every ratio in a currency whose deposits pay tens of percent a year.
  They wait for a risk-free series, and so does any ratio over a risk-free
  rate; the downside deviation above is already the Sortino denominator when
  that day comes.
- Alpha, tracking error, R², value at risk and a volatility chart over time.
- A computation in the backend or new fields on the history API.
- A drawdown or a rolling return per position.
- Risk figures in the positions table and on the Portföyler table of the all
  view, and against more than one benchmark at a time.
