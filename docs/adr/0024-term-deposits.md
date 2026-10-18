# ADR 0024: Term deposits valued by a computed price source

Status: accepted (2026-10-18).

## Context

Many Turkish savers hold a vadeli mevduat: a bank deposit with a fixed term and
a fixed rate, paid out at maturity net of stopaj (withholding tax). The app
could only record it as cash, which earns nothing in the charts, or as a
hand-typed series of manual prices.

The holdings engine ([ADR 0019](0019-core-engine-and-auth.md)) values a
position as quantity times the latest close of an instrument, and the history
sweep ([ADR 0011](0011-portfolio-returns.md)) reads closes the same way. A
deposit has no market and no quote, but its value on any day follows from its
terms alone.

## Decision

### A deposit is a hidden per-user instrument in market `DEPOSIT`

Each deposit is one instrument row in market `DEPOSIT`, with the deposit's
currency (`TRY`, `USD` or `EUR`) and a generated symbol. The user never sees it
as an instrument: it is not searchable and has no instrument page. The
holdings engine, the history sweep, the allocation and the position tables see
an ordinary position, so none of them learns a new asset class. The
instrument's name is `Vadeli mevduat <currency> <maturity>` with the date as
YYYY-MM-DD; the frontend never prints the symbol or that name for a deposit
position but builds a localized label from the currency and the date at the end
of the name (`positionLabel` in `frontend/src/lib/positions.ts`).

A `term_deposits` table holds the terms: principal, rate, stopaj, start date,
maturity date, currency and the link to the instrument. It carries `user_id`.

### The price is computed, not stored

The close of a deposit on a day is computed in Go from the terms. No `prices`
or `manual_prices` rows exist for a deposit. The price source is a function of
the deposit and the date, called wherever a close is read.

Rejected:

- **Stored `manual_prices` rows**, written at open for the whole term or by a
  daily job. They go stale when the terms are edited, need a job to extend, and
  a backfill after any change. A computed close cannot disagree with the terms.
- **A new asset class in the holdings engine**, with its own accrual state.
  Every consumer of positions (history, returns, allocation, import, export)
  would need the new case. The synthetic instrument reuses all of them.

### The formula

Simple interest, actual/365, net of stopaj:

```
close(d) = 1 + rate × days / 365 × (1 − stopaj)
days     = min(d − start, maturity − start)
```

`rate` and `stopaj` are fractions: 0.45 is 45%. `days` is capped at maturity,
so a deposit left unbooked after maturity stays at its final value. There is no
close before the start date. All arithmetic is `decimal`
([PRINCIPLES](../PRINCIPLES.md) #3).

### Booking rows

Opening a deposit books a `buy` on the start date: quantity is the principal,
price 1. The position costs the principal and is worth `principal × close`.

At maturity two kinds of rows are booked:

- a `sell` of the whole quantity at price 1, which returns the principal to
  cash;
- an interest row: a `dividend` with no instrument, the gross interest in
  `amount` and the stopaj in `fee`, both rounded to 2 decimals, as the
  expected-dividend stopaj is ([ADR 0009](0009-dividends.md)). A dividend with
  no instrument is interest in the holdings engine: it adds the net to cash and
  appears in the timeline's `interest`.

Until maturity the position is valued unrounded. The booked net interest is
rounded, so the portfolio total can step by at most one kurus on the maturity
day. That step is accepted.

Only `deposit` and `withdrawal` are external flows for the returns
([ADR 0011](0011-portfolio-returns.md)). The deposit's `buy`, `sell` and
interest rows are not, so accrued interest counts as return, not as money put
in.

### Maturity suggestion and renewal

As for dividends ([ADR 0009](0009-dividends.md)), the app suggests and does not
write. A deposit at or past maturity with no booked `sell` shows a suggestion
with the `sell` and the interest row pre-filled. The user books them with one
click through the deposit routes.

A matured deposit can be renewed: the same request books its maturity rows and
opens a new deposit starting on the maturity date, with the rate, term and
stopaj pre-filled and editable; the principal is the old payout and is not
editable. A deposit is renewed at most once; the
new deposit can be renewed in turn.

Out of scope: an early break (closing before maturity), automatic rollover, a
compound or variable rate. A user who breaks a deposit early deletes it and
books the actual amounts by hand.

### Opening a deposit is an ordinary buy from cash

The `buy` takes the principal out of the portfolio's cash like any buy. The
open request has an optional fund flag: when set, the same request first books
the principal as a cash `deposit` on the start date, so the user need not book
it separately. The flag is off by default, so cash the user already recorded is
not counted twice. Without the flag and without recorded cash, the cash-gap
notice ([ADR 0011](0011-portfolio-returns.md)) offers the missing deposit, as
for any buy.

### The `user_id` rule with a table that has none

`instruments` has no `user_id`, since it holds shared market data
([PRINCIPLES](../PRINCIPLES.md) #2). A deposit instrument is user data, so the
rule holds like this:

- `term_deposits` carries `user_id`. The price source reads a deposit only
  through it, scoped by user, so a user's close never comes from another
  user's terms.
- Every generic route refuses a `DEPOSIT` instrument for everybody, the owner
  included. Instrument search excludes it. Transaction create and update, the
  statement import and the price routes answer as for an unknown instrument.
  The owner changes a deposit only through the deposit routes.
- Manual prices are refused for `DEPOSIT`.
- Deleting an account or a portfolio deletes the user's deposit instrument
  rows together with the deposit rows, because nothing else references them.

### Import, export and admin

- A CSV row naming market `DEPOSIT` is a parse error: a deposit is opened
  through the deposit routes only.
- The CSV export writes the deposit's `buy` and `sell` rows as they are. They
  do not round-trip through import.
- The admin instruments route refuses the market.
