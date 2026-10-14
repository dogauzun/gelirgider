# ADR 0021: Product and UI decisions of Phases 15, 20 and 21, and i18n

Status: accepted (GLG-714, 2026-10-01); amended 2026-10-01 (GLG-716, the
cited ADR 0005 line moved); superseded in part by
[ADR 0023](0023-ui-design-system-v2.md) (GLG-868, GLG-875, 2026-10-04): UI
v2 replaces the Defter look and the header, and a chart library is allowed;
amended 2026-10-05 (GLG-913, the charts moved to `components/charts/` and
the bar chart is drawn by Recharts); amended 2026-10-05 (GLG-936, the
İşlemler toolbar and its always-visible row actions); amended 2026-10-05
(GLG-934, Özet layout A: the KPI strip and the İstatistikler card);
amended 2026-10-06 (GLG-947, the allocation views drawn as a donut).
It records decisions taken in Phases 15, 20 and 21 that had no ADR, and the
i18n approach, which had no record at all.

## Context

Phases 15 and 20 were planned with no new ADR (Phases 15
and 20), and Phase 21 recorded its decisions as
amendments to ADR 0006, 0009 and 0011 only (Phase 21). The
rest lives only in the roadmap, which is about to be split. The English UI
shipped as a Phase 6 bullet with no decision record (Phase 6).

This ADR describes the code as it is on 2026-10-01 and cites the roadmap as
it is today. A reason is given where the roadmap, a commit or a code comment
recorded one; otherwise it says "reason not recorded". Decisions already in
ADR 0006, 0009, 0011 and 0014 are not repeated.

## Decision

### Phase 15: quick wins

**Transactions CSV export.** `GET /api/portfolios/{id}/transactions.csv`
lets transactions leave the app (commit 192b7ca, GLG-563).

- It is comma separated, UTF-8 with no BOM, with dot decimals, raw type
  codes and the oldest row first (`backend/internal/transaction/handlers.go:609-612`).
- Split rows are left out, because they are derived from market data and
  not entered (`backend/internal/transaction/handlers.go:612-613`,
  `backend/internal/transaction/handlers.go:675`).
- The columns are frozen as
  `date,type,symbol,quantity,price,amount,fee,currency,note,market`, so a
  spreadsheet or script built on them keeps working; a new column goes at
  the end (`backend/internal/transaction/handlers.go:604-607`). Phase 21
  appended `market` after `note` (GLG-600, Phase 15).
- A symbol, note or market cell starting with `=`, `+`, `-` or `@` gets a
  leading `'`, so a spreadsheet does not run it as a formula
  (`backend/internal/transaction/handlers.go:710-717`). The roadmap names
  the note only (Phase 15); GLG-623 extended the guard to
  the symbol and the market (commit 7890425).
- The export takes the list's `type`, `instrument_id`, `from` and `to`
  filters (GLG-601, GLG-613), `from` and `to` being an inclusive
  `YYYY-MM-DD` range (`backend/internal/transaction/handlers.go:473-492`,
  `backend/internal/transaction/handlers.go:614-616`).

> Amended 2026-10-05 (GLG-936): the export also takes `import_id`, the
> Kaynak filter of İşlemler. The page's toolbar is one row from `md`:
> instrument search, type switch, a "Tarih" popover holding the two date
> inputs (`frontend/src/components/DateRangeFilter.tsx`), the Kaynak select,
> then the count and the CSV link; below `md` the filters sit in a sheet
> behind one "Filtrele" button.

**Privacy mode.** An eye button in the header masks every money amount, for
showing the app to someone (`frontend/src/lib/privacy.ts:1-2`,
`frontend/src/components/Header.tsx:89-99`).

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-875):
> `Header.tsx` is deleted. The eye button is `PrivacyToggle` in
> `frontend/src/components/Sidebar.tsx`, pinned at the bottom of the sidebar
> from `lg` and in the top bar of `frontend/src/components/AppShell.tsx`
> below it. What it masks is unchanged.

- Money is masked as `•••` by the format helpers; quantities and
  percentages stay visible (`frontend/src/lib/format.ts:4-6`,
  `frontend/src/lib/format.ts:31`); reason not recorded. The LineChart
  tooltip and axis and the BarChart ticks are masked too
  (`frontend/src/components/LineChart.tsx:19-32`,
  `frontend/src/components/BarChart.tsx:14-16`).
- The flag lives in `localStorage['privacy']`, per device, like the theme
  and the locale, and storage that is disabled leaves amounts visible
  (`frontend/src/lib/privacy.ts:7-17`).
- App keys its subtree on the flag, so every amount re-renders when it
  flips (`frontend/src/App.tsx:73-76`).

> Superseded 2026-10-05 (GLG-913): `frontend/src/components/LineChart.tsx`
> and `frontend/src/components/BarChart.tsx` are deleted. The charts are
> `frontend/src/components/charts/LineChart.tsx` (GLG-912) and
> `frontend/src/components/charts/BarChart.tsx`, drawn by Recharts
> ([ADR 0023](0023-ui-design-system-v2.md)); their ticks and tooltips are
> masked as before.

> Superseded 2026-10-06 (GLG-1031): App no longer keys its subtree on the
> flag; it subscribes to it (`usePrivacy()` in `frontend/src/App.tsx`) and
> re-renders, so every amount still re-runs its formatter and component
> state survives a toggle. The remount key is the locale alone.

**Stale-price badge.** A fetched price older than 2 calendar days for
CRYPTO, which trades daily, or older than 7 for every other market, shows a
warning pill with its date (`frontend/src/lib/date.ts:45-54`). The reason is
that a fetcher that stopped updating an instrument must not keep valuing it
silently (`frontend/src/pages/SummaryPage.tsx:155-157`). Manual prices are
exempt, since they carry their own dated pill
(`frontend/src/pages/SummaryPage.tsx:157-160`). There is no holiday
calendar, so a long bayram can flag BIST briefly
(`frontend/src/lib/date.ts:48`).

> Superseded 2026-10-05 (GLG-933): the positions table moved to
> `frontend/src/components/PositionsTable.tsx`. A stale or manual price is a
> small "eski" or "manuel" pill after the price, its date in the pill's
> tooltip rather than on the pill. The rule above is unchanged.

**Installable PWA.** The app ships a manifest with 192 and 512 px icons,
`display: standalone` and a theme color
(`frontend/public/manifest.webmanifest:1-11`), plus an Apple touch icon and
one `theme-color` meta per OS color scheme (`frontend/index.html:6-11`).
There is no service worker; an offline service worker is listed as not in
the epic (Phase 15). Reason not
recorded.

**Allocation views.** The allocation card toggles between one row per
symbol, per market and per currency, and starts on symbol
(`frontend/src/components/Allocation.tsx:25-27`,
`frontend/src/components/Allocation.tsx:58`). In the currency view each
currency's cash joins that currency's positions, so USD cash counts as USD
exposure (`frontend/src/components/Allocation.tsx:60-82`,
Phase 15).

> Amended 2026-10-06 (GLG-947): the views and their rules moved unchanged
> into `allocationRows` in `frontend/src/lib/allocation.ts`, and the card
> draws them as a donut beside a legend with amounts and shares instead of
> one stacked bar ([ADR 0023](0023-ui-design-system-v2.md)).

**Adding an instrument.** `POST /api/admin/instruments` is admin-only,
because instruments are a global table
(`backend/internal/instrument/routes.go:21-27`). It is idempotent: a known
market and symbol returns 200 with the existing instrument, name unchanged
(`backend/internal/instrument/routes.go:94-95`,
`backend/internal/instrument/instrument.go:277-301`). Reason for the
idempotency not recorded. The allowed market and currency pairs come from
`seedSources`, so a new seeded market is accepted without a second list
(`backend/internal/instrument/routes.go:83-92`).

### Phase 20: UI/UX round 2

Each fix was one small frontend ticket with no API change, inside the
Defter system of ADR 0005 (Phase 20).

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868): the Defter system is
> replaced by UI v2. The fixes below keep their behavior; their look follows
> ADR 0023.

- **Expected dividends panel.** It became a native `<details>` element,
  collapsed by default, with the count and net total in its summary line,
  and the count badge left the İşlemler tab, because it counted pending
  dividends and read as a transaction count (Phase 20,
  `frontend/src/components/ExpectedDividends.tsx:66-69`). Phase 21 changed
  this: the panel moved to the Temettü tab and renders open by default
  (`docs/adr/0009-dividends.md:111-113`,
  `frontend/src/pages/DividendsPage.tsx:227`,
  `frontend/src/components/ExpectedDividends.tsx:218`).

  > Superseded 2026-10-05 (GLG-939): it is the first card on the Temettü
  > tab, always open, and no longer a `<details>` element.

- **KPI block on phone.** It is a 2-column grid; desktop breakpoints are
  unchanged (Phase 20). The total-value card and the last
  card, realized P/L, span both columns, and the half cells use a 16 px
  figure so a signed seven-digit amount is not clipped
  (`frontend/src/pages/SummaryPage.tsx:122-133`,
  `frontend/src/pages/SummaryPage.tsx:1241`,
  `frontend/src/pages/SummaryPage.tsx:1293`). The roadmap names the
  total-value card only.

  > Superseded 2026-10-05 (GLG-934, UI v2 core pages): the KPI block is
  > one strip of five cells, five columns from `lg`. On a phone it is two
  > columns with only the total value across both; realized P/L is a half
  > cell like the other three. The half-cell figure sizes are unchanged.

- **Position row on phone.** The last price sits in the right column under
  the P/L, and the price editor takes the full row width only while
  editing (Phase 20,
  `frontend/src/pages/SummaryPage.tsx:199-203`).

  > Superseded 2026-10-05 (GLG-933, UI v2 core pages): the phone row has no
  > price line and no editor. Prices are entered from Fiyat gir in a desktop
  > row's menu, which opens the editor as a dialog anchored to the row
  > (`frontend/src/components/PriceDialog.tsx`). The same menu holds Al, Sat
  > and, for a manual price, Elle fiyatı kaldır: the Al and Sat buttons that
  > Phase 15 put in every row (GLG-558) and the remove control beside a
  > manual price are gone. The list gets bottom padding of the floating add
  > button's height.

- **Sticky tab row on phone.** Below `sm` the tab row sticks under the app
  header, so a long list can switch tabs without scrolling back up
  (`frontend/src/components/PortfolioHeader.tsx:275-282`). Phase 21 changed
  the row from three tabs to four: Özet, İşlemler, Temettü and İçe aktar,
  with Özet alone on the `all` scope
  (`frontend/src/components/PortfolioHeader.tsx:288-303`).

  > Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md)
  > (GLG-875): `PortfolioHeader.tsx` shrank to a page header without the
  > portfolio switcher, so the cited lines no longer match; the tab row is its
  > `portfolio.tabs` nav. Below `sm` it now sticks under the 48 px top bar
  > that `AppShell.tsx` shows below `lg`, as there is no app header any more.
  > The four tabs, and Özet alone on `all`, are unchanged.

- **Language switch.** It left the signed-in app header; the auth pages
  keep theirs, and Hesap → Dil is the place to change it once signed in
  (Phase 20, `frontend/src/components/AuthForm.tsx:125`,
  `frontend/src/pages/SetupPage.tsx:208`,
  `frontend/src/pages/AccountPage.tsx:164`). Reason not recorded.

  > Superseded 2026-10-06 (GLG-968, UI v2 secondary pages): Hesap's
  > one-field cards are gone. Dil is a row of the Tercihler card, beside
  > the reporting currency and Görünüm (`LanguageRow` in
  > `frontend/src/pages/AccountPage.tsx`); it is still the place to change
  > the language once signed in.

- **Empty portfolio card.** A card with no value, cash or P/L says "Henüz
  işlem yok" with an İşlem ekle link instead of three zeros
  (Phase 20, `frontend/src/pages/HomePage.tsx:211-221`).
  The list API has no transaction count, so all-zero figures with no
  missing rate stand in for "nothing recorded yet"
  (`frontend/src/pages/HomePage.tsx:120-127`).

  > Superseded 2026-10-05 (GLG-935): the home page and its cards are gone.
  > Tüm portföyler's Portföyler table lists every portfolio, an empty one
  > with its zero value, and the portfolio's own Özet offers the first
  > transaction.

- **Transaction row click.** On desktop a click on a row opens its edit
  dialog, as a tap does on phone; the hover icons stay for keyboard users
  (Phase 20,
  `frontend/src/pages/TransactionsPage.tsx:197-206`,
  `frontend/src/pages/TransactionsPage.tsx:236`).

  > Superseded 2026-10-05 (GLG-936): the edit, copy and delete icons are
  > always visible; they no longer wait for a hover. The row click stays.

- **Shortcut hint.** The keyboard shortcut hint in the transaction dialog is
  hidden on devices with no hover (Phase 20,
  `frontend/src/components/TransactionDialog.tsx:716-719`); reason not
  recorded.

  > Superseded 2026-10-05 (GLG-937): the ⌘↵ hint (Ctrl+↵ off a Mac) renders
  > only when `matchMedia('(pointer: fine)')` matches, in place of the
  > `hover:none` CSS rule, because a touch screen has no such key. It shows
  > in add mode only, at the start of the dialog's sticky footer, before the
  > Vazgeç, Ekle ve yeni and Ekle buttons
  > (`frontend/src/components/TransactionDialog.tsx`).

- **Admin page.** It has in-page section links under its heading; reason
  not recorded (Phase 20, `frontend/src/pages/AdminPage.tsx:29-39`).

  > Amended 2026-10-06 (GLG-969): the links are one row that scrolls
  > sideways, under `PageHeader`, in the portfolio pages' wrapper (see
  > [ADR 0023](0023-ui-design-system-v2.md)).

### Phase 21: stats, ranges, AI copy, clone, bar chart

**Stats.** The holdings engine keeps per-currency `Stats`: deposits,
withdrawals, fees, gross dividends, their stopaj and interest, each a
positive amount (`backend/internal/holdings/holdings.go:110-122`). The
summary API returns them as `stats` with `first_transaction_date`,
`by_currency`, and `reporting`, the sum in the reporting currency at
today's rate (`backend/internal/server/summary.go:136-159`). `reporting` is
null when a currency with a nonzero figure has no rate, rather than a
partial sum (`backend/internal/server/summary.go:175-206`). Today's rate is
the Phase 5 simplification, as for `realized_pl`
(`backend/internal/server/summary.go:172-174`, Phase 21).
The `all` scope sums the stats per currency
(`backend/internal/holdings/holdings.go:350-371`). The reason was that Özet
had no totals for what the user put in, earned and paid (commit d796906).
The İstatistikler card on Özet shows net contributions, dividends net of
stopaj, interest, fees and the first transaction with the time since it
(`frontend/src/pages/SummaryPage.tsx:965-1008`). Without reporting totals
the money cells show `-` (`frontend/src/pages/SummaryPage.tsx:966-968`). It
has five columns from `lg`, not `sm`, because at `sm` a cell leaves about
70 px for a 20 px figure (`frontend/src/pages/SummaryPage.tsx:1015-1016`).

> Superseded 2026-10-05 (GLG-934): from `lg` the card sits beside Dağılım in
> half the page, so it has three columns from `sm`, the fifth cell spanning
> two, for the same reason. The cells are unchanged.

**Range picker.** Özet and the position page share one `RangePicker`
(`frontend/src/pages/SummaryPage.tsx:1419-1422`,
`frontend/src/pages/PositionPage.tsx:203`). The choice is not persisted
(`frontend/src/components/RangePicker.tsx:9`); reason not recorded.

> Superseded 2026-10-05 (GLG-930): the range is kept in the query string by
> `frontend/src/lib/useRange.ts`, as `?range=<key>` or, for Özel,
> `?from=…&to=…`, so it survives a reload and the links between Özet, Tüm
> portföyler and the position page. Below `sm` the options are one `Select`
> instead of chips. The cited lines no longer match; on Özet the picker sits
> in `frontend/src/components/ReturnCard.tsx` (GLG-934).

- The chips are the presets 1M, 3M, 1Y, Tümü and the Phase 15 YTD, then up
  to three previous calendar years back to the first transaction, then
  Özel (`frontend/src/lib/ranges.ts:13-19`,
  `frontend/src/components/RangePicker.tsx:7-28`). Both pages start on 3M
  (`frontend/src/pages/SummaryPage.tsx:1038`,
  `frontend/src/pages/PositionPage.tsx:66`).
- `resolveRange` gives the API `from` and `to`: a year covers 1 January to
  31 December, YTD starts on 1 January of today's Istanbul year, and Tümü
  sends no `from` (`frontend/src/lib/ranges.ts:25-37`).
- Özel shows two native date inputs and starts from the range shown so
  far, so the chart does not jump; a date after today or a `from` after
  `to` is not applied (`frontend/src/components/RangePicker.tsx:34-47`,
  `frontend/src/components/RangePicker.tsx:73-93`).

**Copy for AI.** A button on the Özet positions card copies the open
positions as a markdown table, cash, totals and an analysis prompt to the
clipboard, in the UI language (`frontend/src/pages/SummaryPage.tsx:739-748`,
`frontend/src/pages/SummaryPage.tsx:803-807`,
`frontend/src/lib/aiPrompt.ts:9-69`).

> Superseded 2026-10-05 (GLG-933): the card and its button moved to
> `frontend/src/components/PositionsTable.tsx`; the table's Ağırlık column
> uses the same weight base as the prompt.

- The amounts are full, unmasked numbers even in privacy mode, since the
  clipboard is not the screen (`frontend/src/lib/aiPrompt.ts:10-11`,
  Phase 21).
- The weights use the allocation card's base, positive position values plus
  positive cash (`frontend/src/lib/aiPrompt.ts:15-22`, GLG-662).
- The prompt asks for a neutral review and no buy or sell recommendations
  (`frontend/src/i18n/strings.ts:513-514`, Phase 21). Reason
  not recorded.

**Transaction clone.** A clone button opens the add dialog prefilled from
the row and dated today, so a recurring buy is not retyped; saving POSTs a
new row (`frontend/src/pages/TransactionsPage.tsx:465-472`,
Phase 21).

**SVG bar chart.** The dividend bars are `BarChart`, an inline SVG with theme
colors and no chart library (Phase 21), because ADR 0005
rejects a charting library (`docs/adr/0005-ui-design-system.md:169`). It
takes decimal strings, shows two y ticks and a tooltip per bar on hover or
keyboard focus, and draws a zero bar as a 1 px hairline
(`frontend/src/components/BarChart.tsx:25-30`).

> Superseded 2026-10-01 (GLG-716): the ADR 0005 line cited above moved when
> GLG-716 and later tickets added dated notes to that ADR. The rejection is
> the "A charting library" row of the Chart decision in ADR 0005's
> "Alternatives considered" section.

> Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868): ADR 0023 allows a chart
> library, and the charts sub-project moves the charts, `BarChart` included,
> to Recharts. Until it ships, `BarChart` stays the inline SVG described
> above.
>
> Amended 2026-10-05 (GLG-913): it shipped. `BarChart` is
> `frontend/src/components/charts/BarChart.tsx`, drawn by Recharts; the
> decimal strings, the two y ticks, the tooltip per bar and the 1 px zero
> hairline stay. `frontend/src/components/BarChart.tsx` is deleted.

### i18n

- **Own module, no library.** All UI strings are one flat key-to-string map
  per locale in `frontend/src/i18n/strings.ts`, with no i18n library
  (`frontend/src/i18n/strings.ts:1-5`). Reason not recorded. Every UI
  string goes through a key, in `tr` and `en` (`CLAUDE.md:56`).
- **`tr` is the default and defines the key set.** `en` is typed against
  it, so `tsc -b`, the first step of `npm run build`, fails on a missing or
  extra key (`frontend/src/i18n/strings.ts:702-707`,
  `frontend/package.json:11`). A `<key>.one` entry is the English singular,
  picked when `count` is 1; Turkish needs none
  (`frontend/src/i18n/strings.ts:704-707`,
  `frontend/src/i18n/strings.ts:1420`).
- **The locale is stored in the browser only.** It lives in
  `localStorage['locale']`, not on the user row, because it affects
  rendering alone; the upgrade path is a `locale` column and
  `PUT /api/me/locale` (`frontend/src/i18n/strings.ts:1373-1384`). Disabled
  storage falls back to `tr`, and `<html lang>` follows the locale
  (`frontend/src/i18n/strings.ts:1386-1397`).
- **A switch re-renders the tree.** `t()` stays a plain function call, and
  the root component keys its subtree on `useLocale()`
  (`frontend/src/i18n/strings.ts:1411-1416`, `frontend/src/App.tsx:72-76`).
  Numbers and dates follow the locale through `Intl`
  (`frontend/src/lib/format.ts:15-16`, `frontend/src/lib/date.ts:16-23`).
- **The backend sees the locale once.** Registration sends it, and the new
  user's default portfolio is named "Portföy" or "Portfolio" from it
  (`frontend/src/api/auth.ts:17`, `backend/internal/server/server.go:47-52`,
  `backend/internal/server/server.go:93-99`, GLG-150).
