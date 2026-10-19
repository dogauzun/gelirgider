# ADR 0007: Crypto — BtcTurk public API, market `CRYPTO`, TRY and USD pairs

Status: accepted (2026-09-23).

## Context

gelirgider tracks BIST, US and TEFAS holdings. Users also hold crypto (BTC,
ETH, ...) and want it next to the rest, priced in TRY or USD, with values that
update every day, weekends included, because crypto trades every day. USD
pairs reuse the multi-currency model of ADR 0003 as it is.

## Decision

### Source: BtcTurk public API, no API key

Both endpoints are public and need no key:

- `GET https://api.btcturk.com/api/v2/server/exchangeinfo` lists the pairs
  (`numerator`, `denominator`, `status`) and the currency names.
- `GET https://graph-api.btcturk.com/v1/klines/history?symbol=BTCTRY&resolution=1D&from=<unix>&to=<unix>`
  returns daily candles. `t` is 00:00 UTC of the candle's day, candles cover
  calendar days, weekends included, and history goes back to 2017, so a full
  backfill is one request per pair. The last candle is the current,
  unfinished day.

robots.txt of the real API hosts allows both (checked 2026-09-23), per the
rule of checking the host the fetcher actually calls.

### Market `CRYPTO`, one instrument per pair

Each pair is one instrument in market `CRYPTO`. The symbol is `BASE-QUOTE`:

| Symbol    | Currency | Priced from BtcTurk pair |
| --------- | -------- | ------------------------ |
| `BTC-TRY` | `TRY`    | `BTCTRY`                 |
| `BTC-USD` | `USD`    | `BTCUSDT`                |

The instrument name is the base currency name from `exchangeinfo`. TRY and
USD are the only quote currencies. USD pairs convert to the reporting
currency through the TCMB `fx_rates` path of ADR 0003, like US stocks.

Simplification: **1 USDT = 1 USD.** USDT is not an ISO currency code and
`Intl.NumberFormat` throws on it, so the instrument currency is `USD`.
Ceiling: a USDT depeg misprices every USD pair by the size of the depeg.
Upgrade path: a true USD source for those pairs.

### Prices: finished daily candles, tracked pairs only

- Only the close of a **finished** daily candle is stored; the current day's
  candle is ignored.
- A close is stored under the candle's **UTC date**, `source = 'btcturk'`.
  The candle closes at 03:00 Europe/Istanbul the next day, so the stored date
  is the UTC day, not the Istanbul day the close happened in.
- Numbers are decoded with `json.Number` and parsed with
  `decimal.NewFromString`, never through `float64`.
- Only tracked pairs are fetched: pairs held in at least one transaction or
  watched by any user (ADR 0032), the same rule as US and TEFAS.
- Per-unit prices (last price, average cost) are not rounded to 2 decimals in
  the API or the UI, because many pairs trade below 1 TRY.

### Scheduler: every day

The daily scheduler runs every day. A source runs on Saturday and Sunday only
when it opts in with `EveryDay`; BIST, TCMB and US stay weekday-only. Crypto
opts in, and so do `tufe` (ADR 0012) and `gold` (ADR 0017).

### Out of scope

Quote currencies other than TRY and USD, crypto broker statement import,
wallet transfers, fees paid in coin (record the net quantity), staking and
airdrops as their own transaction type (use a buy at price 0 or a dividend),
intraday prices.

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
- A USDT depeg misprices USD pairs. Accepted, with the upgrade path above.
- A close is stored under its UTC date, not the Istanbul date. Accepted.
