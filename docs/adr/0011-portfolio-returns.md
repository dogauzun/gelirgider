# ADR 0011: Portfolio returns

Status: accepted (2026-09-25).

## Context

Every percentage in the app other than the chart header's is P/L over cost.
That reads a deposit as a gain unless the flows are taken out, and says
nothing about how the portfolio itself did. The chart header shows one
percentage for the selected range, and it has to survive deposits and
withdrawals in that range.

## Decision

### Three period returns, chosen on the chart header

For the selected range the app shows one of three returns:

- **simple**: the gain over the money put in.
- **time-weighted (TWR)**: the return of the portfolio itself, with the
  timing and size of deposits and withdrawals taken out.
- **money-weighted (MWR)**: the true internal rate of return of the range.

The user picks the method with a selector on the chart header only, and the
choice is remembered in `localStorage`, like the theme and the locale. The
header also compares the return against one benchmark (ADR 0012). The home
portfolio cards and the KPI card do not show these returns, and there are no
per-position returns.

The API returns period returns. The chart header annualizes the period return
in the frontend, `(1 + r)^(365 / days) − 1`, only for ranges a year or more
long. `days` is the gap between the first and last point; a gap of 364 to 366
days is one year, so every calendar year and the 1Y preset annualize to their
own period return. MWR is a period IRR, so the same formula applies to all
three methods.

### Computed in the backend, returned on the history API

The returns are computed in the history sweep and returned on
`GET /api/portfolios/{id}/history` as `returns: {simple, twr, mwr}`. Each
value is a fraction (0.25 is 25%) as a string with 6 decimals, or `null` when
it is not defined.

Computing them in the frontend was rejected. What counts as a flow (the carry
of flows with no FX rate yet, the first-price jumps, gross fees, the
first-day rule) lives in the sweep, and the response is the documented API
shape (PRINCIPLES #8). A second copy of those rules in TypeScript would
drift.

Two views of the same rules sit beside it:

- Each history point also carries `twr`, the running product up to that day
  minus one: `"0.000000"` on the first point, a skipped day repeats the
  previous day's value, and the last point equals `returns.twr`. When
  `returns.twr` is `null`, `twr` is `null` on every point, never on some.
  The Performans tab's drawdown is a transform of that series in the
  frontend: how far `1 + twr` is below its highest value so far.
- `GET /api/portfolios/{id}/returns/periods` returns the three returns, and
  the benchmark's, for every calendar month and year from the first
  transaction to today, oldest first; the first and the current bucket are
  `partial`. A bucket's returns are the history endpoint's for that
  period's range, cut from one sweep over the whole span. The first day of a
  period is its base with its flows inside it, so the return of each
  period's first day is in neither bucket and twelve months do not chain
  exactly to their year. A bucket's `benchmark` is `null` when none was
  asked for or the benchmark has no level on some day of the bucket, never a
  number over part of it.

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

- `V_i` is the exact total value of that day in the reporting currency, the
  same value the chart draws.
- `F_i` is the external flow that entered on day `i`, in the reporting
  currency.

`F_0 = 0`: a flow on the first day is already inside `V_0`. The range starts
at the first transaction when the requested range opens before it, so the
first deposit is not weighed as a mid-range flow.

An external flow is a converted deposit minus a converted withdrawal, gross,
plus the first-price jump pseudo-flows (a position held before its first
price enters the value at that price). Fees, buys, sells, dividends and
splits are not flows: they move value inside the portfolio or are part of its
performance. Interest is a `dividend` with no instrument, so it counts as
return, not as a flow.

Flows are end-of-day: a flow on day `i` does not earn anything on day `i`. A
carried flow, one in a currency with no FX rate yet, counts on the day it
converts and takes that day's position, because the cash is not in `V` before
it converts.

### No return without a positive base

All three returns are `null` when `V_0 <= 0`, or when the day's cash,
converted to the reporting currency, is negative on any day of the range. A
portfolio that records buys but not the deposits that paid for them has
negative cash, so `V` is only the P/L of positions bought with money never
put in, and any ratio over it is meaningless. The chart header then shows a
dash for the percentage. The same rule makes the benchmark's return `null`.

Treating the part of a buy that takes cash below zero as an implicit deposit
was rejected: it would invent flows the user never recorded. Instead the app
proposes recorded deposits for the cash gaps, one per day a currency's cash
reaches a new low below zero (`GET` and `POST /api/portfolios/{id}/cash-gaps`),
and writes them only when the user confirms.

On a one-point range there is nothing to compare: all three returns are
`null` and `net_flow` is zero.

### simple

```
simple = (V_last − V_0 − ΣF) / (V_0 + ΣF)
```

The gain over everything put in: the start value plus the net flows. It is
`null` when the base `V_0 + ΣF` is not positive.

Known weakness: a huge deposit on the last day pulls it toward 0. That is
what "gain over money put in" means; TWR and MWR are the answer to it.

### twr

```
twr = Π_{i≥1} (V_i − F_i) / V_{i−1}  −  1
```

Each day's factor is the day's value without the flow that just came in, over
the previous day's value. A day with `V_{i−1} <= 0` is skipped: there was
nothing to earn a return on. The result is `null` when any factor is
negative, or when no day produced a usable factor. A factor of exactly 0 is
kept: it is a real −100%.

### mwr

The period rate `R` that solves

```
V_0·(1+R) + Σ F_i·(1+R)^{w_i} = V_last,   w_i = (n−1−i)/(n−1)
```

`w_i` is the fraction of the range left after day `i`. It is solved by
bisection on `[−1, hi]`. `hi` starts at 1 and doubles while `f(hi) <= 0`, up
to `1e6`. The result is `null` when there is no sign change on the interval,
or with fewer than two points.

The table of worked examples (values, flows and the three expected returns)
is the test in `backend/internal/server/returns_test.go`.

## Recorded simplifications

- Multiple IRR roots, which alternating large flows can produce, are not
  detected.
- A range whose first value is not positive, or with cash below zero on any
  day, shows no return at all, even when a shorter range inside it would have
  one.
- Interest is booked, net of its fee, to cash only: there is no position to
  carry realized P/L, so on the KPI card it shows in cash and total value but
  not in `realized_pl`, and it stays out of `fx_pl`. Upgrade path: an
  income-per-currency map on the holdings summary, added to realized P/L,
  with the interest then dropped from the `fx_pl` flows so it is not taken
  out twice.
