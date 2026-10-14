# ADR 0008: Stock splits and bonus issues

Status: accepted (GLG-262, 2026-09-23); amended 2026-09-23 (GLG-265, BIST
bonus-issue source); amended 2026-10-01 (GLG-741, bonus shares booked by an
import); amended 2026-10-03 (GLG-828, bonus shares held from the ex-date)

## Context

A stock split or a BIST bonus issue (bedelsiz sermaye artırımı) corrupts
holdings today, because nothing models them. Every stored close is
unadjusted:

- BIST: the bulletin's `KAPANIS FIYATI` column (ADR 0001).
- US: Finnhub `c` / `pc` for the latest close, Tiingo `close` for history
  (ADR 0003, GLG-236).

On the ex-date the price drops by the split ratio while the quantity stays
the same. The position loses value, the average cost stays at the pre-split
level, and value history shows a step. ADR 0001 already promised this fix:
"Corporate actions (bonus issues, splits) are out of scope for Phase 3; the
history API (GLG-37) will show a step on those dates until Phase 6."

Two places carry every holdings calculation:

- `holdings.State.Apply` folds one transaction into positions, cost basis
  and cash.
- `transaction.Repo.List` returns a portfolio's transactions in order. The
  summary, value history, portfolio list and statement preview all read it.

This ADR records the decisions of epic GLG-261 (Phase 10).

## Decision

### Splits are market data, not user data

One global table, like `prices`:

```
splits(instrument_id, date, ratio, source)   unique (instrument_id, date)
```

No `user_id`: the table holds no user data. Users do not enter splits. They
come from providers, and an operator CLI flag is the fallback.

### Ratio = new shares per old share

The ratio is an exact decimal stored as TEXT, never a float. It must be
greater than 0.

| Event                  | Ratio |
| ---------------------- | ----- |
| 2-for-1 split          | `2`   |
| 50% bonus issue        | `1.5` |
| 100% bonus issue       | `2`   |
| 1-for-10 reverse split | `0.1` |

### Date = ex-date

The date is the ex-date, a market `DATE`. The split applies at the start of
that date, before any transaction on the same date, because trades on the
ex-date are already at post-split prices.

### Engine effect

```
Qty     *= ratio
AvgCost /= ratio
```

Cost basis and realized P/L do not change. Prices stay unadjusted: scaling
the quantity at the ex-date is what makes value history continuous.

### One choke point: `transaction.Repo.List`

`transaction.Repo.List` returns synthetic `split` rows for instruments that
appear in the portfolio, ordered by date, split first, then id. Every reader
of holdings gets splits without any change of its own.

`split` is a holdings-engine type only. It is not in `validTypes`, so the
transactions API rejects it and the paged transaction list never shows it.

### Sources

- **US:** Tiingo's `splitFactor` field on the existing daily-history call
  (used since GLG-236). Finnhub's latest-close call carries no split data,
  so a daily pass also reads a recent Tiingo window per tracked US
  instrument.
- **BIST:** see "BIST source" below.
- **CRYPTO and TEFAS:** never split. No source.

  > Amended 2026-09-28 by [ADR 0017](0017-gold.md) (GLG-534): the `GOLD`
  > market added in Phase 18 is never split either. ADR 0017 keeps it out of
  > everything keyed on market `BIST`, splits included.

### BIST source

Decided by spike GLG-265 (2026-09-23). BIST bonus issues (bedelsiz) come from
the **capital increase table on İş Yatırım's "Temel Değerler ve Oranlar"
page**, read as server-rendered HTML once a day. Source name `isyatirim`.
Fetcher: GLG-282.

```
GET https://www.isyatirim.com.tr/tr-tr/analiz/hisse/Sayfalar/Temel-Degerler-Ve-Oranlar.aspx
    -> 200 text/html, ~1.6 MB, ~3 s; no key, no login, default Go User-Agent works
```

The table (`data-csvname="temelsermaye"`, `<tbody id="SermayeRows">`) has one
`<tr class="SermayeRow">` per capital event; each cell carries a stable class
name. Excerpt:

```
<tr class="SermayeRow"><td class="SermayeColumn HISSE_KODU" title="Hareket Proje"><a href="...?hisse=HRKET">HRKET</a></td>
  <td class="... SHHE_TARIH" data-order="20260921000000">21.09.2026</td>
  <td class="... SHHE_BDLI_ORAN">0,00</td>        rights issue % (not a split)
  <td class="... SHHE_RHK_ORAN">0,00</td>         restricted-rights issue % (not a split)
  <td class="... SHHE_BDSZ_IK_ORAN">1.300,00</td> bonus % from internal resources
  <td class="... SHHE_BDSZ_TM_ORAN">0,00</td>     bonus % from dividend
  <td class="... SHHE_NAKIT_TM_ORAN">0,00</td> ...  cash dividend (ignored)
```

- **Ex-date:** `SHHE_TARIH` (`data-order`, first 8 digits).
- **Ratio:** `1 + (SHHE_BDSZ_IK_ORAN + SHHE_BDSZ_TM_ORAN) / 100`, Turkish
  number format (`.` thousands, `,` decimal). HRKET `1.300,00` -> `14`;
  KORDS `1.799,90` -> `18.999`; YEOTK `75,03 + 58,77` -> `2.338`. Rows with no
  bonus part (cash dividends, rights issues) are skipped. Rows dated after
  today are planned and skipped until their date.

  > Amended 2026-09-23 by [ADR 0009](0009-dividends.md) (GLG-286): the
  > cash dividend column `SHHE_NAKIT_TM_ORAN`, marked ignored in the excerpt
  > above, is read on the same fetch and stored in `dividends`. A row with a
  > cash part and no bonus part gives a dividend and no split; only a row
  > with neither, a rights issue, is skipped.

- **History depth:** the current calendar year only (326 rows on
  2026-09-23: every event since 2026-01-02 plus dated planned ones). The
  page's year filter calls `/_layouts/15/IsYatirim.Website/StockInfo/CompanyInfoAjax.aspx/GetSermayeArttirimlari`,
  which robots.txt disallows, so earlier years are not read. A plain form
  postback of the year returns the current year unchanged. Older bonus
  issues go through the `-add-split` operator flag.
- **Rate:** one request per day for all symbols. No published limit.
- **robots.txt** (`www.isyatirim.com.tr`, 2026-09-23): `User-agent: *`,
  `Disallow: /_layouts/`, `/_vti_bin/`, `/_catalogs/`, membership and search
  pages. `/tr-tr/analiz/` is allowed. No AI agent is named. The fetcher must
  never call anything under `/_layouts/`.

Verification (2026-09-23): every bonus row dated 2026-07-13 to 2026-09-22
(19 rows) was checked against the Borsa Istanbul bulletin for the same days.
Each one falls on the exact date the bulletin flags column 15 `OZSERMAYE
HALI = 03`, and each bulletin close is within the ±10% daily price limit of
`previous close / ratio`:

| Symbol | Ex-date    | Bonus %            | Ratio  | Prev close | Prev / ratio | Close |
| ------ | ---------- | ------------------ | ------ | ---------- | ------------ | ----- |
| KORDS  | 2026-09-14 | 1799.90            | 18.999 | 77.65      | 4.087        | 4.02  |
| MEGMT  | 2026-09-21 | 860                | 9.6    | 34.60      | 3.604        | 3.59  |
| ORGE   | 2026-08-19 | 400                | 5      | 113.10     | 22.620       | 22.64 |
| VAKFN  | 2026-07-21 | 20 (from dividend) | 1.2    | 1.57       | 1.308        | 1.31  |
| YEOTK  | 2026-07-31 | 75.03 + 58.77      | 2.338  | 93.00      | 39.778       | 39.00 |

The reverse also holds: every `03` flag in that window has a bonus row.
Flag `01` rows (SEKFK, KRTEK, CVKMD) are rights issues, and flag `06` rows
are cash dividends.

#### Rejected alternatives

| Candidate                                                                                                                       | What it offers                                                                              | Verdict                                                                                                                                                                                                |
| ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Borsa Istanbul bulletin, column 15 `OZSERMAYE HALI` (same zip as ADR 0001; robots.txt on `www.borsaistanbul.com` is `Allow: /`) | Ex-date and a type code (`03` bonus, `01` rights, `06` cash dividend) back to 2015-11, free | No ratio. Working the ratio out from the price drop is off by up to the ±10% daily limit, so 50% and 51.54% cannot be told apart. Kept as a cross-check only.                                          |
| Borsa Istanbul DataStore (`datastore.borsaistanbul.com`, robots.txt empty `User-agent: *`)                                      | Corporate action products                                                                   | Paid, needs an account.                                                                                                                                                                                |
| Borsa Istanbul archive (`/data/tgh/`, company news)                                                                             | Announcements                                                                               | Stops at 2009-12-31.                                                                                                                                                                                   |
| KAP (`www.kap.org.tr`)                                                                                                          | Official disclosures with ratios, corporate actions calendar                                | `www.kap.org.tr` answers every plain request, `robots.txt` included, with an HTTP 666 bot-block page. We do not work around bot detection. The official KAP API goes through MKK and needs membership. |
| Yahoo `v8/finance/chart?events=split`                                                                                           | Splits for `.IS` symbols                                                                    | robots.txt on `query1.` and `query2.finance.yahoo.com`: `User-agent: *` / `Disallow: /`.                                                                                                               |
| İş Yatırım `/_layouts/` AJAX endpoints (`GetSermayeArttirimlari`, `HisseTekil`)                                                 | All years, JSON                                                                             | Disallowed by robots.txt (`Disallow: /_layouts/`).                                                                                                                                                     |
| İş Yatırım research posts (`arastirma.isyatirim.com.tr`, robots.txt allows)                                                     | A daily table of capital increases                                                          | The table is behind a login.                                                                                                                                                                           |
| Fintables (`fintables.com`)                                                                                                     | Capital increase history                                                                    | Cloudflare challenge on every request, `robots.txt` included.                                                                                                                                          |

Risks specific to this source: the page is HTML meant for a browser, not an
API, so a redesign breaks the parser. The fetcher matches cells by class name
and fails loudly when the table or a class is missing. It is a broker's page,
and its terms of use (a PDF) were not reviewed in the spike. If it disappears,
BIST splits fall back to `-add-split`, and the bulletin's `03` flag still says
which days need one.

### Bonus shares booked by an import

> Amended 2026-10-01 (GLG-741, decision on GLG-731).

A broker statement's bonus row (`Bedelsiz`) is booked in its portfolio as a
`buy` at price 0 (GLG-739). The same event is also a global `splits` row, so
applying both scales the position twice, whichever arrived first. The rule,
in `transaction.Repo.List`:

> A split of instrument I with ex-date D is left out for a portfolio that has
> a `buy` of I at price 0 dated within 10 calendar days of D, before or after.

The statement row is the broker's record of what that account received,
fractions included, so it wins for that portfolio. Other portfolios still
get the split, and price scaling by the instrument's splits in the summary
and history is unchanged: prices are market data. The window covers the
credit date a statement prints, which can trail the ex-date by a few days.
The statement preview's reconciliation applies the same rule to the rows it
proposes.

> Amended 2026-10-03 (GLG-828): the buy is held from the ex-date.

Since the price is scaled from the ex-date on, `transaction.Repo.List` also
moves each such buy to the ex-date of the split it books. Held only from its
own date, the position was valued at the scaled price with the pre-split
quantity in between, a dip in value history and returns (a spike for a buy
dated before the ex-date). The statement import's duplicate check reads the
rows on their stored dates instead (`transaction.Repo.ListStored`), so a
re-imported bonus row still pairs with the one already booked.

Ceiling: any zero-price buy in the window suppresses the split, so a gift of
shares entered by hand next to a split loses the split. The upgrade path is
an explicit marker on imported bonus rows.

### Recorded simplifications

- **Fractional shares are kept after a split.** BIST pays the fraction
  (küsurat) in cash; that payment is not modelled.
- **A zero-price buy typed to mimic a bonus issue double counts** once the
  split lands. There is no prod instance, so there is no migration for it.

  > Amended 2026-10-01 (GLG-741): no longer true inside the window of
  > "Bonus shares booked by an import" above.

- **Rights issues (bedelli) and spin-offs are not splits** and are out of
  scope.
- **Last writer wins** between sources for the same `(instrument_id, date)`.

### Not in this epic

- Dividends from provider data.

  > Amended 2026-09-23 by [ADR 0009](0009-dividends.md) (GLG-7): dividends
  > from provider data were delivered by the Phase 6 epic right after this
  > one.

- Rights issues and spin-offs.
- Split rows in the statement import. None were seen in the Midas samples;
  `HİSSE TRANSFERLERİ` stays skipped.
- User-entered splits.

## Known risks

- A missed split leaves the old behaviour for that instrument: a step in
  value history and a stale average cost. The `-add-split` operator flag is
  the fallback.
- A wrong ratio or date from a provider corrupts every portfolio holding
  that instrument, because the table is global. Last writer wins, so a
  correction by the operator flag or a later fetch replaces it.

## Consequences

- GLG-263 adds the `splits` table and `split.Repo`.
- GLG-264 makes `holdings.State.Apply` handle a `split` row.
- GLG-265 spikes a BIST split and bonus-issue source and appends its
  outcome here: İş Yatırım's capital increase table (current year).
- GLG-282 fetches BIST bonus issues from that table daily.
- GLG-268 makes `transaction.Repo.List` return split rows for the
  portfolio's instruments.
- GLG-269 stores US splits from Tiingo `splitFactor` on the existing history
  fetch.
- GLG-271 adds the `-add-split` operator flag.
- GLG-273 adds a `splits` field per position to the summary API.
- GLG-276 shows a split badge on positions with the split date and ratio.
- GLG-279 adds the daily US split pass over a recent Tiingo window.
