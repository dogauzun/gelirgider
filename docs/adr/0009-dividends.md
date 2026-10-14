# ADR 0009: Dividends from provider data

Status: accepted (GLG-291, 2026-09-23); amended 2026-09-25 (GLG-374, one
ex-date per recorded dividend); amended 2026-09-29 (GLG-594, Phase 21
timeline and Temettü tab)

## Context

Dividends are entered by hand today, or come from the Midas statement import
(ADR 0006). No provider dividend data is read, so a user who does not type
each dividend in sees it nowhere. ADR 0008 left "Dividends from provider
data" out of the splits epic, and the Phase 6 roadmap still lists it.

The `dividend` transaction type already exists. The Midas import stores the
gross in `amount` and the stopaj in `fee`, and the holdings engine nets the
fee out of a dividend (ADR 0006). Midas dividend rows carry the payment date.

This ADR records the decisions of epic GLG-7 (Phase 6).

## Decision

### Dividends are market data, not user data

One global table, like `prices` and `splits` (ADR 0008):

```
dividends(instrument_id, date, amount, currency, source)   primary key (instrument_id, date)
```

No `user_id`: the table holds no user data.

- `date` is the ex-date, a market `DATE`.
- `amount` is the gross cash per share, an exact decimal stored as TEXT,
  greater than 0.
- `currency` is the instrument's currency.
- Last writer wins for the same `(instrument_id, date)`.

### Suggest, do not write

Providers never create user transactions. The app suggests expected
dividends per portfolio:

```
GET /api/portfolios/{id}/dividends/expected
```

Each suggestion is the quantity held before the ex-date times the gross per
share. The user adds each one with one click, which creates a normal
`dividend` transaction through the existing create-transaction API.

Rejected:

| Option                                                 | Verdict                                                                 |
| ------------------------------------------------------ | ----------------------------------------------------------------------- |
| Synthetic rows in `transaction.Repo.List`, like splits | Double counts against dividends entered by hand or imported from Midas. |
| Transactions created automatically from provider data  | Same double-counting risk.                                              |

### Gross in `amount`, stopaj in `fee`

The same convention as the Midas import (ADR 0006). Stopaj is pre-filled at a
fixed rate per market:

| Market    | Stopaj                      |
| --------- | --------------------------- |
| `BIST`    | 15%                         |
| `US`      | 20% (US-Turkey treaty rate) |
| any other | 0                           |

The user can edit it before or after adding. For dividends the fee field is
labelled Stopaj / Withholding tax instead of Komisyon / Commission.

### Sources

- **BIST:** the cash dividend columns of the İş Yatırım capital increase
  table that GLG-282 already fetches for bonus issues (ADR 0008). Gross TRY
  per share = `SHHE_NAKIT_TM_ORAN` / 100. Same fetch, no second request,
  source `isyatirim`. The same robots.txt limit applies: nothing under
  `/_layouts/`, so current calendar year only.
- **US:** Tiingo `divCash` on the existing daily-history call, source
  `tiingo`, currency `USD`. The backfill and the daily recent pass
  (GLG-279) both go through it, so no new HTTP calls.
- **TEFAS and CRYPTO:** no source.

  > Amended 2026-09-28 by [ADR 0017](0017-gold.md) (GLG-534): the `GOLD`
  > market added in Phase 18 has no dividend source either. ADR 0017 keeps it
  > out of everything keyed on market `BIST`, dividends included.

robots.txt was checked on 2026-09-23 for `www.isyatirim.com.tr`,
`api.tiingo.com` and `finnhub.io`. All three allow these paths.

### Hiding and dismissing

- A suggestion is hidden when the portfolio already has a `dividend`
  transaction for that instrument dated from ex-date −7 to +45 days. This
  covers dividends entered by hand and Midas imports, which use the payment
  date. Each recorded dividend hides one ex-date only: the latest one on or
  before its date, else the earliest one after it (GLG-374).
- The user can dismiss a suggestion. Dismissals live in a per-user
  `dividend_dismissals` table. There is no undo.

### Timeline

Phase 21 (GLG-603) adds a read-only view of the dividends already recorded:

```
GET /api/portfolios/{id}/dividends/timeline
```

It returns monthly and yearly buckets, each with `gross`, `tax`, `net` and
`interest`, and totals per instrument, all in the reporting currency. Each
`dividend` transaction is converted at the latest `fx_rates` rate on or
before its own date, the `fxWalk` of `backend/internal/server/history.go`.
A row with no such rate is left out and its currency listed in
`missing_rates`. The route takes a numeric portfolio id only; the Phase 17
`all` scope stays a 404 (ADR 0014).

The expected dividends panel lives on the Temettü tab, open by default
(GLG-605). It is no longer on İşlemler, which supersedes the collapsed
İşlemler placement of GLG-568.

> Superseded 2026-10-05 (GLG-939): the panel, now "Onay bekleyen temettüler",
> is the first card on the Temettü tab and always open; it is no longer a
> `<details>` element.

### Recorded simplifications

- **The transaction date is the ex-date, not the payment date.** Neither
  source gives a payment date.
- **Stopaj rates are fixed per market.** No per-instrument rate for ETFs or
  funds, and a law change is a code change.
- **BIST history covers the current calendar year only.**
- **The quantity counts every row dated before the ex-date**, including
  splits.
- **Interest is not in the dividend bars.** The timeline reports it in its
  own `interest` field per bucket.
- **The timeline converts at the transaction-date rate, the Özet stats at
  today's rate** (Phase 21, GLG-595), so the two dividend totals in the
  reporting currency differ when the rate moved.

### Not in this epic

- Stock dividends. Bonus issues are already splits (ADR 0008).
- Dividend reinvestment.
- Fund distributions.
- Crypto staking rewards.
- Payment dates.
- An undo for dismissals.

## Consequences

- GLG-285 adds the `dividends` table and `dividend.Repo`.
- GLG-286 parses BIST cash dividends from the İş Yatırım capital increase
  table on the existing fetch.
- GLG-287 stores US dividends from Tiingo `divCash` on the existing history
  fetch.
- GLG-288 labels the dividend fee field Stopaj / Withholding tax.
- GLG-289 adds `GET /api/portfolios/{id}/dividends/expected` with stopaj
  pre-filled.
- GLG-290 adds dismissing an expected dividend.
- GLG-292 adds the expected dividends panel with add, add all and dismiss.
- GLG-291 writes this ADR and the Phase 6 roadmap section.
