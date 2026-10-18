# ADR 0031: Dividend income forecast

Status: accepted (2026-10-18).

## Context

The Temettü tab shows what a portfolio received (the timeline), what is
expected now (the suggestions to book) and what is upcoming (ex-dates in the
`dividends` table, a few weeks out), all from [ADR 0009](0009-dividends.md). It
shows nothing about the year ahead, and "how much dividend income will this
portfolio pay me over the next twelve months, and when" is the question a
dividend-minded user opens the tab for.

The forecast could come from the `dividends` table, but that table is thin: BIST
rows exist only from the instance's first sync in the calendar year (ADR 0009),
and TEFAS, crypto and gold have no source. The portfolio's own recorded
`dividend` rows are what was actually paid, for every market, and need no fetch.
So the forecast is one pure function over rows the app already holds and one
route; the planned ex-dates of the `dividends` table are laid over it.

## Decision

### The window

The forecast reads every `dividend` transaction of the portfolio that has an
instrument and is dated from the first day of the month eleven months before the
current one through today: the trailing twelve calendar months, the current one
included. A row's projected month is its month plus twelve, so the forecast
covers the twelve months starting next month.

### Per share and scaling

A row's per-share amount is its `amount` over the quantity held at the start of
its date, the holdings state after the rows dated before it. A row with no
holding then is skipped. A split of the instrument dated after the row's date
and on or before today divides the per-share amount by its ratio, and a bonus
issue booked as a zero-price buy divides it by the growth that buy adds to the
holding, so the projected gross stays the recorded amount when the holding did
not change otherwise ([ADR 0008](0008-splits.md)). The projected gross is the per-share
amount times the quantity held now, after every row dated on or before today,
rounded to 2 decimals. A current quantity of zero projects nothing. All
arithmetic is `decimal` ([PRINCIPLES](../PRINCIPLES.md) #3).

Stopaj is the market's rate from `stopaj_rates`, applied as the expected list
applies it (ADR 0009). The recorded `fee` of the source row is not reused: the
rate may have changed, and the forecast shows what a payment today would net.

### Planned rows win

The rows `dividend.Upcoming` lists, ex-dates after today in the `dividends`
table at today's quantity, that fall inside the twelve forecast months are
included as planned. A planned row replaces the projected row of the same
instrument in the same month.

### Route

`GET /api/portfolios/{id}/dividends/forecast`. It takes a numeric portfolio id
only; the `all` scope is a 404, like the timeline ([ADR 0014](0014-all-portfolios-view.md)).

The response has:

- `currency`: the reporting currency.
- `months`: exactly twelve, next month first, each with `period` and `net` in
  the reporting currency, zero-filled.
- `rows`: each projected or planned payment, with the instrument, `period`, a
  planned flag, the date for a planned one, quantity, per share, gross, stopaj
  and net in the instrument's currency, and `net_reporting`, null when there is
  no rate.
- `missing_rates`: the currencies with no rate.

### Reporting figures at today's rate

`net` of a month and `net_reporting` of a row convert at the latest `fx_rates`
rate on or before today, since the payments have not happened and have no rate
of their own. This is the rate of the Özet stats, not the own-date rate of the
timeline (ADR 0009). A row with no rate has a null `net_reporting`, is left out
of the month totals, and its currency is listed in `missing_rates`.

### Yields are computed in the frontend

The page already loads the summary (ADR 0016), so the route returns no yield.
The portfolio's forward yield is the twelve months' net over the summary's
`positions_value`. A position's is its rows' `net_reporting` over its
`market_value_reporting`. The received yield on cost stays as it is.

### Where it shows

The Temettü tab gets two KPI cells, the expected twelve-month net and the
forward yield on value, and a third option in the received chart's switch that
draws the forecast months. The upcoming card becomes the forecast calendar,
with projected rows marked "tahmini" and planned rows "planlanan". The
by-instrument table gets an expected annual column. The position page's Temettü
cell gets a sub-line with the instrument's expected annual net and forward
yield.

### Recorded simplifications

- **A row dated on the payment date scales by the quantity on that date.**
  Midas imports use the payment date (ADR 0009), so a quantity change between
  ex-date and payment date shifts the per-share figure.
- **A dividend recorded after a full sell is skipped**, since no quantity was
  held at the start of its date.
- **Last year's payment is the forecast.** A dividend that was cut, raised, or
  paid twice or not at all last year is projected as it was; only a planned row
  corrects it.

## Out of scope

- Dividend growth rates.
- A forecast beyond twelve months.
- Deposit interest in the forecast.
- The `all` scope.
- Instruments that never paid and have no planned row.
- Special dividend detection.
