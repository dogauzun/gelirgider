# ADR 0023: UI design system v2 ("soft depth") — Inter, shadow tokens, Base UI, sidebar shell

Status: accepted (2026-10-04).

This ADR replaces the look, the app shell and the component and chart library
rules of [ADR 0005](0005-ui-design-system.md); the rules of ADR 0005 that
survive are listed below.

## Context

The owner found the app plain and amateur-looking. Defter (ADR 0005) was flat
by design: warm paper, hairline cards, no shadows, ink primary. The complaint
was about that direction, not about bugs in it. The owner chose, in this
order: the problem is visual design, not information design or flows; the
target look is crisp SaaS (Linear, Stripe Dashboard, Vercel); a full redesign
that includes the app shell and the charts; headless primitives and a chart
library are allowed; visual language B, "soft depth": Inter, a tinted ground,
white cards that float on layered shadows, pill chips, a filled chart.

## Decision

### Design language

- The look is "soft depth": a cool tinted ground (`paper`), white cards
  (`surface`) that float on shadow tokens instead of hairline borders, fully
  round pills and badges, a blue `accent` primary, Inter.
- Colors are semantic tokens in `frontend/src/index.css`, with a light and a
  dark value. The dark values live in the `[data-theme='dark']` block and in
  the `prefers-color-scheme` media query, which stay identical. Components use
  the tokens; the values are not repeated here.
- Depth is three shadow tokens: `shadow-card` for resting cards, `shadow-ctl`
  for buttons, inputs and the active tab or segment, `shadow-pop` for menus,
  dialogs, toasts and tooltips. Their values live in `--elevation-*`, which
  the dark blocks redefine: Tailwind v4 copies a `--shadow-*` theme value into
  its utility and does not reference the variable, so redefining
  `--shadow-*` alone changes nothing in dark mode. Shadows do not read on a
  dark ground, so the dark recipe is a 1 px light ring plus a top highlight
  under the same token names.
- Cards have no border: the ring inside `shadow-card` replaces it, and
  floating layers drop their border because `shadow-pop` carries its own ring.
  The `card` utility in `index.css` (`rounded-card bg-surface shadow-card`) is
  a CSS utility, not a React component. `line` stays for table rules,
  dividers and tab underlines.
- Radii are `--radius-card` 12 px and `--radius-ctl` 8 px.
- Type is Inter, self-hosted as a variable font under `frontend/public/fonts`
  with its license, in a latin and a latin-ext subset (Turkish letters and the
  lira sign need latin-ext). Tabular numerals stay on `body`. IBM Plex and
  `font-mono` are gone: symbols are Inter semibold. The two `theme-color`
  metas, the manifest `background_color` and the icon files in
  `frontend/public` mirror the `surface`, `paper` and `ink` colors by hand and
  change with them.
- Contrast is tested, not eyeballed. `frontend/src/index.css.test.ts` is the
  authority over every token pair it checks, in all three theme blocks: where
  a value fails, the token is adjusted until the test passes. Text pairs need
  4.5:1. `faint` (placeholders, disabled text, axis labels) needs 3:1 on
  `paper` and `surface` and less contrast than `muted`; it also draws the
  boundary of every control that must be seen on a card (resting input and
  `Select` trigger border, unchecked checkbox edge, import drop zone edge),
  because WCAG 1.4.11 asks 3:1 for a control's boundary and `line-strong`
  does not reach it. `line-strong` is for chart reference lines and the Katkı
  card's centre line. A graphic needs 3:1: `cash` and `cat-6` are each 3:1
  against `surface` and 3:1 apart, `cash` the lighter, so the donut's "Diğer"
  and "Nakit" slices never read as one block.
- Migration was token-first and in place: token names did not change, `main`
  worked after every ticket, with no feature flag and no parallel component
  tree.

### Components

- `frontend/src/components/ui/` holds the primitives. `Button` has primary
  (`accent` fill, `on-ink` text), secondary and danger-quiet (`raised` fill; secondary hovers with
  `raised-hover`, danger-quiet with `loss-soft`), ghost and danger (`loss` fill); a link styled as a
  button takes `buttonClass()`. `inputClass({ size, width })` styles inputs and
  the `Select` trigger. Tabs are styled `NavLink`s with a 2 px `accent`
  underline on the active tab; the router owns the state.
- Every segmented control renders through `ui/Segmented`, which owns the
  `role="radiogroup"` markup, the single tab stop on the checked item and the
  arrow keys; its track is `well` and the selected item `raised` with
  `shadow-ctl`. ESLint rejects any other `role="radiogroup"`.
- Base UI (`@base-ui/react`, stable 1.x) provides the headless primitives:
  `Menu`, `Select`, `Popover` (the KPI hints and the date range panel),
  `Checkbox` and `Autocomplete` (the instrument search). A native `<select>`, `type="checkbox"` and a hand-rolled
  `role="combobox"` are banned in components.
- `Select` inside a native modal `<dialog>` portals into that dialog, because a
  popup portaled to `<body>` sits under the dialog's top layer and is inert.
- `deploy/nginx.conf` sends `default-src 'self'` with no `style-src`, so
  injected `<style>` elements are blocked. `main.tsx` wraps the app in Base
  UI's `CSPProvider disableStyleElements`, and the one rule Base UI would
  inject lives in `index.css`. The policy stays unchanged.
- `ConfirmDialog`, `Toast`, `Notice`, `Skeleton` and `Toggletip` keep their
  own code and the native `<dialog>`.
- `docs/PRINCIPLES.md` #1 still says a new dependency comes last. Base UI and
  Recharts are the exception because the owner chose library-built menus,
  selects and charts over hand-built ones to reach the target look faster. No
  other dependency is added under this ADR.

### App shell

- `AppShell` renders the sidebar and an `<Outlet/>`; every signed-in route
  nests under it. `/login`, `/register` and `/setup` stay outside it: a
  centered card on `paper` with the brand mark. The remount key is the locale
  alone and wraps the whole tree, so a privacy toggle does not remount.
- The sidebar (`Sidebar.tsx`) is 232 px wide. Top to bottom: the brand mark;
  `Tüm portföyler` with the combined total; a `Portföyler` section with a `+`
  that creates a portfolio; one row per portfolio (a color dot `cat-1` to
  `cat-6` by list index, the name, `total_value` in compact notation, masked
  by privacy mode); pinned at the bottom `Yönetim` (admins only), the account
  menu and the privacy toggle. Clicking a row keeps the current tab, and from
  a position page goes to the portfolio's Özet. The sidebar reads the
  `['portfolios']` query, so a transaction or price change invalidates it. On
  a query error it shows no rows and no values, and its static links stay.
- `PortfolioHeader` is the page header: title (with the goal progress pill on
  Özet), tabs, `İşlem ekle` and a `Menu` with the goal item and, on a
  portfolio, rename and delete. There is no portfolio switcher in it; the
  sidebar does that job.
- From `lg` (1024 px) the sidebar is fixed. Below `lg` a 48 px top bar holds
  the menu button, the brand mark, the page title (a plain span, never a
  heading), the privacy toggle and the account menu, and the same `Sidebar`
  opens in a native `<dialog>` as a left drawer that closes on any
  navigation. Below `md`, `İşlem ekle` is a floating round `+`.
- The skip link is the first focusable element. The sidebar is a
  `<nav aria-label>` and the active row has `aria-current="page"`. A
  signed-in user's unknown URL renders the not-found page inside the shell.
- `/` redirects to `/portfolios/all`.

### Charts

- Recharts draws the charts, behind thin wrappers under
  `frontend/src/components/charts/`. Ours stay: printed amounts from the exact
  decimal strings, y ticks and domain from `axisTicks`, the date labels, the
  privacy masking, gaps that are never drawn to zero, and the legend as our
  own HTML. Legend entries are toggle buttons that hide and show their
  series, per chart and unsaved.
- Pages get a chart from `LazyChart.tsx`, which loads all charts and Recharts
  as one separate chunk on the first chart a page renders, with a skeleton of
  the chart's height meanwhile. When the chunk fails to load, the chart's card
  shows an error and the rest of the page works until a reload.
  `conventions.test.ts` fails when a file outside `components/charts/`
  imports `recharts`, or one other than `LazyChart.tsx` imports from
  `components/charts/`.
- The line chart takes `unit: 'money' | 'percent'` and `onZoom`. In percent it
  has a zero line, no area fill and signed percentages that privacy mode does
  not mask, and the return card's "% | ₺" switch (`chartMode` in
  `localStorage`, percent by default) picks the unit; the percent line is
  always time-weighted. Drag to zoom is mouse only: press, drag at least 8 px
  across the plot, release turns the span into an Özel range through
  `useRange`; Escape cancels, touch and pen never zoom.
- The allocation card draws a donut (`AllocationDonut`) beside an HTML legend,
  which is the accessible content; the legend goes beside the donut once the
  card body is 28 rem wide. The Katkı card draws plain HTML bars, not
  Recharts, so it never waits for the charts chunk.

### Özet layout

From `lg`, top to bottom: the return card (`ReturnCard.tsx`) with the return
split card under it ([ADR 0027](0027-return-attribution.md)), beside the
"Dikkat" card at 2:1; the KPI strip of five cells (Toplam değer, Günlük,
Nakit, Gerçekleşmemiş K/Z, Gerçekleşmiş K/Z); İstatistikler (`StatsCard`);
the positions table; then Dağılım beside Katkı at equal heights; then the
goal card when the scope has a goal ([ADR 0029](0029-goals-and-projection.md)).
Below `lg` the cards stack in that order. Each number is on the page once: the detail
behind a KPI (the cost, the currency gain, cash per currency, the prices'
date) is a `hint` popover on its cell, and the positions table's footer keeps
its own Değer and K/Z totals. The attention rules are one pure function,
`attentionItems` in `lib/attention.ts`, with the thresholds in `ATTENTION`;
the card's Ekle books the expected dividend at once and Yoksay dismisses it.

Tüm portföyler has the same layout summed over every portfolio, with a
"Portföyler" table between İstatistikler and the positions table; its "Dikkat"
card groups each portfolio's items under the portfolio's name
(`mergeAttention`). See [ADR 0014](0014-all-portfolios-view.md).

A "Performans" tab at `/portfolios/:id/performance`, for a portfolio and for
`all`, shows the benchmark and method selects over a heatmap of periodic
returns (`PeriodReturnsTable.tsx`): a row per year, twelve month cells and the
year's own cell. Its text stays `ink`, which the contrast test holds to 4.5:1
on every heat step.

### Rules that survive from ADR 0005

- Semantic tokens only: no raw palette classes and no `dark:` variants in
  components, no raw `shadow-sm|md|lg|xl|2xl`, no `font-mono`. A scanner test,
  `frontend/src/conventions.test.ts`, fails the build when a banned class
  comes back; a ticket that removes the last use of a pattern adds it there.
- An explicit `+` / `−` on every P/L figure, with color as the second cue.
- Number and percent formatting follows the locale through
  `Intl.NumberFormat`.
- The cash-flow sign on transaction amounts.
- Native `<dialog>` with `showModal()` for dialogs and for the drawer. A
  dialog that centres from `md` is a bottom sheet below it, takes its panel
  from `ui/sheetPanel`, and ESLint rejects a hand-built one; the position
  dialog is full screen on a phone instead.
- The page never scrolls sideways; a table wider than its card scrolls inside
  the card, and a table that cannot is replaced by a list on a phone.
- The theme choice lives in `localStorage` and is applied by
  `frontend/public/theme.js`.
- Icons are inline SVG paths in one `Icon` component.

## Alternatives considered

- Visual language A (Linear-like, cool grays, hairline borders) and C
  (Vercel-like, black on white): not chosen by the owner. A risked the same
  "plain" verdict as Defter in a cooler gray. Keeping Defter and polishing it
  was rejected because the complaint was about its flat direction.
- Shell: a sidebar that lists pages with the portfolio in a switcher costs two
  clicks between portfolios and hides their values; a top bar only is the
  smallest change and the least "new app".
- Accent: violet is Stripe's brand color, teal reads too close to `gain`
  (the accent must never read as profit), ink drifts back to plain.
- Zero new dependencies (the native Popover API and hand-drawn SVG charts) was
  the brainstorming recommendation; the owner chose Base UI and Recharts.
  Radix was rejected because Base UI has the same authors and is actively
  developed.
- A parallel `/v2` component tree behind a flag would mean two copies of every
  page and every fix landing twice; shell first, then skin, would style the
  shell twice.
- A `Card` React component would be a component for one class string.
