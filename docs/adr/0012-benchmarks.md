# ADR 0012: Benchmarks

Status: accepted (GLG-487, 2026-09-27); amended 2026-09-28 (GLG-493,
TÜFE source); amended 2026-09-28 (GLG-492, BIST 100 source); amended
2026-09-28 (GLG-534, gold is now covered by [ADR 0017](0017-gold.md), a
fifth benchmark `GOLD` in TRY); superseded in part by
[ADR 0017](0017-gold.md) (GLG-534, 2026-09-28): gold is no longer out of
scope, US CPI and foreign indices still are; amended 2026-09-29 (GLG-641, no benchmark
return without a positive base); amended 2026-10-01 (GLG-694, Phase 22 adds
an instrument as a benchmark, `instrument:<id>`; see "Not in this epic");
amended 2026-10-05 (GLG-952, a daily `benchmark_twr` on every history point)

## Context

The Özet chart shows the portfolio's value and its return for the selected
range as simple, time-weighted or money-weighted
([ADR 0011](0011-portfolio-returns.md)). There is nothing to compare that
return against. The question the user wants answered is "did I beat the
dollar / the index / inflation with the money I actually put in".

This ADR records the decisions of epic GLG-486 (Phase 13), taken with the
user on 2026-09-27.

## Decision

### Four benchmarks, one at a time

The user can compare the portfolio against one benchmark: USD/TRY, EUR/TRY,
BIST 100 (XU100) or Turkish CPI (TÜFE). The benchmark is picked with a
selector on the chart header next to the return method: none / USD / EUR /
BIST 100 / TÜFE. The choice is remembered in `localStorage` under the key
`benchmark`, like `returnMethod`. The home portfolio cards and the KPI card
are unchanged.

> Amended 2026-09-28 by [ADR 0017](0017-gold.md) (GLG-534): GOLD, the gram
> gold price in TRY, is a fifth fixed benchmark.

> Amended 2026-10-01 (GLG-701, GLG-702): besides the five fixed benchmarks,
> any instrument with a transaction in the scope can be the benchmark as
> `instrument:<id>`, and the selector lists the portfolio's positions under
> the fixed ones (see "Not in this epic").

The USD option is hidden when the reporting currency is USD, since it would
be a flat line. EUR likewise.

### Shadow portfolio

The benchmark is a second, hypothetical portfolio that received the same
external flows on the same days as the real one, all invested in the
benchmark. Its value per day is drawn as a dashed line on the chart.

> Correction 2026-09-27 (GLG-491): the benchmark line has always been
> dotted, so it does not read as the dashed cost line.

Inputs for each day `i` of the range, `0` to `n−1`:

- `V_0`, the portfolio's value on the first day, the same `V_0` the
  returns use.
- `F_i`, the external flow of day `i`: the same `dayExternal` the Phase 12
  returns use, with `F_0 = 0`.
- `P_i`, the benchmark price on day `i` in the reporting currency.

The shadow starts with the portfolio's value and buys units with every flow
at that day's price:

```
u_0 = V_0 / P_0
u_i = u_{i−1} + F_i / P_i
S_i = u_i · P_i
```

Flows are end-of-day, as in ADR 0011: a flow on day `i` buys units at
`P_i` and earns nothing on day `i`.

The shadow's return is `periodReturns(S, F)`, the same function and the
same method (simple / TWR / MWR) the user already picked for the portfolio.
So the benchmark's TWR equals the benchmark's own price change over the
range, `P_last / P_0 − 1`, and its simple and MWR answer "what would the
same deposits have made in the benchmark".

When the portfolio has no positive base (ADR 0011: `V_0 <= 0`, or cash
below zero on any day of the range), the benchmark's returns are `null`
too, since `S_0` is that same `V_0` and the portfolio's negative cash is
passed to the shadow's `periodReturns` (GLG-641). The excess is then not
shown either. Otherwise the benchmark is unaffected.

> Amended 2026-10-05 (GLG-952): each history point also carries
> `benchmark_twr`, the shadow's daily time-weighted return by the rule of
> ADR 0011's per-point `twr`, ending on `benchmark.returns.twr`. It is
> `null` on every point without a benchmark, when the shadow has no value
> for the range, or when the benchmark's own `twr` is `null`.

The chart header also shows the excess return

```
excess = (1 + p) / (1 + b) − 1
```

where `p` is the portfolio's return and `b` the benchmark's, both by the
selected method. Against TÜFE this is the real, inflation-adjusted return.

### Worked examples

The test table of the shadow ticket (GLG-489).

| V_0  | flows        | prices         | S                  | twr       | simple    |
| ---- | ------------ | -------------- | ------------------ | --------- | --------- |
| 1000 | [0, 1000, 0] | [10, 8, 7.6]   | [1000, 1800, 1710] | -0.240000 | -0.145000 |
| 1000 | [0, −500, 0] | [1, 1.1, 1.21] | [1000, 600, 660]   | 0.210000  | 0.320000  |

- Row 1: `u = [100, 225, 225]`. `S` is the same series as ADR 0011 row 2,
  so its returns are that row's, and the TWR is the price change
  `7.6 / 10 − 1 = −0.24`.
- Row 2: `u_1 = 1000 − 500 / 1.1 = 545.45…`, `S_1 = 600`,
  `S_2 = 545.45… × 1.21 = 660`. TWR is `1.21 − 1`; simple is
  `(660 − 1000 + 500) / (1000 − 500) = 0.32`.

### Benchmark price in the reporting currency

Every benchmark is a currency plus a level per day:

| benchmark | currency | level           |
| --------- | -------- | --------------- |
| USD       | `USD`    | 1               |
| EUR       | `EUR`    | 1               |
| XU100     | `TRY`    | the index close |
| TUFE      | `TRY`    | the CPI level   |

`P_i` is the level converted into the reporting currency with the sweep's
FX rates for day `i`: `level · rate(currency) / rate(reporting)`, the same
conversion as `holdings.Convert` ([ADR 0003](0003-multi-currency-and-market-data-sources.md)).

### TÜFE daily level

TÜFE is published monthly. Month `M`'s published index is treated as the
level at the end of `M`, and the level is spread geometrically over the
month's days. Day `d` of month `M`, which has `D` days:

```
CPI_{M−1} · (CPI_M / CPI_{M−1})^(d/D)
```

So the last day of `M` is `CPI_M`. Days of the first stored month have no
level, since there is no `CPI_{M−1}`.

Past the last published month `L`, the line continues at the last known
monthly rate: each later month uses the ratio `CPI_L / CPI_{L−1}` again.
Those days are an estimate. The history response carries `estimated_from`,
the first day of month `L+1`, and the tooltip marks estimated points.

### Missing data

A benchmark with no level on or before the range's first point is not shown
for that range: all its returns are `null` and there is no
`benchmark_value`. Later gaps carry the last level forward, like FX.

### Shadow values in float64

Shadow values and CPI levels are hypothetical and derived from ratios, so
they are computed in float64 like the returns (ADR 0011) and rounded to 2
decimals for the response. Real money, the portfolio's own values and flows,
stays decimal (PRINCIPLES #3).

### Storage

USD and EUR come from `fx_rates` (TCMB, ADR 0003), which is already fetched.
No new source.

BIST 100 and TÜFE are stored in one global market-data table
`benchmark_values(code, date, value, source)`. It has no `user_id`, like
`prices` and `fx_rates`: it is market data, not user data.

### Sources

The XU100 and TÜFE sources are decided by the spikes in epic GLG-486
(GLG-492 for XU100, GLG-493 for TÜFE). Each spike appends its chosen source
to this section.

> Resolved 2026-09-28 (GLG-492, GLG-493): both sources are appended below,
> under "BIST 100 source" and "TÜFE source", each with its own robots.txt
> check of the host it picked.

robots.txt, checked 2026-09-27:

- `www.borsaistanbul.com`: `Allow: /`.
- `datastore.borsaistanbul.com`: no Disallow.
- `veriportali.tuik.gov.tr`: explicitly allows Claude bots.
- `evds3.tcmb.gov.tr`: serves no robots.txt.

The spikes re-check robots.txt on the real API host they pick.

#### BIST 100 source

GLG-492 picks TCMB EVDS series `TP.MK.F.BILESIK`, "(FİYAT) BİST 100 Endeks
(XU100), Kapanış Fiyatlarına Göre (Ocak 1986=0,01)", for both the daily
close and the backfill. EVDS names Borsa İstanbul as its data source. It
needs the API key of a free EVDS account, which the user chose to register
on 2026-09-28. GLG-517 adds the key to the provider settings, GLG-518 builds
the fetcher.

> Resolved 2026-09-29 (GLG-517, GLG-518): the key is the
> `provider.evds.api_key` setting, and the `xu100` scheduler source fetches
> the series with `backend/internal/evds`.

```
GET https://evds3.tcmb.gov.tr/igmevdsms-dis/series=TP.MK.F.BILESIK&startDate=21-09-2026&endDate=25-09-2026&type=json
key: <provider.evds.api_key>
```

- Key: sent as the `key` HTTP header, never in the URL. Without the header
  the answer is 403 `Required request header 'key' is not present`, with a
  wrong key 401 `Invalid API Key`. The project User-Agent works.
- Format: JSON `{"totalCount":N,"items":[...]}`. Each item has `Tarih`
  (`DD-MM-YYYY`), the value under the series code with `_` for `.`, and
  `UNIXTIME`, which is midnight Europe/Istanbul of that date, not a publish
  time. The value is a JSON string with 8 decimals. The index has 2, so the
  fetcher parses it as a decimal and stores it without the trailing zeros.
- Sample item:
  `{"Tarih":"25-09-2026","TP_MK_F_BILESIK":"12899.35000000","UNIXTIME":{"$numberLong":"1790283600"}}`
- Calendar: business days only. Weekends are absent. A weekday holiday is
  present with `null`: 2026-04-23, 2026-05-01, 2026-05-19 and 2026-05-27 to
  2026-05-29 in the range checked. The half day 2026-05-26 has a value. A
  range with no data answers `{"totalCount":0,"items":[]}`.
- History: the first value is 1986-02-07 (0.0115). It is weekly in 1986,
  sparse in 1987 and daily from 1988. The whole series is on today's scale: 2020-07-24 is
  1192.02 and 2020-07-27 is 1195.67. There is no step at the 2020-07-27
  removal of two zeros, so the fetcher divides nothing.
- Limit: at most 1000 observations per call, says the EVDS web service
  guide. A longer range is cut silently to the 1000 observations before
  `endDate`, still with 200: 01-01-2020 to 25-09-2026 returned
  `totalCount` 1000, starting at 28-11-2022. Holidays count. So the
  backfill asks for one calendar year per call, at most 262 observations,
  and a response with 1000 items is an error. The guide states no request
  rate limit and asks for one pull a day. The `X-Ratelimit-*` headers are
  all 0.
- Publication: on Monday 2026-09-28 at 13:14 Europe/Istanbul the newest
  item was Friday 2026-09-25, and Monday was absent. The response has no
  Last-Modified, and EVDS lists the data group's update only as a daily
  job. The same-evening time was not observed. So every run re-reads the
  last 14 days, and a close one run misses lands with the next, like TEFAS.
  The daily run is at 20:00, after Borsa İstanbul's own index file at about
  18:20.

Verification, 2026-09-28:

- 2026-09-25: EVDS 12899.35. Borsa İstanbul's `/datum/PayEndeksleri.zip`
  has `XU100;BIST 100;BIST 100;TRY;25/09/2026;12899.35`, and the
  borsa.doviz.com XU100 page shows "Önceki Kapanış 12.899,35".
- 2026-09-23: EVDS 13251.85. haber.doviz.com's market-open report of
  2026-09-24 says the index closed the day before at 13.251,85, up 0.40%.
  EVDS gives 13251.85 / 13198.84 − 1 = 0.40%.
- 2025-09-26 and 2021-09-28, as a ratio: the doviz.com past-investment
  calculator for XU100 says 100,000 TL invested on 2025-09-26 is worth
  112,494.71 TL now, and on 2021-09-28 906,545.89 TL. With the EVDS closes
  11151.20 and 1383.77 both give the same current level, 12544.51, to 8
  significant digits.

robots.txt, checked 2026-09-28: `evds3.tcmb.gov.tr` answers `/robots.txt`
with its SPA HTML page, so there are no rules. The fetcher must call this
host and path. The references: `borsa.doviz.com` and `www.doviz.com` allow
`/` (`www` disallows `/api/` and `/user-api/`), and `haber.doviz.com` has
no robots.txt (404).

Rejected:

- Borsa İstanbul `/datum/PayEndeksleri.zip`: keyless and official, with the
  XU100 close at about 18:20. It holds the latest day only, so it cannot
  backfill.
- Borsa İstanbul datastore, "Gün Sonu Endeks ve İşlem Hacmi Bilgileri":
  free monthly files, but a download needs an account with a captcha login,
  and the newest file is about a month behind.
- Borsa İstanbul `/data/thb/`: the equity bulletin has no index rows.
- Borsa İstanbul `/data/teg/`: the BIST 100 intraday files end at
  2015-11-30.
- Borsa İstanbul `/datum/EVM_ENDEKSVOLATILITE.D.zip`: volatility values
  only, no closes.
- `bigpara.hurriyet.com.tr`: robots.txt disallows `anthropic-ai`,
  `ClaudeBot` and `Claude-Web`.
- `stooq.com`: robots.txt disallows everything for all but Google and Bing.
- `tr.investing.com`: robots.txt answers 403, a bot wall.
- `borsa.doviz.com`: its history table comes from `api.doviz.com` with an
  obfuscated bearer token, a key in disguise.
- `finans.mynet.com`: no history table, and its data paths are under the
  disallowed `/api/`.
- Yahoo and İş Yatırım `/_layouts/`: blocked by robots.txt (ADR 0001,
  amendment of 2026-09-23).

#### TÜFE source

GLG-493 picks the TÜİK data portal's file download for dataflow
`DF_TUFE_SDMX_TT10` ("Index numbers and rate of changes in the consumer
price index"). It needs no key. GLG-515 builds the fetcher.

> Resolved 2026-09-28 (GLG-515): the `tufe` scheduler source fetches the
> file with `backend/internal/tuik`.

```
GET https://veriportali.tuik.gov.tr/api/en/dataflows/DF_TUFE_SDMX_TT10+V1.0/file/csv
User-Agent: gelirgider/1.0 (+https://github.com/dogauzun/gelirgider)
```

- Format: CSV, `;` separated, every field quoted, UTF-8 with a BOM. The
  `en` file uses `.` decimals, the `tr` file `,`. One header row, then 15
  columns. The header text embeds sample values that change every month,
  so columns are read by position.
- Each month has five rows: `Index` and four rates of change. The fetcher
  keeps the rows whose 4th column is `Index`. The 8th column is the month
  (`YYYY-MM`), the 9th the level, the 5th the base year.
- Sample row:
  `"Türkiye";"Monthly";"CPI";"Index";"2025";"CPI";"Consumer Price Index";"2026-08";"134.75";"";"";"";"";"";""`
- History: 2005-01 to the latest month, 260 months on 2026-09-28. The file
  has no date filter: every download is the full history, about 175 KB.
- Auth and limits: no key, no documented rate limit,
  `Cache-Control: public,max-age=300`. One request per run.
- User-Agent: curl's default User-Agent gets 403 "Access denied" on this
  URL. The project's own User-Agent, the one `tefas` sends (ADR 0004),
  gets 200. No browser headers are sent.
- Publication: TÜİK releases the CPI on the 3rd of the month at 10:00
  Europe/Istanbul. August 2026 has `updatedAt` 2026-09-03T10:00. The portal
  says files update within 30 minutes of a release.
- The dataflow id carries its version (`+V1.0`). A new version changes the
  URL, and the fetcher fails with the 404 instead of guessing.

TÜİK rebased the CPI from 2003=100 to 2025=100 with the January 2026
index. This file already carries the whole history on 2025=100: the levels
up to 2025-12 are the old series rescaled (6 decimals, 2025 average exactly
100.000000), the levels from 2026-01 are the new index (2 decimals). The
series neither stops nor jumps: 2025-12 is 110.386963 and 2026-01 is
115.73, a ratio of 1.048403, which is TÜİK's January 2026 change of 4.84%.
So the fetcher stores the levels as published and does no chaining. It
upserts every `Index` row on every run. If TÜİK rebases again and
republishes the back series, one run rewrites the whole history on the new
base. A file whose `Index` rows carry more than one base year fails without
writing anything.

Verification, 2026-09-28. The ratio of two consecutive levels against the
monthly change TÜİK announced:

- January 2025: 88.578291 / 84.334173 − 1 = 5.0325%. TÜİK announced 5.03%
  in its January 2025 CPI release of 2025-02-03.
- January 2026, across the rebase: 115.73 / 110.386963 − 1 = 4.8403%. The
  file's published monthly change is 4.84%.
- August 2026: 134.75 / 132.31 − 1 = 1.8442%. Published: 1.84%.
- 258 of the file's 259 consecutive pairs round to its published monthly
  change. The exception is 2012-04, 1.514999% against 1.52%, a rounding
  artefact of the 6-decimal back series.

robots.txt on `veriportali.tuik.gov.tr`, checked 2026-09-28: `User-agent: *`
with `Allow: /`, and a second group that names `anthropic-ai`, `ClaudeBot`,
`Claude-Web` and other bots, also with `Allow: /`. The fetcher must call
this host and path.

Rejected:

- TÜİK SDMX web service (`nsiws.tuik.gov.tr`): every call needs a bearer
  token from an SMS-verified TÜİK account. It answers 401 without one, also
  at `/robots.txt`.
- MEDAS (`biruni.tuik.gov.tr/medas`): a stateful web UI with no API, which
  the portal says stops being updated in 2027. Its `/robots.txt` answers 503.
- TCMB EVDS (`evds3.tcmb.gov.tr`), read from its public web service guide
  only: it needs a registered account's API key in a `key` header (403
  without one) and returns at most 1000 observations per call. There is no
  key, and a keyless source works. `TP.FG.J0` is the 2003=100 series; how
  EVDS continues it after the rebase was not checked. evds3 serves its SPA
  page at `/robots.txt`.
- TÜİK press releases and PDFs: rejected up front.

> Amended 2026-09-29 (GLG-517): "There is no key" in the TCMB EVDS item
> above held when GLG-493 checked it; the user registered an EVDS key for
> the BIST 100 source the same day, and GLG-517 stores it, but TÜFE stays on
> the keyless TÜİK file.

## Recorded simplifications

- A monthly CPI value is treated as the end-of-month level and spread
  geometrically over the month, although it is really a monthly average.
- Past the last published month, TÜFE is extrapolated at the last monthly
  rate.
- A benchmark missing at the range's first point hides it for the whole
  range, even when levels exist for later days.
- Shadow units can go negative when withdrawals exceed the shadow value.
  The returns then follow the ADR 0011 `null` rules.

### Not in this epic

- Several benchmarks on the chart at once.
- Gold, US CPI or other foreign indices.
- Benchmarks on the home cards or per position.
- Annualized returns.
- Custom user benchmarks. Phase 22 (GLG-693) narrows this:
  `?benchmark=instrument:<id>` compares against any instrument with a
  transaction in the scope (GLG-701), and the Özet selector lists the
  portfolio's positions under the fixed benchmarks (GLG-702). The level is
  the instrument's price from the sweep's own price series, in the
  instrument's currency, converted like any other benchmark above. A split
  inside the range shows as a drop; prices are not back-adjusted (ADR 0008).

> Superseded 2026-09-28 by [ADR 0017](0017-gold.md) (GLG-534): gold is the
> fifth fixed benchmark, GOLD; US CPI and foreign indices are still out.

> Amended 2026-09-29 by [ADR 0011](0011-portfolio-returns.md) (GLG-607): the
> chart header annualizes the portfolio's and the benchmark's period returns
> in the frontend for ranges a year or more long; the API still returns
> period returns only.

## Consequences

- GLG-487 writes this ADR and the Phase 13 roadmap section.
- GLG-488 adds the `benchmark_values` table and `benchmark.Repo`.
- GLG-489 adds the pure shadow-portfolio function.
- GLG-490 adds the pure TÜFE daily levels from monthly CPI.
- GLG-491 adds the optional dashed benchmark line to `LineChart`.
- GLG-492 and GLG-493 pick the XU100 and TÜFE sources, append them to
  "Sources" above, and create the fetcher tickets.
- GLG-494 adds `?benchmark=` for USD and EUR to the history API and
  `benchmark` to the response.
- GLG-495 adds XU100 and TÜFE from `benchmark_values` to the history API.
- GLG-496 adds the benchmark selector, the benchmark's return and the excess
  to the chart header.
- USD and EUR work end to end once GLG-489, GLG-491, GLG-494 and GLG-496
  merge. BIST 100 and TÜFE show "no data" until their fetchers run.

> Correction 2026-09-27 (GLG-491): the benchmark line GLG-491 added is
> dotted, not dashed.

> Resolved 2026-09-29 (GLG-515, GLG-518): both fetchers have shipped, the
> `tufe` source on 2026-09-28 and the `xu100` source on 2026-09-29, so BIST
> 100 and TÜFE have data once their sources have run.
