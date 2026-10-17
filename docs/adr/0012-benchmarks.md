# ADR 0012: Benchmarks

Status: accepted (2026-09-27).

## Context

The Özet chart shows the portfolio's return as simple, time-weighted or
money-weighted ([ADR 0011](0011-portfolio-returns.md)), with nothing to compare
it against. The user wants to know whether it beat the dollar, the index or
inflation, given the money actually put in.

## Decision

### Benchmarks, one at a time

The chart header has a selector next to the return method: none, USD/TRY,
EUR/TRY, BIST 100 (XU100), Turkish CPI (TÜFE), gram gold (GOLD,
[ADR 0017](0017-gold.md)), and the scope's open positions, each as
`instrument:<id>` (the API accepts any instrument with a transaction in the
scope). One shows at a time and the choice is remembered in `localStorage`. USD
and EUR are hidden when that is the reporting currency, since the line would
be flat.

### Shadow portfolio

The benchmark is a hypothetical portfolio that received the same external
flows on the same days as the real one, all invested in the benchmark, drawn
as a dotted line. For day `i` of the range, `V_0` is the portfolio's first-day
value (the one the returns use), `F_i` the external flow of day `i` (`F_0 = 0`)
and `P_i` the benchmark price in the reporting currency:

```
u_0 = V_0 / P_0    u_i = u_{i-1} + F_i / P_i    S_i = u_i * P_i
```

Flows are end-of-day, as in ADR 0011: a flow on day `i` buys units at `P_i`
and earns nothing that day. The shadow's returns come from the same
`periodReturns` function and the method the user picked, so the benchmark's
TWR is its own price change over the range, and its simple and MWR answer
"what would the same deposits have made in the benchmark". When the portfolio has no positive base (`V_0 <= 0`,
or cash below zero on any day) the benchmark's returns and the excess are
`null` too. Each history point also carries `benchmark_twr`, the shadow's
daily TWR by the rule of ADR 0011's per-point `twr`.

The header shows the excess return `(1 + p) / (1 + b) - 1` of the portfolio's
return `p` over the benchmark's `b`. Against TÜFE it is the real return.

### Price in the reporting currency, alignment, missing data

USD and EUR have level 1 in their own currency, XU100, TUFE and GOLD are `TRY`
levels (index close, CPI level, gram price). `P_i` is the level converted with the sweep's FX rates for day
`i`, the same conversion as `holdings.Convert`
([ADR 0003](0003-multi-currency-and-market-data-sources.md)). An instrument
benchmark uses its price from the sweep's own series in its currency. Prices
are not back-adjusted ([ADR 0008](0008-splits.md)), so the level is the price
times a share count that starts at 1 and grows by the ratio on each split
ex-date inside the range: a split is not a drop.

A benchmark with no level on or before the range's first point is not shown
for that range: its returns are `null` and there is no `benchmark_value`, even
when later levels exist. Later gaps carry the last level forward, like FX.
Shadow values are float64 like the returns; the portfolio's own money stays
decimal (PRINCIPLES #3).

### TÜFE daily level

TÜFE is published monthly. Month `M`'s index is treated as the level at the end
of `M` and spread geometrically over its `D` days: day `d` is
`CPI_{M-1} * (CPI_M / CPI_{M-1})^(d/D)`. Past the last published month the line
continues at the last monthly rate, and the history response carries
`estimated_from`, the first estimated day, which the tooltip marks.

### Storage and sources

USD and EUR come from `fx_rates` (TCMB, ADR 0003). XU100, TUFE and GOLD live in
the global `benchmark_values(code, date, value, source)`.

**XU100: TCMB EVDS** series `TP.MK.F.BILESIK` (Borsa İstanbul's close). It
needs the API key of a free EVDS account, which the user registered; the key
is the `provider.evds.api_key` setting, sent as the `key` header, never in the
URL. No keyless source gives history: Borsa İstanbul's index file holds the
latest day only, and bigpara, stooq and investing.com block bots. EVDS serves
no robots.txt rules. A call returns at most 1000 observations and cuts longer
ranges silently, so the backfill asks one calendar year at a time and treats
a 1000-item answer as an error. Weekday holidays come back `null` and are
skipped. Every run re-reads the last 14 days because the newest day can lag.

**TÜFE: the TÜİK data portal's file download** for dataflow
`DF_TUFE_SDMX_TT10`, CSV, no key. TÜİK's robots.txt allows everything, Claude
bots included. The file carries the whole history on one base year, so the
fetcher stores levels as published and does no chaining; a file with more
than one base year fails without writing. The request must send the project's
own User-Agent, since curl's default gets 403. TÜİK releases the CPI on the
3rd of the month at 10:00 Europe/Istanbul. The dataflow id carries its
version, so a new version fails with a 404 instead of guessing. The SDMX web
service needs an SMS-verified account, so it is rejected.

**GOLD:** the gram price of ADR 0017. Not offered: several benchmarks at once,
home-card or per-position benchmarks, US CPI, foreign indices and
user-defined series.
