# UI v2 Charts Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the line chart and the bar chart on Recharts in the approved "style A" look, with animation and a legend toggle, loaded as a separate chunk, without changing any page layout or chart prop.

**Architecture:** Pure helpers and types move to `lib/chart.ts`. Recharts code lives only in `components/charts/` and draws; our helpers still decide ticks, formats, privacy masking, markers and gaps. Pages reach the charts through one lazy entry, `components/LazyChart.tsx`.

**Tech Stack:** React 19, TypeScript, Vite, Tailwind v4 tokens, Vitest + Testing Library (jsdom), `recharts` 3.x (new).

**Spec:** `docs/superpowers/specs/2026-10-05-ui-v2-charts-foundation-design.md`. Visual reference: `docs/superpowers/specs/2026-10-05-ui-v2-charts-mockup.html`. Executors read both.

## Global Constraints

- One task is one GLG ticket, one PR, one agent, branch `GLG-<n>-short-slug`. Merge order is the task order; tasks 3 and 4 may run in parallel.
- No AI attribution in commits, PRs or files.
- The only new dependency is `recharts` 3.x. Nothing else is added.
- Chart props do not change: `LineChart({ points, benchmarkLabel, estimatedFrom, markers, labels })`, `BarChart({ bars, currency, height })`.
- Money is decimal: `Number()` only for positions on the plot; every printed amount is formatted from the exact string.
- Semantic colour and shadow tokens only (ADR 0023). Colours reach SVG as `var(--color-*)`.
- Every new UI string has a `tr` and an `en` key.
- `deploy/nginx.conf` is not changed. A CSP violation stops the ticket.
- Animation stays on Recharts' default `isAnimationActive="auto"` so reduced motion is honoured; duration 300 ms.
- Browser checks use synthetic data on the agent's own backend and Vite ports and its own host name; never the owner's dev instance, database or `.env`.
- Checks from the repo root, all exit 0: `make lint`, `make test`, `make build-frontend`.
- Recharts prop names in this plan come from the 3.x docs (`responsive`, `accessibilityLayer`, `isAnimationActive`, `Tooltip content`/`cursor`, `Area type="linear"`, `Line connectNulls`/`hide`, `YAxis ticks`/`domain`/`tickFormatter`/`width`, `CartesianGrid vertical`). Confirm each against the installed version before use.

## Review Focus

Inputs the spec implies and a happy-path test would miss. Each has a test in the task named.

1. **Privacy toggled after the chart rendered** (task 2): axis labels and tooltip must mask at once. Recharts may keep tick labels when the formatter's identity does not change.
2. **A series that starts with nulls or is all null** (task 2): no line to zero, the axis ignores the nulls, the legend still names the series only if it has a value.
3. **Negative values** (task 2): negative positions and overdraft are deliberate in this app. The axis includes them and the area fills toward the axis floor, not toward zero across the plot.
4. **Two line charts on one page** (tasks 2 and 4): the position page. Gradient ids differ; hiding a series in one chart does not affect the other.
5. **A long range** (task 2): about 1800 daily points. No per-point DOM node (`dot={false}`), and the test renders it without timing out.

---

### Task 1: Docs to main; helpers and types to `lib/chart.ts`

**Files:**

- Create: `frontend/src/lib/chart.ts`, `frontend/src/lib/chart.test.ts`
- Modify: `frontend/src/components/LineChart.tsx`, `frontend/src/components/BarChart.tsx`, `frontend/src/components/LineChart.test.tsx`, `frontend/src/pages/SummaryPage.tsx`, `frontend/src/pages/PositionPage.tsx`, `frontend/src/pages/DividendsPage.tsx`, `docs/ROADMAP.md`
- Carry: the spec, this plan and the mockup (cherry-pick the orchestrator's local commit; the ticket names it)

**Interfaces:**

- Produces, all exported from `frontend/src/lib/chart.ts`:
  - `type ChartPoint`, `type ChartMarker`, `type Bar` (moved verbatim)
  - `niceTicks(min: number, max: number): number[]`
  - `axisTicks(values: number[]): number[]`
  - `formatTicks(ticks: number[]): string[]`
  - `rangeDelta(points: HistoryEntry[], netFlow: string): string | null`
  - `formatChartValue(value: string | number): string` (today's private `formatValue` in `LineChart.tsx`; masks in privacy mode)
  - `formatDateLabel(iso: string, withYear: boolean): string` (today's private `formatDate`)

- [ ] **Step 1:** Move the functions and types verbatim, with their comments. Move the `niceTicks`, `axisTicks` and `rangeDelta` tests to `lib/chart.test.ts` unchanged.
- [ ] **Step 2:** Point every import at `lib/chart`. Remove the `react-refresh/only-export-components` disables that the move makes unnecessary. `LineChart.tsx` and `BarChart.tsx` export only their component.
- [ ] **Step 3:** `docs/ROADMAP.md`: add "Phase 25 — UI v2: charts foundation (epic key)" under "In progress" with a link to the spec; under "Next" replace sub-project 3 with 3b (allocation donut, P/L per position, drag to zoom) and 3c (return-index series: percent mode, periodic returns, drawdown); 4 and 5 stay.
- [ ] **Step 4:** Run `make lint`, `make test`, `make build-frontend`. Expected: exit 0, the same number of frontend tests as before.
- [ ] **Step 5:** Commit. No behaviour changes in this task; say so in the PR.

### Task 2: Recharts and the line chart in style A

**Files:**

- Create: `frontend/src/components/charts/LineChart.tsx`, `frontend/src/components/charts/LineChart.test.tsx`, `frontend/src/components/charts/ChartTooltip.tsx`, `frontend/src/test/chartSize.ts`
- Delete: `frontend/src/components/LineChart.tsx`, `frontend/src/components/LineChart.test.tsx`
- Modify: `frontend/package.json`, `frontend/package-lock.json`, `frontend/src/pages/SummaryPage.tsx`, `frontend/src/pages/PositionPage.tsx` (import path only), `frontend/src/index.css` (focus ring for the chart, if needed), `docs/adr/0023-ui-design-system-v2.md`, `CLAUDE.md`, `frontend/README.md`

**Interfaces:**

- Consumes: everything task 1 produced.
- Produces:
  - `export type LineChartProps = { points: ChartPoint[]; benchmarkLabel?: string; estimatedFrom?: string | null; markers?: ChartMarker[]; labels?: { value: string; cost: string } }`
  - `export function LineChart(props: LineChartProps)`
  - `export function ChartTooltip({ title, rows }: { title: string; rows: { key: string; swatch: ReactNode; label: string; value: string; note?: string }[] })`: the card only (`bg-surface rounded-ctl shadow-pop`, `role="tooltip"`, `aria-live="polite"`). Each chart adapts Recharts' tooltip payload to these props.
  - `mockChartSize(width = 600, height = 240): () => void` in `test/chartSize.ts`: stubs `getBoundingClientRect` and `ResizeObserver` so Recharts renders in jsdom; returns the restore function.

- [ ] **Step 1:** `npm install recharts@^3` in `frontend/`. Confirm the lock file adds nothing unexpected at the top level.
- [ ] **Step 2: Write the failing tests** in `charts/LineChart.test.tsx`, with `mockChartSize()` in `beforeEach`. Port these from the old file, asserting behaviour and not coordinates:
  - draws the value series and the cost series; names both in the legend; `labels` renames them
  - no benchmark series or legend row without `benchmark_value`; with gaps, the benchmark is drawn in separate segments
  - a hovered or keyboard-reached benchmark point from `estimatedFrom` on carries `chart.benchmark.estimated`
  - a marker is drawn only for a date in the series; its kind is listed in the legend and in the tooltip row of that date
  - the chart's accessible name is the summary sentence; ArrowRight then shows a tooltip with the first date, ArrowRight again the second
  - the empty state when every value is zero; a single point renders one date label
  - date labels carry the year when the range spans calendar years
  - tooltip amounts are the exact formatted strings (give a value like `1234567.89` and expect `formatChartValue` of the string, not of a rounded number)
  - Review Focus 1: render, set privacy on, re-render; every axis label and tooltip amount is the mask
  - Review Focus 2: a cost series of all nulls draws no cost path and no cost legend row; a series with leading nulls has no segment before its first value
  - Review Focus 3: values from `-500` to `1500` give y ticks that include a negative tick
  - Review Focus 4: two `LineChart`s in one render have different gradient ids
  - Review Focus 5: 1800 points render and the DOM holds no per-point dot elements
- [ ] **Step 3:** Run the new test file. Expected: fails, the module does not exist.
- [ ] **Step 4: Implement `charts/LineChart.tsx`.** A `ComposedChart` with `responsive`, the aspect and minimum height of today (2:5, 200 px floor) through CSS; horizontal dashed grid; `XAxis` on `date` with the same labelled dates as today through `formatDateLabel`; `YAxis` with `ticks` and `domain` from `axisTicks` over the visible values and labels from `formatTicks`; an `Area` for the value with the gradient (`useId` for its id) and straight segments; `Line`s for cost and benchmark with the dashes of the spec, `dot={false}`, gaps not connected; markers as custom shapes reusing today's `MarkerShape`; `Tooltip` with the cursor of the spec and `ChartTooltip` as content; the legend as our own HTML below the plot (static in this task). The drawing values are `Number()`; a second field per point keeps the exact strings for the tooltip.
- [ ] **Step 5:** Run the tests. Expected: pass. Delete the old component and test; point the two pages at `components/charts/LineChart`.
- [ ] **Step 6: Stop-rule checks** on a production build (`make build-frontend`, served with the response header `Content-Security-Policy` copied from `deploy/nginx.conf`), synthetic data, at 375 px and 1280 px, light and dark:
  1. no CSP violation in the console on Özet and on a position page
  2. at 375 px a vertical drag starting on the chart scrolls the page
  3. after a touch ends, the tooltip value can still be read
  4. Left and Right move the cursor; the focus ring is the app's
  5. with reduced motion emulated, nothing animates

  If 1, 2 or 3 fails, or an item of the spec's "What stays ours" cannot be met: comment on the ticket with what was found and the options, open no PR, report.

- [ ] **Step 7: Docs.** ADR 0023 dated note (Recharts version; files; what Recharts owns and what stays ours in two sentences; Home and End dropped; role `application`; the split of sub-project 3 into 3a, 3b, 3c; bundle sizes raw and gzip before and after). `CLAUDE.md` frontend stack line: add "Recharts for charts". `frontend/README.md` and any ADR that calls the charts hand-drawn as current: fix or add a dated note (`git grep -n -i -E "hand-drawn|polyline|no chart" -- docs frontend CLAUDE.md README.md`).
- [ ] **Step 8:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 3: Bar chart on Recharts

**Files:**

- Create: `frontend/src/components/charts/BarChart.tsx`, `frontend/src/components/charts/BarChart.test.tsx`
- Delete: `frontend/src/components/BarChart.tsx`, `frontend/src/components/BarChart.test.tsx`
- Modify: `frontend/src/pages/DividendsPage.tsx` (import path only)

**Interfaces:**

- Consumes: `Bar` from `lib/chart`, `ChartTooltip`, `mockChartSize`.
- Produces: `export type BarChartProps = { bars: Bar[]; currency: string; height?: number }`, `export function BarChart(props: BarChartProps)`.

- [ ] **Step 1: Write the failing tests:** one bar per entry; a zero entry is drawn as a 1 px hairline, not skipped; the y ticks are compact money with the sign before the digits (today's test, GLG-893); the chart is one focus stop with the `chart.bars.summary` name and ArrowRight shows the first bar's label and exact amount; the empty state when every value is zero; privacy masks the ticks and the tooltip; a negative value is drawn as zero, as today.
- [ ] **Step 2:** Run. Expected: fails, the module does not exist.
- [ ] **Step 3: Implement** a Recharts `BarChart` with `responsive` and the `height` prop (default 200): accent bars with the top corners rounded 3 px, the grid and axis style of task 2, two y ticks (0 and the max) through `formatCompactMoney` as today, x labels thinned by Recharts so they never overlap, `ChartTooltip` as tooltip content.
- [ ] **Step 4:** Run the tests. Expected: pass. Delete the old files; point `DividendsPage` at the new one.
- [ ] **Step 5:** Browser check on Temettü at 375 px and 1280 px, light and dark: bars, labels that do not overlap, tooltip on hover and on arrow keys, no console error.
- [ ] **Step 6:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 4: Legend toggle

**Files:**

- Modify: `frontend/src/components/charts/LineChart.tsx`, `frontend/src/components/charts/LineChart.test.tsx`, `docs/adr/0023-ui-design-system-v2.md` (one sentence in the task 2 note)

**Interfaces:**

- Consumes: `LineChart` from task 2. Props do not change.
- Produces: nothing new outside the file. State inside the component: which of `cost` and `benchmark` are hidden.

- [ ] **Step 1: Write the failing tests:**
  - the cost and benchmark legend entries are buttons with `aria-pressed="true"`; the value entry and marker entries are not buttons
  - clicking the cost button sets `aria-pressed="false"`, removes the cost path, and the tooltip reached with ArrowRight has no cost row; clicking again restores all three
  - the y axis refits: with value around 1000 and cost around 100, hiding cost changes the lowest tick label
  - Review Focus 4: with two charts rendered, hiding cost in the first leaves the second's cost path
- [ ] **Step 2:** Run. Expected: the new tests fail.
- [ ] **Step 3: Implement.** A hidden series is not drawn, is left out of the values given to `axisTicks`, and is left out of the tooltip rows. The buttons use the legend's current look plus the app's focus ring; a hidden entry keeps its `text-muted` label, struck through, with its swatch at reduced opacity (`text-faint` failed text contrast, GLG-922). No new i18n key: the series label is the button's name and `aria-pressed` carries the state.
- [ ] **Step 4:** Run the tests. Expected: pass.
- [ ] **Step 5:** Browser check on Özet with a benchmark chosen: hide and show both series by mouse and by keyboard (Tab, Space); the animation runs on toggle; light and dark.
- [ ] **Step 6:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 5: Lazy chunk, placeholder, failure state, import convention

**Files:**

- Create: `frontend/src/components/LazyChart.tsx`, `frontend/src/components/LazyChart.test.tsx`
- Modify: `frontend/src/components/ErrorBoundary.tsx`, `frontend/src/pages/SummaryPage.tsx`, `frontend/src/pages/PositionPage.tsx`, `frontend/src/pages/DividendsPage.tsx`, `frontend/src/conventions.test.ts`, `docs/adr/0023-ui-design-system-v2.md`

**Interfaces:**

- Consumes: `LineChartProps`, `BarChartProps` (type-only imports), `ErrorBoundary`.
- Produces:
  - `ErrorBoundary` gains `fallback?: ReactNode`; when set, it is rendered instead of the full-page fallback.
  - `LazyChart.tsx` exports `LineChart(props: LineChartProps)` and `BarChart(props: BarChartProps)`. Each is `ErrorBoundary` (fallback: `<p role="alert" className="text-sm text-muted">{t('chart.error')}</p>`) around `Suspense` (fallback: the `Skeleton` at the chart's height) around the `React.lazy` component.

- [ ] **Step 1: Write the failing tests:**
  - `LazyChart`: renders the placeholder first, then the chart's accessible name
  - `LazyChart`: when the dynamic import rejects, shows `chart.error` and the sibling content of the test page is still rendered
  - `ErrorBoundary`: with `fallback`, a throwing child renders the fallback and no `<main>`
  - `conventions.test.ts`: `from 'recharts'` appears only in files under `components/charts/`; an import of `components/charts/` appears only in `components/LazyChart.tsx` and in files under `components/charts/`
- [ ] **Step 2:** Run. Expected: the new tests fail.
- [ ] **Step 3: Implement.** Point the three pages at `components/LazyChart`. Keep their own `Skeleton` for the data-loading state as it is.
- [ ] **Step 4:** Run the tests. Expected: pass.
- [ ] **Step 5:** `make build-frontend`. Read the chunk list: Recharts is in a chunk of its own and the main chunk is not larger than before task 2 plus a small margin. Put the four numbers (main and charts chunk, raw and gzip) in the ADR 0023 note, replacing the provisional ones of task 2.
- [ ] **Step 6:** Browser check with the network throttled: the placeholder shows, then the chart, without a layout jump; block the chunk's URL and reload: `chart.error` in the card, the rest of Özet works.
- [ ] **Step 7:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 6: Docs: move Phase 25 to HISTORY

Runs after the orchestrator's close-out browser check of combined `main` and after the tickets that check files are merged.

**Files:**

- Modify: `docs/HISTORY.md`, `docs/ROADMAP.md`

- [ ] **Step 1:** `docs/HISTORY.md`: add "Phase 25 — UI v2: charts foundation" after Phase 24 in the format of its neighbours, one bullet per Done child of the epic, each described from its merged PR.
- [ ] **Step 2:** `docs/ROADMAP.md`: remove Phase 25 from "In progress"; "Next" keeps 3b, 3c, 4 and 5.
- [ ] **Step 3:** `make lint`, `make test`, `make build-frontend`. Commit.
