# ADR 0018: Garanti BBVA statements

Status: accepted (2026-10-01).

## Context

Garanti BBVA is a common custodian of Turkish fund holdings. It offers no CSV
export and no API, only two monthly PDFs by e-mail: one for the bank's fund
account (Garanti BBVA) and one for the brokerage account at Garanti BBVA
Yatırım. Entering a month of fund trades by hand is the work the statement
import of [ADR 0006](0006-broker-statement-import.md) exists to remove.

Neither PDF has the Midas shape. Text extraction returns one row per page for
both, hundreds of cells long, so a row is not a table row. The layouts were
read from real statements kept outside the repository; no customer data from
them appears in the repo (ADR 0006, PII).

## Decision

### Two parsers, two broker keys

`garanti` is the bank's fund statement and `garanti_yatirim` the brokerage
statement, each its own package under `backend/internal/statement`. They are
separate because the documents share nothing: one lists fund trades by fund
name in TL and foreign currency, the other a TRY ledger of share trades,
commissions and bank transfers. Both are pure and partial, as ADR 0006
decided: an unreadable record is a parse error and parsing goes on.

Both walk a flat stream of the page cells, switching on section and page
titles. A record is read by its anchors (a fund code followed by a currency,
a date followed by a time, a date and the ledger shape), never by a fixed cell
count, and headers and page boilerplate are skipped wherever they appear,
since pages break inside sections.

### Fund statement

- Movements of the same date, fund and price are netted into one buy or sell.
  The bank account the funds are paid from is outside the portfolio, so each
  date's net cash effect is booked as one deposit or withdrawal. The
  statement's stopaj goes into the fee of the day's net trade.
- Every fund is a TEFAS fund. The statement carries the TEFAS market hint, so
  its balances reconcile against the portfolio's TEFAS positions, not its BIST
  shares.
- A movement prints the fund's name, truncated, not its code. The parser maps
  a name to a code only through the same statement's balances, since names
  change while the code stays. A fund sold out within the month has no
  balance, so its movements need the user's attention in the preview.

### Fund discovery and renames through TEFAS

Name resolution happens outside the parser, in the shared resolver, so the
parser stays pure. In order: the user's pick for the printed name, then the
fund names learned from the user's earlier committed statements, then the
fund names TEFAS publishes. A name TEFAS knows only in full matches a cut or
abbreviated one word by word, with `GPY` and `GP` as `GARANTİ PORTFÖY` and
`1.` as `BİRİNCİ`; the closest match wins and two equally close funds leave
the row unknown. A pick always wins, a learned name outlives a rename until a
statement prints the new one, and a name two funds share stays unknown.

A fund the portfolio does not know yet is looked up on TEFAS when the import
sees its code, and created only when TEFAS confirms it (ADR 0006). A tracked
fund's name follows TEFAS's published name (ADR 0004), so a renamed fund keeps
resolving.

### Brokerage statement

- A buy or sell is dated on the trade date its description prints when that
  date is inside the period, else on the ledger's value date. The sell code
  is read like the buy code. A description the parser does not recognise is
  an `unknown_row` error, never a guess.
- Fees (custody, account-keeping and order-cancel charges) are booked as a
  fee, or as a deposit when positive. IPO payments and refunds are cash
  movements, and a fractional share cashed out in the ledger is a sell.
- A commission is split pro rata over the trades of its date, on the side whose
  amounts sum to its base when one does. A commission on a date with no trade
  stays a fee.
- A transfer between the bank and brokerage accounts is a deposit or a
  withdrawal by its sign. A deposit is dated on the earliest trade date of the
  buys it pays for, because dated on settlement it would leave cash negative
  in between, and returns are null for a range with negative cash.
- A dividend is booked at its printed amount with tax 0, and its transfer row
  is a withdrawal. The printed amount looks like the payment after
  withholding, so tax 0 does not mean the dividend was untaxed: no gross or
  tax is derived from a withholding rate, and cash matches the account.
- The opening balance row is skipped; its cash comes from the previous month.
- The ledger is the source of truth for trades and cash. The share movement
  list is read only for what the ledger does not show: bonus issues, IPO
  allotments and transfers in, never the trades the ledger already books.
- The holdings and cash balance feed the reconciliation. The time of day is
  dropped, since transactions carry a date only.

## Not in scope

- The Garanti gold account, assets other than shares at Garanti BBVA Yatırım,
  other banks and importing statements from e-mail.
- One portfolio per account is recommended: the brokerage cash reconciliation
  compares the whole portfolio's TRY cash with the statement's, so a portfolio
  that also holds the fund account or another broker's TRY cash shows a
  mismatch.
