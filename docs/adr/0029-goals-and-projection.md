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
The amount must be positive and the target date must be after today. A target
date later this month is therefore valid; the projection then has no month to
run (see "The projection").

### Units are relative to the reporting currency

- `nominal`: the amount is in the reporting currency.
- `real`: the amount is in the reporting currency at today's prices, deflated
  by TÜFE. Allowed only when the reporting currency is TRY.
- `usd`: the amount is in US dollars. Allowed only when the reporting
  currency is not USD.

The write path rejects any other combination with 400. A real unit for another
reporting currency is out of v1: the app holds no price index for it.

The goal row stores no currency, and changing the reporting currency does not
touch the user's goals: `PUT /api/me/reporting-currency` only updates the
setting. A stored goal can therefore meet a reporting currency its unit would
not be accepted for. The projection route handles that without an error. A
`real` goal under a reporting currency other than TRY answers an empty fan (see
"The projection"). A `nominal` amount is read in the new currency, and a `usd`
goal under a USD reporting currency converts at 1. The user replaces or deletes
the goal.

### Routes

`GET`, `PUT` and `DELETE /api/portfolios/{id}/goal` and
`GET /api/portfolios/{id}/projection`. `{id}` is a number or `all`. A scope with
no goal answers 404 on the `GET` and `DELETE` of the goal and on the
projection. The goal routes, the allocation targets routes of
[ADR 0028](0028-target-allocation.md) and the cash-gaps `POST` of ADR 0011 are
the routes that accept `all` for writing
([ADR 0014](0014-all-portfolios-view.md)): `PUT` and `DELETE` on `all` write the
goal whose `portfolio_id` is NULL. The goal and allocation targets routes
resolve `{id}` to a nullable portfolio id with `scopePortfolioID`; the
projection resolves it with the transaction-list resolver of ADR 0014, as a
history read does. The all-portfolios goal costs no extra route. Only the owner
reads or writes a goal.

### The projection reuses the history sweep

The returns periods route already cuts one sweep into calendar months, each
with its TWR ([ADR 0011](0011-portfolio-returns.md)). The projection takes
from that sweep:

- the monthly TWRs of the last 36 full months, the partial first and current
  buckets left out (fewer than 36 are all used);
- the flow `f` of each of the last 12 full months, which is the sum of the
  sweep's daily external flows over every day of the calendar month, the 1st
  included (fewer than 12 are all used);
- today's value, from the same sweep.

A month's `f` is not read from its periods bucket: a bucket treats its first
day as the base, with that day's flow inside the base value, so a deposit made
on the 1st of every month would show as a flow of zero and the one-line answer
would ask for money the user already pays.

A month whose TWR is `null` (ADR 0011: no positive base, or negative cash) is
left out of the percentiles. With fewer than 6 months that have a TWR there is
no projection: the route answers an empty `months` and the count of usable
months in `history_months`.

The recurrence, for month `k` from 1 up to the target month, where month 1 is
the month after today:

```
v[0] = today's value
v[k] = v[k-1] * (1 + r) + f
```

`f` is the median of the monthly flows, by nearest rank. `r` is a percentile of
the monthly TWRs, by nearest rank (the value at position `ceil(p * n)` of the
sorted list): the 25th for the pessimistic path, the 50th for the median path
and the 75th for the optimistic path. The three paths share `f`. The paths are
a fan from past behaviour, not a forecast: they say what the same pace would
give, and nothing more.

Floats are fine here. A projection is an estimate, not money, and the same
argument as for the return ratios in ADR 0011 applies; the goal's amount stays
decimal text in the table and in the response.

The fan is empty, with no `reached_at` and no `monthly_gap`, in these cases:

- the scope has fewer than 6 usable months (`history_months` says how many);
- the target month is the current month or an earlier one, since the months
  start next month; `progress` and `history_months` are still answered;
- there is nothing to project from: the scope has no transactions or its first
  transaction is after today, the unit's TÜFE level or USD rate is missing, or
  the goal is `real` under a reporting currency other than TRY. `progress` and
  `history_months` are 0.

### Real and USD conversions

Every projected value is converted into the goal's unit. Today's value is
converted at month 0.

- `nominal`: the path as it is.
- `real`: a nominal value at month `k` is multiplied by `(1 + m)^-k`, so the
  fan is in today's prices. `m` is the monthly rate of TÜFE over the last year,
  `m = (L_today / L_12ago)^(1/12) − 1`, with `L` the daily TÜFE level of
  ADR 0012, its estimated part included. The rate is held constant for every
  future month. A goal set long ago is not re-anchored: the amount means
  today's prices whenever the projection is read.
- `usd`: every month is divided by today's USD rate, held constant. No
  currency forecast is made.

The comparison with the goal's amount is in the goal's unit.

### The one-line answer

The answer comes from the median path, in the goal's unit, over the months 1 to
the target month:

- When some month reaches the amount, `reached_at` is the first such month.
  Month 0 is not tested, so `reached_at` is never the current month, even for a
  portfolio that already is at the amount; `progress` shows that.
- Otherwise `monthly_gap` is the extra monthly flow `x` that makes it: the
  smallest `x` for which the median path, run with `f + x` in place of `f`,
  reaches the amount in the target month. `x` is found by bisection on the same
  recurrence over `0` to the amount converted back to the reporting currency at
  the target month, in 40 halvings, and the answer is rounded up to a whole
  unit of the reporting currency.

`reached_at` and `monthly_gap` are never both set. The card words them as "at
this pace the goal is reached in <month>" and "the goal needs <x> more a
month".

### Progress

Progress is today's value in the goal's unit over the amount. For `real` that
equals the nominal value over the amount, since today's prices are the unit, so
inflation alone never moves it. The projection route serves it, and the
portfolio header shows it as a pill next to the name on the summary page. It
has no route of its own, so the header and the card cannot disagree.

## Out of scope

- Withdrawal and retirement modelling.
- Several goals per scope, and several scenarios per goal.
- Monte Carlo.
- A real unit for a non-TRY reporting currency.
- Email reminders.
