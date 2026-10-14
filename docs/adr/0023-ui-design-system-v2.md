# ADR 0023: UI design system v2 ("soft depth") — Inter, shadow tokens, Base UI, sidebar shell

Status: accepted (GLG-868, 2026-10-04); amended 2026-10-04 (GLG-870,
correction: dark shadows go through `--elevation-*`; GLG-871, buttons fill
with `raised`, `raised-hover` token; GLG-872, `well` and `raised` tokens for
segmented controls; GLG-874, `CSPProvider` for the `Select` popup; GLG-875,
the sidebar on a query error; GLG-876, the drawer below `lg`; GLG-879, the
icon files follow the colors); amended 2026-10-05 (GLG-882, the top bar's
title; GLG-912, GLG-913, the line and bar charts drawn by Recharts,
sub-project 3 split; GLG-914, legend entries hide and show their series;
GLG-915, the charts load as a separate chunk; GLG-922, the line chart places
its tooltip; GLG-934, Özet layout A; GLG-935, Tüm portföyler replaces the
home page); amended 2026-10-06 (GLG-946, date labels by width and the
legend's reserved height; GLG-947, the allocation donut; GLG-949, drag
to zoom; GLG-948, the Katkı card; GLG-1033, `cash` at 3:1 on `surface`;
GLG-1045, `cash` and `cat-6` 3:1 apart; GLG-954, the return card's percent
mode; GLG-955, the Performans tab and its heatmap; GLG-963, `ui/Checkbox`
and `PageHeader`; GLG-969, Yönetim and the sources table; GLG-971, every
sub-project shipped; GLG-1076, the unchecked checkbox's border is `faint`;
GLG-1079, the input and `Select` trigger border is `faint`; GLG-1083, the
import drop zone's edge is `faint`); amended 2026-10-07 (GLG-1086, segmented
controls render through `ui/Segmented.tsx`); amended 2026-10-09 (GLG-1229,
the segmented classes in `ui/segmentedClass.ts`; GLG-1248, the transaction
dialog's type picker renders `ui/Segmented`); amended 2026-10-10 (GLG-1285,
the instrument search is Base UI `Autocomplete`); amended 2026-10-13
(GLG-1356, dark `muted` and `loss` at 4.5:1 on `raised-hover`)

Visual reference: the mockup
[`docs/superpowers/specs/2026-10-04-ui-v2-mockup.html`](../superpowers/specs/2026-10-04-ui-v2-mockup.html)
(open it in a browser; it has a light/dark switch and uses mock data). The
full design is in the spec
[`2026-10-04-ui-v2-design-language-and-shell-design.md`](../superpowers/specs/2026-10-04-ui-v2-design-language-and-shell-design.md),
and the task order in the plan
[`2026-10-04-ui-v2-design-language-and-shell.md`](../superpowers/plans/2026-10-04-ui-v2-design-language-and-shell.md).

This ADR supersedes parts of [ADR 0005](0005-ui-design-system.md); the list
is under "What this supersedes in ADR 0005" below.

## Decision

The owner found the app plain and amateur-looking. Defter (ADR 0005) is flat
by design: warm paper, hairline cards, no shadows, ink primary. The complaint
was about that direction, not about bugs in it. The owner chose, in this
order: the problem is visual design, not information design or flows; the
target look is crisp SaaS (Linear, Stripe Dashboard, Vercel); a full
redesign, including the app shell and the charts; headless primitives and a
chart library are allowed; visual language B, "soft depth": Inter, a tinted
ground, white cards that float on layered shadows, pill chips, a filled
chart; shell 2, a sidebar that lists the portfolios with their values; a blue
accent; dark mode as shown in the mockup.

Migration is token-first and in place. Token names do not change, only their
values, so color and type flip app-wide when `frontend/src/index.css`
changes. `main` works after every ticket; there is no feature flag and no
parallel component tree.

The redesign is five sub-projects, each with its own spec, plan and ticket
wave: (1) design language, (2) app shell, (3) charts on Recharts, (4) core
pages, (5) secondary pages. This ADR records the design language and the
shell (1 and 2) in full, and the decision to allow a chart library (3). Pages
keep their Defter-era layouts inside the new skin and shell until
sub-projects 4 and 5; the owner accepted that mixed period. Everything here
is frontend and docs: no backend, API or schema change.

> Amended 2026-10-05 (GLG-934): Özet has its sub-project 4 layout, "layout
> A" of [`2026-10-05-ui-v2-core-pages-design.md`](../superpowers/specs/2026-10-05-ui-v2-core-pages-design.md).
> From `lg`, top to bottom: the return card
> (`frontend/src/components/ReturnCard.tsx`: the range's return by the chosen
> method, the change in value, a pill with the benchmark's return and the
> difference coloured by its sign, the benchmark and method selects as wide
> as their longest label, the range picker, a 220 px chart) beside the
> "Dikkat" card at 2:1; one KPI strip of five cells (Toplam değer, Günlük,
> Nakit, Gerçekleşmemiş K/Z, Gerçekleşmiş K/Z); the positions table;
> Dağılım beside İstatistikler. Below `lg` the cards stack in that order,
> the strip in two columns. Each number is on the page once: the old hero's
> sub-lines (unrealized against cost, the cost, the currency gain, cash per
> currency, the prices' date) are `hint` popovers on the KPI cells they
> explain (`KpiCell` in `frontend/src/components/ui/Kpi.tsx`); the positions
> table's footer keeps its own Değer and K/Z totals. The attention rules are
> one pure function, `attentionItems` in `frontend/src/lib/attention.ts`,
> with the thresholds in `ATTENTION`: a manual price older than 7 days, one
> position above 25% of the total including cash, cash above 40%, one
> foreign currency above 50%, and the two largest day movers; a share
> exactly at its threshold is not flagged. The card's Ekle books the
> expected dividend at once, as one row of the Temettü tab's "Tümünü ekle";
> Yoksay dismisses it. The `all` scope has no attention card until
> Tüm portföyler gets its own layout, so it keeps the missing-rates notice.

> Amended 2026-10-05 (GLG-935): Tüm portföyler has the same layout summed
> over every portfolio, with a "Portföyler" table between the KPI strip and
> the positions table, and replaces the home page (see
> [ADR 0014](0014-all-portfolios-view.md)). Its "Dikkat" card holds each
> portfolio's items under that portfolio's name (`mergeAttention` in
> `frontend/src/lib/attention.ts`); Bugün keeps the two largest movers over
> all of them. The missing-rates notice is gone: the card names the
> currencies, and the total, cash and unrealized P/L carry the same
> `MissingRatesMark` as the sidebar total.

> Amended 2026-10-07 (GLG-1099): İstatistikler (`StatsCard` in
> `frontend/src/pages/SummaryPage.tsx`) sits directly under the KPI strip,
> so it shows without scrolling. From `lg`, top to bottom, Özet is now: the
> return card beside "Dikkat", the KPI strip, İstatistikler, the positions
> table, then Dağılım beside Katkı; Tüm portföyler puts its "Portföyler"
> table between İstatistikler and the positions table. Below `lg` the cards
> stack in that order. This replaces the order in the GLG-934 amendment
> above and the GLG-948 one under Dependencies.

> Amended 2026-10-06 (GLG-955): a "Performans" tab sits between Özet and
> İşlemler at `/portfolios/:id/performance`, for a portfolio and for `all`
> (`frontend/src/pages/PerformancePage.tsx`). It has the benchmark and method
> selects, stored as Özet stores them (`frontend/src/lib/useReturnSettings.ts`),
> over the "Dönemsel getiri" card: a heatmap table
> (`frontend/src/components/PeriodReturnsTable.tsx`) with a row per year,
> newest first, twelve month cells and the year's own cell. A cell prints the
> chosen method's return with one decimal and a `*` on a partial period, whose
> `title` gives its dates and the exact percentage; a month without a bucket
> shows `·`, a `null` return `—`. Its background is `gain` or `loss` mixed
> into `surface` with `color-mix(in srgb, …)` at 10, 18, 27, 36 and 45% for
> the five strengths of `heatStep` (`frontend/src/lib/periods.ts`: below 1%,
> then from 1, 2, 4 and 8%); the text stays `ink`, which
> `frontend/src/index.css.test.ts` holds to 4.5:1 on every step in all three
> theme blocks. With a benchmark, "Portföy | Fark" (a segmented control)
> shows each cell's excess over it, (1 + p) / (1 + b) − 1 as on the return
> card's pill, "Reel" against TÜFE. Every return `null` shows the
> explanation instead of the table. Below `md` the table scrolls sideways
> inside its card with the year column sticky, the one card that scrolls
> sideways.

> Amended 2026-10-06 (GLG-971): all five sub-projects have shipped, each in
> its own phase ([HISTORY.md](../HISTORY.md)): (1) design language and
> (2) app shell in Phase 24 (epic GLG-867); (3) charts in three parts, 3a
> charts foundation in Phase 25 (GLG-910), 3b chart additions in Phase 27
> (GLG-945) and 3c return series in Phase 28 (GLG-951); (4) core pages in
> Phase 26 (GLG-928); (5) secondary pages in Phase 29 (GLG-962). No page
> keeps a Defter-era layout.

### Semantic color tokens

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

`on-ink` keeps its name and gains one job: it is also the text color on an
`accent` fill (the primary button). Its values already fit: white in light,
near-black in dark.

**Contrast is tested, not eyeballed.** `frontend/src/index.css.test.ts`
already checks `muted` on `paper`, `surface` and `chip` at 4.5:1 in all three
blocks. It is extended to `gain` and `loss` on `surface` and on their own
`-soft` background; `accent` on `surface`; `accent-ink` on `accent-soft`;
`on-ink` on `accent`; `warn` on `warn-soft`; `plum` on `plum-soft`. The test
is the authority over the table above: where a value fails, the token is
adjusted until the test passes. `faint` (placeholders, disabled text, axis
labels) is exempt from 4.5:1 but keeps the existing test's rule: at least 3:1
on `paper` and `surface` in every theme block, and less contrast than
`muted`. Light `faint` is `#828d9d` (3.16:1 on `paper`, 3.36:1 on
`surface`); dark `#6f7888` passes at 4.28 and 3.92. That test stays; GLG-869
does not delete it.

> Amended 2026-10-06 (GLG-1033): `cash` is `#8b939f` light and `#5f6879`
> dark, up from `#c5ccd6` and `#3b4352`, which drew a cash-only donut at
> 1.62:1 and 1.75:1 on `surface`. Now 3.10:1 and 3.11:1, the 3:1 a graphic
> needs. It keeps `cat-6`'s slate hue, a step lighter than it in light mode
> and darker in dark (1.60:1 and 1.83:1 apart), so it still reads as "not
> invested". The contrast test checks both: at least 3:1 on `surface` and
> 1.5:1 from `cat-6`, in all three blocks.

> Amended 2026-10-06 (GLG-1045): `cat-6` is `#344054` light and `#5f6879`
> dark, down from `#667085` and `#8a94a6`, and dark `cash` is `#c0c7d2`, up
> from `#5f6879`; light `cash` stays `#8b939f`. In the donut "Diğer" and
> "Nakit" sit side by side, and at 1.60:1 (light) and 1.84:1 (dark) apart
> they read as one grey block, with the lighter of the two flipping between
> themes. Both keep the slate hue and now part by lightness: 3.37:1 apart in
> light and 3.30:1 in dark, cash the lighter one in both. On `surface` cash
> is 3.10:1 light and 10.24:1 dark, `cat-6` 10.46:1 and 3.11:1. Both stay
> near-neutral against the saturated `cat-1..5`. The contrast test checks
> all of it in the three blocks: each at least 3:1 on `surface`, at least
> 3:1 apart, and cash the lighter.

> Amended 2026-10-13 (GLG-1356): dark `muted` is `#949eaf` and dark `loss`
> `#f57882`, up from `#8791a2` and `#f26d78`; light stays. A hovered table
> row fills with `raised-hover` (`#2d3545`), where the old values drew the
> instrument name at 3.87:1 and loss figures at 4.24:1. Now 4.55:1 and
> 4.62:1. A darker `raised-hover` was the other way out, but at 4.5:1 for
> `muted` it sits almost on `raised` and the secondary button's hover would
> vanish. The contrast test also checks `muted` and `loss` on
> `raised-hover`, in all three blocks.

> Amended 2026-10-06 (GLG-1076): `faint` also draws the unchecked
> checkbox's border, which `line-strong` drew at 1.45:1 (light) and 1.57:1
> (dark) on `surface`. A control's boundary needs 3:1 (WCAG 1.4.11), and the
> test above already holds `faint` to that on `paper` and `surface`.

> Amended 2026-10-06 (GLG-1079): `faint` also draws the resting border of
> every `inputClass` control: text, number, date and time inputs and the
> Base UI `Select` trigger (the app has no `<textarea>` and no native
> `<select>`). On `surface` that edge goes from 1.45:1 to 3.36:1 in light
> and from 1.57:1 to 3.92:1 in dark. `line-strong` stays for the chart
> reference lines, the Katkı card's centre line and the import drop zone's
> dashed edge. A note under the Primitives table below marks the `Field`
> row superseded.

> Amended 2026-10-06 (GLG-1083): `faint` also draws the import drop zone's
> dashed edge, which `line-strong` drew at 1.45:1 (light) and 1.57:1 (dark)
> on `surface`; it is now 3.36:1 and 3.92:1. Drag-over keeps its `accent`
> edge. `line-strong` stays for the chart reference lines and the Katkı
> card's centre line, none of which is a control.

### Depth

New in v2. Three shadow tokens in `@theme`, redefined in both dark blocks
exactly as the colors are. Tailwind v4 turns `--shadow-*` into `shadow-*`
utilities.

> Correction 2026-10-04 (GLG-870): Tailwind v4 copies a `--shadow-*` theme
> value into its `shadow-*` utility and does not reference the variable, so
> redefining `--shadow-*` in the dark blocks changed nothing, and until
> GLG-870 dark mode drew the light shadows. The `@theme` tokens are now
> `--shadow-card`, `--shadow-ctl` and `--shadow-pop` set to
> `var(--elevation-card)`, `var(--elevation-ctl)` and `var(--elevation-pop)`.
> The values live in `--elevation-card`, `--elevation-ctl` and
> `--elevation-pop`, which `:root` and both dark blocks define. The utility
> names `shadow-card`, `shadow-ctl` and `shadow-pop` and the values in the
> table below are unchanged.

| Token         | Used for                               | Light                                                                                               | Dark                                                                                                      |
| ------------- | -------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `shadow-card` | Resting cards                          | `0 0 0 1px rgb(16 24 40 / .05), 0 1px 2px rgb(16 24 40 / .06), 0 6px 16px -4px rgb(16 24 40 / .08)` | `0 0 0 1px rgb(255 255 255 / .07), inset 0 1px 0 rgb(255 255 255 / .04), 0 8px 20px -8px rgb(0 0 0 / .6)` |
| `shadow-ctl`  | Buttons, inputs, active tab or segment | `0 0 0 1px rgb(16 24 40 / .1), 0 1px 2px rgb(16 24 40 / .08)`                                       | `0 0 0 1px rgb(255 255 255 / .08)`                                                                        |
| `shadow-pop`  | Menus, dialogs, toasts, tooltips       | `0 0 0 1px rgb(16 24 40 / .06), 0 2px 6px rgb(16 24 40 / .06), 0 8px 24px -4px rgb(16 24 40 / .16)` | `0 0 0 1px rgb(255 255 255 / .1), 0 12px 32px -6px rgb(0 0 0 / .7)`                                       |

Shadows do not read on a dark ground, so the dark recipe is a 1 px light ring
plus a top highlight. Same token names, so components do not branch.

Cards drop `border border-line`; the 1 px ring inside `shadow-card` replaces
it. `line` stays for table rules, dividers and tab underlines. Floating
layers drop their border too, because `shadow-pop` carries its own ring.

The raw `shadow-sm`, `shadow-lg` and `shadow-2xl` utilities in components
move to `shadow-ctl` (was `sm`) and `shadow-pop` (was `lg`, `2xl`), so no raw
shadow utility is left in components. This is part of the token rule.

### Shape

`--radius-card` goes from 10 px to 12 px and `--radius-ctl` from 6 px to
8 px. Pills and badges are fully round (`rounded-full`).

A `card` utility in `index.css` (`@utility card`, applying
`rounded-card bg-surface shadow-card`) replaces the inline
`rounded-card border border-line bg-surface` strings. It is a CSS utility,
not a React component: it works on any element and has no props.

### Type

Inter, variable weight axis, self-hosted under `frontend/public/fonts` with
its OFL 1.1 license file, `font-display: swap`. Two subset files with
`unicode-range`: latin and latin-ext. Turkish needs latin-ext (ğ, ş, ı, İ),
and so does the lira sign (₺, U+20BA).

- `--font-sans: 'Inter', system-ui, sans-serif`.
- `--font-mono` and IBM Plex Mono are removed. Instrument symbols become
  Inter semibold; chart axis labels become Inter. No component uses
  `font-mono`.
- The four IBM Plex files and their license file are deleted. The preload in
  `frontend/index.html` points at the Inter latin file.
- Tabular numerals stay on `body`.
- The two Inter files, from `@fontsource-variable/inter` 5.3.0
  (GLG-869): `InterVariable-latin.woff2` 48,256 bytes and
  `InterVariable-latin-ext.woff2` 85,068 bytes, 133,324 bytes together. The
  Plex set was about 247 KB.

The base text size stays 13 px, so the inline `text-[13px]`-style classes
stay; a type scale is added when sizes must change app-wide.

The colors that live outside `index.css` follow the tokens: the two
`theme-color` metas in `frontend/index.html` (`surface`: `#ffffff` light,
`#161a22` dark) and `background_color` (`paper`, `#f6f8fb`) in
`frontend/public/manifest.webmanifest`.

> Amended 2026-10-04 (GLG-879): the icon files in `frontend/public` mirror
> the light `ink` (`#1a2233`) and `paper` (`#f6f8fb`) by hand too:
> `favicon.svg`, and `apple-touch-icon.png`, `icon-192.png` and `icon-512.png`,
> which are rendered from it with `sips`.

### Primitives

All under `frontend/src/components/ui/`, plus the `card` utility in
`index.css`.

| Piece                                                       | Decision                                                                                                                                                                                 |
| ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `card` utility                                              | `rounded-card bg-surface shadow-card`; replaces the inline card strings.                                                                                                                 |
| `Button`                                                    | Variants: primary (`accent` fill, `on-ink` text), secondary (`surface` + `shadow-ctl`), ghost, danger (`loss` fill). The ink-filled primary of ADR 0005 becomes blue.                    |
| `Field` and inputs                                          | No class change. Inputs keep `border border-line-strong`, so the control edge stays visible on a white card, and the `accent` focus ring.                                                |
| `Pill`, `Badge`                                             | Fully round, soft backgrounds.                                                                                                                                                           |
| Segmented control                                           | `RangePicker` and the `Allocation` view toggle: sunken `chip` track, raised `surface` + `shadow-ctl` active item. Keyboard handling stays in `lib/radioGroup.ts`. No library.            |
| Tabs                                                        | Styled `NavLink`s with a 2 px `accent` underline on the active tab. The router owns the state. No library.                                                                               |
| `Menu`                                                      | New, on Base UI `Menu`. Replaces the hand-rolled menus in `PortfolioHeader.tsx` and `HomePage.tsx`, and is the account menu in the sidebar.                                              |
| `Select`                                                    | New, on Base UI `Select`. Replaces every native `<select>`. Same control on desktop and phone. Inside a native `<dialog>` it portals into that dialog, so it is not under the top layer. |
| `ConfirmDialog`, `Toast`, `Notice`, `Skeleton`, `Toggletip` | Keep their own code and the native `<dialog>`. They inherit the tokens and take `shadow-pop` where they float.                                                                           |
| `Icon`                                                      | Gains the sidebar glyphs (menu, plus, all-portfolios, admin) in the existing inline-SVG component.                                                                                       |

> Amended 2026-10-04 (GLG-872): a segmented control uses two new color
> tokens instead of `chip` and `surface`. `well` (light `#eaeef3`, dark
> `#0d1016`) is the sunken track; `raised` (light `#ffffff`, dark `#232936`)
> is the selected item on it, with `shadow-ctl`. In light they equal `chip`
> and `surface`; in dark the track is darker than the card and the selected
> item lighter, as in the mockup. `ui/segmented.ts` holds the classes, and the
> transaction dialog's type picker uses the same two tokens.

> Superseded 2026-10-07 (GLG-1086): where the "Segmented control" row and the
> note above put the code no longer holds. Since PR #1040 every segmented
> radiogroup renders through `Segmented` in `ui/Segmented.tsx`, which owns the
> `role="radiogroup"` markup, the single tab stop on the checked item and the
> arrow keys (`radioGroupKeyDown` from `lib/radioGroup.ts`); a new segmented
> control uses it instead of a hand-written radiogroup. `ui/segmented.ts` is
> gone: `ui/Segmented.tsx` exports the track and item classes it held
> (`segmentedTrack`, `segmentedItem`), which the `aria-pressed` language
> toggles (`LanguageSwitch`, the Hesap page's language row) reuse. The
> transaction dialog's `TypeSegmented` stays a hand-written radiogroup on
> `lib/radioGroup.ts` with its own track, a grid of two rows below `sm`.

> Amended 2026-10-09 (GLG-1229): the track and item classes move from
> `ui/Segmented.tsx` to `ui/segmentedClass.ts`, so the component file exports
> only the component and Fast Refresh keeps its state.

> Superseded 2026-10-09 (GLG-1248): the last sentence of the GLG-1086 note no
> longer holds. `TypeSegmented` is gone; the transaction dialog's type picker
> renders `ui/Segmented`, whose `trackClassName` keeps the two rows of four
> below `sm`, and eslint rejects any other `role="radiogroup"` ("use
> ui/Segmented").

> Amended 2026-10-04 (GLG-871): the secondary and danger-quiet buttons fill
> with `raised`, not `surface`, and secondary hovers with a third new token,
> `raised-hover` (light `#f1f4f8`, dark `#2d3545`), instead of `paper`. In
> light nothing changes but the hover (`raised` is white); in dark a `surface`
> button on a `surface` card showed only its faint ring, and the `paper` hover
> read as a dent. `ui/buttonClass.ts` holds the
> classes, and a link styled as a button takes them from `buttonClass()`.

> Superseded 2026-10-06 (GLG-1079): the `Field` row no longer holds. Inputs
> and the `Select` trigger (all of `inputClass` in `ui/Field.tsx`) keep the
> `accent` focus ring but rest on `border border-faint`, at least 3:1 on
> `surface`; `line-strong` drew that edge at 1.45:1 (light) and 1.57:1
> (dark).

> Superseded 2026-10-10 (GLG-1284): `inputClass({ size, width })` lives in
> `ui/inputClass.ts`, not `ui/Field.tsx`; callers pick a size and width
> instead of overriding its classes.

Base UI is used for two components, `Menu` and `Select`. Tooltip and popover
move to Base UI only when a page needs something the current `Toggletip`
cannot do. Thin wrappers ship without tests.

> Amended 2026-10-05 (GLG-934, GLG-936): the KPI hints (`ui/Kpi.tsx`, whose
> strip's `overflow-hidden` would clip a `Toggletip`) and the "Tarih" range
> panel (`DateRangeFilter.tsx`) are Base UI `Popover`. Base UI now covers
> `Menu`, `Select` and `Popover`; the later mentions of "`Menu` and `Select`"
> in this ADR read the same way.

> Amended 2026-10-06 (GLG-963): `ui/Checkbox` is a fourth Base UI primitive,
> on Base UI `Checkbox`, and replaces every native checkbox: `accent` fill
> when checked, `line-strong` edge when not, the global focus ring, a mixed
> state for "select all". With a visible label the whole row is a 44 px tap
> target. `conventions.test.ts` bans `type="checkbox"` in components.
> `PageHeader` is the header of a page that is not a portfolio page: a 20 px
> h1, visually hidden below `lg` like the portfolio pages' title.

> Superseded 2026-10-06 (GLG-1076): the unchecked checkbox's edge is
> `faint`, not `line-strong`, at least 3:1 on `surface`.

> Amended 2026-10-10 (GLG-1285): the instrument search
> (`InstrumentSearchInput.tsx`) is Base UI `Autocomplete`, the Base UI
> combobox for free text with suggestions, in place of a hand-rolled ARIA
> combobox. Base UI now covers `Menu`, `Select`, `Popover`, `Checkbox` and
> `Combobox`/`Autocomplete`. ESLint bans `role="combobox"` in components.

> Amended 2026-10-06 (GLG-969): Yönetim sits in the portfolio pages' wrapper
> under `PageHeader`, its section links one row that scrolls sideways with
> 44 px targets, its cards titled at 14 px in a header strip. Kaynaklar is one
> compact table (`frontend/src/components/admin/SourcesTable.tsx`, the
> classes of `ui/table.ts`): source, on or off, time (İstanbul), last
> success, and the actions; below `md` each row is a grid list item. A
> source is stale when it is on and has never succeeded or last succeeded
> more than 48 hours ago (`isStale` in `frontend/src/api/sources.ts`); its
> last success is then a `warn` pill. A source that is off is never stale.
> The table takes no props, so setup's step 2 can render it.

### App shell

`App.tsx` gets a layout route. `AppShell` renders the sidebar and an
`<Outlet/>`; every signed-in route nests under it, and `ProtectedRoute` wraps
the layout once. `/login`, `/register` and `/setup` stay outside the shell: a
centered card on `paper` with the brand mark. The locale and privacy remount
key keeps wrapping the whole tree, shell included.

> Superseded 2026-10-06 (GLG-1031): the remount key is the locale alone and
> still wraps the whole tree, shell included. A privacy toggle re-renders App
> through its `usePrivacy()` subscription without a remount, so component
> state survives it.

**Sidebar** (`frontend/src/components/Sidebar.tsx`; `Header.tsx` is deleted),
232 px wide, top to bottom:

1. Brand mark and name, linking to `/`.
2. `Tüm portföyler` with the combined total, linking to `/portfolios/all`.
3. Section `Portföyler` with a `+` button that opens the create-portfolio
   dialog, extracted from `HomePage.tsx` into its own component.
4. One row per portfolio: a color dot (`cat-1` to `cat-6` by list index,
   cycling; nothing is stored), the name (truncated), and `total_value` in
   the portfolio's currency in compact notation (`₺1,28 Mn` in `tr`), masked
   by privacy mode like any other amount. Clicking a row keeps the current
   tab; from a position page it goes to the portfolio's Özet. The section
   scrolls when the list is long.
5. Pinned at the bottom: `Yönetim` (admins only), the privacy toggle, and the
   account menu (account page, sign out).

The sidebar reads the existing `['portfolios']` query; no new request. Since
it never remounts, a transaction change and a manual price also invalidate
that query.

**Page header.** `PortfolioHeader.tsx` shrinks to a page header: title, tabs,
`İşlem ekle`, and the rename/delete `Menu`. Its portfolio `<select>` is
removed; the sidebar does that job.

**Breakpoints.** From `lg` (1024 px) the sidebar is fixed and the content
column is at least 792 px. Below `lg` a 48 px top bar holds the menu button,
the page title and the privacy toggle, and the same `Sidebar` opens in a
native `<dialog>` as a left drawer that closes on navigation. Below `md`,
`İşlem ekle` is a floating round `+` at the bottom right (the same button,
positioned with CSS). `useIsDesktop` is unchanged.

**States.** Loading shows skeleton rows in the portfolio section; on a query
error the sidebar still navigates and values show `—`; with no portfolios
the section shows a call to create the first one.

> Amended 2026-10-04 (GLG-875): on a query error the sidebar shows no
> portfolio rows and no values at all, `Tüm portföyler` included, rather than
> `—`; its static links stay. A portfolio created from the sidebar's `+` opens
> that portfolio's Özet. Below `lg` the top bar also holds the account menu,
> and until GLG-876 adds the drawer it holds only the brand link, the privacy
> toggle and the account menu; for an admin that menu also lists `Yönetim`,
> so the admin page stays reachable there.

> Amended 2026-10-04 (GLG-876): the drawer is in, at every width below `lg`,
> so the account menu no longer lists `Yönetim`; the sidebar's row in the
> drawer is the way there. The bar keeps the account menu, after the menu
> button, the brand mark and the page title. The drawer is not hidden from
> `lg` by CSS, so a tablet turned past the breakpoint is never left inert
> behind an invisible modal; any navigation closes it. Toggling privacy from
> the drawer remounts the app, which closes the drawer, and focus goes to the
> menu button.

> Superseded 2026-10-06 (GLG-1031): toggling privacy from the drawer no
> longer remounts the app, so the drawer stays open and the pressed toggle
> keeps focus. The GLG-681 focus restore now serves only the language switch.

> Amended 2026-10-05 (GLG-882): the bar's title is a plain span, not a
> heading: the portfolio's name on its pages, otherwise the page's own title
> (`Portföyler`, `Hesap`, `Yönetim`, `Sayfa bulunamadı`), never the app's
> name; it is the only item in the bar that shrinks. A signed-in user's
> unknown URL renders the not-found page inside the shell; a signed-out
> visitor still gets the plain page, with no redirect to login.

**Accessibility.** The skip link stays the first focusable element. The
sidebar is a `<nav aria-label>`; the active row gets `aria-current="page"`.
The drawer button has an accessible name and `aria-expanded`. Every new UI
string has a `tr` and an `en` key.

**Deliberate shortcut.** `/` keeps rendering `HomePage`, reskinned by the
tokens only. It overlaps the sidebar, but it holds per-portfolio cards that
the "Tüm portföyler" view lacks. Sub-project 4 folds those cards into "Tüm
portföyler" and deletes `HomePage`.

> Superseded 2026-10-05 (GLG-935): `HomePage` is deleted; `/` redirects to
> `/portfolios/all`, whose Portföyler table took over the per-portfolio
> cards.

### Rules that survive from ADR 0005

- Semantic tokens only: no raw palette classes and no `dark:` variants in
  components. From this ADR the rule also covers shadows (no raw
  `shadow-sm|md|lg|xl|2xl`), `font-mono`, and native `<select>` once the
  last one is migrated. A scanner test, `frontend/src/conventions.test.ts`
  (GLG-869), fails the build when a banned class comes back; each ticket that
  removes the last use of a pattern adds it there.
- An explicit `+` / `−` on every P/L figure, with color as the second cue.
- Number and percent formatting follows the locale through
  `Intl.NumberFormat`.
- The cash-flow sign on transaction amounts.
- Native `<dialog>` with `showModal()` for dialogs and for the drawer.
- The page never scrolls sideways; a table wider than its card scrolls inside
  the card.
- The theme choice lives in `localStorage` and is applied by
  `frontend/public/theme.js`.
- Icons are inline SVG paths in one `Icon` component.

### What this supersedes in ADR 0005

- The Defter color values (warm paper, ink primary, the `#2f5d9c` accent).
- "No shadow" on cards, and shadows only for floating layers. Every surface
  now has a shadow token.
- The radii, 10 px and 6 px.
- IBM Plex Sans and Mono, and the use of mono for symbols and axis labels.
- The ink-filled primary button.
- "No component library": Base UI is used for `Menu` and `Select`.
- "No charting library": Recharts is allowed and arrives in sub-project 3.

  > Amended 2026-10-05 (GLG-912, GLG-913): Recharts is in; both the line and
  > the bar chart are drawn by it.

- "No new runtime dependency".
- The header-based app shell with a portfolio switcher in `PortfolioHeader`.

## Alternatives considered

The options were shown to the owner as mockups during brainstorming on
2026-10-04.

| Decision        | Option                                                               | Verdict    | Why                                                                                                                                     |
| --------------- | -------------------------------------------------------------------- | ---------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Visual language | B "soft depth": Inter, tinted ground, floating cards                 | **chosen** | Owner's choice for the crisp SaaS target.                                                                                               |
| Visual language | A, Linear-like: cool grays, hairline borders, no shadow              | rejected   | Not chosen by the owner. Concern at the time: a flat hairline system like Defter, at risk of the same "plain" verdict in a cooler gray. |
| Visual language | C, Vercel-like: black on white, mono numerals                        | rejected   | Not chosen by the owner, who gave no reason. It was presented as the most distinctive and the least warm.                               |
| Visual language | Keep Defter and polish it                                            | rejected   | The complaint is about Defter's flat direction, not about bugs in it.                                                                   |
| Shell           | 2: sidebar that lists the portfolios with their values               | **chosen** | Owner's choice; it also replaces the portfolio switcher in `PortfolioHeader`.                                                           |
| Shell           | 1: sidebar that lists the pages, portfolio in a switcher             | rejected   | Two clicks between portfolios, and the other portfolios' values stay hidden in the switcher.                                            |
| Shell           | 3: top bar only                                                      | rejected   | The smallest change from today, and the least "new app" feeling.                                                                        |
| Accent          | Blue                                                                 | **chosen** | Owner's choice.                                                                                                                         |
| Accent          | Violet `#635bff`                                                     | rejected   | It is Stripe's own brand color.                                                                                                         |
| Accent          | Teal                                                                 | rejected   | Too close to the `gain` green; the accent must never read as "profit".                                                                  |
| Accent          | Ink (no accent)                                                      | rejected   | Drifts back toward the plain look.                                                                                                      |
| Type            | Inter, self-hosted variable woff2                                    | **chosen** | Visual language B is built on it; tabular numerals stay.                                                                                |
| Type            | Keep IBM Plex                                                        | rejected   | Not part of language B.                                                                                                                 |
| Dependencies    | Headless primitives (Base UI) and a chart library (Recharts)         | **chosen** | The owner's choice, to reach the target look faster.                                                                                    |
| Dependencies    | Zero new dependencies: the native Popover API and our own SVG charts | rejected   | This was the brainstorming session's recommendation; the owner chose headless primitives and a chart library instead.                   |
| Primitives      | Base UI for `Menu` and `Select`, local wrappers elsewhere            | **chosen** | One package, stable 1.x, from the authors of Radix and Floating UI, and actively developed.                                             |
| Primitives      | Radix instead of Base UI                                             | rejected   | Base UI comes from the same authors and has the advantages in the row above.                                                            |
| Chart           | Recharts (sub-project 3)                                             | **chosen** | Follows from the dependency decision above; the redesign includes the charts.                                                           |
| Chart           | Keep the hand-drawn SVG charts                                       | rejected   | Part of the zero-dependency option the owner did not choose.                                                                            |
| Migration       | Token-first, in place, `main` always works                           | **chosen** | Token names stay, so one file flips the look; no flag and no second tree.                                                               |
| Migration       | A parallel `/v2` tree behind a flag                                  | rejected   | Two copies of every page for weeks, every fix lands twice, and it collides with the tickets the hourly worker picks.                    |
| Migration       | Shell first, then skin                                               | rejected   | The shell would be styled twice, and the visible change would come last.                                                                |
| Cards           | `card` CSS utility                                                   | **chosen** | Works on any element and has no props.                                                                                                  |
| Cards           | A `Card` React component                                             | rejected   | A component for one class string.                                                                                                       |

## Dependencies

- `@base-ui/react` (Base UI, stable 1.x), added by GLG-873 for `Menu` and
  then `Select`. The package name and the React 19 peer range are confirmed
  at install time.
- Recharts, decided here and added by the charts sub-project (3), not by this
  epic.

  > Amended 2026-10-05 (GLG-912): `recharts` 3.10.1 is in. Sub-project 3 is
  > split into 3a (charts foundation: Recharts, the line and bar charts in the
  > new look, epic GLG-910), 3b (allocation donut, P/L per position, drag to
  > zoom) and 3c (a return-index series for percent mode, periodic returns and
  > drawdown), each with its own spec, plan and epic; the 3a spec is
  > [`2026-10-05-ui-v2-charts-foundation-design.md`](../superpowers/specs/2026-10-05-ui-v2-charts-foundation-design.md).
  > The line chart is `frontend/src/components/charts/LineChart.tsx`, with
  > the tooltip card in `charts/ChartTooltip.tsx` and the jsdom size helper
  > in `src/test/chartSize.ts`; the old `components/LineChart.tsx` is gone.
  > Recharts draws: scales, paths, the area, the grid, the cursor, tooltip
  > placement (but see GLG-922 below), animation (300 ms, left on
  > `isAnimationActive="auto"` so reduced motion turns it off), resizing,
  > touch and the keyboard layer. Ours
  > stay: printed amounts from the exact decimal strings, the y ticks and
  > domain from `axisTicks` and `formatTicks`, the date labels, privacy
  > masking, the marker shapes, gaps that are never drawn to zero, and the
  > legend as our own HTML. Its cost and benchmark entries are toggle buttons
  > that hide and show their series, per chart and unsaved (GLG-914). Two
  > behaviour changes: Home and End no longer jump
  > to the ends (Recharts has no supported way to set the active point from
  > outside; Left and Right still move point by point), and the chart's role
  > is `application`, Recharts' default, instead of `img`. A touch tooltip now
  > stays until the next tap elsewhere instead of two seconds. Bundle: main
  > chunk `index-*.js` 730.68 kB (218.69 kB gzip), charts chunk `charts-*.js`
  > 386.81 kB (110.60 kB gzip), against 739.97 kB (221.52 kB gzip) for the
  > main chunk before Recharts (2d922af) and 1,116.76 kB (329.62 kB gzip) with
  > both charts in it (d029c98). Since GLG-922 the line chart places its
  > tooltip itself, through the `Tooltip` `position` prop: 12 px below the
  > active point in the upper half of the plot and above it in the lower
  > half, inside the plot, as before the rewrite (GLG-779), for mouse, touch
  > and keyboard alike.
  >
  > Amended 2026-10-05 (GLG-915): pages get their charts from
  > `frontend/src/components/LazyChart.tsx`, which loads both charts and
  > Recharts as one chunk (`components/charts/index.ts`) on the first chart
  > a page renders, with a skeleton of the chart's height meanwhile; pages
  > without a chart never fetch it. `conventions.test.ts` fails when a file
  > outside `components/charts/` imports `recharts`, or one other than
  > `LazyChart.tsx` imports from `components/charts/`. When the chunk fails
  > to load, the chart's card shows `chart.error` and the rest of the page
  > works. It stays so on every chart page until a reload: React keeps a
  > rejected lazy component, and Chromium keeps a failed module fetch for
  > the document (a second `import()` of the same URL fails with no
  > request), so an in-page retry would only repeat the error.
  >
  > Amended 2026-10-06 (GLG-946): the line chart labels one date per about
  > 120 px of plot width, at least three and at most seven, evenly spaced and
  > always the first and the last (`dateTicks` in `lib/chart.ts`, fed the
  > width of Recharts' plot area), instead of the first, middle and last at
  > any width; the year rule (GLG-809) stays. Its legend reserves two lines
  > below `sm` and one from `sm` up (`LEGEND_HEIGHT`), and the placeholder
  > reserves the same, so the card no longer grows when the chunk arrives
  > with a legend that wraps.
  >
  > Amended 2026-10-06 (GLG-947): the allocation card draws a donut in place
  > of the stacked bar: `components/charts/AllocationDonut.tsx` on Recharts'
  > `PieChart`, 150 px with a 16 px ring, slices in the legend's order from
  > twelve o'clock with a 1° gap, "Toplam" and the compact total in the
  > middle as our own HTML, and the shared tooltip card on hover. It comes
  > from the same charts chunk through `LazyChart.tsx`, with a ring of its
  > size as the placeholder; when the chunk fails nothing takes its place.
  > Beside it, and under it below `md`, the legend is an HTML list of swatch,
  > name, amount and share; "Diğer (n)" names its members in `title`. The
  > legend is the accessible content: the donut is `aria-hidden`, not a focus
  > stop and has no keyboard layer. Hovering a slice or a legend row
  > highlights the pair. The grouping rules are unchanged and live in
  > `allocationRows` (`lib/allocation.ts`), which sums the amounts exactly;
  > the slices use `var(--color-cat-*)` and `var(--color-cash)`.
  >
  > Amended 2026-10-06 (GLG-1027): the legend goes beside the donut once the
  > card body is 28rem wide (a `@container` with `@md:flex-row` in
  > `Allocation.tsx`), not from the `md` viewport, and under it below that;
  > in the half-width card from `lg` it stays under the donut until the card
  > itself reaches that width.
  >
  > Amended 2026-10-06 (GLG-949): drag to zoom. `LineChart` takes
  > `onZoom(from, to)`, the first prop added since the charts foundation:
  > with a mouse, a press, a sideways drag and a release on the plot shade the
  > span in the accent colour (a `ReferenceArea` with a line at each end) and
  > hand its first and last dates to `onZoom`, which Özet, Tüm portföyler and
  > the position page turn into an Özel range through `useRange`
  > (`useZoom`), so the headline return, the benchmark and the URL follow. A
  > drag within one point or released outside the plot does nothing, and
  > Escape cancels. Touch and pen never zoom: the drag reads the pointer type
  > of the `pointerdown`, adds no listener that calls `preventDefault`, and
  > its window listeners for Escape and a release outside the chart live only
  > while a drag is on. Özel then shows a clear button returning to the range
  > before the first drag, kept in component state, so after a reload it
  > returns to the default range. A drag must also span at least 8 px
  > sideways (`MIN_ZOOM_DRAG_PX`, GLG-1032), so a wobbly click on a dense
  > series, a point every 1.5 px, does not zoom.
  >
  > Amended 2026-10-06 (GLG-948): Özet's lower rows are Dağılım beside a new
  > "Katkı" card, stretched to one height from `lg`, then İstatistikler as a
  > full-width `KpiStrip` of five cells (two columns on a phone, three from
  > `sm`, five from `lg`) whose heading is for screen readers only; below
  > `lg` they stack in that order, on Özet and Tüm portföyler alike. Katkı
  > (`frontend/src/components/ContributionCard.tsx`) draws one bar per open
  > position on a shared zero line, a gain right in `gain` and a loss left
  > in `loss`, the longest spanning half the track, with the signed amount at
  > the row's end; a "Toplam | Bugün" switch picks the reporting-currency
  > unrealized P/L or day change. It shows the four best and the four worst
  > (`contributionRows` in `lib/contribution.ts`, ties by symbol, positions
  > without the value left out) and "Tümünü göster" expands to all. The bars
  > are plain HTML in a CSS subgrid, not Recharts, so the card never waits
  > for the charts chunk; the list of symbols and amounts is the accessible
  > content. Symbols link to the position page except on Tüm portföyler;
  > privacy mode masks the amounts and keeps the bars.
  >
  > Amended 2026-10-07 (GLG-1099): İstatistikler moved from under Dağılım
  > and Katkı to directly under the KPI strip; the page order is in the
  > GLG-1099 note at the top of Decision.
  >
  > Amended 2026-10-06 (GLG-954): percent mode. `LineChart` takes
  > `unit: 'money' | 'percent'` (default `'money'`). In percent it reads the
  > same point fields as fractions: y ticks from `axisTicks` printed by
  > `formatPercentTicks` (`lib/chart.ts`), a solid zero line in
  > `line-strong`, no area fill, and signed percentages (`signedPercent`) in
  > the tooltip and the screen reader summary, never masked in privacy mode;
  > with the benchmark shown the tooltip adds "fark", (1 + value) /
  > (1 + benchmark) - 1, as the return card's pill computes it, named
  > "reel" against TÜFE as the pill is (`labels.excess`, GLG-1050). The return
  > card has a "% | ₺" segmented switch, stored in `localStorage` as
  > `chartMode` and opening in percent, that passes the history points'
  > `twr` and `benchmark_twr` with no cost line, so both lines start at 0%
  > and a deposit does not bend them; "₺" (the reporting currency's symbol)
  > is the value chart as before. The percent line is always time-weighted:
  > with another method its legend entry reads "Portföy (zaman ağırlıklı)",
  > with a `title` saying why it can end at another number than the
  > headline. With `twr` null for the range the card shows the value chart,
  > disables the switch and shows `chart.return.undefined` beside it. Drag
  > to zoom, the legend toggles and the range in the URL work in both modes.

`docs/PRINCIPLES.md` #1 still says a new dependency comes last, and that
stays true. These two are the exception because the owner chose
library-built menus, selects and charts over hand-built ones to reach the
target look faster. No other dependency is added under this ADR.

## Known risks

- **Select tests.** Tests that use `selectOptions` on a native `<select>`
  break when Base UI `Select` replaces it. This is the main hidden cost of
  GLG-874 and GLG-877.
- **CSP.** `deploy/nginx.conf` sends `default-src 'self'` with no
  `style-src`, so injected `<style>` elements and inline style attributes in
  HTML are blocked, while styles set through the CSSOM (React style props)
  work. GLG-873 runs a production build behind that policy and confirms that
  Base UI's popup positioning works before anything is migrated. If it
  injects a `<style>` element, the ticket stops and the owner decides; any
  policy change is recorded here.

  > Amended 2026-10-04 (GLG-874): `Menu` injects nothing, but `Select.Popup`
  > renders a `<style>` element (the `base-ui-disable-scrollbar` rule), which
  > the policy blocks. The policy is unchanged. `main.tsx` wraps the app in
  > Base UI's documented `CSPProvider disableStyleElements`, so no Base UI
  > component renders a `<style>` element, and the same two-line rule lives
  > in `index.css`. A production build behind the policy shows no violation.

- **Top layer.** `TransactionDialog` is a native modal `<dialog>` in the
  browser's top layer. A `Select` popup portaled to `<body>` would sit under
  it and be inert, so the `Select` primitive portals into the closest
  `<dialog>` when it has one.
- **Glyph coverage.** If the Inter subset lacks ₺ or a Turkish letter, that
  glyph falls back to the system font and the numbers stop lining up. GLG-869
  checks this in a browser.
- **Collisions.** The hourly worker may open frontend PRs mid-epic. GLG-870
  and GLG-875 are the ones that conflict; the merge order below and a quiet
  board during GLG-870 limit this.
- **Mixed period.** Until sub-projects 3 to 5 ship, pages have Defter layouts
  and hand-drawn charts inside the new skin and shell. Accepted by the owner.

  > Amended 2026-10-05 (GLG-912): the line chart is drawn by Recharts; the
  > bar chart stays hand-drawn SVG until GLG-913.
  >
  > Amended 2026-10-05 (GLG-913): the bar chart is drawn by Recharts too, in
  > `frontend/src/components/charts/BarChart.tsx`. Like the line chart its
  > role is `application` and Home and End are gone; focus starts on the
  > first bar, the hovered bar is marked by a `--color-well` band behind its
  > slot instead of an ink outline, and Recharts thins the x labels.
  >
  > Amended 2026-10-05 (GLG-928): Phase 26 shipped sub-project 4, so Özet,
  > Tüm portföyler, İşlemler, the position page and Temettü have their UI v2
  > layouts. Only the pages of sub-project 5 keep Defter layouts: İçe aktar,
  > Hesap, Yönetim and the signed-out pages.
  >
  > Amended 2026-10-06 (GLG-971): Phase 29 shipped sub-project 5, so the
  > mixed period is over: İçe aktar, Hesap, Yönetim, the signed-out pages
  > and setup have their UI v2 layouts too.

## Consequences

Epic GLG-867 (Phase 24), one PR per ticket, label `ui`:

- GLG-868 writes this ADR and the dated notes in ADR 0005 and ADR 0021, and
  updates the ADR index, `CLAUDE.md`, `frontend/README.md` and
  `docs/ROADMAP.md`.
- GLG-869 sets the color, shadow, radius and font tokens, replaces IBM Plex
  with Inter, removes `font-mono`, extends the contrast test and adds the
  convention scanner.
- GLG-870 adds the `card` utility, replaces the inline card strings and moves
  the raw shadow classes to the shadow tokens. It merges alone, with no other
  frontend PR open.
- GLG-871 restyles `Button` and `Badge`.
- GLG-872 restyles the segmented controls and the portfolio tabs.
- GLG-873 adds Base UI and the `Menu` primitive and migrates the portfolio
  menus, after the CSP check.
- GLG-874 adds the `Select` primitive and migrates the transaction dialog.
- GLG-875 adds `AppShell` and `Sidebar`, deletes `Header.tsx`, extracts the
  create-portfolio dialog, shrinks `PortfolioHeader` and invalidates
  `['portfolios']` on transaction and price changes.
- GLG-876 adds the drawer and page title below `lg` and the floating `+`
  below `md`.
- GLG-877 migrates the remaining native selects.

Merge order: GLG-868, GLG-869, GLG-870 alone, then GLG-871 to GLG-873 in
parallel, then GLG-874, then GLG-875, then GLG-876 and GLG-877 in parallel.

- The "no chart library (ADR 0005)" comments in `LineChart.tsx` and
  `BarChart.tsx` stay until the charts sub-project makes them false.

  > Amended 2026-10-05 (GLG-912): the `LineChart.tsx` comment went with the
  > rewrite on Recharts; `BarChart.tsx` carried no such comment.

- Two runtime dependencies are allowed under this ADR: `@base-ui/react` now
  and Recharts later.

  > Amended 2026-10-05 (GLG-912, GLG-913): Recharts arrived with the charts
  > foundation (3a); both dependencies are now in.
