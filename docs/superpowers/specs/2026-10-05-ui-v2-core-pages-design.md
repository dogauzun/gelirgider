# UI v2: core pages

Status: approved by the owner on 2026-10-05 (brainstorm of the same day).
Visual reference: [`2026-10-05-ui-v2-core-pages-mockup.html`](2026-10-05-ui-v2-core-pages-mockup.html).
Parent decisions: [ADR 0023](../../adr/0023-ui-design-system-v2.md). This is
sub-project 4 of that ADR. Phases 24 and 25 changed the skin, the shell and
the charts and left every page layout as it was; this spec changes the
layouts.

## Why

Measured on `main` on 2026-10-05, at 1280 px with a 15-position portfolio:

- Özet is 3,097 px tall (3.4 screens). It leads with lifetime totals. The
  first position row starts at y=1015, below the first screen.
- A position row is 109 to 133 px tall, mostly because of the Al and Sat
  buttons and the manual-price pill. Fifteen rows take 1,827 px.
- Unrealized P/L is shown three times on Özet. The portfolio total is shown
  on the home page, on "Tüm portföyler" and in the sidebar.
- The İşlemler table is 1,121 px wide in a 1,000 px card, so the edit, copy
  and delete icons are cut off. Its toolbar wraps to two rows.
- The position page spends about 200 px on a back link and two headings
  before its first number, and has no last price outside the chart.
- Expected dividends, the only thing on Temettü that asks for an action, sit
  collapsed at the bottom.

## Owner's decisions

| Question                | Decision                                                                                                            |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------- |
| What Özet answers first | "How am I doing?" (return against the yardstick) and "What needs my attention?".                                    |
| Attention items         | Data gaps, dividends to confirm, today's movers, concentration.                                                     |
| Özet layout             | Layout A: return card with the chart and an attention card side by side, one KPI strip, compact positions table.    |
| Tüm portföyler          | The combined Özet on top and one row per portfolio below. It replaces the home page.                                |
| İşlemler                | Built for two jobs: checking what was imported, and adding by hand.                                                 |
| Position page           | Leads with the owner's result on that instrument.                                                                   |
| Temettü                 | Leads with income over time; dividends to confirm move to the top.                                                  |
| Devices                 | Desktop first. The phone layout must be complete and usable, but gets no layouts of its own.                        |
| Backend                 | One addition is allowed: filter transactions by import. Listing imports already exists.                             |
| Attention thresholds    | Manual price older than 7 days; one position above 25%; cash above 40%; one foreign currency above 50%; two movers. |

## Scope

- New layouts for Özet, Tüm portföyler, İşlemler, the position page and
  Temettü, as in the mockup.
- The home page (`/`) is removed.
- One backend addition: a filter on the transactions list (below).
- The layout debts recorded on GLG-867 and GLG-910 that belong to these
  pages.

### Out of scope

- New charts, drag to zoom, more date labels on the x axis (sub-project 3b).
- Percent mode, periodic returns, drawdown, and the flat value line when the
  cost line pulls the y axis down (sub-project 3c).
- İçe aktar, Hesap, Yönetim, the auth and setup pages, and their layout
  debts (sub-project 5).
- Phone-specific layouts beyond stacking.

## Shared pieces

- **KPI strip.** `FitFigure`, the KPI card and its label and figure classes
  move out of `pages/SummaryPage.tsx` into `components/ui/Kpi.tsx`. Every
  page uses them; the position page drops its own KPI classes.
- **Compact table.** One set of table classes in `components/ui/table.ts`:
  40 px rows on Özet, 44 px on İşlemler, header row, group row, numeric
  cells right-aligned. A page's table never scrolls sideways inside its card
  at 1280 px.
- **Range in the URL.** The chosen range lives in the query string
  (`?range=3A`, or `?from=…&to=…` for a custom range), read through one
  hook. Özet, Tüm portföyler and the position page share it, so the range
  survives a visit to a position page and a reload. The default stays 3A.
- **Range picker on a phone.** Below `sm` the range chips become a `Select`,
  so nothing is cut off.
- **Headings.** The page title is 20 px everywhere. Below `lg` the top bar
  already shows the page title, so the page's own h1 is visually hidden
  there (it stays for screen readers).
- **Empty states** are cards with a line of text and one action, as on Özet
  today; İşlemler and Temettü get one.

## Özet (layout A)

Top to bottom at 1280 px:

1. **Return card and attention card**, side by side (about 2:1).
2. **KPI strip**, one row: Toplam değer, Günlük, Nakit, Gerçekleşmemiş K/Z,
   Gerçekleşmiş K/Z.
3. **Positions** table.
4. **Dağılım** and **İstatistikler**, side by side.

The first two position rows are on the first screen at 1280×900.

### Return card

- Headline: the return of the chosen range by the chosen method, as a
  percentage, with the amount (`rangeDelta`) next to it and the range in
  words.
- A pill with the benchmark's return and the difference, coloured by the
  sign of the difference. No pill without a benchmark.
- Controls on the right: benchmark select, method select. The selects size
  to their content, so no label is cut in Turkish or English. The range
  picker sits under the headline.
- The line chart, about 220 px tall. Its legend, tooltip and toggles are
  unchanged.
- The notices about missing rates and prices for the range stay under the
  chart.
- When a benchmark has no values in the range, the card says so in place of
  the pill (the existing `chart.benchmark.noData` text), so the select never
  names a benchmark that silently draws nothing.

### Attention card ("Dikkat")

A list in four groups. A group with nothing to show is hidden. With nothing
at all the card shows one line, "Her şey yolunda", and stays small.

| Group         | Item                                                                                           | Action                             |
| ------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------- |
| Veri          | A currency without an FX rate, with how many positions are left out of the total               | none                               |
| Veri          | A position without a price                                                                     | "Fiyat gir" opens the price editor |
| Veri          | Manual prices older than 7 days, as one line with the count                                    | "Güncelle" jumps to the first      |
| Veri          | The benchmark's values are estimates from a date on                                            | none                               |
| Temettü onayı | Each expected dividend not yet recorded or dismissed: symbol, net amount, date                 | "Ekle", "Yoksay"                   |
| Bugün         | The two positions with the largest absolute day change in the reporting currency, amount and % | the symbol links to the position   |
| Yoğunlaşma    | A position above 25% of the total; cash above 40%; assets in one foreign currency above 50%    | none                               |

- The rules are one pure function, `attentionItems`, with its thresholds in
  one exported constant. The card only renders what the function returns.
- Shares are of the total value including cash, in the reporting currency.
- "Bugün" is hidden when no position has a day change.
- The card's header shows the count of Veri and Temettü items (the ones that
  ask for something), not of all items.
- Concentration is stated as a fact. The app gives no advice.

### KPI strip

One row of five cells. The sub-lines of today's hero (unrealized against
cost, FX gain, cost) move to tooltips on the cells they explain, so each
number is on the page once.

### Positions table

- Columns: Enstrüman (symbol, then the name and a currency tag on the same
  line), Ağırlık (share of the total with a small bar), Adet, Ort. maliyet,
  Fiyat, Günlük, Değer, K/Z (amount and percent), and a row menu.
- Rows are 40 px. The full name shows as far as the column allows, with the
  rest on hover.
- The row menu (`ui/Menu`) holds Al, Sat, Fiyat gir and, for a manual
  price, Elle fiyatı kaldır. The Al and Sat buttons leave the rows.
- A manual or stale price is a small pill after the price, with today's
  explanation in its tooltip.
- The header cells sort the table (Değer descending by default); the sort is
  component state.
- An "Açık | Kapalı" switch replaces the "n kapalı pozisyon" toggle.
- Realized P/L and FX gain per position leave the table; they are on the
  position page.
- The footer row keeps the totals of Değer and K/Z.
- Below `md` the list of today stays, without the manual-price line; the
  floating add button must not cover a row's numbers (the list gets bottom
  padding of the button's height).

### Phone

The cards stack in this order: return, attention, KPI strip (two columns),
positions, allocation, statistics.

## Tüm portföyler

- `/portfolios/all` shows the same return card, attention card and KPI strip
  as Özet, summed over all portfolios. Attention items name their portfolio.
  Expected dividends are fetched per portfolio.
- Below: a **Portföyler** table, one row per portfolio: colour dot and name,
  Değer, Pay (share with a bar), Getiri for the chosen range and method with
  a sparkline of the value, Günlük. A row opens that portfolio's Özet. The
  return and the sparkline come from one history request per portfolio,
  made after the page's own data has loaded.
- A "Yeni portföy" button in the page header.
- Then the merged positions table (read-only, as today), Dağılım and
  İstatistikler.
- `/` redirects to `/portfolios/all`, and the brand link goes there.
  `HomePage.tsx` is deleted. With no portfolio at all, the page shows the
  empty state that the home page shows today.
- Rename and delete stay in each portfolio's own header menu.

## İşlemler

- **Toolbar in one row:** instrument search, type switch, a "Tarih" popover
  holding the two date inputs, a "Kaynak" select; on the right the count
  and a CSV button.
- **Table that fits its card:** Tarih, Tür, Enstrüman (symbol, name, and a
  note as grey text with the full text on hover), Adet × fiyat in one cell,
  Kesinti (commission and withholding together), Tutar, Kaynak, and three
  always-visible action icons. Rows are 44 px. Month group rows stay.
- **Kaynak** is "Elle" or the import the row came from, shown as a pill with
  the broker and the period ("Midas · Haz 2026"). The select filters by one
  import or by hand-entered rows. The CSV download carries the filter.
- **After an import** the page opens with a banner naming the import and its
  row count, with "Sadece bunları göster".
- **Add dialog:** an "Ekle ve yeni" button beside "Ekle". It saves, keeps
  the type and the date, clears the rest and focuses the first field. The
  ⌘↵ hint stays on desktop and is hidden on touch screens.
- **Phone:** the filters sit behind one "Filtrele" button with a count of
  active filters; in a list row the amount stays right-aligned on the first
  line.

### Backend addition

- `GET /api/portfolios/{id}/transactions` and `.csv` accept `import_id`: a
  number for one import, `none` for rows entered by hand.
- The imports themselves are already listed by `GET /api/imports` (`id`,
  `broker`, `portfolio_id`, `period_start`, `period_end`, `currency`,
  `transaction_count`, `created_at`); the page filters that list by
  portfolio, as the import page does today. No new endpoint.
- No schema change. Swagger annotations are updated.

## Position page

- **Title row:** "Özet ›" link, the symbol, the name and market, the last
  price with today's change as a pill; on the right Al, Sat and Fiyat gir
  buttons. It replaces the back link and the two stacked headings.
- **KPI strip:** Toplam sonuç (unrealized + realized + dividends received,
  all time, in the reporting currency), Piyasa değeri (with the quantity and
  the weight in the portfolio), Gerçekleşmemiş (with percent and FX part),
  Gerçekleşmiş, Temettü (with the number of payments), Ort. maliyet.
- **One chart card** with a "Fiyat | Değer ve maliyet" switch and the range
  picker, in place of two cards.
- **Transactions** of this instrument in the compact table.
- The header's "İşlem ekle" opens prefilled with this instrument.
- A closed position shows Toplam sonuç, Gerçekleşmiş and Temettü only.

## Temettü

- **Dividends to confirm** as the first card, only when there are some, with
  Ekle and Yoksay per row and "Tümünü ekle".
- **KPI strip:** Son 12 ay net, Maliyete göre verim (last 12 months net over
  the cost of the open positions, labelled as such), Bu yıl net, Toplam net,
  Stopaj and Faiz.
- **Bar chart:** a non-zero month is at least 3 px tall; a zero month stays
  a 1 px line.
- **Table:** Enstrüman, Ödeme sayısı (was "Adet"), Pay, Net (with the
  foreign-currency amount for a foreign dividend), Son ödeme with its year.
  It fits at 375 px.

## Layout debts

| Debt                                                            | Where it is settled         |
| --------------------------------------------------------------- | --------------------------- |
| Tall position rows, truncated names, price cell crowding        | Özet positions table        |
| Chart header wraps; benchmark and method labels truncate        | Return card                 |
| Range chips overflow at 375 px; Özet range resets               | Shared pieces               |
| Name shown twice below `lg`; heading sizes differ               | Shared pieces               |
| İşlemler toolbar wraps, table wider than card, uneven rows      | İşlemler                    |
| Tüm portföyler has one stretched tab at 375 px                  | Tüm portföyler (no tab row) |
| Empty Temettü and İşlemler are a bare line                      | Shared pieces               |
| Toasts sit over the table and can touch the floating add button | Shell polish ticket         |
| Inline rename input is 208 px wide                              | Shell polish ticket         |
| Benchmark with no values draws nothing without a word           | Return card                 |
| Small dividend months look like zero months                     | Temettü                     |
| Three date labels; legend placeholder height; flat value line   | 3b and 3c                   |
| Hesap, Yönetim, İçe aktar preview                               | Sub-project 5               |

## Docs that must change

| Document                                    | Change                                                                                                |
| ------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `docs/adr/0023-ui-design-system-v2.md`      | Dated notes: the core-page decisions, the attention rules, the home page removed, range in the URL.   |
| `docs/adr/0014-all-portfolios-view.md`      | Dated note: the view gains the Portföyler table and replaces the home page.                           |
| `docs/adr/0021-product-and-ui-decisions.md` | Dated notes where it describes the positions table, the İşlemler toolbar or the home page as current. |
| `docs/adr/0006-broker-statement-import.md`  | Dated note: transactions can be filtered by import.                                                   |
| `docs/ROADMAP.md`, `docs/HISTORY.md`        | Phase 26 in progress with the first ticket; moved to HISTORY with the last.                           |
| The 2026-10-04 spec                         | Its "Deliberate shortcut: `HomePage` stays" section gets a dated note when the home page goes.        |

## Delivery

Epic "Phase 26 - UI v2: core pages". One ticket, one PR, one agent. `main`
works after every ticket.

| #   | Ticket                                                        | After   |
| --- | ------------------------------------------------------------- | ------- |
| 1   | Docs to `main`; KPI and table primitives out of `SummaryPage` |         |
| 2   | Range in the URL; range picker as a select on a phone         | 1       |
| 3   | Backend: filter transactions by import                        |         |
| 4   | Attention rules and card                                      | 1       |
| 5   | Compact positions table                                       | 1       |
| 6   | Özet layout A                                                 | 2, 4, 5 |
| 7   | Tüm portföyler; home page removed                             | 6       |
| 8   | İşlemler page                                                 | 1, 3    |
| 9   | "Ekle ve yeni" in the add dialog                              |         |
| 10  | Position page                                                 | 2, 5    |
| 11  | Temettü page                                                  | 1       |
| 12  | Shell polish: toasts, rename input, hidden duplicate title    | 6       |
| 13  | Docs: move Phase 26 to HISTORY                                | all     |

Tickets 3 and 9 can start at once. Tickets 4 and 5 run in parallel after 1.
Tickets 8, 10 and 11 touch different pages and run in parallel.

## Testing

- `attentionItems` gets a unit test per rule, including each threshold's
  edge (exactly 25% is not above 25%) and the empty result.
- The range hook: default, a preset, a custom range, an invalid value falls
  back to the default.
- Each page keeps its existing tests; a test that asserted the old layout is
  rewritten for the new one, never deleted without a replacement.
- The backend addition ships with a test for the `user_id` filter (another
  user's import id returns nothing) and for `import_id=none`.
- Every ticket that changes a page checks it in a browser at 1280 px and
  375 px, light and dark, with synthetic data, in headless Chrome over the
  DevTools protocol (the recipe is in the ticket): no sideways scroll, the
  measured row heights, and for Özet the y position of the first position
  row.
- `make lint`, `make test`, `make build-frontend` on every ticket;
  `make swagger` on the backend ticket.

## Risks

- **`SummaryPage.tsx` is 1,694 lines** and four tickets touch it. Ticket 1
  and ticket 5 move code out first, and the order above keeps one ticket in
  the file at a time.
- **One history request per portfolio** on Tüm portföyler. Fine for a
  handful of portfolios; the requests start after the page's own data.
- **"Maliyete göre verim" and "Toplam sonuç" are derived in the browser.**
  The tickets check that realized P/L does not already contain dividends,
  and label the yield with its definition.
- **Attention noise.** With manual prices everywhere the card could be all
  warnings; stale manual prices are one line with a count for that reason.

## Done when

- At 1280×900 Özet shows the period return against the yardstick, the
  attention card and the first two position rows without scrolling.
- No core page scrolls sideways at 1280 px or 375 px, and no toolbar wraps
  at 1280 px.
- Position rows are 40 px and İşlemler rows 44 px at 1280 px.
- `/` opens Tüm portföyler; `HomePage.tsx` is gone; each total is on a page
  once.
- İşlemler can show only one import's rows or only hand-entered rows.
- Every debt in the table above marked for this epic is settled.
- No document contradicts the new pages.
- `make lint`, `make test`, `make build-frontend` pass on `main`.
