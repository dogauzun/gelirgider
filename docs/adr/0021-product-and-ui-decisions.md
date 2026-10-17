# ADR 0021: i18n and CSV export

Status: accepted (2026-10-01).

## Context

Two product decisions had no record: how the UI is translated and what the
transactions CSV export promises. UI rules are in
[ADR 0023](0023-ui-design-system-v2.md).

## Decision

### i18n

- **Own module, no library.** All UI strings are one flat key-to-string map per
  locale, composed in `frontend/src/i18n/strings.ts` from one file per area
  (`tr/<area>.ts`, `en/<area>.ts`). Every UI string goes through a key, in `tr`
  and `en`.
- **`tr` is the default and defines the key set.** Each `en` area file is typed
  against its `tr` counterpart, so `tsc -b`, the first step of `npm run build`,
  fails on a missing or extra key.
  A `<key>.one` entry is the English singular, picked when `count` is 1;
  Turkish needs none.
- **The locale is stored in the browser only**, in `localStorage['locale']`,
  not on the user row, because it affects rendering alone; the upgrade path is
  a `locale` column and `PUT /api/me/locale`. Disabled storage falls back to
  `tr`, and `<html lang>` follows the locale.
- **A switch re-renders the tree.** `t()` stays a plain function call, and the
  root component keys its subtree on `useLocale()`. Numbers and dates follow
  the locale through `Intl`.
- **The backend sees the locale once.** Registration sends it, and the new
  user's default portfolio is named "Portföy" or "Portfolio" from it.

### Transactions CSV export

`GET /api/portfolios/{id}/transactions.csv` lets transactions leave the app.

- It is comma separated, UTF-8 with no BOM, with dot decimals, raw type codes
  and the oldest row first. Split rows are left out, since they are derived from
  market data, not entered.
- The columns are frozen as
  `date,type,symbol,quantity,price,amount,fee,currency,note,market`, so a
  spreadsheet or script built on them keeps working. A new column goes at the
  end, which is why `market` comes after `note`.
- A symbol, note or market cell starting with `=`, `+`, `-` or `@` gets a
  leading `'`, so a spreadsheet does not run it as a formula.
- It takes the transaction list's filters (`type`, `instrument_id`, `import_id`,
  `from`, `to`), with `from` and `to` an inclusive `YYYY-MM-DD` range.
- `GET /api/export.zip`, from the account menu, is one zip with one such CSV
  per portfolio of the user, named `<portfolio name>-transactions.csv` (the
  portfolio id when the name is empty or clashes), each written with no filter.
  It holds transactions only, so it is not a backup: the database snapshot is
  ([deploy/RUNBOOK.md](../../deploy/RUNBOOK.md)). To get a portfolio back, the
  user imports its CSV through the import page ([ADR 0006](0006-broker-statement-import.md)).
