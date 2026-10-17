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
