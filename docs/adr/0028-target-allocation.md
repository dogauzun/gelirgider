# ADR 0028: Target allocation and the contribution planner

Status: accepted (2026-10-18).

## Context

The allocation card shows what the user holds, by asset class
([ADR 0026](0026-instrument-classification.md)), but not what the user meant to
hold. A user who invests a fixed amount each month wants to see how far the
portfolio is from a plan, and where the next contribution should go.

## Decision

### Targets are per asset class

A target is a weight for one of the six classes of ADR 0026: `equity`, `fund`,
`commodity`, `crypto`, `deposit`, `cash`. The backend has no constant for them
elsewhere, so the package that owns targets carries the list. Per-instrument
and per-sector targets are out of v1.

### One target set per scope

Table `allocation_targets` holds `user_id`, a nullable `portfolio_id`,
`asset_class` and `weight`. `weight` is an exact decimal text in percent, and a
set sums to 100 (the header comment of `037_allocation_targets.sql` says
"a fraction"; that is wrong, and the file is not edited because the checksum
guard of ADR 0002 would fail startup on every database that applied it). A set belongs to one portfolio; `portfolio_id` NULL is the
all-portfolios set. Like the term deposits of [ADR 0024](0024-term-deposits.md),
the table is per-user data and every query filters by `user_id`.

### Routes

`GET` and `PUT /api/portfolios/{id}/allocation-targets`. `{id}` is a number or
`all`: these routes are an exception to the 404 that most portfolio routes
return for `all` ([ADR 0014](0014-all-portfolios-view.md)), and `PUT` on `all`
writes the set whose `portfolio_id` is NULL. The all-portfolios set costs no
extra route. Only the owner reads or writes a set. `PUT` replaces the whole set
in one transaction.

### Drift is computed in the frontend

Drift is the actual share minus the target, in percentage points, per class. It
is computed from the summary's positions and never stored. The share is the one
the allocation card already shows.

### The `allocation_drift` Dikkat rule

A per-user attention rule, default threshold 5 points. A class whose drift is
above the threshold, in either direction, becomes an attention item. The rule
is evaluated for the page's own scope: the portfolio's set on a portfolio page,
the all-portfolios set on the all page. It yields nothing when the scope has no
targets.

### The planner buys only and names classes

The planner runs in the frontend on the summary's data. It takes an amount in
the reporting currency and answers with an amount per class, in the reporting
currency only. It never sells.

With `current` the value of a class today and `T = holdings + amount`, each
class's gap is `max(0, weight / 100 × T − current)`.

- When the gaps sum to at most the amount, each class gets its gap and the
  remainder is split by target weight.
- Otherwise the amount is split in proportion to the gaps.

Worked example: targets `equity` 50, `fund` 30, `commodity` 20; holdings 700 in
`equity` and 300 in `fund`; amount 200, so `T = 1200`. The targets are worth
600, 360 and 240, so the gaps are 0, 60 and 240. They sum to 300, above the
amount, so the 200 is split in proportion: `fund` 40, `commodity` 160.

## Out of scope

- Sell suggestions.
- Per-instrument and per-sector targets.
- Tax-aware rebalancing.
- Scheduled saving plans.
- A history of targets.
- Per-class currencies in the planner.
