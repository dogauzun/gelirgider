# ADR 0030: Saving plans

Status: accepted (2026-10-18).

## Context

Most Turkish retail investors put money in every month: a fixed amount of one
fund or share, on a chosen day. The app has nothing recurring, so a user types
the same buy twelve times a year. The contribution planner of
[ADR 0028](0028-target-allocation.md) answers a one-off question, and the
projection of [ADR 0029](0029-goals-and-projection.md) measures the monthly
flows the user already made; neither records the standing intention.

The app already has the pattern for this: it suggests and does not write
([ADR 0009](0009-dividends.md)), and the user books each suggestion with one
click. A saving plan is a new source of suggestions. The booking is an ordinary
transaction.

## Decision

### One table, and `next_date` is the whole state

Table `saving_plans` holds `user_id`, `portfolio_id`, a nullable
`instrument_id`, `currency`, `amount` as exact decimal text, `day_of_month`
(1 to 31), `start_date`, a nullable `end_date`, `next_date`, `fund` (0 or 1),
`note` and `created_at` (UTC). The dates are market dates (`Europe/Istanbul`).
A NULL `instrument_id` is a cash deposit plan. Like the other per-user tables,
every query filters by `user_id`.

`next_date` is the oldest occurrence not yet booked or skipped. There is no
occurrence table and no link from a transaction to its plan: a booked row is an
ordinary transaction that carries the plan's note.

- A new plan's `next_date` is the first occurrence on or after `start_date`.
- Booking or skipping moves `next_date` to the following occurrence.
- A plan is due when `next_date` is today or earlier and the plan has not ended.
  It has ended when `end_date` is set and `next_date` is after it.
- A plan that has ended stays stored and is never due.

### The occurrence rule

Plans are monthly only. An occurrence falls on `day_of_month` of each month; a
day beyond the month's length falls on the month's last day, so day 31 gives
28 or 29 February and 30 April. Two payments a month are two plans.

A plan that nobody opened for months is not caught up in one step: each missed
occurrence stays due in turn, oldest first, and each is booked or skipped on its
own date.

### Booking a buy plan

A buy plan's amount is in the instrument's currency, since a buy must match it.
Booking creates one `buy` dated `next_date`, with fee 0:

- The close is the latest close on or before `next_date`, a manual price over a
  fetched one, as `holdings.LatestCloses` reads it.
- `quantity = amount / close`, rounded down to the market's scale: BIST 0,
  US 4, TEFAS 6, CRYPTO 8, GOLD 4 decimals.
- The booked `amount` is `quantity × close`, rounded to 2 decimals.

All arithmetic is `decimal` ([PRINCIPLES](../PRINCIPLES.md) #3). The proposal
the user sees before clicking is this same computation.

Two cases answer 409 and book nothing: the instrument has no close on or before
`next_date`, and the quantity rounds down to zero. The user books by hand and
skips the occurrence. There is no explicit price or fee on booking in v1.

### Deposit plans and the fund flag

A deposit plan books a `deposit` of its amount in the plan's currency, dated
`next_date`.

The `fund` flag on a buy plan makes the booking first write a `deposit` of the
booked amount, then the `buy`, in one SQL transaction with the deposit on the
lower id. This is the fund flag of the term deposits
([ADR 0024](0024-term-deposits.md)), for the user who does not record cash
movements. The flag is off by default, so cash the user already recorded is not
counted twice. Booking and the move of `next_date` are in the same transaction
as the rows, so a failure leaves the plan due.

### Routes

- `POST` and `GET /api/portfolios/{id}/plans`
- `GET /api/portfolios/{id}/plans/due`
- `PUT` and `DELETE /api/plans/{id}`
- `POST /api/plans/{id}/book`
- `POST /api/plans/{id}/skip`

`{id}` is a numeric portfolio id: plans have no `all` scope. Only the owner
reads or writes a plan. The all-portfolios page queries each portfolio's due
list, as it does for due deposits ([ADR 0014](0014-all-portfolios-view.md)).

### Edit

A plan's amount, day, end date, fund flag and note can change. Its instrument,
currency and start date are fixed: the user deletes the plan and creates a new
one. After an edit, `next_date` becomes the first occurrence with the new day on
or after the old `next_date`. Moving the day from the 10th to the 20th while
`next_date` is the 10th of March gives the 20th of March; moving it from the
20th to the 10th gives the 10th of April.

### Dikkat and the Planlar tab

The Dikkat card gets a group "Planlı alımlar" with one item per due plan. An
item shows the proposed quantity and close, or that there is no close, with
Kaydet (book) and Atla (skip). These are data items, like the expected
dividends and due deposits: the attention rules of
[ADR 0023](0023-ui-design-system-v2.md) never hide or reorder them.

A Planlar tab lists the portfolio's plans with their next date. From it the
user creates, edits and deletes a plan and books or skips a due one.

## Out of scope

- Weekly or quarterly frequency.
- An explicit price or fee on booking.
- Booking without a click, and "book all".
- Pausing a plan.
- Plans on the `all` scope.
- Linking plans to the goal projection or to the contribution planner.
