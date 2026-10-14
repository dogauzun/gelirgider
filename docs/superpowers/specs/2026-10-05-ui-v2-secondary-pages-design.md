# UI v2: secondary pages

Status: approved by the owner on 2026-10-05 (brainstorm of the same day).
Visual reference: [`2026-10-05-ui-v2-secondary-pages-mockup.html`](2026-10-05-ui-v2-secondary-pages-mockup.html).
Parent decisions: [ADR 0023](../../adr/0023-ui-design-system-v2.md). This is
sub-project 5, the last of the redesign: İçe aktar, Hesap, Yönetim, and the
signed-out pages. It builds on the core pages
([spec](2026-10-05-ui-v2-core-pages-design.md)) for the table classes and
for İşlemler's import filter.

## Why

Measured on `main` on 2026-10-05:

- **İçe aktar** opens with a 448 px form in a 1,000 px column and puts the
  list of past imports, the only undo, at the bottom. During review the
  first screen is two stacked warnings that repeat the same rows; error rows
  are moved to the end, so dates run out of order; the table is 1,017 px in
  a 1,000 px card when a status or the instrument picker shows; two buttons
  labelled "İşlem ekle" do different things. `ImportPage.tsx` is 2,054
  lines.
- **Hesap and Yönetim** sit in a centred 624 px column (x=444) while every
  portfolio page is left-aligned (x=256, 1,000 px). Their titles are 24 px
  against 20 px, their card titles 16 px against 14 px.
- **Yönetim** is 2,579 px tall, 1,351 px of it nine identical source cards;
  whether data is flowing is buried in them. At 375 px its gutter is 16 px
  against 24 px elsewhere and its four section links wrap, each 20 px tall.
- **Setup step 2** is 2,370 px tall with "Atla" and "Bitir" only at the
  bottom.
- The signed-out not-found page has no brand mark and no language switch.
- Checkboxes are the browser's own, on İçe aktar and Yönetim.

## Owner's decisions

| Question                         | Decision                                                                                               |
| -------------------------------- | ------------------------------------------------------------------------------------------------------ |
| İçe aktar                        | Clearer flow, easier review, and the import history in view.                                           |
| After a single import            | Land on İşlemler filtered to that import (the core pages build this). A batch ends on the import page. |
| Hesap, Yönetim, signed-out pages | A consistency pass: the same column, gutters, headings, cards and form layout. No new features.        |
| Yönetim's data sources           | One compact table instead of nine cards.                                                               |

## Scope

- İçe aktar: steps, a drop zone, the history beside the upload, a simpler
  review, a tidier batch and done state. The file is split into components
  first.
- Hesap, Yönetim, login, register, setup, not-found: the consistency pass.
- One checkbox primitive and one page header shared by these pages.

### Out of scope

- Any backend, API or schema change. In particular: an import's file name
  is not stored (ADR 0006 keeps only the period and the hash, for privacy),
  and a CSV import keeps an empty currency.
- New import features: no new broker, no change to parsing, matching,
  duplicate rules or what a commit writes.
- New settings on Hesap or Yönetim.
- Native date and time inputs stay; they are styled like the other fields.

## Shared pieces

- **`ui/Checkbox`**: one checkbox built on Base UI's checkbox, in the
  field style (accent when checked, the app's focus ring, a 44 px tap
  target with its label). Every native checkbox in the app is replaced, and
  `conventions.test.ts` bans `type="checkbox"` in components.
- **`PageHeader`**: the header of a page that is not a portfolio page: a
  20 px title, an optional line under it, optional actions on the right. It
  follows the rule of the core pages below `lg` (the top bar shows the
  title, so the page's own h1 is visually hidden).
- **Content column**: Hesap and Yönetim use the same wrapper as the
  portfolio pages (`max-w-6xl`, 24 px gutter at every width), left-aligned.
  Forms inside it keep a readable width; they are not stretched to
  1,000 px.
- **Card titles** are 14 px on every page.

## İçe aktar

### Steps

A row of three steps under the tabs: "Dosya seç", "İncele", "Bitti". The
current step is marked; a finished one shows a check. In a batch the second
step reads "İncele · Ekstre 2 / 3".

### Step 1: choose files

- Two cards side by side from `lg`: **Ekstre yükle** (about 5/9 of the
  width) and **Geçmiş içe aktarmalar**.
- **Ekstre yükle:** the broker select (Otomatik first) in the card's
  header; a drop zone that takes dropped files and opens the file dialog on
  click or Enter; one line naming the brokers that are recognised; the CSV
  column list behind a "CSV biçimi" disclosure. Choosing or dropping files
  starts the preview at once (today a second "Önizle" press is needed).
  Files of a type the chosen broker does not accept are refused with a
  message, as today.
- **Geçmiş içe aktarmalar:** newest first, each with a pill of broker and
  period, the row count, the currency when there is one, and the import's
  date in the user's time zone (today it is the UTC day). Two actions:
  "İşlemlerde gör", which opens İşlemler filtered to that import, and
  "Geri al" with today's confirmation. With no imports the card says so in
  one line.
- Parse errors (already imported, too large, not a statement) show in the
  upload card, next to the drop zone.
- Below `lg` the cards stack, upload first.

### Step 2: review

- **Header line:** the file name, then broker, period and currency.
- **One filter instead of two warnings:** a "Tümü N | Sorunlu N" switch.
  "Sorunlu" holds the rows that cannot be read and the rows that need a fix
  (an unknown instrument and the like), each counted once. It is selected
  on entry when there are any. Warnings about the whole file (same period,
  overlap) stay as a notice above the table.
- **Rows stay in date order.** An unreadable row sits where its date puts
  it; without a date it goes last. Its raw line opens under it on demand.
- **The table fits its card** with the compact table classes: checkbox,
  Tarih, Tür, Enstrüman, Adet × fiyat, Kesinti, Tutar, Durum, edit. The
  instrument picker of an unknown symbol is wide enough for its placeholder.
- **A bar fixed to the bottom of the screen:** how many rows will be
  imported and how many will not, "+ Satır ekle" (renamed from "İşlem
  ekle"), and the buttons Vazgeç, "Bu ekstreyi atla" in a batch, and İçe
  aktar. The table gets bottom padding so the bar never covers a row.
- Every rule of the review stays as it is: which rows can be selected,
  duplicates, the "Yine de içe aktar" confirmation, the holdings and
  reconciliation panel of a PDF, the opening rows.
- Below `md` the list of today stays, with the same filter and the same
  bar.

### Batch and step 3

- The queue card keeps its content and statuses and sits above the review,
  folded to one line by default ("Ekstre 2 / 3 · temmuz.pdf").
- A batch ends on the import page at "Bitti": one line per imported
  statement with its row count and "İşlemlerde gör", the skipped and failed
  files with their reason, and "Başka ekstre yükle", which returns to
  step 1.
- A single statement ends on İşlemler, filtered to it (core pages).

### Split first

Before any change of behaviour, `ImportPage.tsx` is split into files under
`components/import/` (queue, preview table and list rows, instrument
picker, holdings panel, file picker, past imports), with its tests moved
alongside. A pure move: no behaviour change, the same tests pass.

## Hesap

- `PageHeader` "Hesap" and two cards side by side from `lg`:
  **Tercihler** (reporting currency, language, appearance as three rows of
  one card) and **Şifre** (the form as today, in a readable width).
- Saving, errors and toasts are unchanged.

## Yönetim

- `PageHeader` "Yönetim" and the section links as one row that scrolls
  sideways when it does not fit, each link at least 44 px tall. The 24 px
  gutter at 375 px.
- **Kaynaklar** becomes one compact table: source, on or off, time
  (İstanbul), last success, and the actions "Şimdi çalıştır" and "Geçmişi
  doldur". The options above it (fill all, catch up on start, backfill on
  start, backfill start date) stay as a row of controls. A source that is
  on and has never succeeded, or last succeeded more than 48 hours ago,
  shows its "last success" as a warning pill, so a stopped source stands
  out. Below `md` each source is a two-line list item.
- **Veri sağlayıcılar**, **Enstrüman ekle**, **Kullanıcılar**: the same
  content in cards with 14 px titles; the users table uses the compact
  table classes.
- Every control does what it does today.

## Signed-out pages and setup

- Login, register, setup step 1 and the signed-out not-found page share one
  frame: the brand mark above, a card, the language switch below. Their
  titles are one size. The not-found page gains the brand and the switch.
- **Setup step 2** uses the same Kaynaklar table and provider cards as
  Yönetim, and keeps "Atla" and "Bitir" in a bar fixed to the bottom of the
  screen.

## Docs that must change

| Document                                    | Change                                                                                                       |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `docs/adr/0023-ui-design-system-v2.md`      | Dated notes: the checkbox primitive, `PageHeader`, the import flow's steps, the sources table.               |
| `docs/adr/0006-broker-statement-import.md`  | Dated note where it describes the import page's layout or the two-step upload as current.                    |
| `docs/adr/0021-product-and-ui-decisions.md` | Dated notes where it describes Hesap's or Yönetim's layout as current.                                       |
| `deploy/README.md`, `deploy/RUNBOOK.md`     | Check the first-run setup description against the new setup step 2; fix if it names moved controls.          |
| `docs/ROADMAP.md`, `docs/HISTORY.md`        | Phase 29 in progress with the first ticket; moved to HISTORY with the last. "Next" then holds no UI v2 item. |

## Delivery

Epic "Phase 29 - UI v2: secondary pages". One ticket, one PR, one agent.

| #   | Ticket                                             | After                              |
| --- | -------------------------------------------------- | ---------------------------------- |
| 1   | Docs to `main`; `ui/Checkbox` and `PageHeader`     |                                    |
| 2   | Split `ImportPage.tsx` into components             | 1                                  |
| 3   | İçe aktar step 1: steps, drop zone, history beside | 2, and İşlemler's filter (GLG-936) |
| 4   | İçe aktar step 2: review                           | 3, and the table classes (GLG-929) |
| 5   | İçe aktar batch and "Bitti"                        | 4                                  |
| 6   | Hesap                                              | 1                                  |
| 7   | Yönetim                                            | 1, and the table classes (GLG-929) |
| 8   | Signed-out pages and setup                         | 7                                  |
| 9   | Docs: move Phase 29 to HISTORY                     | all                                |

Tickets 2 to 5 share the import files and run one after the other. Tickets
6 and 7 can run beside them.

## Testing

- The split (ticket 2) changes no test body, only imports and file names.
- Each import ticket keeps the existing behaviour tests green and adds
  tests for what it changes: the steps' states; drop and click on the drop
  zone; the history's date, link and undo; the filter's counts and default;
  date order with an unreadable row; the fixed bar; the batch's end state.
- Yönetim: the stale-source rule at exactly 48 hours, never succeeded, and
  switched off; every source action still calls its endpoint.
- Browser checks in headless Chrome over the DevTools protocol at 1280 px
  and 375 px, light and dark, with synthetic CSV files and, for setup, an
  empty database: no table wider than its card, the measured page heights
  before and after, the left offset of Hesap and Yönetim equal to a
  portfolio page's.
- `make lint`, `make test`, `make build-frontend` on every ticket.

## Risks

- **The import page is the most stateful page of the app** (2,054 lines,
  2,724 lines of tests). The split comes first, each later ticket changes
  one step, and no parsing or commit rule is touched.
- **Starting the preview on file choice** removes a confirmation step. A
  wrong file costs one "Vazgeç"; nothing is written before "İçe aktar".
- **A PDF statement cannot be produced synthetically for browser checks**
  (the repo has no fixture). The PDF-only parts (holdings panel, opening
  rows) are covered by component tests; the tickets say so rather than
  claim a browser check they did not do.

## Done when

- İçe aktar shows its three steps, takes dropped files, shows the history
  beside the upload with a link into İşlemler, and reviews with one
  "Sorunlu" filter in date order; no table is wider than its card.
- Hesap and Yönetim start at the same left offset and use the same title
  and card sizes as the portfolio pages; Yönetim's sources are one table
  and a stopped source stands out.
- Setup step 2 keeps its buttons in view; the signed-out not-found page has
  the brand and the language switch.
- No native checkbox remains in a component.
- No document describes the old layouts as current, and the roadmap holds
  no UI v2 item.
- `make lint`, `make test`, `make build-frontend` pass on `main`.
