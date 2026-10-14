# Cash gap deposits: guide the user to the missing deposits

Date: 2026-10-07

## Problem

The Performans tab shows only this when every return is `null`:

> Getiri hesaplanamıyor: dönem değersiz başlıyor ya da nakit bir gün eksiye
> düşüyor. Alımları karşılayan para girişlerini kaydet.

The returns are `null` when `V_0 <= 0` or when the day's cash is negative on
any day of the range (ADR 0011, GLG-641). The usual cause is a portfolio that
records buys, often from a statement import, without the deposits that paid
for them. The message names no day, no currency and no amount, and offers no
action. The user has to find the gaps alone.

## Goal

Show where and by how much the cash goes negative, and fill the gaps with
one confirmed click. The app never computes a return over money the user did
not record: the deposits it proposes become real transactions only after the
user confirms them.

Success: a portfolio with buys and no deposits shows the gaps on the
Performans tab; after one confirm, the returns, the periodic return heatmap
and the drawdown show numbers.

## Decisions

- **Guide plus one click.** Silent implicit deposits stay rejected (ADR 0011).
  The app proposes recorded deposits and writes them only on confirm.
- **Minimal deposits, one per dip.** Per portfolio and currency, each day
  the cash balance reaches a new low below zero gets one deposit of the
  extra shortfall, dated that day.
- **Deposit in the gap's currency.** A USD buy paid from TRY with no FX leg
  recorded gives a USD gap, and the app proposes a USD deposit. Proposing an
  FX exchange instead was rejected: it needs a rate per day and more UI. The
  user can still record the exchange by hand.
- **Only when the returns are null.** The notice replaces the existing
  message and shows nowhere else. A USD gap that positive TRY cash offsets
  keeps the returns defined, so nothing shows for it.
- **All portfolios too.** The `all` scope lists the gaps per portfolio, and
  one confirm writes each deposit into its own portfolio. Cash never moves
  between portfolios, so the walk runs per portfolio.
- **Undo by hand.** The deposits carry a note; the user deletes them in the
  transactions list like any other row. No undo button, no flag column.
- **Out of scope:** detection in the statement import review, a hint on the
  chart header's dash, one deposit per uncovered buy.

## Backend

### Gap walk

A function takes one portfolio's transactions and returns its gaps:

```
[{date, currency, amount}]
```

For each currency it walks the cash ledger in date order, using the same
cash rules as holdings (deposits, withdrawals, buys, sells, fees, dividends,
interest, FX legs). It keeps the lowest balance seen so far, starting at 0.
At the end of a day whose balance is below that low, it emits a gap of
`low − balance` dated that day and moves the low to the balance.

The sum of a currency's gaps equals its deepest shortfall. With the gaps
deposited, no currency's balance is below zero on any day, so the converted
cash is not negative either.

Amounts are `decimal`. Gaps are ordered by date, then currency.

The function reuses the holdings cash computation; it does not copy the cash
rules. If holdings has no per-day cash per currency to reuse, the plan adds
the smallest hook to expose it.

### Routes

`{id}` is a numeric id or `all`, resolved like the history and periods
routes (`backend/internal/server/scope.go`). The walk runs once per
portfolio in scope.

- `GET /api/portfolios/{id}/cash-gaps` returns
  `[{portfolio_id, date, currency, amount}]`, ordered by portfolio, date,
  currency; `[]` when there are none.
- `POST /api/portfolios/{id}/cash-gaps` with body `{"note": "<text>"}`
  recomputes the gaps on the server and inserts one `deposit` transaction per
  gap into that gap's portfolio, all in one database transaction, for every
  portfolio in scope. It returns the created transactions'
  ids. The client sends no amounts, so a stale or edited list cannot be
  written. With no gaps it inserts nothing and returns `[]`.

Both filter by `user_id` and return 404 for a portfolio the user does not
own, like the other portfolio routes. The note is the localized text the
client sends, for example "Eksik para girişi (otomatik)", stored in the
existing `transactions.note` column, so the user can find and delete these
deposits later. No schema change.

Both routes get swag annotations; `make swagger` regenerates
`backend/apidocs/`.

### ADR

ADR 0011 gets a dated amendment under "No return without a positive base":
implicit deposits are still rejected; the app may propose recorded deposits
for the cash gaps, written only when the user confirms.

### Tests

One table test for the gap walk: no gaps; one dip; two dips; a dip, a
recovery, then a deeper dip (only the extra part); a dip that recovers and
dips again less deep (no second gap); two currencies. One handler test for
POST: the deposits are inserted, a second POST inserts nothing, `all`
writes each gap into its own portfolio, another user's portfolio gives 404.

## Frontend

### CashGapNotice

A new component replaces the bare `performance.periods.undefined` text in
`frontend/src/pages/PerformancePage.tsx` and
`frontend/src/components/DrawdownCard.tsx`. It fetches
`GET /api/portfolios/{id}/cash-gaps` with TanStack Query.

- **Gaps present:** it names the first gap's date and the shortfall per
  currency (the sum of that currency's gaps); in the `all` scope, one line
  per portfolio with gaps, with the portfolio's name, for example "Nakit 12.03.2024'te
  eksiye düştü: ₺48.200, $1.150 eksik. Alımları karşılayan para girişleri
  kayıtlı değil.", and shows a button "Eksik para girişlerini ekle".
- **No gaps:** the returns are null for the other reason, `V_0 <= 0`. It
  shows a message about the range starting with no value, without the
  negative cash part.

The button opens a Base UI dialog that lists each proposed deposit: date,
currency, amount, and the portfolio in the `all` scope. Confirm posts to `POST /api/portfolios/{id}/cash-gaps` with
the localized note, then invalidates the history, summary, transactions,
returns periods and cash gaps queries. Cancel closes it. A failed post shows
the usual error text in the dialog.

> Superseded 2026-10-07 (GLG-1110): the confirm dialog is the app's native
> `<dialog>` through `useModal` ([ADR 0023](../../adr/0023-ui-design-system-v2.md)),
> not a Base UI dialog.

Every new string has `tr` and `en` keys.

### Tests

One component test: gaps render with date and per-currency totals; the
`all` scope groups them by portfolio; confirm posts and closes; with no gaps
the fallback message renders and no button.

## Tickets

1. Backend: gap walk, both routes, swag, ADR 0011 amendment.
2. Frontend: `CashGapNotice` and dialog, wired into both places. Blocked by
   ticket 1 (a Blocks link in Jira).
