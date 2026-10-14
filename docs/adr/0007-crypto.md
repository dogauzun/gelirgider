# ADR 0007: Crypto — BtcTurk public API, market `CRYPTO`, TRY and USD pairs

Status: accepted (GLG-246, 2026-09-23); amended 2026-09-29 (GLG-515,
GLG-555, `tufe` and `gold` run every day too, see "Scheduler")

## Context

gelirgider tracks BIST, US and TEFAS holdings. Users also hold crypto (BTC,
ETH, ...) and want it next to the rest, priced in TRY or USD, with values that
update every day, weekends included, because crypto trades every day.

This ADR records the decisions of epic GLG-245 (Phase 9). It changes nothing
in the multi-currency model of ADR 0003; USD pairs reuse it as they are.

## Decision

### Source: BtcTurk public API, no API key

Both endpoints are public and need no key.

**Pair list:**

```
GET https://api.btcturk.com/api/v2/server/exchangeinfo
    -> data.symbols[]     numerator, denominator, status   (e.g. BTC, TRY, TRADING)
    -> data.currencies[]  symbol, name                     (e.g. BTC, Bitcoin)
```

379 pairs on 2026-09-23: 190 with denominator `TRY`, 189 with `USDT`, all
`TRADING`.

**Daily candles:**

```
GET https://graph-api.btcturk.com/v1/klines/history?symbol=BTCTRY&resolution=1D&from=<unix>&to=<unix>
    -> {"s":"ok","t":[...],"o":[...],"h":[...],"l":[...],"c":[...]}
```

`t` is 00:00 UTC of the candle's day. Candles cover calendar days, weekends
included. History goes back to 2017; one request returned 2202 days, so a
full backfill is one request per pair. The last candle is the current,
unfinished day.

**robots.txt**, checked 2026-09-23 on the real API hosts, per the rule
recorded after GLG-82 (check the host the fetcher actually calls):

```
https://api.btcturk.com/robots.txt        -> 404 (no robots.txt)
https://graph-api.btcturk.com/robots.txt  -> 404 (no robots.txt)
https://www.btcturk.com/robots.txt        -> User-agent: * / Allow: /
```

### Market `CRYPTO`, one instrument per pair

Each pair is one instrument in market `CRYPTO`. The symbol is `BASE-QUOTE`:

| Symbol    | Currency | Priced from BtcTurk pair |
| --------- | -------- | ------------------------ |
| `BTC-TRY` | `TRY`    | `BTCTRY`                 |
| `BTC-USD` | `USD`    | `BTCUSDT`                |

The instrument name is the base currency name from `exchangeinfo`
(`Bitcoin`). TRY and USD are the initial quote currencies. Another quote
currency later is one more seed CSV plus one mapping line.

Simplification, recorded with its ceiling: **1 USDT = 1 USD.** The USD pairs
are priced from BtcTurk's `USDT` pairs. USDT is not an ISO currency code and
`Intl.NumberFormat` throws on it, so the instrument currency is `USD`.
Ceiling: a USDT depeg would misprice every USD pair by the size of the
depeg. Upgrade path: a true USD source for those pairs.

### No FX work

USD pairs convert to the reporting currency through the existing TCMB
`fx_rates` path (ADR 0003), the same way US stocks do. Nothing new in FX.

### Prices: finished daily candles, tracked pairs only

- Only the close of **finished** daily candles is stored. The last candle
  (the current day) is ignored.
- A close is stored under the candle's **UTC date**, `source = 'btcturk'`.
  Simplification, recorded: the candle closes at 24:00 UTC, which is 03:00
  Europe/Istanbul the next day, so the stored date is the UTC day, not the
  Istanbul day the close happened in.
- Numbers are decoded with `json.Number` and parsed with
  `decimal.NewFromString`, never through `float64`.
- Only tracked pairs are fetched: pairs that appear in at least one
  transaction, the same rule as US and TEFAS.

### Scheduler: every day, sources opt in to weekends

The daily scheduler runs every day. BIST, TCMB and US sources stay
weekday-only; only sources marked for every day run on Saturday and Sunday.
The crypto source is the first such source.

> Amended 2026-09-29 by [ADR 0012](0012-benchmarks.md) and
> [ADR 0017](0017-gold.md) (GLG-515, GLG-555): crypto is no longer the only
> every-day source. `tufe` (TÜİK, 2026-09-28) runs every day because the
> TÜFE release follows a calendar date, not a weekday, and `gold` (BtcTurk
> PAXG-TRY, 2026-09-29) runs every day like crypto.

### Per-unit prices are not rounded to 2 decimals

37 of 190 TRY pairs trade below 1 TRY (BONK 0.00019, PEPE 0.00024). Per-unit
prices — last price and average cost — stop being rounded to 2 decimals in
the API and the UI, so a small-price coin does not show `0.00`.

### Not in this epic

- Quote currencies other than TRY and USD (EUR etc.).
- Crypto broker statement import.
- Wallet transfers.
- Fees paid in coin. Record the net quantity received.
- Staking and airdrops as a new transaction type. Use a buy at price 0 or a
  dividend.
- Intraday or live prices.

## Alternatives considered

| Decision          | Option                          | Verdict    | Why                                                                                           |
| ----------------- | ------------------------------- | ---------- | --------------------------------------------------------------------------------------------- |
| Crypto source     | BtcTurk public API              | **chosen** | No key, TRY and USDT pairs, full daily history in one request per pair, robots.txt allows it. |
| Crypto source     | CoinGecko                       | rejected   | `api.coingecko.com/robots.txt` disallows `/api/v3`.                                           |
| Crypto source     | Binance                         | rejected   | `api.binance.com` and `api.binance.me` are `Disallow: /`.                                     |
| Crypto source     | Binance TR                      | rejected   | `/api/` disallowed in robots.txt.                                                             |
| Crypto source     | Coinpaprika                     | rejected   | Free plan has no history.                                                                     |
| Crypto source     | CryptoCompare                   | rejected   | API key required.                                                                             |
| Crypto source     | Kraken                          | rejected   | Allowed, but USD only and capped at 720 daily candles.                                        |
| Crypto source     | Coinbase                        | rejected   | robots.txt returns 401, cannot verify.                                                        |
| USD pair currency | `USD`, priced from `USDT` pairs | **chosen** | Reuses the TCMB `fx_rates` path of ADR 0003; 1 USDT = 1 USD recorded above.                   |
| USD pair currency | `USDT` as its own currency      | rejected   | Not an ISO code; `Intl.NumberFormat` throws on it.                                            |

## Known risks

- BtcTurk can change its endpoints or response shape at any time, the same
  kind of risk ADR 0001 accepted for the BIST bulletin. Provider quirks stay
  inside `internal/btcturk` (`docs/PRINCIPLES.md` #9).
- A USDT depeg misprices USD pairs. Accepted and recorded above with its
  upgrade path.
- A close is stored under its UTC date although it happens at 03:00
  Istanbul the next day. Accepted and recorded above.

## Consequences

- GLG-247 imports BtcTurk TRY and USDT pairs as market `CRYPTO` instruments
  (seed CSV and importer in `tools/`).
- GLG-248 adds `internal/btcturk`, the daily close fetcher for tracked pairs.
- GLG-249 makes the scheduler run every day and lets a source opt in to
  weekends.
- GLG-250 shows per-unit prices at full precision in the API and the UI.
- GLG-251 adds the Kripto/Crypto market label in instrument search and the
  login tagline mention.
- GLG-252 wires crypto prices into the daily job, the `-fetch-crypto` flag
  and backfill.
- GLG-253 fetches crypto prices on demand after a transaction is saved.

> Superseded 2026-10-06 (GLG-1093): the symbol importers in `tools/` are one
> binary, `backend/cmd/symbols` (#1045), so the crypto lists are refreshed
> with `go run ./cmd/symbols -market crypto` from `backend/`; the
> `-fetch-crypto` flag is now `-fetch crypto` with `-date` (#1050); see
> `deploy/RUNBOOK.md` §5.
