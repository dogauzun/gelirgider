# UI v2 Core Pages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Özet, Tüm portföyler, İşlemler, the position page and Temettü the layouts of the approved mockup, so each page leads with what the owner wants from it and no table or toolbar overflows.

**Architecture:** Shared pieces first (KPI strip, table classes, range in the URL), then one ticket per page. The attention rules are one pure function; the pages only render. One small backend addition filters transactions by import; the imports list already exists.

**Tech Stack:** React 19, TypeScript, Vite, Tailwind v4 tokens, TanStack Query, react-router, Base UI (`Menu`, `Select`), Recharts through `components/LazyChart.tsx`; Go `net/http` and SQLite for the backend ticket.

**Spec:** `docs/superpowers/specs/2026-10-05-ui-v2-core-pages-design.md`. Visual reference: `docs/superpowers/specs/2026-10-05-ui-v2-core-pages-mockup.html`. Executors read both and open the mockup in a browser.

## Global Constraints

- One task is one GLG ticket, one PR, one agent, branch `GLG-<n>-short-slug`. The ticket's "Depends on" is the merge order.
- No AI attribution in commits, PRs or files.
- No new dependency.
- Money is decimal: `Number()` only for sorting, shares and plot positions; every printed amount comes from the exact string through the existing formatters.
- Every table that holds user data is filtered by `user_id` (backend ticket).
- Semantic colour and shadow tokens only (ADR 0023); the `card` utility for cards; `ui/Select` and `ui/Menu`, never a native `<select>`.
- Every new UI string has a `tr` and an `en` key.
- Privacy mode masks every amount a ticket adds.
- Desktop first: the 1280 px layout is the reference; below `md` content stacks and must not scroll sideways at 375 px.
- A doc that a ticket makes false is corrected in that ticket (dated note for an ADR).
- Browser checks use synthetic data on the agent's own ports and host name, in headless Chrome over the DevTools protocol (recipe named in each ticket); never the owner's dev instance, database or `.env`.
- Checks from the repo root, all exit 0: `make lint`, `make test`, `make build-frontend`; plus `make swagger` for task 3.

## Review Focus

Inputs the spec implies and a happy-path test would miss. Each has a test in the task named.

1. **A portfolio with cash only, or with every position closed** (tasks 4, 5, 6): no division by zero in shares, no "Bugün" group, an empty positions table that says so.
2. **Missing data inside the attention rules** (task 4): a position with no price has no market value; it must appear under Veri and must not count toward a concentration share or the movers.
3. **Two currencies** (tasks 4, 5): shares and movers use the reporting-currency fields; a position whose reporting value is missing (no FX rate) is left out of the denominator, as `Allocation` does today.
4. **A stale URL** (task 2): `?range=XYZ`, `?from=2026-13-01`, `from` after `to`: the default range is used and the page does not crash.
5. **Another user's import id** (task 3): `import_id` of an import that belongs to another user returns an empty list, not an error that reveals it exists.

---

### Task 1: Docs to main; KPI and table primitives out of `SummaryPage`

**Files:**

- Create: `frontend/src/components/ui/Kpi.tsx`, `frontend/src/components/ui/table.ts`
- Modify: `frontend/src/pages/SummaryPage.tsx`, `frontend/src/pages/HomePage.tsx`, `frontend/src/pages/DividendsPage.tsx`, `frontend/src/pages/PositionPage.tsx`, `docs/ROADMAP.md`
- Carry: the spec, this plan and the mockup (cherry-pick the commit the ticket names)

**Interfaces:**

- Produces from `components/ui/Kpi.tsx`: `KpiStrip({ columns, children })` (the hairline grid card), `KpiCell({ label, children, hint? })`, `FitFigure` (moved verbatim), and the class constants `KPI_LABEL`, `KPI_FIGURE` under their current names.
- Produces from `components/ui/table.ts`: `TABLE`, `TH`, `TD`, `TD_NUM`, `ROW_40`, `ROW_44`, `GROUP_ROW` class strings. Introduced here, used by tasks 5, 8, 10, 11.

- [ ] **Step 1:** Move `FitFigure`, `KPI_CARD`, `KPI_LABEL`, `KPI_FIGURE` out of `SummaryPage.tsx` into `ui/Kpi.tsx`; add `KpiStrip` and `KpiCell` as thin wrappers over the same classes. Point `HomePage`, `DividendsPage` and `SummaryPage` at the new file.
- [ ] **Step 2:** `PositionPage` uses `KpiStrip`/`KpiCell`/`FitFigure` instead of its own cell classes. No layout change beyond that.
- [ ] **Step 3:** Add `ui/table.ts` with the class strings; no page uses them yet.
- [ ] **Step 4:** `docs/ROADMAP.md`: "Phase 26 — UI v2: core pages (epic key)" under "In progress" with a link to the spec; remove sub-project 4 from "Next".
- [ ] **Step 5:** `make lint`, `make test`, `make build-frontend`. Expected: exit 0, the same number of tests. Commit. The PR says there is no behaviour change apart from the position page's KPI figures now shrinking to fit.

### Task 2: Range in the URL; range picker as a select on a phone

**Files:**

- Create: `frontend/src/lib/useRange.ts`, `frontend/src/lib/useRange.test.tsx`
- Modify: `frontend/src/components/RangePicker.tsx` and its test, `frontend/src/pages/SummaryPage.tsx`, `frontend/src/pages/PositionPage.tsx`, `frontend/src/components/Sidebar.tsx` (portfolio links keep the range)

**Interfaces:**

- Produces: `useRange(): [Range, (next: Range) => void]`, where `Range` is the type `RangePicker` uses today. It reads and writes `?range=<preset>` or `?from=YYYY-MM-DD&to=YYYY-MM-DD` with `replace: true`; no parameter means the default (3A).

- [ ] **Step 1: Failing tests** for `useRange`: default with no parameter; a preset; a custom range; `?range=XYZ`, an invalid date and `from` after `to` each give the default (Review Focus 4); setting the default removes the parameters.
- [ ] **Step 2:** Implement the hook; `SummaryPage` and `PositionPage` use it in place of `useState`.
- [ ] **Step 3: Failing test** for `RangePicker`: below `sm` it renders a `ui/Select` with the same options and the same accessible name; from `sm` up the chips as today. (Test both branches through the component's own breakpoint classes or a `matchMedia` stub, whichever the component uses.)
- [ ] **Step 4:** Implement. A link from Özet to a position page and the sidebar's portfolio links carry the current range parameters.
- [ ] **Step 5:** Browser check at 375 px: no sideways scroll in the chart card on Özet and the position page; choose 1Y on Özet, open a position, go back: still 1Y; reload: still 1Y.
- [ ] **Step 6:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 3: Backend: filter transactions by import

**Files:**

- Modify: `backend/internal/transaction/handlers.go` and its repository (the filter), their tests, `docs/adr/0006-broker-statement-import.md`
- Modify: `frontend/src/api/transactions.ts` (the query option and the CSV link builder; no page uses them yet)

**Interfaces:**

- Consumes: the existing `GET /api/imports` and `useImports()` in `frontend/src/api/imports.ts` (every import of the user: `id`, `broker`, `portfolio_id`, `period_start`, `period_end`, `currency`, `transaction_count`, `created_at`). No new list endpoint.
- Produces: `import_id` query parameter on `GET /api/portfolios/{id}/transactions` and `/transactions.csv`: a positive integer for one import, `none` for rows with no import; anything else is a 400 like the other malformed parameters.
- Produces (frontend): `importId?: number | 'none'` in the transactions query options and the CSV link builder.

- [ ] **Step 1: Failing Go tests:** `import_id=<n>` returns that import's rows; `import_id=none` returns hand-entered rows; another user's import id returns an empty list (Review Focus 5); `import_id=abc` is a 400; the CSV honours the filter; the cursor still pages correctly with the filter on.
- [ ] **Step 2:** Implement in the repository (portable SQL, `user_id` in every query) and the handlers; update the swag annotations.
- [ ] **Step 3:** Check how a CSV import is recorded (`broker` and `currency` values in `GET /api/imports`) and say in the PR what name the frontend should show for it.
- [ ] **Step 4:** Frontend query option and CSV link; ADR 0006 dated note.
- [ ] **Step 5:** `make lint`, `make test`, `make build-frontend`, `make swagger`. Commit.

### Task 4: Attention rules and card

**Files:**

- Create: `frontend/src/lib/attention.ts`, `frontend/src/lib/attention.test.ts`, `frontend/src/components/AttentionCard.tsx`, `frontend/src/components/AttentionCard.test.tsx`
- Modify: `frontend/src/i18n/strings.ts`

**Interfaces:**

- Produces:
  - `ATTENTION = { staleManualDays: 7, positionShare: 0.25, cashShare: 0.4, currencyShare: 0.5, movers: 2 } as const`
  - `type AttentionItem` = a discriminated union on `kind`: `'missing-rate' | 'missing-price' | 'stale-manual-prices' | 'estimated-benchmark' | 'dividend' | 'mover' | 'concentration'`, each carrying the fields its line prints (symbol, instrument id, exact decimal strings for amounts, a number for a share, the portfolio name when given).
  - `attentionItems(input: { summary: Summary; expected: ExpectedDividend[]; estimatedFrom?: string | null; today: string; portfolioName?: string }): AttentionItem[]`, in display order.
  - `AttentionCard({ items, onAddDividend, onDismissDividend, onEditPrice })`. Rendering only.

- [ ] **Step 1: Failing unit tests**, one per rule and edge: a missing rate with its position count; a position without a price; three manual prices older than 7 days give one item with count 3, one of exactly 7 days gives none; an estimated benchmark; each expected dividend; movers are the two largest absolute `day_change_reporting`, none when all are null or zero; a position at exactly 25% gives nothing and at 25.01% one item; cash 40%; one foreign currency 50%; Review Focus 1, 2 and 3; an empty portfolio gives `[]`.
- [ ] **Step 2:** Implement `attentionItems`. Shares follow `Allocation`'s rule for the denominator (positions without a reporting value are left out).
- [ ] **Step 3: Failing component tests:** groups render in order with their headings; an empty group is absent; no items shows `attention.allClear`; the header count is Veri plus Temettü items; Ekle and Yoksay call their handlers with the dividend; amounts are masked in privacy mode.
- [ ] **Step 4:** Implement `AttentionCard` with the mockup's look. Strings in `tr` and `en`.
- [ ] **Step 5:** `make lint`, `make test`, `make build-frontend`. Commit. The card is not on a page yet; the PR says so.

### Task 5: Compact positions table

**Files:**

- Create: `frontend/src/components/PositionsTable.tsx`, `frontend/src/components/PositionsTable.test.tsx`
- Modify: `frontend/src/pages/SummaryPage.tsx` and its test (the positions code moves out), `frontend/src/i18n/strings.ts`, `docs/adr/0021-product-and-ui-decisions.md` (where it describes the Al and Sat buttons in rows as current)

**Interfaces:**

- Consumes: `ui/table.ts`, `ui/Menu`.
- Produces: `PositionsTable({ positions, total, cash, readOnly, onTrade, onEditPrice, onRemovePrice })`. `readOnly` is the Tüm portföyler mode (no row menu, symbols are plain text). It owns the sort state and the Açık/Kapalı switch.

- [ ] **Step 1: Failing tests:** the columns of the spec in order; a row is one line (assert the row class `ROW_40` and that Al/Sat are not in the row); Ağırlık is the reporting value over the total including cash; clicking the Değer header toggles the order, clicking K/Z sorts by it, `aria-sort` follows; the row menu holds Al, Sat, Fiyat gir, and Elle fiyatı kaldır only for a manual price, and calls the handlers; a manual or stale price shows its pill with the existing tooltip text; the Kapalı switch shows closed positions; `readOnly` has no menu; the footer totals; Review Focus 1 (no positions) and 3 (a position without a reporting value shows "—" for Ağırlık); privacy masks amounts.
- [ ] **Step 2:** Move `PositionsCard` and its row components out of `SummaryPage.tsx` into `PositionsTable.tsx`, then rebuild the desktop table to the spec. Below `md` keep today's list, drop the manual-price line, and add bottom padding of the floating button's height.
- [ ] **Step 3:** The inline price editor opens from the menu's Fiyat gir in a small popover or dialog anchored to the row (reuse the existing editor's fields and mutation).
- [ ] **Step 4:** Browser check with 15 positions: every row 40 px at 1280 px; no sideways scroll at 1280 px and 375 px; the full names of the seed data fit or truncate with a title; sort and menu by keyboard.
- [ ] **Step 5:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 6: Özet layout A

**Files:**

- Create: `frontend/src/components/ReturnCard.tsx`, `frontend/src/components/ReturnCard.test.tsx`
- Modify: `frontend/src/pages/SummaryPage.tsx` and its test, `frontend/src/components/Allocation.tsx` (card header like the others), `frontend/src/i18n/strings.ts`, `docs/adr/0023-ui-design-system-v2.md`, `docs/adr/0021-product-and-ui-decisions.md`

**Interfaces:**

- Consumes: `useRange`, `AttentionCard`/`attentionItems`, `PositionsTable`, `KpiStrip`, the expected-dividends query and its add and dismiss mutations.
- Produces: `ReturnCard({ history, range, onRange, benchmark, onBenchmark, method, onMethod, … })`: headline return, amount, benchmark pill or the no-data text, controls, chart, notices.

- [ ] **Step 1: Failing tests:** `ReturnCard` shows the chosen method's return as the headline with `rangeDelta` beside it; with a benchmark the pill shows the benchmark's return and the difference with the sign's colour; without one no pill; a benchmark with no values shows `chart.benchmark.noData`; the three controls are reachable by name. `SummaryPage`: the order of the sections; the KPI strip has five cells and unrealized P/L appears once on the page; the attention card receives the expected dividends and adding one calls the mutation; Dağılım and İstatistikler render side by side from `lg` (class assertion); Review Focus 1 (cash-only portfolio renders without the Bugün group and with an empty positions message).
- [ ] **Step 2:** Implement `ReturnCard` (moving the chart header and controls out of `SummaryPage.tsx`) and the new page composition. The sub-lines of the old hero become `hint` tooltips on the KPI cells.
- [ ] **Step 3:** Below `md` stack in the spec's order.
- [ ] **Step 4:** Browser check at 1280×900 with the 15-position seed: the bottom of the second position row is above y=900, so it is visible without scrolling; the chart card's header is one row with and without a benchmark, in `tr` and `en`; page height reported before and after. At 375 px: no sideways scroll; the stack order.
- [ ] **Step 5:** ADR 0023 dated note (layout A, attention rules and thresholds, each number once); ADR 0021 notes where needed.
- [ ] **Step 6:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 7: Tüm portföyler; home page removed

**Files:**

- Create: `frontend/src/components/PortfoliosTable.tsx`, `frontend/src/components/PortfoliosTable.test.tsx`
- Delete: `frontend/src/pages/HomePage.tsx` and its test
- Modify: `frontend/src/App.tsx` and its test, `frontend/src/pages/SummaryPage.tsx`, `frontend/src/components/Sidebar.tsx` (brand link), `frontend/src/components/AppShell.tsx` (page title for `/` goes), `frontend/src/components/PortfolioHeader.tsx` (the `all` scope: no tab row, a "Yeni portföy" button), `docs/adr/0014-all-portfolios-view.md`, `docs/adr/0023-ui-design-system-v2.md`, the 2026-10-04 spec's "`HomePage` stays" section

**Interfaces:**

- Produces: `PortfoliosTable({ portfolios, total, range, method })`: one row per portfolio; each row runs its own history query (enabled after the list has loaded) for the return and the sparkline.

- [ ] **Step 1: Failing tests:** `/` redirects to `/portfolios/all`; the brand link points there; with no portfolio the page shows the create-portfolio empty state; `PortfoliosTable` rows show name, value, share, return of the chosen method, day change, and link to the portfolio; a row whose history fails shows "—" for the return and the rest of the row; shares sum to 100% within rounding; privacy masks amounts; the `all` scope renders no tab row; attention items on this page carry the portfolio name.
- [ ] **Step 2:** Implement; the per-portfolio day change is the sum of that portfolio's `day_change_reporting` (from one summary query per portfolio, or from the list if the ticket finds the field there; say which in the PR).
- [ ] **Step 3:** Delete `HomePage`; move what the empty state needs.
- [ ] **Step 4:** Browser check: `/` lands on Tüm portföyler; three portfolios show three rows with sparklines; clicking a row opens it; at 375 px the table fits or becomes a list without sideways scroll.
- [ ] **Step 5:** Docs: ADR 0014 and 0023 dated notes; the 2026-10-04 spec's note.
- [ ] **Step 6:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 8: İşlemler page

**Files:**

- Modify: `frontend/src/pages/TransactionsPage.tsx` and its test, `frontend/src/pages/ImportPage.tsx` (navigate to İşlemler with the import id after a commit, if it does not already), `frontend/src/i18n/strings.ts`, `docs/adr/0021-product-and-ui-decisions.md` (toolbar description)
- Create: `frontend/src/components/DateRangeFilter.tsx` and its test (the "Tarih" popover)

**Interfaces:**

- Consumes: `useImports`, the existing `useImports`, the `importId` query option (task 3), `ui/table.ts`.
- Produces: URL parameter `?import=<id>|none` next to the existing `type`, `instrument`, `symbol`, `from`, `to`.

- [ ] **Step 1: Failing tests:** the toolbar is one flex row holding search, type, Tarih, Kaynak, count and CSV (assert order and that CSV is inside it); the Tarih popover sets and clears `from` and `to` and shows the active range on its button; Kaynak lists "Tümü", "Elle" and one entry per import named broker and period, and sets `?import`; the CSV link carries `import_id`; the table's columns of the spec; quantity and price share a cell; commission and withholding share "Kesinti" and it is empty when both are zero; a note renders after the name with a `title`; the Kaynak cell shows "Elle" or the import's pill; the three action icons are in every row without hover; arriving with `?import=<id>` and router state from an import shows the banner with the import's name and count, and "Sadece bunları göster" keeps the filter while ✕ clears it; the empty state is a card.
- [ ] **Step 2:** Implement. Rows use `ROW_44`. Below `md`: one "Filtrele" button with the active count opens the filters in a sheet (`useModal`); list rows keep the amount on the first line.
- [ ] **Step 3:** Browser check with 68 transactions including one import: table width equals the card's inner width at 1280 px (`scrollWidth === clientWidth`); every data row 44 px; action icons inside the card; the toolbar's height is one row; filter by the import and by "Elle"; 375 px without sideways scroll and the filter sheet.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 9: "Ekle ve yeni" in the add dialog

**Files:**

- Modify: `frontend/src/components/TransactionDialog.tsx` and its test, `frontend/src/i18n/strings.ts`

- [ ] **Step 1: Failing tests:** an "Ekle ve yeni" button beside "Ekle" in add mode, absent in edit mode; pressing it saves, keeps the dialog open, keeps the type and the date, clears instrument, quantity, price, commission and note, and focuses the first field; ⌘↵ does the same as today; the ⌘↵ hint is not rendered when the device has no fine pointer (`matchMedia('(pointer: fine)')`).
- [ ] **Step 2:** Implement on top of the existing "add and continue" path.
- [ ] **Step 3:** Browser check: add three trades in a row by keyboard only; on a 375 px touch emulation the hint is gone and both buttons fit.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 10: Position page

**Files:**

- Modify: `frontend/src/pages/PositionPage.tsx` and its test, `frontend/src/components/PortfolioHeader.tsx` (prefilled add), `frontend/src/i18n/strings.ts`

**Interfaces:**

- Consumes: `useRange`, `KpiStrip`, `ui/table.ts`, the dividends timeline query (for the instrument's dividends), the price editor and trade dialog used by `PositionsTable`.

- [ ] **Step 1:** Read how `realized_pl` is computed in the backend and state in the PR whether it contains dividends. "Toplam sonuç" must count dividends exactly once.
- [ ] **Step 2: Failing tests:** the title row holds the "Özet" link, symbol, name, market, last price and the day-change pill, and the page has one h1; Al, Sat and Fiyat gir open their dialogs prefilled; the KPI strip's six cells with Toplam sonuç equal to unrealized plus realized plus dividends in exact decimal arithmetic; the weight is the position's reporting value over the portfolio total; a closed position shows three cells; one chart card with a switch whose two states render the price chart and the value chart; the range comes from the URL; the header's İşlem ekle opens with this instrument; the transactions use the compact table; privacy masks amounts.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Browser check: at 1280 px the first KPI figure's y position (before and after); the chart switch; a closed-and-reopened position; 375 px without sideways scroll.
- [ ] **Step 5:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 11: Temettü page

**Files:**

- Modify: `frontend/src/pages/DividendsPage.tsx` and its test, `frontend/src/components/ExpectedDividends.tsx` and its test, `frontend/src/components/charts/BarChart.tsx` and its test, `frontend/src/i18n/strings.ts`

- [ ] **Step 1: Failing tests:** with expected dividends the confirm card is the first card and is open; with none it is absent; the KPI strip's cells, with Maliyete göre verim equal to the last 12 months' net over the open positions' reporting cost and "—" when the cost is zero; a bar for a small non-zero value is at least 3 px and a zero bar 1 px (through the props given to Recharts); the table's headers ("Ödeme sayısı", "Pay", "Son ödeme"), the date with its year, the share column, a foreign dividend's own-currency amount when the API gives it (if it does not, leave it out and say so in the PR; do not add a backend change); the empty state is a card.
- [ ] **Step 2:** Implement. The table fits at 375 px (drop Pay there if needed).
- [ ] **Step 3:** Browser check at 1280 px and 375 px, with and without expected dividends.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 12: Shell polish

**Files:**

- Modify: `frontend/src/components/ui/Toast.tsx` (or where toasts are placed) and its test, `frontend/src/components/PortfolioHeader.tsx` and its test, `frontend/src/components/AppShell.tsx`

- [ ] **Step 1: Failing tests:** below `lg` the page's own h1 is visually hidden (`sr-only`) while the top bar shows the title, and from `lg` it is visible; the inline rename input takes the width of the title row up to a maximum, not a fixed 208 px; the toast container is positioned inside the content column and above the floating add button's area below `md` (class or style assertion).
- [ ] **Step 2:** Implement.
- [ ] **Step 3:** Browser check: at 1280 px a toast is centred on the content column, not the viewport; at 375 px a long toast does not touch the floating button; the portfolio name appears once below `lg`.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 13: Docs: move Phase 26 to HISTORY

Runs after the orchestrator's close-out browser check of combined `main` and after any ticket that check files has merged.

**Files:**

- Modify: `docs/HISTORY.md`, `docs/ROADMAP.md`

- [ ] **Step 1:** `docs/HISTORY.md`: "Phase 26 — UI v2: core pages" after Phase 25, in the format of its neighbours, one bullet per Done child of the epic, each described from its merged PR.
- [ ] **Step 2:** `docs/ROADMAP.md`: remove Phase 26 from "In progress".
- [ ] **Step 3:** `make lint`, `make test`, `make build-frontend`. Commit.
