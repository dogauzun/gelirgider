# UI v2: return series

Status: approved by the owner on 2026-10-05 (brainstorm of the same day).
Visual reference: [`2026-10-05-ui-v2-return-series-mockup.html`](2026-10-05-ui-v2-return-series-mockup.html).
Parent decisions: [ADR 0023](../../adr/0023-ui-design-system-v2.md) for the
UI, [ADR 0011](../../adr/0011-portfolio-returns.md) for how returns are
computed, [ADR 0012](../../adr/0012-benchmarks.md) for benchmarks. This is
sub-project 3c. It builds on the core pages
([spec](2026-10-05-ui-v2-core-pages-design.md)): the return card and the tab
row.

## Why

- The owner's first question on Özet is "How am I doing?" against a
  yardstick. Today's chart draws money: a deposit bends the line upward
  although nothing was earned, and the benchmark is a shadow value, not a
  percentage, so the two cannot be read against each other at a glance.
- With the cost line shown, the y axis is pulled far down and the value
  line goes flat at the top (a debt recorded on GLG-910).
- There is no view of returns per month or year, and none of how far the
  portfolio has fallen from its peak.

All three need the same thing: the return as a daily series, net of
deposits and withdrawals. The backend already chains those daily factors to
produce the range's time-weighted return; it only returns the end result.

## Owner's decisions

| Question                   | Decision                                                                                                                       |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| Chart mode                 | The return card's chart gets a "% \| ₺" switch and opens in percent. The last choice is remembered.                            |
| Method and the % line      | The headline keeps the chosen method. The percent line is always time-weighted and its legend says so when the method differs. |
| Periodic returns, drawdown | On a new "Performans" tab next to Özet, also on Tüm portföyler.                                                                |
| Periodic view              | A heatmap of months by year, with a switch to bars for the last 24 months.                                                     |

## Scope

- Backend: a daily cumulative return on the history response, for the
  portfolio and for the benchmark; a new endpoint with the returns of every
  calendar month and year.
- Frontend: percent mode on the return card; the Performans tab with
  periodic returns and drawdown.

### Out of scope

- Profit and loss per position over a chosen range.
- A percent mode on the position page's charts.
- Risk figures beyond drawdown (volatility, Sharpe and the like).
- Any schema change. Nothing new is stored.

## Backend

### Daily return on the history response

`GET /api/portfolios/{id}/history` gains two fields per point:

```
{ "date": "2026-09-30", …, "twr": "0.066012", "benchmark_twr": "0.082140" }
```

- `twr` is the cumulative time-weighted return from the range's first day
  to that day: the running product of the daily factors ADR 0011 already
  defines, minus one, as a fraction string with 6 decimals. The first
  point is `"0.000000"`.
- A day that ADR 0011 skips (the previous day's value is not positive)
  repeats the previous day's `twr`.
- When the range's `returns.twr` is `null` (no positive base, negative
  cash on any day, a negative factor), `twr` is `null` on every point. A
  partly defined series would invite a chart that looks valid.
- The last point's `twr` equals `returns.twr`, character for character.
- `benchmark_twr` is the same for the benchmark's shadow series, `null`
  without a benchmark, where the shadow has no value, or when the
  benchmark's own `twr` is `null`.
- The math stays in float64, as ADR 0011 decided for ratios.

### Periodic returns

`GET /api/portfolios/{id}/returns/periods?benchmark=<code>` (a numeric id
or `all`):

```
{
  "currency": "TRY",
  "months": [
    { "period": "2026-09", "from": "2026-09-01", "to": "2026-09-30",
      "partial": false,
      "returns": { "simple": "0.004100", "twr": "0.004000", "mwr": "0.004050" },
      "benchmark": { "simple": "…", "twr": "…", "mwr": "…" } }
  ],
  "years": [ { "period": "2026", … } ]
}
```

- One bucket per calendar month and per calendar year, from the first
  transaction (at most 3660 days back, as on the history endpoint) to
  today, oldest first, with no gaps. The first and the current bucket are
  `partial`.
- A bucket's returns are what the history endpoint returns for the same
  range: the same function, the same flow rules, the same `null` rules. A
  year's numbers equal the chart header's numbers when that year's preset
  is chosen. A test holds the two endpoints to that.
- A bucket's `from` is the period's first day and its `to` the period's
  last day (the range the chart header's calendar preset sends), clamped
  to the first transaction and to today. The history takes the first day's
  value as the base, so the return of each period's first day, from the
  previous period's last value to its own, is in no bucket: months do not
  chain into their year, and years do not chain into the whole history
  ([ADR 0011](../../adr/0011-portfolio-returns.md), note of 2026-10-05,
  GLG-953). A year's cell is the year bucket, never a product of its
  months.
- `benchmark` is `null` without the parameter, or when the benchmark has
  no level on some day of the bucket; the parameter is validated as on the
  history endpoint.
- Computed in one sweep over the history, not one sweep per bucket.
- Filtered by `user_id` like every query; swagger annotations added.

## Frontend

### Percent mode on the return card

- A "% | ₺" switch in the return card. It opens in percent; the choice is
  kept in `localStorage`, as the benchmark and the method are.
- **Percent:** the portfolio's `twr` and the benchmark's `benchmark_twr` as
  two lines from 0%. A solid zero line; percent ticks; no area fill and no
  cost line. The tooltip shows both percentages and their difference. The
  legend toggle for the benchmark still works.
- **Label:** when the chosen method is not time-weighted, the portfolio's
  legend entry reads "Portföy (zaman ağırlıklı)" and its tooltip explains
  in one sentence why the line can end at a different number than the
  headline.
- **₺:** today's chart, unchanged.
- When `twr` is `null` for the range, the card shows the value chart, the
  switch is disabled, and the existing "return undefined" explanation says
  why.
- `LineChart` gets one optional prop, `unit: 'money' | 'percent'`. In
  percent it formats ticks and tooltip values as percentages from
  fractions, and draws the zero line. Everything else it does stays.
- Drag to zoom, the range in the URL and the attention card are not
  affected.

### Performans tab

- A tab "Performans" between Özet and İşlemler, at
  `/portfolios/:id/performance`, for a portfolio and for `all`. Because
  Tüm portföyler now has two tabs, its tab row comes back (the core pages
  removed it when it held a single tab).
- The page has the benchmark and method selects (the same stored choices as
  Özet) and two cards.

**Dönemsel getiri**

- A table: one row per year, newest first; twelve month columns and a
  year column. A cell shows the chosen method's return as a percentage with
  one decimal.
- A cell's background is the gain or loss colour in five steps of strength
  by the size of the return (steps at 1, 2, 4, 8 percent and above); the
  number is always printed, so colour is never the only cue. A month
  without a value shows a middle dot; a `null` return shows a dash.
- A partial period carries a mark and says so in its `title`, with the
  period's dates and the exact percentage.
- "Portföy | fark": with a benchmark chosen, the second view shows each
  cell's difference to the benchmark.
- "Isı haritası | Çubuk": the bar view draws the last 24 months as grouped
  bars, portfolio and benchmark, with the shared tooltip card. It is a
  Recharts chart in the charts chunk.
- It is a real table with row and column headers. Below `md` it scrolls
  sideways inside its card with the year column fixed; this is the one
  place where a card scrolls sideways.

**Zirveden düşüş**

- From the all-time history's `twr`: for each day, how far the return index
  (1 + `twr`) is below its highest value so far, as a percentage. Computed
  in the browser by a pure function from the series the API returns.
- An area chart in the loss colour, zero at the top.
- Two figures in the card's header: the largest drawdown with its peak and
  trough dates, and the current one.
- When `twr` is `null`, the card says the return cannot be computed, with
  the same explanation the header uses.

## Docs that must change

| Document                               | Change                                                                                                             |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `docs/adr/0011-portfolio-returns.md`   | Dated notes: the daily `twr` on history points and its `null` rule; the periods endpoint; the drawdown definition. |
| `docs/adr/0012-benchmarks.md`          | Dated note: `benchmark_twr` per point.                                                                             |
| `docs/adr/0023-ui-design-system-v2.md` | Dated notes: percent mode and the `unit` prop; the Performans tab; the heatmap's colour steps.                     |
| `docs/adr/0014-all-portfolios-view.md` | Dated note: the view has a Performans tab.                                                                         |
| `docs/ROADMAP.md`, `docs/HISTORY.md`   | Phase 28 in progress with the first ticket; moved to HISTORY with the last.                                        |

## Delivery

Epic "Phase 28 - UI v2: return series". One ticket, one PR, one agent.

| #   | Ticket                                                 | After                                    |
| --- | ------------------------------------------------------ | ---------------------------------------- |
| 1   | Docs to `main`; backend: daily `twr` on history points |                                          |
| 2   | Backend: periodic returns endpoint                     |                                          |
| 3   | Percent mode on the return card                        | 1, and the return card (GLG-934)         |
| 4   | Performans tab with the periodic returns heatmap       | 2, and the core pages' tab row (GLG-935) |
| 5   | Periodic returns as bars                               | 4                                        |
| 6   | Drawdown card                                          | 1, 4                                     |
| 7   | Docs: move Phase 28 to HISTORY                         | all                                      |

Tickets 1 and 2 are backend and can start before the core pages land.
Ticket 3 touches `charts/LineChart.tsx`; it runs after the chart additions'
tickets on that file (GLG-946, GLG-949) or before them, never beside them.

## Testing

- Backend: `twr` of the first point is zero; the last equals
  `returns.twr`; a deposit on a day does not move `twr`; a skipped day
  repeats the previous value; a range whose `returns.twr` is `null` has
  `null` on every point; `benchmark_twr` likewise. Periods: a year's and a
  month's returns equal the history endpoint's for the same range; the
  first and current buckets are partial; another user's portfolio is not
  found; an unknown benchmark is a 400.
- Frontend: the percent formatting of ticks and tooltip; the legend label
  by method; the fallback when `twr` is `null`; the stored mode; the
  heatmap's cells, steps, partial marks and the difference view; the
  drawdown function on a rising series (all zero), a fall and recovery, a
  new low at the end, and a `null` series.
- Browser checks in headless Chrome over the DevTools protocol at 1280 px
  and 375 px, light and dark, with synthetic data that includes a deposit
  in the range.
- `make lint`, `make test`, `make build-frontend` on every ticket;
  `make swagger` on the backend tickets.

## Risks

- **Two numbers for one period.** The headline can be money-weighted while
  the line is time-weighted. The owner chose to label the line rather than
  force the method; the label and its tooltip must be plain.
- **Cost of the periods endpoint.** It sweeps the whole history. It reuses
  the history sweep and is fetched only on the Performans tab.
- **`null` returns.** A portfolio that records buys without the deposits
  that paid for them has no defined return (ADR 0011). Every new view has
  an explicit state for that instead of a broken chart.

## Done when

- The return card opens with the portfolio and the yardstick as two percent
  lines from 0%, and a deposit inside the range does not bend the line.
- The Performans tab shows returns per month and year, the difference to
  the benchmark, the bar view and the drawdown, for a portfolio and for
  Tüm portföyler.
- A year's cell equals the chart header's number for that year's preset.
- A portfolio without a defined return shows an explanation on each new
  view.
- ADR 0011, 0012, 0014 and 0023 describe the additions.
- `make lint`, `make test`, `make build-frontend`, `make swagger` pass on
  `main`.
