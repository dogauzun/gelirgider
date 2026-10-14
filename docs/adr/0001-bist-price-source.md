# ADR 0001: BIST end-of-day price source

Status: accepted (GLG-33, 2026-09-13); amended 2026-09-14 (GLG-103, manual
prices go to `manual_prices`); superseded in part by
[ADR 0003](0003-multi-currency-and-market-data-sources.md) (GLG-70,
2026-09-14): no provider interface, each market has its own package that
writes the shared tables; amended 2026-09-21 (GLG-208, `ETF` group added to
the bulletin filter); amended 2026-09-21 (GLG-212, a row's code must end in
its group's suffix); amended 2026-09-21 (GLG-228, unknown bulletin symbols
are inserted as instruments); superseded in part by
[ADR 0008](0008-splits.md) (GLG-264, 2026-09-23): splits and bonus issues
shipped in Phase 10, the holdings engine scales the quantity on the
ex-date; amended 2026-09-23 (GLG-283, see Amendments); amended 2026-10-08
(GLG-1174, `EMS` group added to the bulletin filter); amended 2026-10-10
(GLG-1277, one suffix map, `cmd/symbols` reuses the fetcher's bulletin parse)

## Decision

Daily BIST closing prices come from the **Borsa Istanbul daily equity bulletin**:
one public zip per trading day containing one semicolon-separated CSV with every
listed instrument. No key, no login, no User-Agent tricks. Official data, raw
(unadjusted) closes, one request per day for all symbols, history back to
November 2015 in the same format.

Manual price entry (GLG-32) stays as the permanent fallback. Since GLG-103 it
writes the caller's own `manual_prices` row, not `prices`; a manual close wins
over a fetched close for the same instrument and date
(`holdings.MergeSeries`). The fetcher writes `prices` with
`source = 'bist_bulletin'`. The "manual is the only source for now" comment
in `006_prices.sql` predates both and is stale; applied schema files are
not edited (GLG-42).

## Request and response shape (verified 2026-09-13 with curl)

Request (one date, all symbols):

```
GET https://www.borsaistanbul.com/data/thb/{YYYY}/{MM}/thb{YYYY}{MM}{DD}1.zip
GET https://www.borsaistanbul.com/data/thb/2026/09/thb202609111.zip   -> 200, 294 KB
GET https://www.borsaistanbul.com/data/thb/2026/09/thb202609121.zip   -> 404 (Saturday)
```

`404` means non-trading day or not yet published. The file carried an
18:25 Europe/Istanbul timestamp on both days checked (market closes 18:00).

Response: zip with one file `thb202609111.csv`, ASCII, `;` separated, `.` decimal,
two header rows (Turkish, then English), ~11,000 data rows covering equities,
warrants, funds, and VWAP pseudo-instruments. Filter to equities with column 7
`ENSTRUMAN GRUBU == "EQT"` (630 rows on 2026-09-11; their codes all end in `.E`).
Columns the fetcher needs (1-based): 1 `TARIH` (trade date `YYYY-MM-DD`),
2 `ISLEM KODU` (e.g. `THYAO.E`, strip `.E` to get `instruments.symbol`),
22 `KAPANIS FIYATI` (closing price). Example rows:

```
TARIH;ISLEM  KODU;BULTEN ADI;PAZAR GRUBU;PAZAR;...;ENSTRUMAN GRUBU;...;KAPANIS FIYATI;...
2026-09-11;THYAO.E;TURK HAVA YOLLARI;;Z;MSPOT;EQT;MSPOTEQT;...;299.25;299.25;299.25;298.25;302.75;300.25;300.25;0.334;...
2026-09-11;ASELS.E;...  close 393.75      2026-09-11;KUYAS.E;...  close 72.65
```

Rows with an empty or `0` close (3 of 630 on 2026-09-11: `UMPAS.E`, `ISATR.E`,
`ISKUR.E`; suspended or untraded) are skipped, not stored as zero.

Verified: THYAO, ASELS, KUYAS present with closes matching Is Yatirim; 2026-09-07
file present (Yahoo has no data for that day, see below); files for
2025-09-11, 2024-01-02, 2020-01-02 exist with the identical header. 2015-01-02
returns `tbult_s1.xls` (pre-BISTECH format), so backfill starts at 2015-11-30.

## Alternatives considered

| Source                   | All BIST equities          | Free                                                      | History                         | No login                                      | Stability                                        | Verdict                          |
| ------------------------ | -------------------------- | --------------------------------------------------------- | ------------------------------- | --------------------------------------------- | ------------------------------------------------ | -------------------------------- |
| BIST bulletin            | yes, 630                   | yes                                                       | yes, per-day file since 2015-11 | yes                                           | official publisher                               | **chosen**                       |
| Yahoo `v8/finance/chart` | yes (spot-checked 10)      | yes                                                       | yes, `period1/period2`          | yes, but needs any `User-Agent` (429 without) | unofficial; `v7/quote` already broke (401 crumb) | fallback (see note below)        |
| Is Yatirim `HisseTekil`  | yes                        | yes                                                       | yes, daily since 2005           | yes                                           | undocumented broker endpoint                     | second fallback (see note below) |
| Twelve Data              | lists 654 BIST symbols     | no: Basic plan is US, forex, crypto only (8/min, 800/day) | n/a                             | key                                           | commercial                                       | rejected                         |
| Finnhub                  | international is paid only | no                                                        | n/a                             | key                                           | commercial                                       | rejected                         |

> Superseded 2026-09-23 (GLG-283): the Yahoo and Is Yatirim rows marked
> fallback are blocked by robots.txt and must not be used. See Amendments.

Why Yahoo lost despite being the ticket's front-runner: on 2026-09-13 every
symbol checked (THYAO, ASELS, GARAN, EREGL, KUYAS) had **no data for
2026-09-07**, a normal trading day (bulletin and Is Yatirim both have it). It
also returns float noise (`128.89999389648438`), split-adjusted closes, and
needs one request per symbol (600/day). Is Yatirim lost on stability only: a
single broker's SharePoint data endpoint with no documentation, and its closes
are adjusted for capital increases, so they diverge from what a user paid.

Fallback shapes, so the swap is mechanical if the bulletin dies:

> Superseded 2026-09-23 (GLG-283): both fallbacks below are blocked by
> robots.txt and must not be used. See Amendments.

```
GET https://query1.finance.yahoo.com/v8/finance/chart/THYAO.IS?period1=1788814800&period2=1788901200&interval=1d
    header: User-Agent: gelirgider/1.0     (period = [00:00, 24:00) Europe/Istanbul as unix seconds)
    -> {"chart":{"result":[{"meta":{"currency":"TRY","symbol":"THYAO.IS","exchangeTimezoneName":"Europe/Istanbul",...},
        "timestamp":[1788849000],"indicators":{"quote":[{"open":[296.5],"high":[305.5],"close":[305.0],"volume":[50542920],"low":[295.75]}],
        "adjclose":[{"adjclose":[305.0]}]}}],"error":null}}
    unknown symbol -> 404 {"chart":{"result":null,"error":{"code":"Not Found",...}}}; holiday -> no "timestamp" key

GET https://www.isyatirim.com.tr/_layouts/15/Isyatirim.Website/Common/Data.aspx/HisseTekil?hisse=THYAO&startdate=08-09-2026&enddate=08-09-2026
    -> {"ok":true,"value":[{"HGDG_HS_KODU":"THYAO","HGDG_TARIH":"08-09-2026","HGDG_KAPANIS":305.0000,...}]}
    unknown symbol -> {"ok":true,"value":[]}   (indistinguishable from "no data")
```

## Known risks

- URL pattern and CSV layout are not a documented API. Mitigation: fetcher
  matches columns by header name, not index, and alerts on a header change.
- Publish time is observed (18:25), not promised. Scheduler (GLG-35) runs at
  19:00 Europe/Istanbul and retries hourly on 404 until 23:00.

  > Amended 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-303):
  > 19:00 is now only the default of the `bist` source's `source.bist.at`
  > setting, which an admin can change per source; the hourly retry until
  > 23:00 is unchanged.

- Closes are raw. Corporate actions (bonus issues, splits) are out of scope for
  Phase 3; the history API (GLG-37) will show a step on those dates until Phase 6.

  > Superseded 2026-09-23 by [ADR 0008](0008-splits.md) (GLG-264): splits
  > and bonus issues shipped in Phase 10, not Phase 6; the holdings engine
  > scales the quantity on the ex-date, so value history no longer steps
  > while the stored closes stay raw.

- The file is ~2 MB and covers instruments we do not track; the fetcher streams
  it and keeps only rows in a tracked group whose series code carries that
  group's suffix. Since GLG-228 a kept symbol with no `instruments` row gets
  one inserted rather than being skipped, so that group/suffix filter is the
  only thing keeping junk out of `instruments`.
- Backfill (GLG-36) is one request per trading day: ~250 per year. Rate limit
  unknown; keep a small delay between requests and stop on the first 5xx.
- Encoding is ASCII today; if it becomes Windows-1254, names are affected, not
  symbols or prices.

## Consequences

- GLG-34 (fetcher) parses one bulletin for one date into `prices` rows. No
  provider interface: a second market brings one (ADR then).

  > Superseded 2026-09-14 by
  > [ADR 0003](0003-multi-currency-and-market-data-sources.md) (GLG-70): the
  > second market brought no provider interface either; each market has its
  > own package that writes the shared tables with its own `source` value.

- GLG-36 (backfill) is the same parser in a loop over dates, newest first.

  > Correction 2026-09-13 (GLG-36): the backfill has always walked the dates
  > oldest to newest, from the start date forward
  > (`bist.BackfillRange` in `backend/internal/bist/backfill.go`).

- GLG-208 widened the filter to a second group: `ETF` (BIST-listed exchange
  traded funds, 34 rows on 2026-09-18, codes suffixed `.F`). Group to suffix
  is a map (`EQT` -> `.E`, `ETF` -> `.F`) in both the fetcher and
  `tools/import_bist_symbols.go`; ETFs are stored as ordinary BIST/TRY
  instruments, so nothing downstream changes. GLG-212 made that a match
  rather than a blind trim: a row in a tracked group whose code does not end
  in the group's suffix is skipped, which drops the default-settlement
  (temerrut) series the bulletin publishes inside `EQT` as e.g. `OYAKC.TE`.

  > Superseded 2026-10-06 (GLG-1093): the symbol importers in `tools/` are
  > one binary, `backend/cmd/symbols` (#1045), so the second suffix map is
  > in `backend/cmd/symbols/bist.go`; refresh the list with
  > `go run ./cmd/symbols -market bist` from `backend/`.

  > Amended 2026-10-08 (GLG-1174): a third group, `EMS`, maps to `.S1`. Its
  > only instrument is the Darphane gold certificate `ALTIN.S1`, stored as
  > BIST `ALTIN`. Both maps carry it (`backend/internal/bist/bist.go` and
  > `backend/cmd/symbols/bist.go`).

  > Superseded 2026-10-10 (GLG-1277): the suffix map lives in one place,
  > `wantedGroups` in `backend/internal/bist/bist.go`. `cmd/symbols` calls
  > `bist.Fetcher.FetchBulletin`, the fetcher's own bulletin parse, which
  > keeps suspended rows (empty or zero close) for the symbol list;
  > `FetchCloses` drops them before a price is stored.

- GLG-228 made the fetcher read column 3 `BULTEN ADI` as well, so `bist.Sync`
  inserts a kept bulletin symbol that has no `instruments` row as an ordinary
  BIST/TRY instrument named from that column. A company listed after
  `bist_symbols.csv` was generated is therefore searchable and priceable the
  day it first trades, without re-running `tools/import_bist_symbols.go`. A
  backfill over old bulletins adds companies delisted since, which is wanted:
  a user can hold one historically.

  > Superseded 2026-10-06 (GLG-1093): the importer is now
  > `go run ./cmd/symbols -market bist`, run from `backend/` (#1045).

- The Yahoo and Is Yatirim shapes above are recorded so a fallback fetcher can
  be written without a new spike.

  > Superseded 2026-09-23 (GLG-283): both fallbacks are blocked by
  > robots.txt and must not be used. See Amendments.

## Amendments

### 2026-09-23 (GLG-283): Yahoo and Is Yatirim fallbacks are not usable

Found during spike GLG-265. Both fallback hosts disallow the fallback paths in
robots.txt (checked 2026-09-23 with curl):

- `query1.finance.yahoo.com/robots.txt` and `query2.finance.yahoo.com/robots.txt`:
  `User-agent: *` / `Disallow: /`. The `v8/finance/chart` fallback is blocked.
- `www.isyatirim.com.tr/robots.txt`: `Disallow: /_layouts/`. The `HisseTekil`
  fallback (`/_layouts/15/Isyatirim.Website/Common/Data.aspx/HisseTekil`) is
  blocked.

Do not write a fallback fetcher against either. If the bulletin dies, manual
price entry (GLG-32) is the only fallback until a new spike finds a source
whose robots.txt allows it. The shapes above stay as a historical record of
what was evaluated in GLG-33.
