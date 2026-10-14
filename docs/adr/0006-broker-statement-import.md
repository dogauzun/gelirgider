# ADR 0006: Broker statement import — PDF text extraction, partial parsers, two-step import

Status: accepted (GLG-199, 2026-09-21); amended 2026-09-25 (GLG-358,
GLG-359, GLG-360, GLG-361, GLG-363, GLG-367, Midas FX legs, Diğer Gelir
rows, empty and repeated statements, overlap per currency); amended
2026-09-27 (GLG-415, repeated hand-entered dividends flagged); amended
2026-09-28 (GLG-419, cash reconciliation; GLG-577, several statements at
once; GLG-579, decoded-size bound; GLG-529, GLG-533, Phase 15 interest as
income); amended 2026-09-28 (GLG-581, transaction note format); amended
2026-09-29 (GLG-594, GLG-597, GLG-598, Phase 21 CSV import;
GLG-611, GLG-618, GLG-622, GLG-624, GLG-626, GLG-627, CSV rules; GLG-559,
GLG-640, repeated rows flagged); amended 2026-10-01 (GLG-694, Garanti BBVA
and Garanti BBVA Yatırım statements are covered by
[ADR 0018](0018-garanti-statements.md); the import may create a TEFAS fund
that TEFAS confirms; a statement may carry a market for its reconciliation);
amended 2026-10-01 (GLG-733, TEFAS lookup of a typed fund code); amended
2026-10-01 (GLG-737, opening positions proposed in the preview); amended
2026-10-02 (GLG-816, Midas period-end price for an opening position); amended
2026-10-03 (GLG-819, unknown rows that net to zero do not block openings);
amended 2026-10-04 (GLG-850, fund lookups share a per-IP TEFAS budget);
amended 2026-10-04 (GLG-852, a statement price in another currency does not
price an opening); amended 2026-10-04 (GLG-866, parse writes no user data but
may add shared market data); superseded in part by
[ADR 0023](0023-ui-design-system-v2.md) (GLG-868, GLG-869, GLG-870, 2026-10-04):
the token rule it cites lives in ADR 0023; amended 2026-10-05 (GLG-931,
transactions filtered by import); amended 2026-10-05 (GLG-989, a CSV is capped
at 5000 rows); amended 2026-10-07 (GLG-1113, test fixtures carry invented
figures; GLG-1115, personal disclosure removed); amended 2026-10-08
(GLG-1180, the commit accepts a row dated the
day after the period; GLG-1181, parse takes the batch's fund names; GLG-1174,
`.S1` stripped from a Midas symbol; GLG-1176, and from a CSV symbol; GLG-1177,
Midas `Diğer Gider` is a fee); amended
2026-10-09 (GLG-1182, a file is refused in every portfolio of the user;
GLG-1187, `Ücretsiz İşlem` is a deposit; GLG-1185, an opening buy is priced
at the statement's average cost first; GLG-1189, a BIST opening is priced
after a bulletin sync; GLG-1191, the reconciliation follows the preview's
edits; GLG-1190, closing rows for positions the statement omits; GLG-1196,
IBANs and account numbers masked in the preview); amended
2026-10-10 (GLG-1261, a CSV that is not UTF-8 is read as Windows-1254;
GLG-1269, every statement is capped at 5000 rows)

## Context

Every transaction in gelirgider is typed by hand today. Turkish brokers email
a monthly PDF account statement that already contains every trade, dividend
and cash movement of the period. Importing that file is the single biggest
reduction in manual work available, so Phase 6's placeholder bullet "CSV
import of broker statements" becomes this epic (GLG-198).

Midas is the first supported broker: one Turkish broker, one document type
(`HESAP EKSTRESİ`), two account currencies (USD and TRY) that print slightly
different tables. Two real statements (kept outside the repository) were
used to verify the decisions below (see **PII**).

## Decision

### PDF text extraction: `github.com/ledongthuc/pdf`

Text is extracted with the pure-Go library `github.com/ledongthuc/pdf`, via
`Page.GetTextByRow()`. Verified 2026-09-21 on both sample statements: every
table row comes back as one row whose `Text` runs are exactly the table's
cells, so a row maps to a `[]string` with no coordinate math, no column
inference and no heuristics about whitespace.

Pure Go means no cgo, no `pdftotext`, and nothing new in the backend Docker
image — the same constraint that already picked `modernc.org/sqlite`.

Scanned or image-only PDFs are out of scope. No OCR. A statement with no text
layer parses as "not a Midas statement" and the user keeps entering rows by
hand.

### Two layers: one extractor, one parser package per broker

- `backend/internal/statement/pdftext` — a thin wrapper: PDF bytes in,
  `[][]string` rows out, page after page, empty cells dropped. It knows
  nothing about brokers (GLG-200).
- `backend/internal/statement` — the broker-neutral `Statement` result type
  (trades, cash movements, dividends, period-end holdings, skipped rows,
  parse errors) plus a `Parsers` map from broker key to parse function
  (GLG-200).
- `backend/internal/statement/midas` — rows in, `Statement` out. Pure: no
  database, no HTTP, no user, no clock (GLG-202).

A second broker is one new package and one line in the `Parsers` map. That is
the whole extension point — no provider interface, no registry framework,
the same shape ADR 0003 chose for market data packages and
`docs/PRINCIPLES.md` #9 requires.

The CSV import (Phase 21, GLG-597, GLG-598) is such a parser package too:
`backend/internal/statement/csv`, broker key `csv`. The same `parse` handler
feeds it `encoding/csv` records (BOM stripped, `;` or `,` separator sniffed)
instead of pdftext rows. The first record is the header, its cells matched
against these aliases; only date and type are required:

| Column   | Header aliases                     |
| -------- | ---------------------------------- |
| date     | `date`, `tarih`                    |
| type     | `type`, `tür`, `işlem`, `islem`    |
| symbol   | `symbol`, `sembol`, `kod`          |
| quantity | `quantity`, `adet`, `miktar`       |
| price    | `price`, `fiyat`                   |
| amount   | `amount`, `tutar`                  |
| fee      | `fee`, `komisyon`                  |
| currency | `currency`, `para birimi`, `döviz` |
| note     | `note`, `not`, `açıklama`          |
| market   | `market`, `piyasa`, `borsa`        |

| Type       | Type aliases                                    |
| ---------- | ----------------------------------------------- |
| buy        | `buy`, `alış`, `alis`, `al`                     |
| sell       | `sell`, `satış`, `satis`, `sat`                 |
| dividend   | `dividend`, `temettü`, `temettu`                |
| interest   | `interest`, `faiz`, `nema`                      |
| deposit    | `deposit`, `yatırma`, `para yatırma`, `yatirma` |
| withdrawal | `withdrawal`, `çekme`, `para çekme`, `cekme`    |
| fee        | `fee`, `ücret`, `masraf`                        |

> Amended 2026-10-10 (GLG-1261): Excel in a Turkish locale saves plain "CSV"
> as Windows-1254, whose `İşlem` header and `Yatırma` type no alias matched.
> A file that is not valid UTF-8 after the BOM strip is decoded as
> Windows-1254 by a 32-entry table in the `parse` handler (no new dependency);
> UTF-8 files, with or without a BOM, read as before.

> Amended 2026-10-14 (GLG-1370): a `market` cell is read through aliases too,
> the codes the export writes and the market names the app shows. Any other
> non-empty value is an error row with code `unknown_market` naming the cell,
> instead of a hint that matches nothing. An empty cell is no hint, as before.

| Market | Market aliases           |
| ------ | ------------------------ |
| BIST   | `bist`, `borsa istanbul` |
| US     | `us`, `abd`              |
| TEFAS  | `tefas`                  |
| CRYPTO | `crypto`, `kripto`       |
| GOLD   | `gold`, `altın`, `altin` |

Dates are `2006-01-02`, `02.01.2006` or `02/01/2006`, and numbers such as
`1.234,56` are accepted. A value with one kind of separator in thousands
shape, such as `1.000`, `1,000` or `1.000.000`, is an `ambiguous_number`
error row rather than a guess, the same rule as the transaction form
(GLG-626). The note column is recognised but not imported, since statement
rows carry no note. Upgrade path: a `Note` field on the
statement rows, carried through the preview and the commit. A CSV has no
holdings section, so there is no reconciliation for it. A statement period
must be shorter than a year, since a broker statement covers a month or a
quarter; a CSV has no such cap, because it spans whatever history it holds,
such as the app's own export (GLG-611). A CSV prints no period, so the parse
handler derives it from the rows' dates, error rows included, leaving out a
date the commit refuses (before 1990 or in the future): one mistyped year
must not block the rest of the file (GLG-618).

Phase 22 (GLG-693) adds two PDF parser packages next to `midas` and `csv`:
`backend/internal/statement/garanti`, broker key `garanti`, for the Garanti
BBVA fund statement, and `backend/internal/statement/garantiyatirim`, broker
key `garanti_yatirim`, for the Garanti BBVA Yatırım brokerage statement.
Their layouts and mapping rules are in
[ADR 0018](0018-garanti-statements.md).

`backend/cmd/statement_dump` prints the extracted rows for a PDF. It is how a
developer inspects a new broker's layout, and the only way to run the code
against a real statement, since real statements never enter the repository.

### Parsers are partial

A row the parser cannot read becomes a `ParseError` carrying the section, the
raw cells, a message, and the date when one could be read. Parsing continues
with the next row. A Go `error` is returned only when the file is not a
statement of this broker at all (for Midas: no period row found) or, for a
CSV, when no row follows the header, since such a file has no period to record
(GLG-624). A CSV whose rows are all error rows still previews, without a
warning about the period none of them could give it.

This is the decision the rest of the epic hangs on. A statement is a
best-effort document from a third party: an unseen transaction type, a
renamed column or a new section must not cost the user the other forty rows
that parsed fine. The preview shows error rows, unknown symbols and
deliberately skipped rows, and the user fixes them by hand before committing
(GLG-210). The parser is allowed to be incomplete because the human is in the
loop by construction.

Rejected: all-or-nothing parsing, which turns every broker layout change into
"import is broken" instead of "three rows need attention".

### Two-step flow: parse, review, commit

1. `POST /api/imports/parse` takes the PDF as `multipart/form-data` and
   returns a preview — proposed transactions with a per-row status
   (`ok`, `unknown_instrument`, `skipped`, `error`, `unsupported_currency`),
   the period-end holdings, and warnings. **It writes nothing** (GLG-203).
2. The UI shows the preview, the user unchecks, edits and completes rows
   (GLG-205, GLG-210).
3. `POST /api/imports` commits the reviewed rows in one database transaction
   (GLG-204).

> Amended 2026-10-04 (GLG-866): parse writes no user data, but since GLG-699,
> GLG-737 and GLG-782 it may add shared market data. A TEFAS fund code that
> TEFAS confirms is added as an instrument, a BIST bulletin synced for an
> unknown symbol adds instruments and prices, and pricing an opening buy may
> fetch and store prices and FX rates.

The commit endpoint receives transaction-shaped rows, not a PDF, so it is
broker-agnostic: a bulk transaction create with an import record around it.
It reuses the same validation the single-transaction endpoint runs; a single
invalid row rolls the whole import back.

> Amended 2026-10-01 (GLG-735): the commit may also carry optional `aliases`,
> the instruments the user picked for symbols the preview did not know. They
> are its one broker-aware part: each is stored in the same database
> transaction in `import_aliases`, per (user, broker, symbol), replacing an
> earlier pick, and a later preview of that broker resolves a symbol that
> matches no instrument with it and marks the row `resolved_by: "alias"`. A
> real match always wins over a stored pick. See
> [ADR 0018](0018-garanti-statements.md).
>
> Amended 2026-10-02 (GLG-751): a period-end holding resolves the same way,
> so its reconciliation entry and its opening position follow the stored
> pick.
>
> Amended 2026-10-02 (GLG-766): a holding may carry the name the statement
> printed next to its code. The preview returns each such name once as
> `names`, with the instrument of its code, or 0 when the statement prints
> it for two codes or its code resolves to no instrument. The commit takes
> them back as optional `names`, validated like `aliases` and stored in
> `import_aliases` with `learned = 1`: a learned name replaces a learned
> name, never a pick, and a pick replaces it; a name of instrument 0, or one
> sent with two instruments, removes a learned row and stores nothing. A
> later preview of that broker resolves a symbol no instrument and no pick
> resolves with the one instrument of the learned names that equal it, start
> with it or that it starts with, and marks the row `resolved_by: "name"`;
> names of two instruments leave it unknown, and so does a symbol that is
> one of the statement's holding codes or that matches one of its own
> balance names, which the parser leaves unmapped only when it names two
> funds. The holdings, and so `names`, resolve after the rows, so a fund the
> preview adds from TEFAS (GLG-699) is learned too. The parser stays pure,
> and the commit still never sees the file.
>
> Amended 2026-10-03 (GLG-821): a learned name replaces or removes a learned
> row only for a statement whose period ends on the same day or later
> (GLG-773). A removed name is recorded with that period end in
> `import_forgotten_names`, per (user, broker, symbol), so a statement of an
> older period committed afterwards does not learn it again; a statement of
> the same or a later period removes the record and learns the name.

Rejected: a one-shot "upload and it's in" endpoint. Money rows the user never
saw before they landed in their portfolio is the wrong default for a tool
whose entire output is a P/L number.

The global 64 KB body limit is lifted for the parse route only, replaced by a
5 MB `http.MaxBytesReader` inside the handler (a one-page Midas statement is
21–24 KB; a busy month runs to several pages). The cap bounds the file, not
what its compressed streams decode to: `pdftext` rejects a file whose streams
decode past 8 MB before the library inflates them, so a deflate bomb under
5 MB is a 400, not an out-of-memory crash (GLG-579). Allocations the library
sizes from a plain number in the file are not bounded yet (GLG-583).

> Amended 2026-09-21 (GLG-216): the 64 KB limit is lifted for the commit
> route as well, because a commit carries every reviewed row of a statement,
> and `POST /api/imports` caps its body at 2 MB inside the handler instead.

> Resolved 2026-09-28 (GLG-583, GLG-584): extraction runs in a child process
> with a 256 MB heap limit and a 10 s timeout, and at most two such workers
> run at once, so an allocation the library sizes from the file can no longer
> take the server down.

> Amended 2026-10-05 (GLG-989): a CSV runs in the request, not in a worker,
> and every record becomes a preview row, so a 5 MB file of short lines cost
> gigabytes. The parse reads a CSV record by record and refuses one with more
> than 5000 records under its header as a 400 `file: more than 5000 rows`.
> 5000 reviewed rows still fit in the commit's 2 MB body.

> Amended 2026-10-10 (GLG-1269): the cap covers every statement, not only a
> CSV. After the parser returns, PDF or CSV, the parse handler counts the
> statement's trades, dividends, cash movements, holdings, skipped rows and
> error rows and refuses more than 5000 with the same 400
> `file: more than 5000 rows` (`maxStatementRows` in
> `backend/internal/statement/handlers.go`). The CSV reader keeps its early
> cap on records.

Several statements picked at once (GLG-577) are handled per file on the
client; there is no batch endpoint. The page parses every file once up front
to learn its period and currency and to name the files it cannot import, then
walks them in period order, then currency, so earlier months land first and
the holdings reconciliation of a later month is not flagged for months still
waiting in the queue. Each file is parsed again when its turn comes, because
its `same_period` and `overlap` warnings and its reconciliation depend on
what is already committed, including earlier files of the same batch. Every
money row is still reviewed before its commit; the batch only saves picking
the files one at a time.

> Amended 2026-10-08 (GLG-1181): the batch also shares its fund names. The
> page keeps each preview's `names` with its file, and every parse of a file
> at its turn, including the re-parses after a pick or an undo, sends the
> names of every statement of the same broker in the batch as the optional
> `names` form field, in the shape and limits of `picks`; a name two
> statements give to two instruments is dropped on the client. For that
> request only, parse takes them as learned names (see the GLG-766 note
> above), each winning over a stored one of the same name; an entry of
> instrument 0 is ignored and nothing is stored. So a fund TEFAS has renamed,
> which an earlier statement prints only by its old name, resolves through a
> later statement of the batch before that one is committed.

### Idempotency and undo: `statement_imports` + `transactions.import_id`

Each commit writes a `statement_imports` row (user_id, portfolio_id, broker,
statement currency, period start/end, file sha256, created_at) with `UNIQUE
(user_id, portfolio_id, file_sha256)`, and every transaction it creates carries
`import_id` (GLG-201). An empty statement (every section `Kayıt
bulunmamaktadır.`) is recorded too, as an import with no transactions, so the
month shows in the history and a re-upload is caught (GLG-360).

> Amended 2026-10-05 (GLG-931): `import_id` also filters a portfolio's
> transaction list and its CSV export. `?import_id=<id>` keeps one import's
> rows, `?import_id=none` the rows entered by hand; another user's import id
> matches no row, so it reads as an empty list, not as an error.

- Re-uploading the same file returns 409 `already_imported` instead of
  duplicating a month of trades.

  > Amended 2026-10-09 (GLG-1182): the same file is refused in every
  > portfolio of the user, not only in this one. A statement file is one
  > broker account, so importing it into a second portfolio counted its
  > holdings twice. Parse and commit look its sha256 up across the user's
  > portfolios, still filtered by `user_id`; the unique constraint above
  > stays per portfolio. The 409 carries `import_id`, `portfolio_id` and
  > `portfolio_name`, the earlier import and the portfolio it is in, and the
  > import page names that portfolio and links to it; in a batch the file is
  > marked failed with that message.

- The same statement from another file — same broker, portfolio, statement
  currency and exact period — is **refused** with 409 `already_imported`,
  naming the earlier import, while that import exists (GLG-361). Midas
  regenerates the PDF on every download, so a re-downloaded month has a new
  hash and the constraint above never sees it. The preview flags it with the
  warning code `same_period` and the import page disables the commit,
  pointing at the undo of the earlier import. TRY and USD statements of the
  same month are two accounts, so a different currency is not the same
  statement; an import recorded before the currency was stored (NULL)
  matches any currency. A `csv` import is one file, not an account, so it is
  recorded with no currency (NULL) and a second CSV over the same dates is
  the same period whatever its rows' majority currency (GLG-627). It is a
  check in the commit, not a unique constraint, because of that NULL rule
  and because existing databases may already hold such a pair.
- A partial overlap for the same broker and portfolio (a quarterly statement
  over an imported month) is a **warning** (`overlap`), not an error. The
  other currency's statement of an imported period is the other account and
  gets no warning at all; an unknown currency on either side still warns
  (GLG-367), so a CSV over part of an earlier CSV's dates always does. A
  corrected statement for the same month is a legitimate re-import once the
  first one is undone, which is exactly when the refusal above lets it
  through.
- Row by row, the preview flags a row that repeats an earlier transaction of
  the portfolio with `duplicate_of`, that transaction's date, and the import
  page starts it unchecked (GLG-415, GLG-559). The earlier one may be
  hand-entered or imported, from any broker: a CSV that repeats rows first
  imported from a Midas statement, or from another CSV, gets no warning
  above, only these flags (GLG-627). A trade pairs with one of the same
  instrument, side, date and quantity, a cash row with one of the same type,
  date, currency and amount, and a dividend of an instrument with a
  hand-entered one inside the expected dividends matching window (ADR 0009),
  since that one may be dated on the ex-date; each earlier transaction
  accounts for at most one row. An imported dividend is dated on the payment
  date like the row, so it pairs only on the same date, currency and amount:
  the window would pair a monthly payer's dividend with the previous
  statement's and start every month's row unchecked (GLG-640).
- `DELETE /api/imports/{id}` deletes the import's transactions and the import
  row, which is the undo button (GLG-206). This is why the import is
  recorded at all: a hash column alone would give idempotency, but not a way
  back out.

Imported transactions stay ordinary transactions — editable and deletable one
by one, marked with an "imported" pill in the list.

### Midas row-to-transaction mapping

| Statement section    | Row                                                       | Becomes                                                                                                                                                                                                                                                                     |
| -------------------- | --------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `YATIRIM İŞLEMLERİ`  | `Alış` / `Satış`, status `Gerçekleşti`                    | buy / sell — quantity = Gerçekleşen Adet, price = Ortalama İşlem Fiyatı, amount = İşlem Tutarı, fee = İşlem Ücreti, currency = Para Birimi                                                                                                                                  |
| `YATIRIM İŞLEMLERİ`  | any other status                                          | skipped, reason `not executed`                                                                                                                                                                                                                                              |
| `TEMETTÜ İŞLEMLERİ`  | every row                                                 | dividend — amount = brüt, fee = stopaj                                                                                                                                                                                                                                      |
| `HESAP İŞLEMLERİ`    | `Para Yatırma` / `Para Çekme`, status `Gerçekleşti`       | deposit / withdrawal in the statement currency                                                                                                                                                                                                                              |
| `HESAP İŞLEMLERİ`    | `Döviz Alış` / `Döviz Satış`, status `Gerçekleşti`        | one movement in the amount cell's currency, on the execution date, description kept: `Döviz Satış` is a withdrawal on a USD cell and a deposit on a TRY cell, `Döviz Alış` the reverse (GLG-363); skipped, reason `fx conversion`, when the cell currency is not USD or TRY |
| `HESAP İŞLEMLERİ`    | `Diğer Gelir` (`Aylık Nema Geliri`), status `Gerçekleşti` | dividend with no instrument in the statement currency, description kept (GLG-359, GLG-533); interest income, not a flow, so the returns count it (ADR 0011)                                                                                                                 |
| `HESAP İŞLEMLERİ`    | `Ücretsiz İşlem`, status `Gerçekleşti`                    | deposit in the statement currency, description kept (`Sıfır Komisyonlu İşlem`, GLG-1187)                                                                                                                                                                                    |
| `HESAP İŞLEMLERİ`    | anything else                                             | parse error `unknown cash type <type>`                                                                                                                                                                                                                                      |
| `HİSSE TRANSFERLERİ` | any row                                                   | skipped, reason `stock transfer`                                                                                                                                                                                                                                            |
| `PORTFÖY ÖZETİ`      | every row                                                 | holdings snapshot, reconciliation only, never imported; Toplam Değeri / Adet (6 places) is the period-end price an opening position falls back on (GLG-737, GLG-816), zero when the value is unreadable                                                                     |

> Amended 2026-10-08 (GLG-1177): `HESAP İŞLEMLERİ` `Diğer Gider` (so far only
> `Stopaj`, the withholding on the monthly interest) is a fee for the
> absolute amount, description kept. Any other unlisted type is still the
> parse error `unknown cash type <type>`.

Dividends map gross to the amount and stopaj to the fee because the holdings
engine already nets the fee out of a dividend — no new transaction type and
no second field for withholding tax.

Deliberate simplifications, each with its ceiling:

- **A `Döviz Alış` / `Döviz Satış` row books only its own account's leg**
  (GLG-363; skipped until GLG-358, then booked as both legs). gelirgider
  keeps cash per currency with no implicit conversion (ADR 0003), and real
  accounts are funded through a conversion (USD deposited, sold for TRY, TRY
  withdrawn), so it cannot be skipped. Midas prints each conversion on both
  the TRY and the USD statement, same date and rate, each with only its own
  account's amount, and that amount is exact (it matches the bank transfer
  to the kuruş). Booking both legs from one statement counted every
  conversion twice once both accounts were imported, and the leg derived
  from the 2-decimal rate drifted by up to ~1 USD or ~42 TRY. So each
  statement books its side like a transfer between two of the user's own
  accounts; importing one account leaves the other currency untouched.
- **`Ücretsiz İşlem` (Midas's zero-commission credit) is a deposit**
  (GLG-1187; skipped until then, its cash effect unclear from one sample).
  Statements with many trades settle it: the printed `Nakit Bakiye` is above
  the sum of the other rows by the credit, so Midas pays it into the cash
  account, and skipping it left every month with trades reconciling short.
  It is booked as a flow, not as income, like a deposit. A smaller drift
  remains that the statement does not itemize: on a month of buys with fee
  `0,00` the computed cash ends about 0.13% of the buy total above
  `Nakit Bakiye`, with no row to parse for it.
- **`PORTFÖY ÖZETİ` is never imported.** The period-end holdings are the
  broker's view, not a set of operations; importing them would invent
  transactions with no date or price. They are used to reconcile against the
  computed position and warn about missing earlier months (GLG-207).
  Quantity is compared exactly; average cost is informational only, because
  Midas rounds it and its cost-basis rules are not necessarily ours.
- **Non-executed orders are skipped**, not errors. A cancelled or partially
  filled order is a normal statement row, not a parse failure.
- **Nema imported before GLG-533 stays a deposit.** `Diğer Gelir` rows
  already imported as deposits are left alone: the way to reclassify them is
  to undo that import on the import page and import the statement again.
  There is no data migration, because import rows keep only the
  `Midas <period>` note, not the statement description, so nothing on a row
  tells a nema deposit from a real one.

Layout facts the parser must survive, both observed in the samples:

- **Column header rows are optional per section.** The USD sample prints a
  header row for every section; the TRY sample's `HESAP İŞLEMLERİ` starts
  with data. Sections are therefore located by their title row and a header
  row is detected by its first cell, never assumed by position.
- **Number formats are mixed inside one document.** The header block uses
  `88702.860000` while the tables use Turkish `8.237,46`. One parser
  accepts both: drop `.` when a `,` is present, then swap `,` for `.`. A
  header amount (`Nakit Bakiye`) without a comma is always a plain decimal,
  so `1.500` there is one and a half (GLG-419).
  Quantities carry up to 9 decimals (`0,006218342`), which is exactly why
  money and quantities are `shopspring/decimal` everywhere
  (`docs/PRINCIPLES.md` #3).
- **The company header repeats on every page**, so the section state machine
  treats those rows as noise wherever they appear.
- Dates are `DD/MM/YY` and become `YYYY-MM-DD` in `Europe/Istanbul`, like
  every other market date in the app.

### Instrument matching

Trades and dividends are matched to an existing instrument by **symbol within
a market derived from the row currency**: `USD` → `US`, `TRY` → `BIST`,
anything else → `unsupported_currency`. Midas prints BIST symbols with the
bulletin suffix (`OPT25.F`, `THYAO.E`) while `instruments.symbol` stores the
bare symbol, so the parser strips a trailing `.E` / `.F`.

> Amended 2026-10-08 (GLG-1174, GLG-1176): the Midas and CSV parsers also
> strip `.S1`, so the gold certificate `ALTIN.S1` resolves to BIST `ALTIN`.

The import creates no instrument on its own word. An unmatched symbol
makes the row uncommittable until the user picks an instrument in the
preview or unchecks the row. A statement is not an authority on what an
instrument is, and a typo-created ghost instrument would quietly break
pricing and P/L forever.

The one exception is a TEFAS fund that TEFAS itself confirms (Phase 22,
GLG-699). When a row's market hint is `TEFAS` and its unknown symbol matches
`^[A-Z0-9]{3}$`, the import asks TEFAS's `fonFiyatBilgiGetir`
(ADR 0004) for the code. A code that comes back with NAVs is inserted as a
`TEFAS` instrument in `TRY`, named by `fonUnvan`, and the row matches it; a
code TEFAS does not know stays `unknown_instrument`. At most 10 codes are
looked up per request, and only while the `tefas` source is on, since the
fetcher is what prices the new fund afterwards. TEFAS is the authority on
what a TEFAS fund is, so the ghost-instrument risk above does not apply.
A row that names its fund by name carries no code to ask about, so the
preview picker of a `TEFAS` row offers a TEFAS lookup for a typed code the
search has no TEFAS match for: `POST /api/imports/funds` runs the same
lookup for that one code, under the same rules, and the user picks the
fund it returns (GLG-733). The plain search `GET` never creates one.
Both routes' lookups count against one budget of 20 TEFAS lookups a minute
per client IP (GLG-850), so repeated previews cannot crawl TEFAS and get the
server's IP blocked; a code over the budget stays `unknown_instrument` in
the preview, and the fund route answers 429.

Deriving the market from the currency is enough while the supported markets
are BIST (TRY) and US (USD) — it is not a general rule. Since Phase 21
(GLG-597) `Trade` and `Dividend` carry an optional `Market` hint, which the
CSV `market` column fills. The hint wins when set. Without it the currency
rule above applies first; when that finds nothing, a unique match among the
other markets of that currency (`TRY`: `TEFAS`, `CRYPTO`, `GOLD`; `USD`:
`CRYPTO`) is taken, else the row is `unknown_instrument`. In the same
change `CashMovement` gains a `Fee` field and the kind `fee`, a standalone
charge committed as a `fee` transaction. A CSV row without a currency cell
takes the currency of the instrument it resolves to, `TRY` when none, and
without a hint it is looked up as a `TRY` row; a currency cell that
contradicts the instrument is an error row with the commit's message
(GLG-622).

The holdings reconciliation (GLG-207) keys the statement's holdings by the
market of the statement currency and compares only the positions of that
market. Since Phase 22 (GLG-695) `Statement` carries an optional `Market`.
When set, it is the market of the holdings and of the positions the
statement is silent about, so the Garanti BBVA fund statement
(`Statement.Market = "TEFAS"`, ADR 0018) is reconciled against the
portfolio's TEFAS funds, not its BIST shares. Empty keeps the currency rule.

> Amended 2026-10-09 (GLG-1191): the reconciliation the page shows follows
> the review. Each entry carries the `instrument_id` of its holding (none
> when the holding resolves to no instrument), and the page adds to the
> server's portfolio side the difference between the rows it would commit
> and the rows the preview started with checked, per instrument and for cash
> in the statement currency, by the holdings engine's rules. A line no edit
> moves keeps the server's verdict. Only lines the server sent can move: a
> row added for an instrument it does not list adds no line, since only the
> server knows that position.

The first statement of an account lists the month's movements and the
month-end balances, but not what was held at its start, so an empty
portfolio mismatches and goes negative from the first month. Since Phase 22
(GLG-737) the preview proposes **opening rows**, marked `opening`, for an
instrument the statement holds more of at period end (0 when it does not
list it) than the portfolio would with the preview's `ok` rows, when the
portfolio has no transaction of it before `period_start`; one with earlier
transactions is an ordinary mismatch. Each is a `buy` of the difference,
dated the day before `period_start`, priced with the average cost the
statement prints for the holding (`Holding.AvgCost`, flagged `cost_price`;
since GLG-1185, when it is in the instrument's currency and the preview has
no buy of the instrument, as the period-end average includes those), else
with the stored price on or before that date (fetched or manual), else
after an on-demand fetch (at most 20 instruments per request), else with
the statement's own period-end price (`Holding.Price`, flagged
`approximate_price`; since GLG-852 only when the holding's currency is the
instrument's, so a Garanti foreign-currency fund's USD unit price never
prices a TRY fund), else left as an `error` row with code `opening_price`
for the user to price. One `deposit` per currency on the same day pays
for the priced buys, so cash does not go negative, and the reconciliation
is computed with them. They are ordinary rows from there on: the commit
accepts that one day before the period, stores them with the
import, and the undo removes them. A symbol that resolves to no instrument
gets none, and a preview that still has a buy or sell of an unknown
instrument gets none at all: that row may belong to a holding that resolves,
so the user resolves it and uploads the file again. Since GLG-819 the rows of
an unknown symbol whose buy and sell units net to zero over the preview do
not block: they cannot change a period-end holding. Garanti's money-market
sweep fund, printed by name only and bought and sold within the month, is the
common case.

> Superseded 2026-10-02 (GLG-752): the user no longer uploads the file again.
> The parse endpoint takes optional `picks`, in the shape and limits of the
> commit's `aliases`, and resolves each pick's symbol for that request like a
> stored pick, winning over one. After each inline pick the page re-requests
> the preview with every pick so far, so the opening rows and the
> reconciliation follow the picks; it keeps check marks, picks and dialog
> edits on the other rows, matched by row key, and takes the opening rows from
> the new response. Parse still writes nothing.
>
> Amended 2026-10-04 (GLG-866): parse writes no user data; it may add shared
> market data, see the note under the two-step flow.
>
> Amended 2026-10-08 (GLG-1180): the commit also accepts a row dated the day
> after `period_end`. Midas books a month's interest (`Aylık Nema Geliri`)
> on the first day of the next month and prints it in the statement of the
> month it accrued, so the commit refused that statement's own row. Two days
> after is still refused.
>
> Amended 2026-10-09 (GLG-1189): a BIST opening buy with no stored price on
> or before the day before `period_start` is priced once the BIST bulletin
> of the last weekday on or before that day is synced, before the on-demand
> fetch: one sync a preview, within the client IP's 10 bulletin fetches a
> minute and the opening fetches' deadline. The on-demand fetch syncs only
> the latest bulletin and queues a backfill that lands after the commit, so
> such a buy used to be an `opening_price` error row.
>
> Amended 2026-10-09 (GLG-1190): the preview also proposes **closing rows**,
> the mirror of the openings, marked `closing`. When the statement lists
> holdings and the portfolio would hold more of an instrument at period end
> than the statement, having held it before `period_start`, a `sell` of the
> difference, at most what it held the day before `period_start`, is
> proposed on that day. It is priced like an opening buy but never with the
> average cost, else left as an `error` row with code `closing_price`. One
> `withdrawal` per currency on the same day takes the priced sells' proceeds
> out again, rounded down to the cent. The page starts the sells unchecked
> and keeps the withdrawal in step with the checked ones, so the
> reconciliation leaves closing rows out. Like the openings, none is
> proposed while an unknown instrument's buy or sell blocks them.

**BIST ETFs are a prerequisite** (GLG-208). The symbol seed and the daily
price fetcher keep only the bulletin's `EQT` group today, so every BIST ETF —
including the one traded in the TRY sample — would import as
`unknown_instrument` and never be priced. The bulletin's `ETF` group (34 rows
on 2026-09-18, all with the `.F` suffix) joins the seed and the fetcher.

> Resolved 2026-09-21 (GLG-208): the symbol seed and the daily price fetcher
> keep both the `EQT` and the `ETF` groups of the bulletin, stripping `.E`
> and `.F` respectively (`backend/internal/bist/bist.go`).

> Amended 2026-10-08 (GLG-1174): a third group, `EMS`, is kept with the
> suffix `.S1`; its only instrument is the gold certificate `ALTIN.S1`. The
> Midas parser strips `.S1` too, and the CSV parser since GLG-1176.

### PII

A real statement carries the customer's name, TCKN, address and IBAN. None of
it is ever stored or logged:

- The database keeps the broker, the statement currency, the period and the
  file sha256. Nothing else from the document is persisted, and the file
  itself is not kept.
- The transaction note is the broker name plus the period,
  `Midas <start> – <end>` (for example `Midas 2025-12-01 – 2025-12-31`),
  never the statement's description column (which contains the bank name and
  IBAN on deposit rows).
- Logs record the broker, the portfolio id, row counts per status and the
  error kind. Never the file name, the rows, or any cell. Raw cells of an
  error row go to the client — it is the user's own data and they need to see
  it to fix the row — but never to a log line.

  > Amended 2026-10-09 (GLG-1196): the preview masks IBANs and account
  > numbers (`statement.MaskIdentifiers`, called in
  > `backend/internal/statement/handlers.go`): in a cash row's description,
  > an unknown row's value and the raw cells of a skipped or error row. An
  > IBAN becomes `TR** **** 1234`, an account number `***6-100`; dates are
  > never masked. The parsers still read the raw text.

- Parse errors must not quote customer fields in their message.
- **No real statement enters the repository.** Parser tests use synthetic
  row fixtures as Go literals: the layout of a real statement, with
  placeholders for name, TCKN, address and IBAN and every amount, quantity,
  price, rate and date invented, so no real holding, deposit, dividend or
  account opening date is published (GLG-1113). The sample PDFs stay
  outside the repo and are reachable only through
  `backend/cmd/statement_dump`.

## Alternatives considered

| Decision        | Option                                                  | Verdict    | Why                                                                               |
| --------------- | ------------------------------------------------------- | ---------- | --------------------------------------------------------------------------------- |
| Source format   | Broker PDF statement                                    | **chosen** | It is what the broker actually emails every month; Midas offers no CSV or API.    |
| Source format   | CSV export (the old Phase 6 bullet)                     | rejected   | Assumes an export the broker does not provide (see note below).                   |
| PDF text        | `github.com/ledongthuc/pdf`                             | **chosen** | Pure Go, no cgo, and `GetTextByRow` returns table cells directly on both samples. |
| PDF text        | `pdftotext` (poppler) via exec                          | rejected   | A system binary in the backend image for one feature, plus layout flags to tune.  |
| PDF text        | `unidoc/unipdf`                                         | rejected   | Commercial license for a job a permissive library does.                           |
| PDF text        | OCR                                                     | rejected   | Not needed: these statements have a text layer.                                   |
| Parser layout   | One package per broker behind a `Parsers` map           | **chosen** | A second broker is one package and one line; no framework.                        |
| Parser layout   | A generic table-mapping config per broker               | rejected   | Speculative abstraction for broker number one (`PRINCIPLES.md` #1).               |
| Failure mode    | Partial parse, row-level errors                         | **chosen** | The user finishes the last rows by hand instead of losing the import.             |
| Failure mode    | All-or-nothing parse                                    | rejected   | Any layout change becomes a total outage of the feature.                          |
| Flow            | Parse → review → commit                                 | **chosen** | The user sees every money row before it lands in their portfolio.                 |
| Flow            | Upload commits directly                                 | rejected   | No way to catch a mismapped row before it distorts P/L.                           |
| Idempotency     | `statement_imports` row with file sha256                | **chosen** | Blocks double imports and gives a single-call undo.                               |
| Idempotency     | Match on (date, symbol, amount) per transaction         | rejected   | Would reject legitimate identical trades on the same day.                         |
| Unknown symbols | Block the row until the user picks an instrument        | **chosen** | Keeps `instruments` authoritative; the fix is one click in the preview.           |
| Unknown symbols | Auto-create the instrument from the statement           | rejected   | A ghost instrument never gets a price and silently breaks P/L (see note below).   |
| Holdings table  | Reconciliation only                                     | **chosen** | It is a snapshot, not a list of operations.                                       |
| Holdings table  | Import as opening positions                             | rejected   | Would invent transactions with no date, price or cost basis.                      |
| Sample PDFs     | Kept outside the repo, anonymized row fixtures in tests | **chosen** | The documents contain name, TCKN, address and IBAN.                               |
| Sample PDFs     | Committed as test data                                  | rejected   | Permanently publishes customer PII into git history.                              |

> Amended 2026-09-29 (GLG-597, GLG-598): the rejected CSV option was a
> broker-specific CSV export, which Midas does not offer. The generic CSV
> import of Phase 21, in gelirgider's own column layout, is a separate
> decision, described under "Two layers" above.

> Amended 2026-10-01 (GLG-699): auto-creating an instrument from the
> statement stays rejected, with one exception: a TEFAS fund code that TEFAS
> itself confirms is created, as described under "Instrument matching"
> above.

## Known risks

- Midas can change its statement layout at any time; it is a document, not a
  documented API — the same risk ADR 0001 accepted for the BIST bulletin.
  Mitigation: sections are found by title and columns by header text, not by
  position, and partial parsing degrades to "some rows need attention"
  instead of a hard failure.
- Both samples are single-month, low-volume statements. Multi-page sections,
  partial fills, corporate actions and short positions are unseen. They will
  surface as `ParseError` rows in the preview, which is the designed
  behaviour, and each one becomes a small follow-up ticket.
- The unitemized drift on months with trades (see `Ücretsiz İşlem` above)
  leaves the portfolio's cash slightly above `Nakit Bakiye`. It is visible in
  the reconciliation's cash line, which compares the parsed `Nakit Bakiye`
  with the portfolio's cash in the statement currency (GLG-207, GLG-419),
  not silent.
- Deriving the market from the row currency breaks the moment a second
  TRY-quoted market shows up in a statement (TEFAS). The Phase 21 market
  hint and fallback above cover the CSV import; the Midas parser sets no
  hint yet.
- The 5 MB parse limit is a guess, not a measurement, and the parse endpoint
  does real work on an uploaded file. It is authenticated and per-user, so
  the exposure is bounded; a rate limit is the upgrade path if it is ever
  abused.

  > Amended 2026-09-28 (GLG-584): at most two PDF extractions run at once
  > across all users, each with a 256 MB heap limit and a 10 s timeout, so a
  > burst of uploads cannot starve the host. A per-user rate limit is still
  > the upgrade path.

- The PII rule is a convention enforced in review, like the token rule in
  ADR 0005. The mechanical part of it is enforced: the repository contains no
  PDF, and the schema has no column that could hold a customer field.

  > Superseded 2026-10-04 by [ADR 0023](0023-ui-design-system-v2.md) (GLG-868,
  > GLG-869, GLG-870): the token rule now lives in ADR 0023, and part of it is
  > no longer left to review. `frontend/src/conventions.test.ts` fails the
  > frontend tests when a component uses `font-mono` (GLG-869), the inline card
  > string `rounded-card border border-line bg-surface` or a raw
  > `shadow-sm|md|lg|xl|2xl` class (GLG-870). Raw palette classes and `dark:`
  > variants are still caught in review only. The PII rule is unchanged.

## Consequences

- GLG-200 adds the `Statement` types, the `pdftext` extractor and the dump
  tool. Every other backend ticket builds on it, and it brings the epic's
  only new dependency.
- GLG-201 adds the `statement_imports` table, `transactions.import_id` and
  the import repository.
- GLG-202 implements the Midas parser and the mapping table above.
- GLG-203 and GLG-204 are the parse and commit endpoints; GLG-204 also adds
  the list and delete routes that make undo possible.
- GLG-205 is the import page, GLG-206 the past-imports list with undo and the
  imported pill, GLG-210 the editable preview that makes partial parsing
  useful.
- GLG-208 adds BIST ETFs to the symbol seed and the daily fetcher; without it
  TRY statements import ETFs as unknown instruments.
- GLG-209 extracts the transaction dialog and instrument search from
  `TransactionsPage.tsx` so the preview can reuse them instead of growing a
  second copy.
- GLG-207 reconciles the statement's period-end holdings against the
  portfolio and warns on a mismatch.
- The Phase 6 roadmap bullet "CSV import of broker statements" is replaced by
  Phase 8.
