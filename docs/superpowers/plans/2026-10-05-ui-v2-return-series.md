# UI v2 Return Series Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Return the portfolio's and the benchmark's daily time-weighted return from the API, draw it as a percent chart on the return card, and add a Performans tab with periodic returns and drawdown.

**Architecture:** The backend's history sweep already chains the daily factors; it now emits the running product per point, and a second endpoint emits returns per calendar month and year from one sweep. The frontend draws what the API returns: `LineChart` gains a `unit` prop, a new page holds a heatmap table and two small charts in the charts chunk, and drawdown is a pure function over the returned series.

**Tech Stack:** Go `net/http`, SQLite, float64 for ratios (ADR 0011); React 19, TypeScript, Recharts inside `components/charts/`, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-05-ui-v2-return-series-design.md`. Visual reference: `docs/superpowers/specs/2026-10-05-ui-v2-return-series-mockup.html`. Read ADR 0011 and ADR 0012 before any backend task.

## Global Constraints

- One task is one GLG ticket, one PR, one agent. The ticket's "Depends on" is the merge order.
- No AI attribution in commits, PRs or files.
- No new dependency. No schema change.
- Return math follows ADR 0011 exactly: the same inputs, the same flow rules, the same `null` rules. No second copy of those rules in TypeScript; the browser only transforms series the API returned.
- Ratios are float64 and travel as fraction strings with 6 decimals; money stays decimal.
- Every query filters by `user_id`. A changed or new route updates its swag annotations.
- Only files under `frontend/src/components/charts/` import `recharts`; pages reach charts through `components/LazyChart.tsx`.
- Semantic tokens only. Every new UI string has a `tr` and an `en` key.
- A portfolio without a defined return gets an explicit state on every new view.
- Checks from the repo root, all exit 0: `make lint`, `make test`, `make build-frontend`; plus `make swagger` on tasks 1 and 2.

## Review Focus

1. **A deposit inside the range** (tasks 1, 3): `twr` does not jump on that day; the percent line stays continuous.
2. **A range with no defined return** (tasks 1, 3, 4, 6): `twr` is `null` on every point, never on some; each view shows its explanation.
3. **The first bucket and the current bucket** (task 2): both partial; a portfolio that started this month has one month and one year, both partial.
4. **A benchmark that starts later than the portfolio** (tasks 1, 2): `benchmark_twr` is `null` where the shadow has no value, and a bucket's `benchmark` is `null` rather than a number over a partial window.
5. **A year whose only activity is deposits** (task 2): the return is zero or `null` by ADR 0011's rules, never a division by zero.

---

### Task 1: Docs to main; daily `twr` on history points

**Files:**

- Modify: `backend/internal/server/returns.go`, `backend/internal/server/history.go`, their tests, the benchmark shadow code that produces the benchmark's returns, `frontend/src/api/` (the `HistoryEntry` type), `docs/adr/0011-portfolio-returns.md`, `docs/adr/0012-benchmarks.md`, `docs/ROADMAP.md`
- Carry: the spec, this plan and the mockup (cherry-pick the commit the ticket names)

**Interfaces:**

- Produces: `points[].twr: string | null` and `points[].benchmark_twr: string | null` on `GET /api/portfolios/{id}/history`, as the spec defines.
- Produces (Go): `timeWeightedSeries(values, flows []float64) ([]float64, bool)` beside `timeWeightedReturn`; its last element equals what `timeWeightedReturn` returns.

- [ ] **Step 1: Failing Go tests:** the series' first element is 0; the last equals `timeWeightedReturn` for the worked examples of ADR 0011; a flow on a day leaves that day's cumulative value where the factor rule puts it (Review Focus 1); a skipped day repeats the previous value; a negative factor makes the whole series undefined. Handler tests: `twr` present on every point and equal at the end to `returns.twr`; `null` on every point when `returns.twr` is `null` (Review Focus 2); `benchmark_twr` with a benchmark, `null` without, and `null` where the shadow has no value (Review Focus 4).
- [ ] **Step 2:** Implement in the sweep; no second pass over the transactions.
- [ ] **Step 3:** Swag annotations; the frontend type.
- [ ] **Step 4:** ADR 0011 and ADR 0012 dated notes. `docs/ROADMAP.md`: "Phase 28 — UI v2: return series (epic key)" under "In progress"; remove 3c from "Next".
- [ ] **Step 5:** `make lint`, `make test`, `make build-frontend`, `make swagger`. Commit.

### Task 2: Periodic returns endpoint

**Files:**

- Create: `backend/internal/server/returns_periods.go` and its test
- Modify: the route registration, `frontend/src/api/` (a `usePeriodReturns` query and its types), `docs/adr/0011-portfolio-returns.md`

**Interfaces:**

- Produces: `GET /api/portfolios/{id}/returns/periods?benchmark=<code>` with the response of the spec: `currency`, `months[]`, `years[]`, each bucket `{ period, from, to, partial, returns: { simple, twr, mwr }, benchmark: { … } | null }`.
- Produces (frontend): `usePeriodReturns(scope, benchmark)`, types `PeriodReturns`, `PeriodBucket`.

- [ ] **Step 1:** Read how the frontend's calendar-year preset builds its range and how the history handler treats the first day (`F_0 = 0`, the first point's value as the base). Write down the bucket boundaries that make a year's bucket equal the history endpoint's result for that preset; put that sentence in the ADR note.
- [ ] **Step 2: Failing Go tests:** for a seeded portfolio spanning two years with a deposit mid-year, the 2025 bucket's three returns equal the history handler's `returns` for the 2025 preset range, and one month's bucket equals the history handler's for that month's range; the first and current buckets are `partial` (Review Focus 3); a year with only deposits (Review Focus 5); `benchmark` absent without the parameter, present with it, `null` for a bucket the benchmark does not cover (Review Focus 4); `all` sums the caller's portfolios; another user's portfolio is 404 as elsewhere; an unknown benchmark is 400; buckets are oldest first with no gaps.
- [ ] **Step 3:** Implement from one sweep of daily values and flows, slicing per bucket and calling the existing `periodReturns`.
- [ ] **Step 4:** Swag annotations; frontend query and types; ADR 0011 dated note.
- [ ] **Step 5:** `make lint`, `make test`, `make build-frontend`, `make swagger`. Commit.

### Task 3: Percent mode on the return card

**Files:**

- Modify: `frontend/src/components/charts/LineChart.tsx` and its test, `frontend/src/lib/chart.ts` and its test (percent ticks), `frontend/src/components/ReturnCard.tsx` and its test, `frontend/src/i18n/strings.ts`, `docs/adr/0023-ui-design-system-v2.md`

**Interfaces:**

- Consumes: `twr`, `benchmark_twr` (task 1).
- Produces: `LineChartProps.unit?: 'money' | 'percent'` (default `'money'`). In percent the chart reads the same point fields as fractions.
- Produces: `chartMode` in `localStorage` (`'percent' | 'money'`, default `'percent'`).

- [ ] **Step 1: Failing tests** on `LineChart` with `unit="percent"`: y tick labels are percentages and distinct; a zero line is drawn; no area fill; the tooltip rows show percentages with the sign; privacy mode does not mask percentages (they are not amounts; follow what the chart header does today for its percentage).
- [ ] **Step 2:** Implement, reusing `niceTicks`; add the percent tick formatter to `lib/chart.ts` with its unit test.
- [ ] **Step 3: Failing tests** on `ReturnCard`: opens in percent; the switch changes the chart and is stored; in percent it passes `twr` and `benchmark_twr` and no cost series; with a method other than time-weighted the portfolio's legend label carries the time-weighted suffix and a tooltip; with `twr` null the value chart shows, the switch is disabled and the explanation is visible (Review Focus 2); the benchmark legend toggle still works.
- [ ] **Step 4:** Implement.
- [ ] **Step 5:** Browser check with a deposit inside the range: in percent the line is continuous through the deposit day (Review Focus 1) while the ₺ chart steps up; both lines start at 0%; the tooltip; 375 px.
- [ ] **Step 6:** ADR 0023 dated note (percent mode, the `unit` prop). `make lint`, `make test`, `make build-frontend`. Commit.

### Task 4: Performans tab with the periodic returns heatmap

**Files:**

- Create: `frontend/src/pages/PerformancePage.tsx` and its test, `frontend/src/components/PeriodReturnsTable.tsx` and its test, `frontend/src/lib/periods.ts` and its test
- Modify: `frontend/src/App.tsx` and its test, `frontend/src/components/PortfolioHeader.tsx` and its test (the tab; the tab row for `all`), `frontend/src/components/Sidebar.tsx` (a portfolio link keeps the Performans tab like the other tabs), `frontend/src/i18n/strings.ts`, `docs/adr/0023-ui-design-system-v2.md`, `docs/adr/0014-all-portfolios-view.md`

**Interfaces:**

- Consumes: `usePeriodReturns` (task 2); the stored benchmark and method.
- Produces: `heatStep(value: number | null): -5 … 5` in `lib/periods.ts` (steps at 1, 2, 4, 8 percent and above, sign kept, 0 for null or zero) and `periodGrid(months, years)` returning rows of year, twelve cells and the year cell.
- Produces: route `/portfolios/:id/performance`.

- [ ] **Step 1: Failing unit tests** for `heatStep` at each boundary and for `periodGrid` (a first year starting in December; a current year ending in October; a missing month in the middle).
- [ ] **Step 2: Failing component tests:** a table with a header row of months and a row header per year, newest first; a cell's text is the chosen method's percentage with one decimal; the step class follows `heatStep`; a month without a bucket shows the middle dot, a `null` return a dash; a partial bucket has its mark and a `title` with dates and the exact percentage; "fark" shows the difference to the benchmark and is offered only with a benchmark; the method select changes the numbers; the undefined-return explanation when every return is `null`.
- [ ] **Step 3:** Implement the table and the page (benchmark and method selects from the same stored settings as Özet), the route, the tab in `PortfolioHeader` between Özet and İşlemler, and the tab row for `all`.
- [ ] **Step 4:** Heat colours from the gain and loss tokens in five strengths (for example `color-mix` with the surface); check text contrast of every step in both themes and say the numbers in the PR.
- [ ] **Step 5:** Browser check at 1280 px and 375 px (the table scrolls inside its card with the year column fixed), light and dark, for a portfolio and for `all`; a year's cell equals the Özet header's number for that year's preset.
- [ ] **Step 6:** ADR 0023 and ADR 0014 dated notes. `make lint`, `make test`, `make build-frontend`. Commit.

### Task 5: Periodic returns as bars

**Files:**

- Create: `frontend/src/components/charts/ReturnBars.tsx` and its test
- Modify: `frontend/src/components/charts/index.ts`, `frontend/src/components/LazyChart.tsx` and its test, `frontend/src/pages/PerformancePage.tsx` and its test, `frontend/src/i18n/strings.ts`

**Interfaces:**

- Produces: `ReturnBars({ buckets, benchmarkLabel })` in the charts chunk: grouped bars for the last 24 months, portfolio and benchmark, percent axis with a zero line, `ChartTooltip`.

- [ ] **Step 1: Failing tests:** one group per month for the last 24 buckets; a second series only with a benchmark; negative values draw below the zero line; a `null` return draws no bar and the tooltip says so; the accessible name summarises the range; the "Isı haritası | Çubuk" switch on the page shows one or the other and is component state.
- [ ] **Step 2:** Implement with the conventions of `charts/BarChart.tsx` (sizing, tokens, animation default).
- [ ] **Step 3:** Browser check at 1280 px and 375 px, light and dark; keyboard arrows move the tooltip.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 6: Drawdown card

**Files:**

- Create: `frontend/src/lib/drawdown.ts` and its test, `frontend/src/components/charts/DrawdownChart.tsx` and its test
- Modify: `frontend/src/components/charts/index.ts`, `frontend/src/components/LazyChart.tsx`, `frontend/src/pages/PerformancePage.tsx` and its test, `frontend/src/i18n/strings.ts`, `docs/adr/0011-portfolio-returns.md`

**Interfaces:**

- Consumes: the all-time history's `twr` (task 1).
- Produces: `drawdown(points: { date: string; twr: string | null }[]): { series: { date: string; value: number }[]; max: { value: number; peak: string; trough: string } | null; current: number } | null`: `null` when any `twr` is `null` or there are fewer than two points.

- [ ] **Step 1: Failing unit tests:** a rising series gives zeros and `max` null; a fall and full recovery gives the fall's depth with its peak and trough dates and `current` 0; a new low on the last day; two equal lows keep the first; a `null` series gives `null` (Review Focus 2).
- [ ] **Step 2:** Implement `drawdown` on the index `1 + twr`.
- [ ] **Step 3: Failing component tests:** the card's header shows the largest drawdown with its two dates and the current one; the chart is drawn from the series; the explanation when `drawdown` is `null`.
- [ ] **Step 4:** Implement `DrawdownChart` (an area in the loss colour, zero at the top, percent axis, `ChartTooltip`) and the card.
- [ ] **Step 5:** Browser check at 1280 px and 375 px, light and dark.
- [ ] **Step 6:** ADR 0011 dated note (the drawdown definition). `make lint`, `make test`, `make build-frontend`. Commit.

### Task 7: Docs: move Phase 28 to HISTORY

Runs after the orchestrator's close-out browser check of combined `main`.

- [ ] **Step 1:** `docs/HISTORY.md`: "Phase 28 — UI v2: return series" after Phase 27, one bullet per Done child of the epic, each described from its merged PR.
- [ ] **Step 2:** `docs/ROADMAP.md`: remove Phase 28 from "In progress".
- [ ] **Step 3:** `make lint`, `make test`, `make build-frontend`. Commit.
