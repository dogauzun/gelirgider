# ADR 0005: UI design system ("Defter") — semantic tokens, fonts, theming, native dialogs

Status: accepted (GLG-174, 2026-09-14); amended 2026-09-15 (GLG-178,
GLG-180, `on-ink` token); amended 2026-09-25 (GLG-394, chart range delta and
the `returns` percentage); amended 2026-09-27 (GLG-480, darker `muted`
token); amended 2026-10-02 (GLG-803, `faint` raised to 3:1; GLG-794,
`loss-line` token); superseded in part by
[ADR 0023](0023-ui-design-system-v2.md) (GLG-868, 2026-10-04):
the look is UI v2, the rules ADR 0023 lists as surviving still hold; amended
2026-10-07 (GLG-1115, personal disclosure removed); amended 2026-10-13
(pre-publication review, the unpublished design canvas is no longer cited
as a reference)

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868): ADR 0023 replaces
> the Defter look with UI v2 ("soft depth"). The token colors, the shadows,
> the radii, the fonts, the component and chart library rules and the app
> shell below are superseded; the rules ADR 0023 lists under "Rules that
> survive from ADR 0005" still hold.

Visual reference: a private design canvas that is not published; the
committed mockups under `docs/superpowers/specs/` are the public reference
since [ADR 0023](0023-ui-design-system-v2.md).

## Decision

One visual system for the whole app, in light and dark. Warm paper ground,
white cards with hairline borders, ink primary, one blue accent, IBM Plex
Sans/Mono, tabular numbers. No component library and no charting library —
the app already draws its own chart, and the pieces it needs are small.

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868): a cool tinted ground,
> white cards that float on shadow tokens instead of hairline borders, a blue
> (`accent`) primary instead of ink, Inter instead of IBM Plex. Base UI is
> used for menus and selects, and Recharts is allowed for the charts.

### Semantic color tokens

One set of semantic tokens lives in `frontend/src/index.css` under `@theme`
(GLG-156). Because Tailwind v4 utilities resolve `var(--color-*)`, a single
definition gives `bg-paper`, `text-ink`, `border-line` and friends.

| Token         | Light     | Dark      | Used for                                     |
| ------------- | --------- | --------- | -------------------------------------------- |
| `paper`       | `#f5f4f0` | `#161614` | Page background                              |
| `surface`     | `#ffffff` | `#1e1d1a` | Cards, dialogs, floating layers              |
| `line`        | `#e6e3dc` | `#2c2b27` | Hairline borders, table rules                |
| `line-strong` | `#d4d0c7` | `#3a3934` | Emphasised borders, input outlines           |
| `ink`         | `#1c1b18` | `#ece9e2` | Primary text, headings, numbers              |
| `ink-2`       | `#4a4843` | `#c9c6bd` | Secondary text                               |
| `on-ink`      | `#ffffff` | `#161614` | Text and graphics on an `ink` or `loss` fill |
| `muted`       | `#6b6860` | `#95928a` | Labels, captions, table headers              |
| `faint`       | `#858279` | `#85827a` | Placeholders, disabled, axis text            |
| `chip`        | `#efede8` | `#2a2926` | Chips, pills, inert badges                   |
| `accent`      | `#2f5d9c` | `#7fa6e0` | Primary action, links, focus ring            |
| `accent-ink`  | `#254a7c` | `#a9c4ec` | Accent text on `accent-soft`                 |
| `accent-soft` | `#e9eff8` | `#1f2c40` | Accent background                            |
| `accent-line` | `#c9d7ec` | `#2f4568` | Accent border                                |
| `gain`        | `#1e7a4d` | `#5fc08a` | Positive P/L                                 |
| `gain-soft`   | `#e6f3ec` | `#12301f` | Positive background                          |
| `loss`        | `#b83227` | `#e5766b` | Negative P/L, destructive                    |
| `loss-soft`   | `#f9e9e7` | `#3a1c19` | Negative background                          |
| `loss-line`   | `#efc4bf` | `#5c2a24` | Error border (GLG-794)                       |
| `warn`        | `#8a5a00` | `#e0b45a` | Warnings, missing rates                      |
| `warn-soft`   | `#fbf3dd` | `#2e2510` | Warning background                           |
| `warn-line`   | `#efd9a1` | `#4a3a14` | Warning border                               |
| `plum`        | `#5b3f8a` | `#b79ee0` | Dividends, secondary category                |
| `plum-soft`   | `#efe9f7` | `#2a2138` | Plum background                              |
| `cash`        | `#c8c4ba` | `#4a4843` | Cash slice in allocation                     |
| `cat-1`       | `#2f5d9c` | `#5b86c9` | Allocation category 1                        |
| `cat-2`       | `#2f8f83` | `#4fae9f` | Allocation category 2                        |
| `cat-3`       | `#c08a2a` | `#d4a04a` | Allocation category 3                        |
| `cat-4`       | `#7b5ea7` | `#9b82c6` | Allocation category 4                        |
| `cat-5`       | `#b8633a` | `#cf7d55` | Allocation category 5                        |
| `cat-6`       | `#6b7d8f` | `#8a9bad` | Allocation category 6                        |

> Amended 2026-09-15 (GLG-171): the set also has a `scrim` token for the
> dialog backdrop, which the table above does not list. It is
> `rgb(28 27 24 / 0.35)` in light and `rgb(0 0 0 / 0.6)` in dark, with the
> alpha part of the token so the dark backdrop is not an inverted light one.

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868): the token names stay
> but every value in the table above, and the `scrim` values, are replaced by
> the table in ADR 0023. Three shadow tokens (`shadow-card`, `shadow-ctl`,
> `shadow-pop`) join the set.

Radii are tokens too: `--radius-card: 10px`, `--radius-ctl: 6px`.

**The rule that makes this hold: components use semantic tokens only.** No
raw Tailwind palette classes (`slate-*`, `red-*`, `emerald-*`, `amber-*`) and
no `dark:` variants in components. A color that needs a dark twin becomes a
token instead. Dark mode then works by construction — a page written against
the tokens is correct in both themes without the author thinking about dark
at all, and nothing can drift because there is one place where a color is
decided. A `dark` custom variant exists for the rare one-off, not as the
normal way to write a component.

### Dark mode and the theme choice

The same variables are redefined in two places: `[data-theme="dark"]`, and
`@media (prefers-color-scheme: dark)` scoped to `:root:not([data-theme="light"])`
— so an explicit light choice wins over the OS, and the OS wins when no
choice was made. Each block also sets `color-scheme` so form controls and
scrollbars follow.

The user choice (system / light / dark) lives in `localStorage['theme']` and
is written onto `<html data-theme>` by an inline script before first paint,
so there is no flash of the wrong theme. This is browser-only, exactly like
the locale in GLG-146, and has the same upgrade path: a column on the user
row if multi-device sync is ever wanted. It is not wanted yet — a per-device
theme is arguably the right behaviour anyway.

> Superseded 2026-09-27 (GLG-473): the theme is applied by the file
> `frontend/public/theme.js`, which `index.html` loads before the stylesheet,
> not by an inline script, so the Content-Security-Policy in
> `deploy/nginx.conf` can keep `script-src 'self'`.

### Type

IBM Plex Sans 400/500/600 and IBM Plex Mono 500, self-hosted as woff2 under
`frontend/public/fonts` with the OFL 1.1 license file committed next to them,
`font-display: swap`, system stack as fallback. Tabular numerals everywhere
(`font-variant-numeric: tabular-nums` on `body`) so columns of money line up.
Mono is used for instrument symbols and chart axis labels only, never for
body text.

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868): the font is Inter, a
> variable font self-hosted as two subset woff2 files (latin and latin-ext)
> with its OFL 1.1 license. IBM Plex Sans and Mono and `--font-mono` are
> removed; instrument symbols are Inter semibold and chart axis labels Inter.
> Tabular numerals stay on `body`.

Alternative considered: the system font stack — zero bytes, but numerals
differ across platforms, which is visible in every table in this app.
Rejected 2026-09-14.

### Shape and control sizes

Cards are `rounded-card` (10px) with a 1px `line` border and **no shadow**.
Shadows are reserved for floating layers — dialog, tooltip, toast, menus —
so elevation means "this is on top of the page", not decoration. Controls are
`rounded-ctl` (6px): 40px inputs (44px on auth pages, where the form is the
page), 36px buttons.

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868): `--radius-card` is
> 12px and `--radius-ctl` 8px, pills and badges are fully round, and cards
> have no border and float on `shadow-card`. Controls take `shadow-ctl`;
> floating layers take `shadow-pop`. The primary button is an `accent` fill
> with `on-ink` text.

### Numbers

- Explicit `+` / `−` sign on every P/L figure. Color is the second cue, never
  the only one.
- A percentage next to every absolute where a base exists.
- Percent formatting follows the locale through `Intl.NumberFormat`
  (`% 18,7` in `tr`, `18.7%` in `en`), the GLG-149 mechanism.

Derived figures are computed client-side, and this is how:

- unrealized return % = unrealized ÷ (total value − cash − unrealized)
- position return % = unrealized ÷ cost basis
- chart range delta = last point − first point − `net_flow` (GLG-219), the
  absolute change; the percentage next to it comes from the API's `returns`
  object ([ADR 0011](0011-portfolio-returns.md)), selectable between simple,
  time-weighted and money-weighted

The transactions table shows amounts with the **cash-flow sign**: buy,
withdrawal and fee negative; sell, dividend and deposit positive. This is
presentation only — the API amount stays unsigned and the sign is derived
from the transaction type at render time. Decided 2026-09-14.

`GET /api/portfolios` gains `unrealized_pl` and `cash` so the portfolio cards
can show them without an N+1 of summary calls (GLG-158). Decided 2026-09-14
over a cards-without-P/L variant, which would have left the list page the
only place in the app that shows a value without its P/L.

### Components

Shared primitives are thin wrappers under `frontend/src/components/ui/`:
Button, Field, Notice, Pill, Badge, Skeleton, Icon, ConfirmDialog, Toast.
Icons are inline SVG paths in one `Icon` component — never an icon font,
never emoji.

Dialogs use the native `<dialog>` element with `showModal()`, which brings
focus trapping, `Esc`, the top layer and the backdrop for free. Below `md`
they render as bottom sheets. `window.confirm` is gone; destructive
confirmations name what is lost on the button itself ("Delete 12
transactions"), not just "OK".

Feedback: skeleton blocks while loading, inline errors that keep the page
usable, and a toast store for saves and deletes — a module store on
`useSyncExternalStore`, the same shape as the i18n module, so no context
provider and no new dependency.

Phone layout: below `md`, tables become lists (positions, transactions).
Nothing scrolls horizontally anywhere.

> Amended 2026-09-25 (GLG-322): a table that is wider than its card scrolls
> sideways inside the card, by decision, while the page itself never does.
> This holds for the positions table (`SummaryPage.tsx`, GLG-322), the
> transactions table (`TransactionsPage.tsx`, GLG-322), the import preview
> (`ImportPage.tsx`, GLG-417) and the dividends table (`DividendsPage.tsx`,
> GLG-605).

One app shell: a header (mark, `Portföyler`, language pill, account menu)
plus a `PortfolioHeader` on portfolio pages (name switcher, tabs Özet /
İşlemler, `İşlem ekle`, menu). Per-page "back" links are removed — the shell
is the navigation.

> Amended 2026-09-29 (GLG-572, GLG-605): the language pill left the signed-in
> header on 2026-09-28 (GLG-572), and the language is now chosen on the
> account page. The header holds the mark, `Portföyler`, a `Yönetim` link for
> admins (GLG-306), a privacy toggle that masks money amounts (GLG-564) and
> the account menu. The portfolio tabs are Özet, İşlemler, Temettü (GLG-605)
> and İçe aktar; the all-portfolios view shows Özet only (GLG-540).

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868): the header is replaced
> by a sidebar that lists the portfolios with their values, with `Yönetim`,
> the privacy toggle and the account menu pinned at its bottom, and a drawer
> below `lg`. `PortfolioHeader` becomes a page header without the portfolio
> switcher.

## Alternatives considered

| Decision        | Option                                    | Verdict          | Why                                                                    |
| --------------- | ----------------------------------------- | ---------------- | ---------------------------------------------------------------------- |
| Color           | Semantic tokens in `@theme`               | **chosen**       | One definition point; dark mode holds by construction.                 |
| Color           | Raw palette classes plus `dark:` variants | rejected         | Every component becomes a place dark mode can be forgotten.            |
| Type            | IBM Plex, self-hosted woff2               | **chosen**       | Consistent tabular numerals on every platform; OFL 1.1.                |
| Type            | System font stack                         | rejected         | Zero bytes, but numerals differ per platform — visible in every table. |
| Type            | Google Fonts CDN                          | rejected         | Third-party request on every page load for files we can commit.        |
| Components      | Thin local primitives                     | **chosen**       | Nine small wrappers beat a dependency and its upgrade treadmill.       |
| Components      | A component library (shadcn, MUI, …)      | rejected         | Far more surface than this app uses; conflicts with the token rule.    |
| Dialogs         | Native `<dialog>` + `showModal()`         | **chosen**       | Focus trap, `Esc`, top layer, backdrop — all free, no library.         |
| Dialogs         | `window.confirm` (today)                  | rejected         | Unstyleable, cannot name what is lost, blocks the thread.              |
| Chart           | Keep the hand-drawn SVG chart             | **chosen**       | It already works; theming it is a few token references.                |
| Chart           | A charting library                        | rejected         | A runtime dependency for one line chart and one bar.                   |
| Theme storage   | `localStorage` + `<html data-theme>`      | **chosen**       | Same pattern as the locale (GLG-146); no API, no migration.            |
| Theme storage   | Column on the user row                    | rejected for now | Multi-device sync is not asked for; per-device theme is defensible.    |
| Portfolio cards | `unrealized_pl` + `cash` on the list API  | **chosen**       | One request; the cards match the rest of the app.                      |
| Portfolio cards | Cards without P/L                         | rejected         | Would be the only value in the app shown without its P/L.              |

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868): the Type rows (Inter
> replaces IBM Plex), the Components rows (Base UI is used for `Menu` and
> `Select`; the other primitives stay local) and the Chart rows (Recharts is
> allowed and replaces the hand-drawn charts in the charts sub-project) no
> longer hold.

## Known risks

- The "semantic tokens only" rule is a convention, not something the build
  enforces. It is checked in review; a lint rule banning raw palette classes
  is the upgrade path if it starts to slip.
- Self-hosted fonts add roughly 100 KB of woff2 to the first load.
  Mitigation: `font-display: swap` and a preload of the regular weight only,
  so text is visible immediately in the fallback stack.

  > Correction 2026-09-14 (GLG-156): the four woff2 files that GLG-156
  > committed to `frontend/public/fonts` total about 247 KB, not roughly
  > 100 KB, and they have not changed since.

- Dark values were picked by eye against the light set, not measured for
  contrast ratios. Any pair that turns out to fail WCAG AA in use gets the
  token adjusted — which is a one-line change precisely because there is one
  definition point.

  > Amended 2026-09-27 (GLG-480): `frontend/src/index.css.test.ts` now checks
  > that `muted` text meets WCAG AA (4.5:1) on `paper`, `surface` and `chip`
  > in the light and both dark blocks; the other pairs are still unmeasured.

- The native `<dialog>` element needs no polyfill in current browsers, but
  it does mean the bottom-sheet variant is CSS on the same element rather
  than a second component. Accepted.
- Migration is incremental: each page ticket moves its own page from the raw
  palette to the tokens, so `slate-*` classes and the new tokens coexist
  until the last page ticket lands. That window is the epic, not longer.

  > Resolved 2026-09-15 (GLG-155): the last page ticket of the epic landed,
  > and no component in `frontend/src` uses a raw palette class any more.

## Consequences

- GLG-156 defines the tokens, the `@font-face` rules and the base styles.
  Every other ticket in the epic builds on it.
- GLG-157 replaces the Vite template favicon with the brand mark and deletes
  the unused icon sprite.
- GLG-158 adds `unrealized_pl` and `cash` to `GET /api/portfolios`.
- GLG-159 builds the primitives; GLG-160 and GLG-161 the chart and the
  allocation bar; GLG-162 the ConfirmDialog and Toast.
- GLG-163 to GLG-173 migrate the pages, add the app shell, the transaction
  dialog, the theme switch, the portfolio sub-header and the type filter.
- `frontend/src/index.css` is touched by GLG-156 only; `main.tsx` and
  `Header.tsx` by GLG-166 only. The epic (GLG-155) records the rest of the
  one-ticket-at-a-time file ownership.
- No new runtime dependency anywhere in this epic. The fonts are files.

  > Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868): ADR 0023 adds
  > `@base-ui/react` and, in the charts sub-project, Recharts.
