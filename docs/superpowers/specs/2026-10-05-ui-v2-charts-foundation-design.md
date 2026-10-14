# UI v2: charts foundation

Status: approved by the owner on 2026-10-05 (brainstorm of the same day).
Visual reference: [`2026-10-05-ui-v2-charts-mockup.html`](2026-10-05-ui-v2-charts-mockup.html).
Parent decisions: [ADR 0023](../../adr/0023-ui-design-system-v2.md) and the
spec
[`2026-10-04-ui-v2-design-language-and-shell-design.md`](2026-10-04-ui-v2-design-language-and-shell-design.md).

## Why

Phase 24 gave the app the "soft depth" skin and the sidebar shell. The charts
were left as they were: hand-drawn SVG, lines only, no motion. The owner
wants four things from the charts sub-project, and chose all four:

1. A polished look that matches the approved mockup.
2. Better interaction.
3. New chart types.
4. Less custom drawing code.

That is too much for one spec. The owner agreed to split sub-project 3 of
ADR 0023 into three pieces, each with its own spec, plan and epic:

- **3a, charts foundation (this spec):** Recharts arrives; the line chart and
  the bar chart are rebuilt on it in the new look, with animation and a
  legend that hides and shows series. Frontend only.
- **3b, frontend-only additions:** allocation donut, profit and loss per
  position, drag to zoom. The data is already in the API.
- **3c, return-index series:** one backend addition, a daily cumulative
  return net of deposits, that feeds three things: a value or percent mode on
  the value chart, periodic returns, and drawdown.

## Owner's decisions

| Question             | Decision                                                                                                  |
| -------------------- | --------------------------------------------------------------------------------------------------------- |
| Chart look           | Style A, "precise": straight segments, soft gradient fill, dashed horizontal grid, white tooltip card.    |
| Smoothed curve       | Rejected. A smoothed line rounds off sharp daily moves, which misleads on a money chart.                  |
| How deep Recharts go | Thin wrappers. Recharts draws; our props, tick logic, privacy masking and marker shapes stay.             |
| Library defaults     | Rejected: ticks, tooltip and legend by Recharts defaults would bring back fixed bugs (GLG-775, 807, 809). |
| Keyboard             | Left and Right move point by point (Recharts built-in). The Home and End jump is dropped.                 |
| Split                | 3a, then 3b, then 3c.                                                                                     |
| Interactions wanted  | Animated transitions and legend toggle (3a); drag to zoom (3b); value or percent mode (3c).               |
| New charts wanted    | Allocation donut and P/L per position (3b); periodic returns and drawdown (3c).                           |

## Scope

- Add `recharts` (3.x) as a runtime dependency.
- Rebuild `LineChart` and `BarChart` on Recharts in style A. Their props do
  not change, so no page layout changes.
- One shared tooltip card.
- Animation on data change; a legend whose series are buttons.
- Load the charts as a separate chunk.
- Update every document that the change makes wrong.

### Out of scope

- `Allocation.tsx`. It is not SVG and stays as it is until 3b.
- Any new chart, drag to zoom, percent mode (3b, 3c).
- Page layouts, chart card headers, the range switch (sub-project 4).
- Backend, API and schema. Nothing changes there.
- `deploy/nginx.conf`. The production CSP stays as it is.

## Structure

### Files

| File                                              | Role                                                                                                                                                                                                                   |
| ------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `frontend/src/lib/chart.ts`                       | Pure helpers and types moved out of the chart files: `niceTicks`, `axisTicks`, `formatTicks`, `rangeDelta`, the value and tick formatters that mask in privacy mode, and the types `ChartPoint`, `ChartMarker`, `Bar`. |
| `frontend/src/components/charts/LineChart.tsx`    | The value chart on Recharts. Same props as today.                                                                                                                                                                      |
| `frontend/src/components/charts/BarChart.tsx`     | The bar chart on Recharts. Same props as today.                                                                                                                                                                        |
| `frontend/src/components/charts/ChartTooltip.tsx` | The tooltip card both charts use.                                                                                                                                                                                      |
| `frontend/src/components/LazyChart.tsx`           | The only entry pages use: `LineChart` and `BarChart` loaded with `React.lazy`, a placeholder and a failure state.                                                                                                      |
| `frontend/src/test/chartSize.ts`                  | Test helper that gives a chart a size in jsdom (Recharts reads `getBoundingClientRect` and `ResizeObserver`).                                                                                                          |

`frontend/src/components/LineChart.tsx` and `BarChart.tsx` are deleted when
their replacements land.

Only files under `components/charts/` import `recharts`, and only
`LazyChart.tsx` imports from `components/charts/` (with a dynamic import).
`conventions.test.ts` enforces both, so the separate chunk cannot break
unnoticed.

### What Recharts owns, and what stays ours

Recharts owns the drawing: scales, paths, the area, the grid, bars, the
cursor, tooltip placement, animation, resizing and the keyboard layer.

These stay ours, because each one fixed a bug or carries a product rule:

- **Money is decimal.** Recharts gets `Number()` values for positions only.
  Every amount the chart prints (tooltip, axis, summary) is formatted from
  the exact decimal string, as today.
- **Ticks.** `axisTicks` picks the y ticks and the domain (GLG-775);
  `formatTicks` keeps the labels distinct (`1,00 Mn`, `1,005 Mn`). They are
  passed to Recharts through `ticks`, `domain` and `tickFormatter`.
- **Date labels.** The same dates are labelled as today, with the year when
  the range crosses calendar years (GLG-809).
- **Privacy.** Every printed amount goes through the masking formatters
  (GLG-564).
- **Markers.** Buy, sell, dividend and split keep their own shapes, so the
  kinds tell apart without colour (GLG-807). A marker is drawn only on a date
  that is in the series.
- **Gaps.** A null value, cost or benchmark is a gap in that line, never a
  line to zero (GLG-589). The benchmark breaks at gaps.
- **The legend.** Our own HTML below the plot, not Recharts' `Legend`.
- **The range delta.** `rangeDelta` is unchanged and moves to `lib/chart.ts`.

## Look (style A)

All colours are the existing tokens, read as CSS variables, so dark mode
follows without extra code.

| Element          | Drawing                                                                                                                                     |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Value series     | `--color-accent` line, 1.75 px, straight segments, round joins. Area fill: vertical gradient from the accent at 16% opacity to transparent. |
| Cost series      | `--color-faint`, 1.25 px, dash `4 3`. No fill.                                                                                              |
| Benchmark series | `--color-plum`, 1.25 px, dash `1 3`, round caps. No fill.                                                                                   |
| Grid             | Horizontal lines only, `--color-line`, dash `3 4`. No vertical lines, no axis lines.                                                        |
| Axis labels      | Inter 11 px, `--color-faint`. Y labels on the left, right-aligned; x labels under the plot.                                                 |
| Cursor           | Vertical line, `--color-faint`, dash `2 3`, and a 4.5 px dot on the value line: `--color-surface` fill, accent ring.                        |
| Markers          | Today's shapes and colours, with a `--color-surface` outline so they read on the fill.                                                      |
| Bars             | `--color-accent`, top corners rounded 3 px. A zero bar is a 1 px hairline.                                                                  |
| Tooltip          | `bg-surface`, `rounded-ctl`, `shadow-pop`, 12 px text. No border.                                                                           |

The plot height is unchanged: 2:5 of the width with a 200 px floor for the
line chart, and the `height` prop (default 200) for the bar chart.

Each chart instance gives its gradient a unique id (`useId`): the position
page shows two line charts.

## Behaviour

### Tooltip

- Line chart: the date; one row per visible series with a colour swatch and
  the exact amount; one row per marker on that date; the "estimated" note on
  the benchmark row from `estimatedFrom` on.
- Bar chart: the bar's label and its exact amount.
- It stays inside the plot.
- It is announced politely to screen readers when it changes, as today.

### Pointer and touch

- Hover moves the cursor and the tooltip; leaving the plot clears them.
- On a touch screen a horizontal drag scrubs the tooltip and a vertical drag
  still scrolls the page (`touch-action: pan-y`).
- After the finger lifts, the value stays readable. Today it stays for two
  seconds; the exact time may differ with Recharts.

### Keyboard and screen readers

- The chart is one focus stop. Left and Right move the cursor point by point
  (bar by bar on the bar chart).
- Home and End no longer jump to the ends. Recharts has no supported way to
  set the active point from outside.
- The chart's role becomes `application`, the Recharts default, which JAWS
  and NVDA need to pass the arrow keys through. It was `img`.
- The chart keeps its summary sentence (`chart.line.summary`,
  `chart.line.range`, `chart.bars.summary`) as its accessible name.
- The focus ring uses the app's focus style, not the browser default.

### Animation

- Lines, area and bars animate for about 300 ms on first draw and when the
  data changes (range, benchmark, return method).
- Nothing animates on hover.
- No animation when the system asks for reduced motion. Recharts does this by
  default (`isAnimationActive="auto"`); the tickets must not switch it off.

### Legend toggle

- The legend lists the value series, the cost series, the benchmark when
  there is one, and the marker kinds that occur, as today.
- The cost and benchmark entries are buttons with `aria-pressed`. A click
  hides or shows that series. A hidden entry keeps its `text-muted` label,
  struck through, with its swatch at reduced opacity; `text-faint` would fail
  text contrast for the series name (GLG-922).
- A hidden series leaves the tooltip, and the y axis refits to what is
  visible.
- The value series cannot be hidden. Marker entries are not buttons.
- The state lives in the chart instance. It is not saved; a reload shows
  every series again.

### States

- Empty (every value zero or no points): the existing `chart.empty` text, no
  chart.
- One point: one date label and one dot, as today.
- Loading the chunk: a placeholder with the chart's height.
- The chunk fails to load: the chart card shows `chart.error`; the rest of
  the page works. `ErrorBoundary` gains an optional `fallback` prop for
  this, because its default fallback renders a whole page.

## Dependencies

- `recharts` 3.x, pinned by the lock file. The current release is 3.10.1 and
  it supports React 19.
- Allowed by ADR 0023 ("Recharts later"). No other new dependency.
- The first Recharts ticket records the bundle sizes (main chunk and charts
  chunk, raw and gzip) in the ADR 0023 note.

## Verified in the first Recharts ticket, with a stop rule

The ticket that adds Recharts and rebuilds the line chart checks these on a
production build before it opens its PR:

1. The built app runs under the production CSP of `deploy/nginx.conf`
   (`default-src 'self'`) with no violation in the console, as was done for
   Base UI.
2. At 375 px, a vertical drag that starts on the chart scrolls the page.
3. After a touch ends, the tooltip value can still be read.
4. Left and Right move the cursor; the tooltip is announced.
5. With reduced motion set, nothing animates.

If Recharts cannot meet an item of "What stays ours" or items 1 to 3, the
ticket stops and reports to the owner. It does not ship a regression and it
does not change `deploy/nginx.conf`.

## Docs that must change

| Document                                   | What is wrong after this epic                                                                 | Change                                                                                                                                             |
| ------------------------------------------ | --------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/adr/0023-ui-design-system-v2.md`     | Says Recharts arrives later                                                                   | Dated note: Recharts added, the version, the files, the bundle sizes, Home and End dropped, the role change, the three-way split of sub-project 3. |
| `CLAUDE.md`                                | The frontend stack line names Base UI but not Recharts                                        | Add "Recharts for charts".                                                                                                                         |
| `frontend/README.md`                       | May describe the charts as hand-drawn                                                         | Check and fix.                                                                                                                                     |
| The comment above `LineChart`              | "No charting library"                                                                         | Goes with the rewrite.                                                                                                                             |
| `docs/adr/0005`, `0012`, `0021` and others | May describe the charts as hand-drawn SVG as current                                          | Grep for it; add a dated note where a statement becomes false. 0005 and 0021 already carry one.                                                    |
| `docs/ROADMAP.md`                          | Lists sub-project 3 as one unplanned item                                                     | First ticket: Phase 25 under "In progress"; 3b, 3c, 4 and 5 under "Next".                                                                          |
| `docs/HISTORY.md`                          | No Phase 25                                                                                   | Last ticket, after the close-out check.                                                                                                            |
| Cloud routine prompts                      | Nothing: the worker's rule already allows a dependency that a ticket names and an ADR records | No change.                                                                                                                                         |

## Delivery

Epic "Phase 25 - UI v2: charts foundation". One ticket, one PR, one agent.
`main` works after every ticket.

| #   | Ticket                                                            | After   |
| --- | ----------------------------------------------------------------- | ------- |
| 1   | Docs to `main`; move the pure helpers and types to `lib/chart.ts` |         |
| 2   | Recharts and the line chart in style A, with the stop rule        | 1       |
| 3   | Bar chart on Recharts                                             | 2       |
| 4   | Legend toggle                                                     | 2       |
| 5   | Lazy chunk, placeholder, failure state, import convention         | 3 and 4 |
| 6   | Docs: move Phase 25 to HISTORY                                    | 5       |

Tickets 3 and 4 run in parallel: they touch different chart files.

## Testing

- The helper tests (`niceTicks`, `axisTicks`, `formatTicks`, `rangeDelta`)
  move to `lib/chart.test.ts` unchanged.
- Component tests assert behaviour, with the size helper: the series that are
  drawn; the legend; no benchmark without benchmark values; benchmark gaps;
  the "estimated" note; markers only on dates in the series; the summary for
  screen readers; an arrow key moves the tooltip; the empty state; privacy
  masking; the legend toggle; the year in date labels; one bar per entry; the
  zero hairline; compact money ticks.
- Tests that assert pixel geometry (the measured width, the centred middle
  label, label thinning by width) and the touch-hold timer are dropped:
  Recharts owns geometry. Each ticket replaces them with a browser check at
  375 px and 1280 px, light and dark, on synthetic data.
- `make lint`, `make test` and `make build-frontend` pass on every ticket.

## Risks

- **Bundle size.** Recharts is large. The lazy chunk keeps it off pages
  without a chart; Özet still loads it. The sizes are measured and recorded.
- **Touch behaviour.** Recharts' touch handling is the least known part.
  Covered by the stop rule.
- **Tests in jsdom.** Recharts renders nothing without a size. The size
  helper covers it; tests must not assert Recharts' internal class names
  more than needed.
- **Screen readers.** `role="application"` changes how VoiceOver users reach
  the chart (Quick Nav must be off for arrow keys). The summary sentence
  still gives the range and the change without any key.
- **Home and End.** A small loss for keyboard users, accepted by the owner.

## Done when

- `LineChart` and `BarChart` are drawn by Recharts in style A on Özet, the
  position page and Temettü, light and dark, at 375 px and 1280 px.
- Everything under "What stays ours" still holds.
- The cost and benchmark series can be hidden and shown from the legend.
- The charts are a separate chunk, and no file outside `components/charts/`
  imports `recharts`.
- The production CSP is unchanged and not violated.
- No document in the repo says the charts are hand-drawn or that Recharts is
  still to come.
- `make lint`, `make test` and `make build-frontend` pass on `main`.
