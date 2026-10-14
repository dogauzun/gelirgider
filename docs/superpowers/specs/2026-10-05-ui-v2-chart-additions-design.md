# UI v2: chart additions

Status: approved by the owner on 2026-10-05 (brainstorm of the same day).
Visual reference: [`2026-10-05-ui-v2-chart-additions-mockup.html`](2026-10-05-ui-v2-chart-additions-mockup.html).
Parent decisions: [ADR 0023](../../adr/0023-ui-design-system-v2.md). This is
sub-project 3b: the frontend-only chart additions. It builds on the charts
foundation (Phase 25) and on the core pages
([spec](2026-10-05-ui-v2-core-pages-design.md), Phase 26), which decide
where the charts sit.

## Owner's decisions

| Question         | Decision                                                                                                   |
| ---------------- | ---------------------------------------------------------------------------------------------------------- |
| Allocation       | A donut replaces the stacked bar, with the total in the middle and a legend with amounts and shares.       |
| P/L per position | A new "Katkı" card with bars, switching between Toplam (unrealized since purchase) and Bugün (day change). |
| P/L for a range  | Not here. It needs backend work and belongs to the return-series piece (3c).                               |
| Drag to zoom     | A drag sets the chart's range, like filling in "Özel". Desktop only.                                       |

## Scope

- Allocation donut on Özet and Tüm portföyler.
- "Katkı" card on Özet and Tüm portföyler.
- Drag to zoom on the line chart (Özet, Tüm portföyler, position page).
- Two chart debts from the Phase 25 close-out: more date labels on wide
  charts, and a legend whose height does not jump when the chart loads.

### Out of scope

- Any backend, API or schema change.
- P/L per position over a chosen range, percent mode, periodic returns,
  drawdown (3c).
- Zoom on a touch screen.
- The bar chart's tooltip placement (GLG-924).

## Özet's lower rows

The core pages put Dağılım and İstatistikler side by side under the
positions table. With this piece the rows become:

1. **Dağılım** (donut) and **Katkı**, side by side.
2. **İstatistikler** as a full-width strip of five cells.

Below `md` they stack in that order.

## Allocation donut

- The grouping rules do not change: Sembol, Pazar or Para birimi; the
  largest five keep their own colour (`--color-cat-1` to `5`), the rest fold
  into "Diğer" on `--color-cat-6`, cash has its own muted colour; a position
  without a reporting value is left out and the note about missing rates
  stays. The rules move into a pure function, `allocationRows`, so the donut
  and the legend share one result.
- **Donut:** about 150 px, ring 16 px thick, slices in the legend's order
  starting at twelve o'clock, a 1 px gap between slices. In the middle the
  label "Toplam" and the compact total (masked in privacy mode). Drawn by
  Recharts' `PieChart` inside `components/charts/`, loaded through
  `LazyChart.tsx` like the other charts. The default animation stays, so
  reduced motion is honoured.
- **Legend:** our own HTML list beside the donut: swatch, name, amount,
  share. "Diğer" carries its count ("Diğer (10)") and lists its members in
  its `title`.
- **Hover:** hovering a slice or a legend row highlights the pair (the other
  slices dim); the slice's tooltip is the shared tooltip card with name,
  amount and share.
- **Accessibility:** the legend is the accessible content, a list with all
  the numbers. The donut is hidden from assistive technology
  (`aria-hidden`), is not a focus stop, and has no keyboard layer.
- **Placeholder:** a ring-shaped skeleton of the donut's size while the
  chunk loads; if the chunk fails the legend still renders, without the
  donut.
- On a phone the legend goes under the donut.

## Katkı card

- One bar per position on a shared zero line: profit to the right in the
  gain colour, loss to the left in the loss colour, with the signed amount
  at the right edge of the row. The longest bar spans half the track.
- A switch: **Toplam** (`unrealized_pl_reporting`) and **Bugün**
  (`day_change_reporting`). The choice is component state.
- It shows the four largest gains and the four largest losses, by amount,
  best first. "Tümünü göster" expands to every open position; with eight or
  fewer positions all are shown and there is no button.
- A position without the chosen value (no price, no FX rate, no day change)
  is left out; if that leaves nothing, the card says so in one line.
- The symbol links to the position page. On Tüm portföyler the rows are
  plain text, as its positions table is.
- The bars are plain HTML and CSS, not Recharts: the card must not wait for
  the charts chunk, and a list with numbers is accessible as it is. Each row
  is a list item whose text carries symbol and amount.
- Amounts are masked in privacy mode; the bars keep their lengths.
- The rows come from a pure function, `contributionRows`.

## Drag to zoom

- On the line chart, with a mouse: press on the plot, drag sideways, release.
  While dragging, the span is shaded in the accent colour with a line at
  each end. On release the span's first and last dates become the range
  through `useRange` (`?from=…&to=…`), so the headline return, the
  benchmark, the notices and the URL follow, exactly as with "Özel".
- A drag shorter than two data points, or released outside the plot, does
  nothing. Escape during a drag cancels it. A plain click still does nothing
  new; hover and the tooltip work as before when no button is down.
- While a zoomed range is active, the range picker shows "Özel" with a
  clear button that returns to the range that was active before the drag.
- Touch and pen: unchanged. A horizontal drag keeps scrubbing the tooltip.
- `LineChart` gets one optional prop, `onZoom(from, to)`. Without it the
  chart behaves as today. This is the first prop added since the charts
  foundation; the ADR note records it.
- Keyboard users reach the same result through "Özel" in the range picker;
  the chart's summary sentence does not change.

## Chart polish

- **Date labels by width.** The line chart labels one date per about 120 px
  of plot width, at least three and at most seven, evenly spaced over the
  series, always including the first and the last. The year rule
  (GLG-809) stays. The choice is a pure function, `dateTicks`.
- **Stable legend height.** The line chart's legend reserves two lines
  below `sm` and one line from `sm` up, and the placeholder in
  `LazyChart.tsx` reserves the same, so the card does not grow when the
  chunk arrives.

## Docs that must change

| Document                                    | Change                                                                                                             |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `docs/adr/0023-ui-design-system-v2.md`      | Dated notes: the donut and what replaces the bar; the Katkı card; drag to zoom and the `onZoom` prop; date labels. |
| `docs/adr/0021-product-and-ui-decisions.md` | Dated note where it describes the allocation bar as current.                                                       |
| `docs/ROADMAP.md`, `docs/HISTORY.md`        | Phase 27 in progress with the first ticket; moved to HISTORY with the last.                                        |

## Delivery

Epic "Phase 27 - UI v2: chart additions". One ticket, one PR, one agent.

| #   | Ticket                                              | After                          |
| --- | --------------------------------------------------- | ------------------------------ |
| 1   | Docs to `main`; date labels by width; stable legend |                                |
| 2   | Allocation donut                                    | 1, and Özet layout A (GLG-934) |
| 3   | Katkı card                                          | 2                              |
| 4   | Drag to zoom                                        | 1, and Özet layout A (GLG-934) |
| 5   | Docs: move Phase 27 to HISTORY                      | all                            |

Tickets 2 and 3 both change Özet's lower rows, so they run one after the
other. Ticket 4 touches the line chart and the return card and can run
beside ticket 2.

## Testing

- `allocationRows`, `contributionRows` and `dateTicks` get unit tests,
  including: more than six allocation rows; only cash; a position without a
  reporting value; nine positions with ties in the contribution order; one
  date; a 300 px and a 1,200 px plot.
- Component tests assert behaviour, with the size helper for Recharts: the
  legend's rows and numbers; the donut hidden from assistive technology; the
  Katkı switch and "Tümünü göster"; a mouse drag calls `onZoom` with the two
  dates, a short drag and a touch drag do not.
- Browser checks in headless Chrome over the DevTools protocol at 1280 px
  and 375 px, light and dark, with synthetic data: a real mouse drag zooms
  and the clear button returns; no sideways scroll; the card heights before
  and after the charts chunk loads.
- `make lint`, `make test`, `make build-frontend` on every ticket.

## Risks

- **Recharts and drag.** The chart's own pointer handling drives the
  tooltip; the drag must not break touch scrubbing or vertical page scroll
  on a phone. The zoom ticket carries the same stop rule as the charts
  foundation: if it cannot be done without a regression there, it stops and
  reports.
- **A donut reads shares less precisely than a bar.** The legend carries the
  exact numbers, so nothing is lost.

## Done when

- Özet and Tüm portföyler show the donut with its legend and the Katkı card;
  the stacked bar is gone.
- A mouse drag on any line chart sets the range and the clear button undoes
  it; touch behaviour is unchanged.
- A 900 px wide line chart shows more than three date labels; the chart card
  does not change height when the chunk loads at 375 px with a benchmark.
- No document describes the allocation bar as current.
- `make lint`, `make test`, `make build-frontend` pass on `main`.
