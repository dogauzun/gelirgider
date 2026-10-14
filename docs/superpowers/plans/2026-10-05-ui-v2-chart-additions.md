# UI v2 Chart Additions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the allocation bar with a donut, add a profit-and-loss-per-position card, let a mouse drag on the line chart set the range, and settle two chart debts.

**Architecture:** Pure functions decide the rows and ticks (`allocationRows`, `contributionRows`, `dateTicks`); components render. The donut is a Recharts chart behind `LazyChart.tsx`; the Katkı bars are plain HTML. Zoom is one optional `onZoom` prop on `LineChart`, wired to `useRange` by the pages.

**Tech Stack:** React 19, TypeScript, Tailwind v4 tokens, Recharts 3.x inside `components/charts/`, Vitest + Testing Library with `test/chartSize.ts`.

**Spec:** `docs/superpowers/specs/2026-10-05-ui-v2-chart-additions-design.md`. Visual reference: `docs/superpowers/specs/2026-10-05-ui-v2-chart-additions-mockup.html`. Also read the core pages spec, `docs/superpowers/specs/2026-10-05-ui-v2-core-pages-design.md`, for where the cards sit.

## Global Constraints

- One task is one GLG ticket, one PR, one agent. The ticket's "Depends on" is the merge order; tasks 2 to 4 need the core pages' Özet layout A on `main`.
- No AI attribution in commits, PRs or files.
- No new dependency. No backend change.
- Only files under `frontend/src/components/charts/` import `recharts`, and pages reach charts through `components/LazyChart.tsx` (`conventions.test.ts` enforces both).
- Money is decimal: `Number()` only for bar lengths, slice sizes and ordering; printed amounts come from the exact strings.
- Semantic tokens only; chart colours as `var(--color-*)`.
- Every new UI string has a `tr` and an `en` key. Privacy mode masks every amount.
- Animation stays on Recharts' default so reduced motion is honoured.
- Browser checks: production build, synthetic data, own ports, headless Chrome over the DevTools protocol; never the owner's dev instance, database or `.env`.
- Checks from the repo root, all exit 0: `make lint`, `make test`, `make build-frontend`.

## Review Focus

1. **Zoom must not break touch** (task 4): a touch drag still scrubs the tooltip and a vertical touch drag still scrolls the page; no listener calls `preventDefault` on touch moves.
2. **A drag that ends outside the plot or the window** (task 4): the shaded span is removed and no range is set; no listener is left on `window`.
3. **Allocation with one row or none** (task 2): only cash, or nothing with a reporting value: a full ring or the empty text, no NaN angle.
4. **Negative or zero values in Katkı** (task 3): all gains, all losses, all zero, a single position: bar lengths stay finite and the zero line stays in place.
5. **Very narrow plot** (task 1): at 200 px the date labels do not overlap and there are still at least the first and the last.

---

### Task 1: Docs to main; date labels by width; stable legend height

**Files:**

- Modify: `frontend/src/lib/chart.ts`, `frontend/src/lib/chart.test.ts`, `frontend/src/components/charts/LineChart.tsx` and its test, `frontend/src/components/LazyChart.tsx` and its test, `docs/ROADMAP.md`, `docs/adr/0023-ui-design-system-v2.md`
- Carry: the spec, this plan and the mockup (cherry-pick the commit the ticket names)

**Interfaces:**

- Produces: `dateTicks(dates: string[], plotWidth: number): string[]` in `lib/chart.ts`: one date per about 120 px, at least 3 (fewer when there are fewer dates), at most 7, evenly spaced by index, always the first and the last, no duplicates.

- [ ] **Step 1: Failing unit tests** for `dateTicks`: 93 dates at 900 px give 7 including both ends; at 300 px give 3; at 200 px give 3 (Review Focus 5); one date gives one; two dates give two; 5 dates at 1,200 px give 5.
- [ ] **Step 2:** Implement and use it for the x axis ticks in `LineChart` (the plot width from Recharts' own plot area). The year rule stays.
- [ ] **Step 3: Failing tests:** the legend container has a class reserving two lines below `sm` and one from `sm`; `LazyChart`'s line placeholder reserves the same.
- [ ] **Step 4:** Implement. Browser check at 375 px with a benchmark, charts chunk delayed: the card's height is equal before and after. At 1280 px the Özet chart shows seven date labels that do not overlap.
- [ ] **Step 5:** `docs/ROADMAP.md`: "Phase 27 — UI v2: chart additions (epic key)" under "In progress"; remove 3b from "Next". ADR 0023 dated note for the date labels.
- [ ] **Step 6:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 2: Allocation donut

**Files:**

- Create: `frontend/src/lib/allocation.ts`, `frontend/src/lib/allocation.test.ts`, `frontend/src/components/charts/AllocationDonut.tsx`, `frontend/src/components/charts/AllocationDonut.test.tsx`
- Modify: `frontend/src/components/Allocation.tsx` and its test, `frontend/src/components/charts/index.ts`, `frontend/src/components/LazyChart.tsx` and its test, `frontend/src/i18n/strings.ts`, `docs/adr/0023-ui-design-system-v2.md`, `docs/adr/0021-product-and-ui-decisions.md`

**Interfaces:**

- Produces: `allocationRows(input: { positions; cash; cashByCurrencyReporting; view: 'symbol' | 'market' | 'currency' }): { rows: AllocationRow[]; total: number; other: string[] }`, where `AllocationRow = { key; label; value: number; amount: string; share: number; color: string }`. Today's rules, moved out of `Allocation.tsx` unchanged.
- Produces: `AllocationDonut({ rows, totalLabel, active, onActive })` in `components/charts/`, exported through `charts/index.ts` and wrapped by `LazyChart.tsx` with a ring placeholder and a failure fallback that renders nothing.

- [ ] **Step 1: Failing unit tests** for `allocationRows`: today's `Allocation.test.tsx` cases ported to the function; more than six rows fold into "Diğer" with its members listed; only cash gives one row with share 1 (Review Focus 3); nothing with a reporting value gives no rows; shares sum to 1.
- [ ] **Step 2:** Move the rules into `lib/allocation.ts`; `Allocation.tsx` calls it.
- [ ] **Step 3: Failing component tests:** `Allocation` renders a list with one item per row carrying label, amount and share; "Diğer (n)" has its members in `title`; the donut wrapper is `aria-hidden` and not focusable; hovering a legend row sets the active key passed to the donut; the view switch still changes the rows; the missing-rates note stays; privacy masks amounts and the centre total; with no rows the empty text shows and no donut.
- [ ] **Step 4:** Implement `AllocationDonut` (Recharts `PieChart`, one `Pie` with inner and outer radius for a 16 px ring, the row colours, `startAngle` at twelve o'clock, the centre label as our own HTML over the chart, `ChartTooltip` content) and the new `Allocation` layout: donut left, legend right; legend under the donut below `md`. Remove the stacked bar.
- [ ] **Step 5:** `LazyChart`: export `AllocationDonut` from the same charts chunk; ring placeholder of the donut's size; on a failed chunk the legend alone remains.
- [ ] **Step 6:** Özet and Tüm portföyler: no change of placement in this task (the card sits where layout A put it).
- [ ] **Step 7:** Browser check at 1280 px and 375 px, light and dark: the three views; hover highlight both ways; the card's height before and after the chunk; only-cash portfolio.
- [ ] **Step 8:** ADR 0023 and ADR 0021 dated notes. `make lint`, `make test`, `make build-frontend`. Commit.

### Task 3: Katkı card

**Files:**

- Create: `frontend/src/lib/contribution.ts`, `frontend/src/lib/contribution.test.ts`, `frontend/src/components/ContributionCard.tsx`, `frontend/src/components/ContributionCard.test.tsx`
- Modify: `frontend/src/pages/SummaryPage.tsx` and its test (lower rows), `frontend/src/i18n/strings.ts`, `docs/adr/0023-ui-design-system-v2.md`

**Interfaces:**

- Produces: `contributionRows(positions, mode: 'total' | 'day', limit: number | null): { rows: { instrumentId; symbol; amount: string; value: number; fraction: number }[]; hidden: number }`. `fraction` is the bar length from 0 to 1 against the largest absolute value; with a limit of 4 it returns the four largest gains then the four largest losses, best first; `hidden` is how many positions the limit left out.
- Produces: `ContributionCard({ positions, readOnly })`.

- [ ] **Step 1: Failing unit tests:** ten positions give 4 gains then 4 losses and `hidden` 2; three gains and one loss give 4 rows and `hidden` 0; ties keep symbol order; positions without the chosen value are left out; all zero gives fractions 0 (Review Focus 4); a single position has fraction 1; `limit` null returns all sorted best first.
- [ ] **Step 2:** Implement `contributionRows`.
- [ ] **Step 3: Failing component tests:** a list with one item per row whose text has the symbol and the signed amount; a gain bar on the right half and a loss bar on the left half (class or style assertion); the Toplam | Bugün switch changes the numbers; "Tümünü göster" appears only when `hidden` is above zero and expands; symbols link to the position page unless `readOnly`; the empty line; privacy masks amounts and keeps bar widths.
- [ ] **Step 4:** Implement the card in plain HTML and CSS (no Recharts import).
- [ ] **Step 5:** `SummaryPage`: lower rows become Dağılım | Katkı, then İstatistikler as a full-width `KpiStrip`. Page test: the order of the three; both scopes.
- [ ] **Step 6:** Browser check at 1280 px and 375 px: the two cards side by side with equal heights from `lg`; stacked below `md`; switch and expand; no sideways scroll.
- [ ] **Step 7:** ADR 0023 dated note. `make lint`, `make test`, `make build-frontend`. Commit.

### Task 4: Drag to zoom

**Files:**

- Modify: `frontend/src/components/charts/LineChart.tsx` and its test, `frontend/src/components/ReturnCard.tsx` and its test, `frontend/src/pages/PositionPage.tsx` and its test, `frontend/src/components/RangePicker.tsx` and its test, `frontend/src/i18n/strings.ts`, `docs/adr/0023-ui-design-system-v2.md`

**Interfaces:**

- Produces: `LineChartProps.onZoom?: (from: string, to: string) => void` (ISO dates, `from` before `to`).
- Produces: `RangePicker` shows a clear button on "Özel" when given `onClear`.

- [ ] **Step 1: Failing tests** on `LineChart` with the size helper: a mouse press, move across at least two points and release calls `onZoom` once with the first and last dates of the span in order, whichever direction the drag went; a drag within one point does not; release outside the plot does not and removes the shaded span (Review Focus 2); Escape during the drag cancels; a pointer of type `touch` or `pen` never calls `onZoom` (Review Focus 1); without `onZoom` no span is drawn.
- [ ] **Step 2:** Implement with Recharts' chart mouse events and a `ReferenceArea` for the span. Confirm in the installed Recharts source how the active label is exposed during a mouse move; do not add listeners that call `preventDefault` on touch events.
- [ ] **Step 3: Failing tests:** `ReturnCard` and `PositionPage` pass `onZoom` and set the range through `useRange`; while a zoomed range is active the picker shows "Özel" with a clear button that restores the range active before the drag (kept in component state; after a reload the clear button returns to the default range).
- [ ] **Step 4:** Implement.
- [ ] **Step 5: Stop-rule checks** in headless Chrome with real input events on a production build: a mouse drag zooms on Özet and on the position page, the URL carries `from` and `to`, the clear button returns; with touch emulation a horizontal drag scrubs the tooltip and sets no range, and a vertical drag scrolls the page; no console error. If touch scrubbing or vertical scroll regresses and cannot be fixed, comment on the ticket with the evidence, open no PR, and report.
- [ ] **Step 6:** ADR 0023 dated note (zoom, the `onZoom` prop). `make lint`, `make test`, `make build-frontend`. Commit.

### Task 5: Docs: move Phase 27 to HISTORY

Runs after the orchestrator's close-out browser check of combined `main`.

- [ ] **Step 1:** `docs/HISTORY.md`: "Phase 27 — UI v2: chart additions" after Phase 26, one bullet per Done child of the epic, each described from its merged PR.
- [ ] **Step 2:** `docs/ROADMAP.md`: remove Phase 27 from "In progress".
- [ ] **Step 3:** `make lint`, `make test`, `make build-frontend`. Commit.
