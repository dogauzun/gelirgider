# UI v2 Secondary Pages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give İçe aktar a visible three-step flow with the history in view and a simpler review, and bring Hesap, Yönetim and the signed-out pages to the same column, headings, cards and controls as the portfolio pages.

**Architecture:** Two shared pieces first (`ui/Checkbox`, `PageHeader`). The import page is split into components before any behaviour changes, then changed one step at a time. The other pages are layout work over unchanged logic.

**Tech Stack:** React 19, TypeScript, Tailwind v4 tokens, Base UI (`Checkbox`, `Select`, `Menu`), TanStack Query, Vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-10-05-ui-v2-secondary-pages-design.md`. Visual reference: `docs/superpowers/specs/2026-10-05-ui-v2-secondary-pages-mockup.html`. Read ADR 0006 before any import task.

## Global Constraints

- One task is one GLG ticket, one PR, one agent. The ticket's "Depends on" is the merge order; tasks 2 to 5 share the import files and never run side by side.
- No AI attribution in commits, PRs or files.
- No new dependency (`@base-ui/react` is already installed). No backend, API or schema change.
- No import rule changes: parsing, matching, duplicates, which rows can be selected, the confirmations, what a commit writes.
- Every control on Hesap and Yönetim keeps its behaviour, its endpoint and its confirmation.
- Semantic tokens only; the `card` utility; `ui/Select`, `ui/Menu`, `ui/Checkbox`; the table classes of `ui/table.ts`.
- Every new UI string has a `tr` and an `en` key. Privacy mode masks amounts as before.
- Real statements are personal data: tests and browser checks use synthetic CSV files and fixtures only.
- A doc that a ticket makes false is corrected in that ticket.
- Checks from the repo root, all exit 0: `make lint`, `make test`, `make build-frontend`.

## Review Focus

1. **Dropping the wrong thing** (task 3): a folder, an empty file, ten files, a file type the chosen broker refuses, a second drop while a preview is running: a clear message or a queued batch, never a stuck page.
2. **Leaving a review** (tasks 4, 5): Vazgeç, a tab change or a browser Back with edits in the review still asks before dropping them, as today.
3. **A batch where every file fails or is skipped** (task 5): the end state lists the reasons and offers step 1; no empty "Bitti".
4. **Keyboard and screen readers on the new checkbox** (task 1): Space toggles, the label is announced, "select all" has a mixed state when some rows are selected.
5. **A source that is switched off** (task 7): it never shows the stale warning, however old its last success.

---

### Task 1: Docs to main; `ui/Checkbox` and `PageHeader`

**Files:**

- Create: `frontend/src/components/ui/Checkbox.tsx` and its test, `frontend/src/components/PageHeader.tsx` and its test
- Modify: every component with `type="checkbox"` (`git grep -n 'type="checkbox"' frontend/src`), `frontend/src/conventions.test.ts`, `docs/ROADMAP.md`, `docs/adr/0023-ui-design-system-v2.md`
- Carry: the spec, this plan and the mockup (cherry-pick the commit the ticket names)

**Interfaces:**

- Produces: `Checkbox({ checked, onChange, indeterminate?, disabled?, label?, 'aria-label'? })` on Base UI's checkbox. With `label` it renders the label beside the box and the whole row is the 44 px tap target.
- Produces: `PageHeader({ title, description?, actions? })`: a 20 px h1 that is `sr-only` below `lg` (the top bar shows the title there), an optional muted line, actions on the right.

- [ ] **Step 1: Failing tests** for `Checkbox`: role and checked state; click and Space toggle and call `onChange` with the new value; `indeterminate` gives `aria-checked="mixed"`; disabled does not toggle; the label names it (Review Focus 4). For `PageHeader`: the h1's classes at both widths, the actions slot.
- [ ] **Step 2:** Implement both. Replace every native checkbox; tests that queried `checkbox` by role keep working without change.
- [ ] **Step 3:** `conventions.test.ts`: ban `type="checkbox"` in components.
- [ ] **Step 4:** Browser check on İçe aktar (a review with rows) and Yönetim: checkboxes by mouse and keyboard, light and dark.
- [ ] **Step 5:** `docs/ROADMAP.md`: "Phase 29 — UI v2: secondary pages (epic key)" under "In progress"; remove sub-project 5 from "Next". ADR 0023 dated note.
- [ ] **Step 6:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 2: Split `ImportPage.tsx` into components

**Files:**

- Create: `frontend/src/components/import/` with one file per inline component of `pages/ImportPage.tsx`: `ImportQueue`, `PreviewTable` (row and list item), `InstrumentPicker`, `HoldingsPanel` (with `DifferenceCell`), `FilePicker`, `PastImports`, and a `types.ts` or helper file for what they share
- Modify: `frontend/src/pages/ImportPage.tsx` and `ImportPage.test.tsx`

- [ ] **Step 1:** Record the frontend test count and `ImportPage.test.tsx`'s test names.
- [ ] **Step 2:** Move each inline component and its helpers to its own file, verbatim. `ImportPage.tsx` keeps the page's state and composition. No rename of props, no logic change, no JSX change.
- [ ] **Step 3:** Move the tests that exercise one component alone next to that component only if they need no change beyond imports; otherwise leave them in `ImportPage.test.tsx`.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Expected: the same test names and count pass. Commit. The PR says there is no behaviour change and gives the line counts before and after.

### Task 3: İçe aktar step 1: steps, drop zone, history beside

**Files:**

- Create: `frontend/src/components/import/ImportSteps.tsx` and its test, `frontend/src/components/import/DropZone.tsx` and its test
- Modify: `frontend/src/pages/ImportPage.tsx` and its test, `frontend/src/components/import/FilePicker.tsx`, `frontend/src/components/import/PastImports.tsx` and their tests, `frontend/src/i18n/strings.ts`, `docs/adr/0006-broker-statement-import.md`

**Interfaces:**

- Produces: `ImportSteps({ step: 1 | 2 | 3, batch?: { index: number; total: number } })`.
- Produces: `DropZone({ accept, multiple, disabled, onFiles })`: a button-like region that opens the file dialog on click, Enter and Space and takes dropped files.

- [ ] **Step 1: Failing tests:** `ImportSteps` marks the current step, shows a check on finished ones and the batch counter on step 2. `DropZone`: click and Enter open the input; a drop calls `onFiles` with the files; a dragged-over state class; disabled ignores both; files whose type `accept` refuses are reported, not passed on (Review Focus 1). Page: the two cards render side by side from `lg` with upload first; choosing files starts the preview without an "Önizle" press; a parse error shows in the upload card; `PastImports` shows the import's date in the user's time zone, "İşlemlerde gör" links to İşlemler with `?import=<id>`, "Geri al" keeps its confirmation; the empty history line.
- [ ] **Step 2:** Implement. The broker select moves into the card header; the CSV column list goes behind a "CSV biçimi" disclosure.
- [ ] **Step 3:** Browser check at 1280 px and 375 px, light and dark: drop one CSV, drop three, drop a wrong type; the history link lands on İşlemler filtered; undo works.
- [ ] **Step 4:** ADR 0006 dated note if it describes the two-step upload. `make lint`, `make test`, `make build-frontend`. Commit.

### Task 4: İçe aktar step 2: review

**Files:**

- Modify: `frontend/src/pages/ImportPage.tsx` and its test, `frontend/src/components/import/PreviewTable.tsx` and its test, `frontend/src/components/import/InstrumentPicker.tsx`, `frontend/src/i18n/strings.ts`

**Interfaces:**

- Consumes: `ui/table.ts`, `ui/Checkbox`.
- Produces: `problemRows(rows)` (a pure helper beside the table): the rows that cannot be read plus the rows that need a fix, each once.

- [ ] **Step 1: Failing tests:** `problemRows` counts a row that is both unreadable and unfixed once; the "Tümü N | Sorunlu N" switch shows the counts, is on "Sorunlu" on entry when there are problems and on "Tümü" otherwise, and filters the table; the two stacked notices are gone while file-level warnings remain; rows are in date order with an unreadable row at its date and an undated one last; the raw line opens on demand; the table's columns in order with quantity and price in one cell and commission and withholding in "Kesinti"; the footer is a fixed bar holding the counts, "+ Satır ekle" and the buttons, and the table has the bar's height as bottom padding; every existing rule test of the review still passes unchanged (Review Focus 2).
- [ ] **Step 2:** Implement. The instrument picker gets a minimum width that fits its placeholder in `tr` and `en`.
- [ ] **Step 3:** Browser check with a CSV that has an unknown symbol and unreadable rows: at 1280 px the table's `scrollWidth` equals its `clientWidth`; the bar stays in view while scrolling; the filter; 375 px list with the same filter and bar; light and dark.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Commit. The PR says which parts (holdings panel, opening rows) were covered by component tests only, because no synthetic PDF exists.

### Task 5: İçe aktar batch and "Bitti"

**Files:**

- Modify: `frontend/src/pages/ImportPage.tsx` and its test, `frontend/src/components/import/ImportQueue.tsx` and its test, `frontend/src/i18n/strings.ts`

- [ ] **Step 1: Failing tests:** in a batch the steps show "İncele · Ekstre n / m"; the queue is one folded line naming the current file and unfolds to today's list; at the end step 3 lists each imported statement with its row count and an "İşlemlerde gör" link to its filter, each skipped or failed file with its reason, and "Başka ekstre yükle" returning to step 1; a batch where nothing was imported shows the reasons and no success line (Review Focus 3); leaving mid-batch still asks (Review Focus 2); a single statement still ends on İşlemler.
- [ ] **Step 2:** Implement.
- [ ] **Step 3:** Browser check: five CSV files (three good, one copy, one broken) through to "Bitti"; every link; 375 px.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 6: Hesap

**Files:**

- Modify: `frontend/src/pages/AccountPage.tsx` and its test, `frontend/src/i18n/strings.ts`, `docs/adr/0021-product-and-ui-decisions.md` (if it describes the page's layout)

- [ ] **Step 1: Failing tests:** `PageHeader` with the title; the content sits in the portfolio pages' wrapper (class assertion); one "Tercihler" card with three rows (reporting currency, language, appearance) and one "Şifre" card; card titles use the shared 14 px title class; every existing behaviour test passes unchanged.
- [ ] **Step 2:** Implement: two cards side by side from `lg`, stacked below.
- [ ] **Step 3:** Browser check: the left offset equals a portfolio page's at 1280 px; 375 px; change each preference and the password; light and dark.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 7: Yönetim

**Files:**

- Modify: `frontend/src/pages/AdminPage.tsx`, `frontend/src/components/admin/SourcesSection.tsx`, `ProvidersSection.tsx`, `InstrumentsSection.tsx`, `UsersSection.tsx`, `RegistrationToggle.tsx` and their tests, `frontend/src/i18n/strings.ts`, `docs/adr/0021-product-and-ui-decisions.md` (if it describes the page's layout), `docs/adr/0023-ui-design-system-v2.md`

**Interfaces:**

- Produces: `isStale(source, now)` beside `SourcesSection`: true when the source is on and has never succeeded or last succeeded more than 48 hours before `now`.

- [ ] **Step 1: Failing tests:** `isStale` at exactly 48 hours (false), one minute more (true), never succeeded and on (true), off (false, Review Focus 5). The sources render as one table with the columns of the spec and one row per source; a stale source's last success is a warning pill; each row's enable, time, "Şimdi çalıştır" and "Geçmişi doldur" call the same endpoints as before; the options above the table are unchanged; below `md` the sources are a list. The section links are one row with a scrolling class and 44 px targets. The page uses `PageHeader` and the portfolio pages' wrapper; card titles are 14 px; the users table uses the table classes.
- [ ] **Step 2:** Implement.
- [ ] **Step 3:** Browser check: page height at 1280 px before and after; at 375 px the gutter is 24 px and the section links are one row; every source action once against the agent's own backend with every outside source switched off or failing harmlessly; light and dark.
- [ ] **Step 4:** ADR notes. `make lint`, `make test`, `make build-frontend`. Commit.

### Task 8: Signed-out pages and setup

**Files:**

- Create: `frontend/src/components/SignedOutFrame.tsx` and its test (brand above, a slot, the language switch below), unless `AuthForm.tsx` already holds a frame that can be reused as is
- Modify: `frontend/src/components/AuthForm.tsx`, `frontend/src/pages/LoginPage.tsx`, `RegisterPage.tsx`, `SetupPage.tsx`, `NotFoundPage.tsx` and their tests, `deploy/README.md` and `deploy/RUNBOOK.md` if their first-run text names moved controls

- [ ] **Step 1: Failing tests:** login, register, setup step 1 and the signed-out not-found page render inside the same frame with the brand and the language switch, and their h1s share one size class; the signed-in not-found page is unchanged (it sits in the shell); setup step 2 keeps "Atla" and "Bitir" in a bar with the fixed-to-bottom class and its content has the bar's height as bottom padding; setup step 2 renders the sources table of task 7.
- [ ] **Step 2:** Implement.
- [ ] **Step 3:** Browser check with an empty database (the setup token from the agent's own backend log): both setup steps at 1280 px and 375 px; login, register and a signed-out unknown URL; light and dark.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Commit.

### Task 9: Docs: move Phase 29 to HISTORY

Runs after the orchestrator's close-out browser check of combined `main`.

- [ ] **Step 1:** `docs/HISTORY.md`: "Phase 29 — UI v2: secondary pages" after Phase 28, one bullet per Done child of the epic, each described from its merged PR.
- [ ] **Step 2:** `docs/ROADMAP.md`: remove Phase 29 from "In progress"; with it the last UI v2 item leaves "Next"; use the file's wording for an empty section.
- [ ] **Step 3:** ADR 0023: a dated note that the five sub-projects have shipped, with the phase numbers.
- [ ] **Step 4:** `make lint`, `make test`, `make build-frontend`. Commit.
