# ADR 0026: Instrument classification

Status: accepted (2026-10-18).

## Context

The allocation card groups holdings by what they are, not only by market. An
instrument had only a market, and BIST mixes classes: it lists shares,
exchange-traded funds and a gold certificate.

## Decision

### v1 shows the allocation by asset class

The classes are `equity`, `fund`, `commodity`, `crypto`, `deposit` and `cash`.
The sector view and a country view are out of v1. Sector stays open until the
BIST sector source has been investigated, and then adds its own column.
Country is not planned.

### The market gives the class until an instrument carries one

`instruments.asset_class` is a nullable text column. NULL means no class is
stored, and the class follows from the market:

| Market    | Class       |
| --------- | ----------- |
| `BIST`    | `equity`    |
| `US`      | `equity`    |
| `TEFAS`   | `fund`      |
| `GOLD`    | `commodity` |
| `CRYPTO`  | `crypto`    |
| `DEPOSIT` | `deposit`   |
| `CASH`    | `cash`      |

A stored value is one of `equity`, `fund` or `commodity`. `crypto`, `deposit`
and `cash` are never stored: their market already decides them. A stored value
wins over the market.

### The BIST bulletin's instrument group is the stored class for BIST

The BIST bulletin tags each row with an instrument group: `EQT` for a share,
`ETF` for an exchange-traded fund, `EMS` for the gold certificate. The symbol
seed stores `equity` for `EQT`, `fund` for `ETF` and `commodity` for `EMS`. A
BIST instrument without a group keeps NULL and falls back to the market map,
so it counts as `equity`.

### Sector is a fixed key list over a raw provider string

`instruments.sector` holds the provider's raw string, never mapped when
stored: NULL means not fetched yet, an empty string means fetched and the
provider has none. `SectorKey(market, raw)` maps it in code to one of 11
GICS-like keys (`energy`, `materials`, `industrials`, `consumer_discretionary`,
`consumer_staples`, `health_care`, `financials`, `information_technology`,
`communication_services`, `utilities`, `real_estate`) or `unknown`, with one
lookup map per market, compared case-insensitively with Turkish diacritics
folded. A new mapping needs no data migration. A position always carries a
`sector`, `unknown` when there is no data.

`instruments.sector_override` holds a sector key, written only by the admin
route, and wins over the mapped key.

The sources: Finnhub's `finnhubIndustry` gives US sectors. A TEFAS fund's type
is its sector: the fund list's `fonTurAciklama` is stored raw in the seed's
`sector` column. Its nine types map to six `fund_*` keys in `tefasFundTypes`:
`Para Piyasası Şemsiye Fonu` to `fund_money_market`, `Hisse Senedi Şemsiye
Fonu` to `fund_equity`, `Borçlanma Araçları Şemsiye Fonu` to `fund_debt`,
`Kıymetli Madenler Şemsiye Fonu` to `fund_precious_metals`, `Değişken Şemsiye
Fonu` and `Karma Şemsiye Fonu` to `fund_mixed`, `Serbest Şemsiye Fonu`,
`Katılım Şemsiye Fonu` and `Fon Sepeti Şemsiye Fonu` to `fund_other`. The
`fund_*` keys are not in the admin override list. For BIST, the
İş Yatırım "Temel Değerler ve Oranlar" page, already fetched for capital
increases (ADR 0008), lists one Turkish sector per equity for all of them. KAP
is rejected: it sits behind a bot wall.

The daily BIST capital event run reads the page's summary table
(`summaryBasicData`), finds the "Kod" and "Sektör" columns by header text and
stores each equity's raw sector, only when it differs from the stored one. An
instrument with no row (an ETF, the gold certificate) is left untouched, a page
symbol with no instrument is skipped, and `sector_override` is never written.
The page's closed list of 53 Turkish names maps onto the 11 keys in
`isYatirimSectors`; "Diğer" is left out and answers `unknown`. Holdingler and
Pazarlama map to `industrials`, Perakande - Ticaret to `consumer_staples`.
