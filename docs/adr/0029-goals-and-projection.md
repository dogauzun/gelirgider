# ADR 0029: Goals and the real-terms projection

Status: accepted (2026-10-18).

## Context

The app shows where a portfolio stands and how it did
([ADR 0011](0011-portfolio-returns.md)), but nothing about where it is going.
A user saving toward an amount by a date wants to see whether the pace so far
gets there. In TRY a nominal answer misleads, since the lira loses value every
month, so the projection has to be readable in constant prices or in dollars.
The app already has the TÜFE series ([ADR 0012](0012-benchmarks.md)) and the USD
rate ([ADR 0003](0003-multi-currency-and-market-data-sources.md)) to do that.

## Decision

### One goal per scope

Table `goals` holds `user_id`, a nullable `portfolio_id`, `amount` as exact
decimal text, `unit`, `target_date` as a market date
(`Europe/Istanbul`) and `created_at` (UTC). `portfolio_id` NULL is the
all-portfolios goal. A scope has at most one goal, kept by a unique index per
scope, and a new goal replaces the old one in one transaction, with a new
`created_at`. Like the other per-user tables, every query filters by `user_id`.
The amount must be positive and the target date must fall in a later calendar
month than today (`Europe/Istanbul`), so a goal is never written with a target
month that has no month to project. The target month then arrives with time:
a goal whose target month is the current one or an earlier one stays stored and
the projection answers an empty fan (see "The projection reuses the history
sweep").

### Units are relative to the reporting currency

- `nominal`: the amount is in the reporting currency.
- `real`: the amount is in the reporting currency at the prices of the day the
  goal was set, measured by TÜFE. Allowed only when the reporting currency is
  TRY. The day is the Istanbul date of `created_at`.
- `usd`: the amount is in US dollars. Allowed only when the reporting
  currency is not USD.

The write path rejects any other combination with 400. A real unit for another
reporting currency is out of v1: the app holds no price index for it.

The goal row stores no currency, so a reporting currency change could change
what a goal means. `PUT /api/me/reporting-currency` therefore answers 409
`goals depend on the reporting currency`, and changes nothing, when the new
currency differs from the current one and the user has a goal that the change
would break: any goal whose unit is not `usd`, or any goal at all when the new
currency is USD. The goals of all the user's scopes count. Setting the current
currency again is allowed. The user deletes the goals first. A `usd` goal
survives a change between two currencies other than USD. The projection route
does not rely on the rule: a `real` goal under a reporting currency other than
TRY answers an empty fan.

### Routes

`GET`, `PUT` and `DELETE /api/portfolios/{id}/goal` and
`GET /api/portfolios/{id}/projection`. `{id}` is a number or `all`. A scope with
no goal answers 404 on the `GET` and `DELETE` of the goal and on the
projection. The goal routes, the allocation targets routes of
[ADR 0028](0028-target-allocation.md) and the cash-gaps `POST` of ADR 0011 are
the routes that accept `all` for writing
([ADR 0014](0014-all-portfolios-view.md)): `PUT` and `DELETE` on `all` write the
goal whose `portfolio_id` is NULL. The goal and allocation targets routes
resolve `{id}` to a nullable portfolio id with `scopePortfolioID`. The
projection uses that resolver too, to look up the goal, and then passes `{id}`
to the transaction-list resolver of ADR 0014, as a history read does, to load
the transactions it sweeps. The all-portfolios goal costs no extra route. Only
the owner reads or writes a goal.

### The projection reuses the history sweep

The returns periods route already cuts one sweep into calendar months, each
with its TWR ([ADR 0011](0011-portfolio-returns.md)). The projection takes
from that sweep, which runs from the first transaction (at most 3660 days back)
to today:

- the monthly TWRs of the last 36 full months, the partial first and current
  buckets left out (fewer than 36 are all used);
- the flow `f` of each of the last 12 full months, again without the partial
  buckets (fewer than 12 are all used);
- today's value, in the reporting currency.

`f` of a month is the bucket's own `flow` field, not the flows its TWR uses.
A bucket treats its first day as the base, with that day's flow inside the
base value, so the flows its TWR uses leave the first day out. The `flow` field
is the sum of those flows with the first day's flow added back. It therefore
counts every day of the calendar month, the 1st included. A deposit made on the
1st of every month would otherwise show as a flow of zero and the one-line
answer would ask for money the user already pays.

A month whose TWR is `null` (ADR 0011: no positive base, or negative cash) is
left out of the percentiles but still counts as one of the 36 months and
supplies its flow. With fewer than 6 months that have a TWR there is no
projection: the route answers an empty `months` and the count of usable months
in `history_months`.

The recurrence, for month `k` from 1 up to the target month, where month 1 is
the month after today and the target month is counted in:

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

The fan is empty (`months` is `[]`), with no `reached_at` and no `monthly_gap`,
in these cases:

- the scope has fewer than 6 usable months; `history_months` says how many and
  `progress` is answered;
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
- `real`: a nominal value at month `k` is multiplied by
  `(L_created / L_today) * (1 + m)^-k`, so the fan is in the prices of the day
  the goal was set. `L` is the daily TÜFE level of ADR 0012, its estimated part
  included. `L_created` is the level on the Istanbul date of `created_at`, or
  of the first later day that has a level if TÜFE has none yet on that day. `m`
  is the monthly rate of TÜFE over the last year,
  `m = (L_today / L_12ago)^(1/12) − 1`, held constant for every future month.
  The goal stays anchored to the day it was set: it is not re-based as the
  months pass, and replacing the goal anchors it to the new day.
- `usd`: every month is divided by today's USD rate, held constant. No
  currency forecast is made.

The comparison with the goal's amount is in the goal's unit.

### The one-line answer

Whenever the fan is not empty, the answer comes from the median path, in the
goal's unit:

- When `progress` is already at least 1, `reached_at` is the current month.
- Otherwise `reached_at` is the first of the months 1 to the target month in
  which the median path reaches the amount, if there is one.
- When `reached_at` is not set, `monthly_gap` is the extra monthly flow `x`
  that makes it: the smallest `x` for which the median path, run with `f + x` in
  place of `f`, reaches the amount in the target month. The final value is
  linear in `x`, so `x` is solved for directly, with the amount converted back
  to the reporting currency at the target month, and rounded up to a whole unit
  of the reporting currency. A float error below `1e-6` does not push it up a
  unit.

`reached_at` and `monthly_gap` are never both set. The card words them as "at
this pace the goal is reached in <month>" and "the goal needs <x> more a
month". It words an empty fan two ways: "the target date has passed" when the
target month is the current month or an earlier one, and otherwise "a
projection needs at least 6 months of history", which also covers a fan with
nothing to project from.

### Progress

Progress is today's value in the goal's unit over the amount. For `real` that is
the nominal value times `L_created / L_today`, over the amount, so inflation
since the goal was set lowers it and the same value counts for less. The
projection route serves it, and the portfolio header shows it as a pill next to
the name on the summary page, a link to the goal card. It has no route of its
own, so the header and the card cannot disagree.

## Out of scope

- Withdrawal and retirement modelling.
- Several goals per scope, and several scenarios per goal.
- Monte Carlo.
- A real unit for a non-TRY reporting currency.
- Email reminders.
