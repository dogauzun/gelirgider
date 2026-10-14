# UI v2: design language and app shell

Date: 2026-10-04
Status: design approved in brainstorming; awaiting owner review of this spec
Visual reference: [2026-10-04-ui-v2-mockup.html](2026-10-04-ui-v2-mockup.html)
(open in a browser; it has a light/dark switch and uses mock data)

## Why

The owner finds the app plain and amateur-looking. Two UI rounds have
shipped: Phase 7 "Defter" (ADR 0005) and the Phase 20 audit fixes. Both
started from an agent's audit, and Defter is flat by design: warm paper,
hairline cards, no shadows, one accent. The complaint is about that
direction, not about bugs in it.

The owner chose, in this order:

1. The problem is visual design, not information design or flows.
2. Target look: crisp SaaS (Linear, Stripe Dashboard, Vercel).
3. Depth of change: full redesign, including the app shell and the charts.
4. Dependencies: headless primitives and a chart library are allowed.
5. Visual language B "soft depth": Inter, a tinted ground, white cards that
   float on layered shadows, pill chips, a filled chart.
6. Shell 2: a sidebar that lists the portfolios with their values; a
   portfolio's pages are tabs in the content; a drawer on phones.
7. Accent blue. Dark mode approved as shown in the mockup.
8. Migration: token-first, in place. `main` always works; no flag, no
   parallel tree.

## Scope

A full redesign is too large for one spec. It is split into five
sub-projects, each with its own spec, plan and ticket wave:

1. **Design language**: tokens, depth, shape, type, primitives.
2. **App shell**: sidebar, page header, phone drawer.
3. Charts (Recharts): line, bar, allocation.
4. Core pages: Tüm portföyler, Özet, İşlemler, position, Temettü.
5. Secondary pages: import, account, admin, auth, setup.

**This spec covers 1 and 2.** Sub-projects 3 to 5 are out of scope and
start after this one ships.

Everything here is frontend and docs. No backend change, no API change, no
schema change.

### Out of scope

- Page layouts. Pages keep their Defter-era layouts inside the new skin and
  shell until sub-projects 4 and 5. The owner accepted this mixed period;
  nothing is deployed.
- Chart rewrites and the Recharts dependency (sub-project 3). The decision to
  allow a chart library is recorded now, in ADR 0023.
- Instrument avatars, hero and KPI layout changes (sub-project 4).
- Renaming tokens. `paper`, `surface`, `line`, `ink` and the rest keep their
  names; only values change.
- Type-scale tokens. The base size stays 13 px, so the inline
  `text-[13px]`-style classes (about 240 uses) stay. Add a scale when sizes
  must change app-wide.
- Collapsible sidebar, drag-to-reorder portfolios, stored portfolio colors,
  per-row menus in the sidebar.
- Visual-regression tooling.

## Design language

### Color tokens

All in `frontend/src/index.css`, in the `@theme` block and in both dark
blocks (`[data-theme='dark']` and the `prefers-color-scheme` media query,
which must stay identical). Token names do not change, so no component needs
an edit for color.

| Token         | Light                 | Dark               |
| ------------- | --------------------- | ------------------ |
| `paper`       | `#f6f8fb`             | `#0d1016`          |
| `surface`     | `#ffffff`             | `#161a22`          |
| `line`        | `#e3e8ee`             | `#272d39`          |
| `line-strong` | `#cfd7e0`             | `#353c4a`          |
| `ink`         | `#1a2233`             | `#e9ecf1`          |
| `ink-2`       | `#414c60`             | `#b9c0cc`          |
| `on-ink`      | `#ffffff`             | `#0d1016`          |
| `muted`       | `#606b7c`             | `#8791a2`          |
| `faint`       | `#828d9d`             | `#6f7888`          |
| `chip`        | `#eaeef3`             | `#232936`          |
| `scrim`       | `rgb(16 24 40 / 0.4)` | `rgb(0 0 0 / 0.6)` |
| `accent`      | `#2563eb`             | `#5c9dff`          |
| `accent-ink`  | `#1d4fd0`             | `#a9c8ff`          |
| `accent-soft` | `#eaf0fe`             | `#16233d`          |
| `accent-line` | `#c7d7fb`             | `#26406e`          |
| `gain`        | `#0a7d55`             | `#3ecf8e`          |
| `gain-soft`   | `#e1f6ec`             | `#10291f`          |
| `loss`        | `#cc2d43`             | `#f26d78`          |
| `loss-soft`   | `#fdecef`             | `#3a1c20`          |
| `loss-line`   | `#f6c5cc`             | `#5c2a31`          |
| `warn`        | `#8a5a00`             | `#e0b45a`          |
| `warn-soft`   | `#fdf4dc`             | `#2e2510`          |
| `warn-line`   | `#f0dca0`             | `#4a3a14`          |
| `plum`        | `#6941c6`             | `#b69df8`          |
| `plum-soft`   | `#f1ecfb`             | `#251d3a`          |
| `cash`        | `#c5ccd6`             | `#3b4352`          |
| `cat-1`       | `#2563eb`             | `#5c9dff`          |
| `cat-2`       | `#0e9384`             | `#3fc1b0`          |
| `cat-3`       | `#d98a1f`             | `#e5a84b`          |
| `cat-4`       | `#7a5af8`             | `#a48afb`          |
| `cat-5`       | `#e0563b`             | `#f0806a`          |
| `cat-6`       | `#667085`             | `#8a94a6`          |

`muted`, `gain` and `loss` in light mode are slightly darker than in the
first mockups, so they pass WCAG AA; the committed mockup already uses the
values above.

`on-ink` keeps its name and gains one job: it is also the text color on an
`accent` fill (the primary button). Its values already fit: white in light,
near-black in dark.

**Contrast is tested, not eyeballed.** `frontend/src/index.css.test.ts`
already checks `muted` on `paper`, `surface` and `chip` at 4.5:1 in all three
blocks. Extend it to:

- `gain` and `loss` on `surface` and on their own `-soft` background;
- `accent` on `surface`, `accent-ink` on `accent-soft`;
- `on-ink` on `accent`;
- `warn` on `warn-soft`, `plum` on `plum-soft`.

Measured for the values above: light `muted` 5.07 / 5.40 / 4.63, `gain` 5.15
on `surface` and 4.56 on `gain-soft`, `loss` 5.22 and 4.58, `accent` 5.17;
dark `muted` 5.99 / 5.48 / 4.58, `gain` 8.73, `loss` 6.00, `accent` 6.41,
`on-ink` on `accent` 7.00. The `plum` and dark `-soft` pairs were not
measured; if one fails, adjust the token until the test passes. The test is
the authority over this table. `faint` (placeholders, disabled text, axis
labels) is exempt from 4.5:1 but keeps the existing test's rule: at least
3:1 on `paper` and `surface` in every theme block, and less contrast than
`muted`. Light `faint` is `#828d9d` (3.16:1 on `paper`, 3.36:1 on
`surface`); dark `#6f7888` passes at 4.28 and 3.92. That test stays; the
tokens ticket must not delete it.

### Depth

New in v2. Three shadow tokens in `@theme`, redefined in both dark blocks
exactly as the colors are. Tailwind v4 turns `--shadow-*` into `shadow-*`
utilities.

| Token         | Used for                               | Light                                                                                               | Dark                                                                                                      |
| ------------- | -------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `shadow-card` | Resting cards                          | `0 0 0 1px rgb(16 24 40 / .05), 0 1px 2px rgb(16 24 40 / .06), 0 6px 16px -4px rgb(16 24 40 / .08)` | `0 0 0 1px rgb(255 255 255 / .07), inset 0 1px 0 rgb(255 255 255 / .04), 0 8px 20px -8px rgb(0 0 0 / .6)` |
| `shadow-ctl`  | Buttons, inputs, active tab or segment | `0 0 0 1px rgb(16 24 40 / .1), 0 1px 2px rgb(16 24 40 / .08)`                                       | `0 0 0 1px rgb(255 255 255 / .08)`                                                                        |
| `shadow-pop`  | Menus, dialogs, toasts, tooltips       | `0 0 0 1px rgb(16 24 40 / .06), 0 2px 6px rgb(16 24 40 / .06), 0 8px 24px -4px rgb(16 24 40 / .16)` | `0 0 0 1px rgb(255 255 255 / .1), 0 12px 32px -6px rgb(0 0 0 / .7)`                                       |

Shadows do not read on a dark ground, so the dark recipe is a 1 px light
ring plus a top highlight. Same token names, so components do not branch.

Cards drop `border border-line`. The 1 px ring inside `shadow-card` replaces
it. `line` stays for table rules, dividers and tab underlines.

The 19 existing uses of `shadow-sm`, `shadow-lg` and `shadow-2xl` move to
`shadow-ctl` (was `sm`) and `shadow-pop` (was `lg`, `2xl`), so no raw shadow
utility is left in components. This becomes part of the token rule.

### Shape

`--radius-card` 10 px to 12 px. `--radius-ctl` 6 px to 8 px. Pills and
badges are fully round (`rounded-full`).

### Type

Inter, variable weight axis, self-hosted under `frontend/public/fonts` with
its OFL 1.1 license file, `font-display: swap`. Two subset files with
`unicode-range`: latin and latin-ext. Turkish needs latin-ext (ğ, ş, ı, İ),
and so does the lira sign (₺, U+20BA).

- `--font-sans: 'Inter', system-ui, sans-serif`.
- `--font-mono` and IBM Plex Mono are removed. Instrument symbols become
  Inter semibold; chart axis labels become Inter. Every `font-mono` class in
  components is removed in the same ticket.
- The four IBM Plex files and their license file are deleted. The preload in
  `frontend/index.html` points at the Inter latin file.
- Tabular numerals stay on `body`.
- The ticket records the real byte size of the two files in ADR 0023. The
  Plex set was 247 KB.

The tokens ticket must check in a browser that ₺, ğ, ş, ı and İ render in
Inter and not in the fallback.

The same ticket updates the colors that live outside `index.css`: the two
`theme-color` metas in `frontend/index.html` (`surface`: `#ffffff` light,
`#161a22` dark) and `background_color` (`paper`, `#f6f8fb`) in
`frontend/public/manifest.webmanifest`.

## Primitives

All under `frontend/src/components/ui/`, plus one utility in `index.css`.

| Piece                                                       | Change                                                                                                                                                                                                                                                                  |
| ----------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `card` utility                                              | New. `@utility card` in `index.css` = `rounded-card bg-surface shadow-card`. A CSS utility, not a React component: it works on any element and has no props. One mechanical PR replaces the 27 inline `rounded-card border border-line bg-surface` strings with `card`. |
| `Button`                                                    | Variants: primary (`accent` fill, `on-ink` text), secondary (`surface` + `shadow-ctl`), ghost, danger (`loss` fill). Today's ink-filled primary becomes blue.                                                                                                           |
| `Field` and inputs                                          | No class change. Inputs keep `border border-line-strong`, so the control edge stays visible on a white card, and the `accent` focus ring they already have. They inherit the new token values.                                                                          |
| `Pill`, `Badge`                                             | Fully round, soft backgrounds.                                                                                                                                                                                                                                          |
| Segmented control                                           | Restyle the existing `RangePicker` and the `Allocation` view toggle: sunken `chip` track, raised `surface` + `shadow-ctl` active item. Keyboard handling stays in `lib/radioGroup.ts`. No library.                                                                      |
| Tabs                                                        | Styled `NavLink`s with a 2 px `accent` underline on the active tab. The router owns the state. No library.                                                                                                                                                              |
| `Menu`                                                      | New, on Base UI `Menu`. Replaces the hand-rolled menus in `PortfolioHeader.tsx` and `HomePage.tsx`, and is used by the account menu in the new sidebar.                                                                                                                 |
| `Select`                                                    | New, on Base UI `Select`. Replaces the 9 native `<select>` elements (7 files). Same control on desktop and phone.                                                                                                                                                       |
| `ConfirmDialog`, `Toast`, `Notice`, `Skeleton`, `Toggletip` | Keep their own code and the native `<dialog>`. They inherit the tokens and take `shadow-pop` where they float.                                                                                                                                                          |
| `Icon`                                                      | Add the sidebar glyphs (menu, plus, all-portfolios, admin) to the existing inline-SVG component.                                                                                                                                                                        |

Base UI use in this sub-project is two components, `Menu` and `Select`.
Tooltip and popover move to Base UI only when a page needs something the
current `Toggletip` cannot do.

Thin wrappers ship without tests (project rule: trivial code ships with
none).

## App shell

### Structure

`App.tsx` gets a layout route. `AppShell` renders the sidebar and an
`<Outlet/>`; every signed-in route nests under it, and `ProtectedRoute` wraps
the layout once instead of each route. `/login`, `/register` and `/setup`
stay outside the shell: a centered card on `paper` with the brand mark.

The locale and privacy remount key in `App.tsx` keeps wrapping the whole
tree, shell included.

### Sidebar

New `frontend/src/components/Sidebar.tsx`; `Header.tsx` is deleted. Width
232 px. Top to bottom:

1. Brand mark and name, linking to `/`.
2. `Tüm portföyler` with the combined total, linking to `/portfolios/all`.
3. Section `Portföyler` with a `+` button that opens the create-portfolio
   dialog. That dialog is extracted from `HomePage.tsx` into its own
   component so both can use it.
4. One row per portfolio: a color dot, the name, and a compact value.
   - Dot color: `cat-1` to `cat-6` by list index, cycling. Nothing is stored.
   - Value: `total_value` in the portfolio's `currency`, through
     `Intl.NumberFormat` with `notation: 'compact'` (`₺1,28 Mn` in `tr`).
     The existing privacy mode masks it like any other amount.
   - Clicking a row keeps the current tab (Özet, İşlemler, Temettü, İçe
     aktar), as the switcher in `PortfolioHeader` does today. From a position
     page it goes to the portfolio's Özet.
   - Long names truncate; the section scrolls when the list is long.
5. Bottom, pinned: `Yönetim` (admins only), the privacy toggle, and the
   account menu (`Menu`: account page, sign out).

Data comes from the `['portfolios']` query that `HomePage` and
`PortfolioHeader` already use (`GET /api/portfolios` returns `total_value`,
`cash`, `unrealized_pl`, `currency`). No new request. Today only portfolio create, rename, delete and an import
commit invalidate this query; a transaction change (`api/transactions.ts`)
and a manual price (`api/prices.ts`) do not. `HomePage` hid that by
refetching on mount, but the sidebar is always mounted, so the shell ticket
adds the invalidation to both.

### Page header

`PortfolioHeader.tsx` shrinks to a page header: title, tabs, `İşlem ekle`,
and the rename/delete menu (now the `Menu` primitive). Its portfolio
`<select>` is removed; the sidebar does that job. Its `children` contract
with the pages does not change.

### Breakpoints

- `lg` (1024 px) and up: fixed sidebar. The content column is at least
  792 px, so the tables that switch on at `md` still fit.
- Below `lg`: a 48 px top bar with the menu button, the page title and the
  privacy toggle. The same `Sidebar` renders inside a native `<dialog>` as a
  left drawer, which gives focus trapping, `Esc` and the backdrop for free. It
  closes on navigation.
- Below `md`: `İşlem ekle` is a floating round `+` at the bottom right. It is
  the same button, positioned with CSS.

`useIsDesktop` (the `md` table/list switch) is unchanged.

### States

- Loading: skeleton rows in the portfolio section.
- Query error: the sidebar still navigates; values show `—`.
- No portfolios: the section shows a "create your first portfolio" call that
  opens the create dialog.
- `/portfolios/:id` for a missing portfolio: unchanged (`PortfolioHeader`'s
  not-found state).

### Accessibility

The skip link stays the first focusable element. The sidebar is a
`<nav aria-label>`; the active row gets `aria-current="page"` from `NavLink`.
The drawer button has an accessible name and `aria-expanded`. Every new UI
string gets a `tr` and an `en` key.

### Deliberate shortcut: `HomePage` stays

`/` keeps rendering `HomePage`, reskinned by the tokens only. It overlaps
the sidebar, but it holds per-portfolio unrealized P/L, cash and share cards
that the "Tüm portföyler" view lacks. Sub-project 4 folds those cards into
"Tüm portföyler" and deletes `HomePage`. Redirecting `/` now would lose them
for the time in between.

> Superseded 2026-10-05 (GLG-935): `HomePage` is deleted. `/` redirects to
> `/portfolios/all`, whose Portföyler table shows each portfolio's value,
> share, return and day change; the brand links go there too.

## Dependencies

- `@base-ui/react` (Base UI, stable 1.x), added by the `Menu` ticket. Confirm
  the package name and the React 19 peer range at install time.
- Recharts is decided but not added here (sub-project 3).

`docs/PRINCIPLES.md` still says "new dependency last". That stays true; ADR
0023 records why these two are the exception: the owner chose library-built
menus, selects and charts over hand-built ones to get to the target look
faster.

## Docs that must change

The owner asked that no document be left contradicting the new rules. The
docs ticket lands first, so every later ticket and review reads the new
rules.

| File                                                       | What conflicts                                                                                                                                                          | Change                                                                                                                                                                                   |
| ---------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/adr/0023-ui-design-system-v2.md`                     | Does not exist                                                                                                                                                          | New ADR: this spec's decisions, the alternatives shown (Linear-like A, Vercel-like C; shells 1 and 3; violet, teal, ink accents), the two dependencies, the rules that survive from 0005 |
| `docs/adr/0005-ui-design-system.md`                        | Color table, IBM Plex, "no shadow", radii, control look, "component library rejected", "charting library rejected", "no new runtime dependency", the header-based shell | A dated "Superseded 2026-10-04 by ADR 0023" note at each of those places, per the repo's dated-note convention. No rewrite.                                                              |
| `docs/adr/README.md`                                       | Index has no 0023 row; the 0005 row has no "superseded in part by" entry                                                                                                | Add both                                                                                                                                                                                 |
| `docs/adr/0021-product-and-ui-decisions.md`                | Lines 102 and 216 cite the Defter system and 0005's chart-library rejection as current                                                                                  | Dated note at each                                                                                                                                                                       |
| `CLAUDE.md`                                                | Frontend stack line omits Base UI and Recharts and cites ADR 0005 for the token rule                                                                                    | Update the stack line; cite ADR 0023                                                                                                                                                     |
| `frontend/README.md`                                       | Cites ADR 0005 for the color rule                                                                                                                                       | Cite ADR 0023                                                                                                                                                                            |
| `docs/ROADMAP.md`                                          | No phase for this work                                                                                                                                                  | New phase section under "In progress" (Phase 24 unless that number is taken by then) listing the five sub-projects                                                                       |
| `frontend/index.html`, `frontend/public/fonts/`            | Plex preload and files                                                                                                                                                  | Changed by the tokens ticket, not the docs ticket                                                                                                                                        |
| Comments in `LineChart.tsx`, `BarChart.tsx`                | "no chart library (ADR 0005)"                                                                                                                                           | Changed in sub-project 3, when it becomes false                                                                                                                                          |
| Cloud routine prompts (hourly kanban worker, daily QA run) | Live outside the repo and may name Defter rules                                                                                                                         | Read both prompts when the docs ticket merges; update any line that contradicts ADR 0023                                                                                                 |

No change needed: `docs/PRINCIPLES.md` (see Dependencies), `docs/HISTORY.md`
(an accurate record of shipped phases), `.claude/agents/pr-reviewer.md` (it
reads the ADRs and hard-codes no design rule).

Rules from ADR 0005 that survive and are restated in ADR 0023: semantic
tokens only, no raw palette classes and no `dark:` variants in components;
explicit `+`/`−` on every P/L figure with color as the second cue; locale
number formatting; the cash-flow sign on transaction amounts; native
`<dialog>` for dialogs and the drawer; the page never scrolls sideways;
theme choice in `localStorage` applied by `public/theme.js`.

## Delivery

One epic, label `ui`, one PR per ticket. Jira keys are assigned when the
tickets are created.

| #   | Ticket                                                                                                                                                                                                                                                      | Owns                                                                    | After |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----- |
| 1   | Docs: ADR 0023, dated notes in 0005 and 0021, ADR index, `CLAUDE.md`, `frontend/README.md`, `ROADMAP.md`; check the cloud routine prompts                                                                                                                   | docs                                                                    | —     |
| 2   | Tokens: colors, shadow tokens, radii, Inter files and preload, remove Plex and `font-mono`, extend the contrast test                                                                                                                                        | `index.css`, `index.css.test.ts`, `index.html`, `public/fonts`          | 1     |
| 3   | `card` utility; replace the 27 inline card strings; move raw `shadow-*` to the shadow tokens                                                                                                                                                                | wide and mechanical                                                     | 2     |
| 4   | Restyle `Button`, `Field`, `Pill`, `Badge`, `Notice`, `Toast`, `ConfirmDialog`, `Skeleton`                                                                                                                                                                  | `components/ui/`                                                        | 3     |
| 5   | Restyle the segmented control and the tabs                                                                                                                                                                                                                  | `RangePicker.tsx`, `Allocation.tsx`, tab class in `PortfolioHeader.tsx` | 3     |
| 6   | Add Base UI; `Menu` primitive; migrate the menus in `PortfolioHeader.tsx` and `HomePage.tsx`; CSP check                                                                                                                                                     | `ui/Menu.tsx`, `package.json`                                           | 3     |
| 7   | `Select` primitive; migrate `TransactionDialog.tsx`                                                                                                                                                                                                         | `ui/Select.tsx`                                                         | 6     |
| 8   | `AppShell` and `Sidebar` at `lg` and up, a plain top bar (brand, privacy toggle, account menu) below `lg`; delete `Header.tsx`; extract the create-portfolio dialog; shrink `PortfolioHeader`; invalidate `['portfolios']` on transaction and price changes | `App.tsx`, shell files, `PortfolioHeader.tsx`                           | 5, 6  |
| 9   | Menu button, page title and drawer in the top bar below `lg`; floating `+` below `md`                                                                                                                                                                       | shell files                                                             | 8     |
| 10  | Migrate the remaining native selects (`SummaryPage`, `ImportPage`, `AccountPage`, admin sections)                                                                                                                                                           | pages                                                                   | 7, 8  |

Ticket 3 touches most pages. It merges alone, with no other frontend PR
open. After it, 4 to 7 run in parallel; then 8; then 9 and 10 in parallel.
The epic description carries this order, as the Phase 20 epic did, because
the hourly kanban worker may pick tickets on its own.

## Testing

- Every PR: `make lint`, `make test`, `make build-frontend` (PRINCIPLES #6), then the
  `pr-reviewer` agent.
- Unit tests:
  - the extended contrast test (ticket 2);
  - `Sidebar.test.tsx` (ticket 8): rows from a mocked `['portfolios']` query,
    the active row, the admin link only for admins, compact values masked in
    privacy mode;
  - drawer open, close on navigation and on `Esc` (ticket 9);
  - existing tests that drive a native `<select>` are rewritten against the
    new `Select` in tickets 7 and 10.
- Browser check with screenshots in the PR for tickets 2, 8 and 9: light and
  dark, 1280 px and 375 px.

## Risks

- **Select tests.** Tests that use `selectOptions` on a native `<select>`
  break when Base UI `Select` replaces it. This is the main hidden cost of
  tickets 7 and 10.
- **CSP.** `deploy/nginx.conf` sends `default-src 'self'` with no
  `style-src`, so injected `<style>` elements and inline style attributes in
  HTML are blocked, while styles set through the CSSOM (React style props)
  work. Ticket 6 must run a production build behind that policy and confirm
  that Base UI's popup positioning works before anything is migrated. If it
  injects a `<style>` element, use its CSP provider or record the policy
  change in ADR 0023.
- **Top layer.** `TransactionDialog` is a native modal `<dialog>`, which
  renders in the browser's top layer. A `Select` popup portaled to `<body>`
  would sit under it and be inert. The `Select` primitive portals into the
  closest `<dialog>` when it has one.
- **Glyph coverage.** If the Inter subset lacks ₺ or a Turkish letter, that
  glyph falls back to the system font and the numbers stop lining up. Ticket
  2 checks this in a browser.
- **Collisions.** The hourly worker may open frontend PRs mid-epic. Tickets
  3 and 8 are the ones that conflict; the merge order above and a quiet board
  during ticket 3 limit this.
- **Mixed period.** Until sub-projects 3 to 5, pages have Defter layouts and
  hand-drawn charts inside the new skin and shell. Accepted by the owner.

  > Amended 2026-10-05 (GLG-928): Phase 26 shipped sub-project 4, so Özet,
  > Tüm portföyler, İşlemler, the position page and Temettü have their UI v2
  > layouts, and since Phase 25 the charts are drawn by Recharts (GLG-912,
  > GLG-913). Only the pages of sub-project 5 keep Defter layouts: İçe aktar,
  > Hesap, Yönetim and the signed-out pages.

## Done when

- Every signed-in page renders in the new skin inside the sidebar shell, in
  light and dark.
- Below `lg` the drawer works; on a phone the floating `+` opens the
  transaction dialog.
- No component uses a raw palette class, a raw `shadow-*` utility,
  `font-mono`, or a native `<select>`.
- No document in the repo contradicts ADR 0023, and the cloud routine
  prompts have been checked.
- `make lint`, `make test` and `make build-frontend` pass on `main`.
