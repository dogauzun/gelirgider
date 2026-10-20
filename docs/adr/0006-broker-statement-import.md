# ADR 0006: Broker statement import — PDF text extraction, partial parsers, two-step import

Status: accepted (2026-09-21).

## Context

Every transaction in gelirgider was once typed by hand. Turkish brokers send a
monthly PDF account statement that already holds every trade, dividend and
cash movement of the period, and importing it is the biggest cut in manual
work available. Midas was the first broker, with one document type and an
account per currency; the statements of Garanti BBVA and a generic CSV
followed.

## Decision

### Brokers and what each parser reads

One parser package per source under `backend/internal/statement`, each
registered under a broker key in a `Parsers` map. A new broker is one package
and one line; there is no provider interface and no per-broker mapping config
(`docs/PRINCIPLES.md` #1, #9).

- `midas`: the Midas `HESAP EKSTRESİ`, one statement per account currency
  (USD or TRY). A US statement is matched to US instruments and a TRY
  statement to BIST ones.
- `garanti`: the Garanti BBVA fund statement, and `garanti_yatirim`: the
  Garanti BBVA Yatırım brokerage statement. Both are in
  [ADR 0018](0018-garanti-statements.md).
- `csv`: gelirgider's own column layout, the file the app exports and any
  hand-made file in that shape. It has no holdings section, so there is no
  reconciliation for it. A CSV is never refused for its period, which it does
  not print; the file hash and the per-row duplicate flags keep it from
  booking a row twice.

Parsers are pure: rows in, a broker-neutral `Statement` out, with no database,
no HTTP, no user and no clock. `backend/cmd/statement_dump` prints a PDF's
extracted rows, since real statements never enter the repository.

### PDF text extraction

Text comes from the pure-Go library `github.com/ledongthuc/pdf`, one row of
cells per table row, in a thin `pdftext` wrapper that knows nothing about
brokers. Pure Go keeps cgo, `pdftotext` and any system binary out of the
backend image, the same constraint that picked `modernc.org/sqlite`. There is
no OCR: a scanned PDF with no text layer is "not a statement of this broker"
and the user enters rows by hand. Rejected: `pdftotext` through exec (a system
binary for one feature) and `unidoc/unipdf` (a commercial license).

Extraction runs in a child process with a heap limit and a timeout, and at
most two run at once across all users, so one hostile or broken file cannot
take the server down. The upload is capped at 5 MB, and a file whose
compressed streams decode past 8 MB is refused before the library inflates
them. Every statement, PDF or CSV, is capped at 5000 rows, since each becomes
a preview row and 5000 reviewed rows still fit the commit's 2 MB body.

### Parsers are partial

A row the parser cannot read becomes a `ParseError` with the section, the raw
cells, a message and the date when one could be read, and parsing continues.
A Go error is returned only when the file is not a statement of that broker
at all, or when a CSV has no row after its header. A statement is a
best-effort document from a third party: a new transaction type or a renamed
column must not cost the user the forty rows that parsed fine. The preview
shows error rows, unknown symbols and deliberately skipped rows, and the user
fixes them before committing. Rejected: all-or-nothing parsing, which turns
every layout change into "import is broken".

An unknown row type is an error row, never a guess. In a CSV, a number with
one kind of separator in thousands shape (`1.000`, `1,000`) is an
`ambiguous_number` error row, the same rule as the transaction form; the PDF
parsers read their broker's fixed number format. A CSV that is not valid UTF-8
is read as Windows-1254, which Excel in a Turkish locale writes.

### Two steps: parse, review, commit

1. `POST /api/imports/parse` takes the file and returns a preview: proposed
   rows with a status (`ok`, `unknown_instrument`, `skipped`, `error`,
   `unsupported_currency`), the period-end holdings and warnings. It writes
   no user data. It may add shared market data: a TEFAS fund TEFAS confirms,
   BIST instruments from a bulletin, and prices or FX rates fetched to price
   an opening buy.
2. The user reviews the preview, unchecking, editing and completing rows.
3. `POST /api/imports` commits the reviewed, transaction-shaped rows in one
   database transaction. The commit never sees the file, so it is
   broker-agnostic, and it runs the single-transaction validation: one
   invalid row rolls the whole import back. After it, prices for the
   committed instruments are fetched on demand from the earliest committed
   date, so the new positions show a P/L without waiting for the daily job.

Rejected: a one-shot upload that commits directly. Money rows the user never
saw before they reach the portfolio are the wrong default for a tool whose
output is a P/L number.

The preview carries row flags the page acts on: `duplicate_of` for a row that
repeats an earlier transaction of the portfolio, hand-entered or imported
from any broker, which the page starts unchecked; `opening` and `closing` for
rows the preview proposes (below); and `resolved_by` for an instrument found
through a stored pick or a learned name instead of the symbol.

Several files picked at once are handled one at a time on the client, there
is no batch endpoint. The page learns every file's period and currency first
and then walks them by period, then currency, so earlier months land before
the holdings reconciliation of a later one. Each file is parsed again at its
turn, because its warnings and reconciliation depend on what is already
committed. Every money row is still reviewed.

### Instruments and picks

Rows are matched to an existing instrument by symbol, in the market the
statement's currency implies (USD to US, TRY to BIST) unless the row or the
statement carries a market hint. The import never creates an instrument on a
statement's word; a ghost instrument would quietly break pricing and P/L. An
unmatched symbol makes the row uncommittable until the user picks an
instrument or unchecks it. The one exception is a TEFAS fund code that TEFAS
itself confirms, which is created, within a per-IP lookup budget so previews
cannot crawl TEFAS.

The user's pick for a printed symbol or fund name is stored per user and
broker and resolves later previews; a real match always wins over a pick.
Names the user's committed statements printed for a fund are learned too,
so a renamed fund still resolves, a pick wins over a learned name and a name
that matches two funds stays unknown. A user keeps at most 10,000 learned
names per broker: a commit that goes over evicts the oldest by creation time
in its own transaction, never fails because of it, and never touches a pick.

### Idempotency and undo

Each commit records a `statement_imports` row (user, portfolio, broker,
currency, period, file sha256) and stamps its transactions with `import_id`,
so `DELETE /api/imports/{id}` undoes the whole import. An empty statement is
recorded too. Imported transactions stay ordinary and editable.

- The same file is refused (409 `already_imported`) in every portfolio of the
  user, since one statement file is one broker account and a second portfolio
  would count its holdings twice.
- The same broker, portfolio, currency and exact period from another file is
  refused too, since Midas regenerates the PDF on every download; the user
  undoes the earlier import first. A different currency is another account;
  a CSV has no currency and matches any.
- A partial overlap with an earlier import is a warning, not an error.

### Reconciliation and opening rows

The period-end holdings are the broker's view, not a list of operations, so
they are never imported. They reconcile against the computed positions of the
statement's market, and cash against the printed balance, with quantity
compared exactly and average cost informational only. The page recomputes the
reconciliation as the user edits rows.

The first statement of an account lists the month's movements but not what
was held before it, so the preview proposes opening rows: a buy dated the day
before the period, plus one deposit per currency to pay for them, for what
the statement holds beyond the portfolio. The price is the statement's
average cost, else a stored or fetched price, else the statement's own
period-end price, else the user prices the row. Closing rows are the mirror,
for a portfolio that holds more than the statement. Both are ordinary rows
once proposed, and none is proposed while an unknown instrument's buy or sell
could belong to a holding.

### Midas mapping

Executed buys and sells map to buys and sells, dividends to a dividend with
stopaj as the fee, deposits and withdrawals to cash movements, and monthly
interest to a dividend with no instrument. Orders that did not execute and
stock transfers are skipped, not errors.

- A currency conversion books only its own account's leg, like a transfer
  between the user's own accounts. Midas prints each conversion on both
  accounts' statements with only that account's amount, so booking both legs
  counted every conversion twice.
- The zero-commission row reports the commission waived. It is not paid into
  the cash account, and the account's printed balance excludes it, so it is
  skipped.
- Sections are found by title and columns by header text, never by position,
  and numbers in both Turkish and plain formats are accepted, because the
  layout is a document, not a documented API.

### PII

A real statement carries the customer's name, TCKN, address and IBAN, and
none of it is stored or logged.

- The database keeps the broker, currency, period and file sha256; the file
  is not kept. A transaction note is the broker name and the period, never the
  statement's description column.
- Logs carry the broker, portfolio id, row counts and error kind, never a
  file name, row or cell. Raw cells of an error row go to the client, the
  user's own data, and the preview masks IBANs and account numbers.
- Parse errors do not quote customer fields.
- No real statement enters the repository, and neither do real figures in a
  ticket or a fixture. Parser tests use synthetic row fixtures as Go literals
  with the real layout and invented names, numbers and dates. Sample PDFs stay
  outside the repo and are reached through `statement_dump`. Rejected:
  committing sample PDFs, which would publish customer data into git history.

## Known risks

- A broker can change its layout at any time. Partial parsing degrades that to
  "some rows need attention" instead of a failure.
- The parse endpoint does real work on an uploaded file. It is authenticated,
  and the size, row and worker caps bound it; a per-user rate limit is the
  upgrade path.
- Midas's cash can end slightly above the printed balance on months with
  many trades; the drift is not itemized, and the cash line shows it.
