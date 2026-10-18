# ADR 0029: Goals and the real-terms projection

Status: accepted (2026-10-18).

## Context

The app shows where a portfolio stands and how it did
([ADR 0011](0011-portfolio-returns.md)), but nothing about where it is going.
A user saving toward an amount by a date wants to see whether the pace so far
gets there. In TRY a nominal answer misleads, since the lira loses value every
month, so the projection has to be readable in today's prices or in dollars.
The app already has the TÜFE series ([ADR 0012](0012-benchmarks.md)) and the USD
rate ([ADR 0003](0003-multi-currency-and-market-data-sources.md)) to do that.

## Decision

### One goal per scope

Table `goals` holds `user_id`, a nullable `portfolio_id`, `amount` as exact
decimal text, `unit`, `target_date` as a market date
(`Europe/Istanbul`) and `created_at`. `portfolio_id` NULL is the
all-portfolios goal. A scope has at most one goal and a new goal replaces the
old one. Like the other per-user tables, every query filters by `user_id`.
The target date must be after today.

### Units are relative to the reporting currency

- `nominal`: the amount is in the reporting currency.
- `real`: the amount is in the reporting currency at today's prices, deflated
  by TÜFE. Allowed only when the reporting currency is TRY.
- `usd`: the amount is in US dollars. Allowed only when the reporting
  currency is not USD.

The backend rejects any other combination with 400. A real unit for another
reporting currency is out of v1: the app holds no price index for it.

### Routes

`GET`, `PUT` and `DELETE /api/portfolios/{id}/goal` and
`GET /api/portfolios/{id}/projection`. `{id}` is a number or `all`, as on every
portfolio route ([ADR 0014](0014-all-portfolios-view.md)), so the scope
resolver serves them and the all-portfolios goal costs no extra route. Only the
owner reads or writes a goal.

### The projection reuses the history sweep

The returns periods route already cuts one sweep into calendar months, each
with its TWR and external flow ([ADR 0011](0011-portfolio-returns.md)). The
projection takes from those buckets:

- the monthly TWRs of the last 36 full months, the partial first and current
  buckets left out (fewer than 36 are all used);
- the net external flow of each of the last 12 full months (fewer than 12 are
  all used);
- today's value, from the same sweep.

With fewer than 6 full months there is no projection: the route says so and
returns no paths.

The recurrence, for month `k` from today up to the target month:

```
v[0] = today's value
v[k+1] = v[k] * (1 + r) + f
```

`f` is the median of the monthly flows. `r` is a percentile of the monthly
TWRs, by linear interpolation between ranks: the 25th for the pessimistic
path, the 50th for the median path and the 75th for the optimistic path. The
three paths share `f`. The paths are a fan from past behaviour, not a
forecast: they say what the same pace would give, and nothing more.

Floats are fine here. A projection is an estimate, not money, and the same
argument as for the return ratios in ADR 0011 applies; today's value and the
goal's amount stay decimal at the edges.

### Real and USD conversions

- `nominal`: the path as it is.
- `real`: month `k` is divided by `(1 + m)^k`, where `m` is the monthly
  inflation rate. TÜFE is extended past today at the trailing 12-month rate,
  so `m = (L_today / L_12ago)^(1/12) − 1` with `L` the TÜFE level of
  ADR 0012, its estimated part included. Month 0 is today, so today's value is
  its own real value.
- `usd`: every month is divided by today's USD rate, held constant. No
  currency forecast is made.

The conversion is applied to the paths, and the comparison with the goal's
amount is in the goal's unit.

### The one-line answer

The answer comes from the median path, in the goal's unit:

- When some month up to the target date reaches the amount, the answer is the
  first such month: "at this pace the goal is reached in <month>". A portfolio
  that already is at the amount answers with month 0.
- Otherwise the answer is the extra monthly flow `x` that makes it: the
  smallest `x` for which the median path, run with `f + x` in place of `f`,
  reaches the amount in the target month. The value at the target month grows
  with `x`, so `x` is found by bisection on the same recurrence, the upper
  bound doubling until the amount is reached. The line reads "the goal needs
  <x> more per month", in the reporting currency.

### Progress

Progress is today's value in the goal's unit over the amount. The projection
route serves it, and the portfolio header shows it as a pill. It has no route
of its own, so the header and the card cannot disagree.

## Out of scope

- Withdrawal and retirement modelling.
- Several goals per scope, and several scenarios per goal.
- Monte Carlo.
- A real unit for a non-TRY reporting currency.
- Email reminders.
