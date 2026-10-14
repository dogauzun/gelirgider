# ADR 0004: TEFAS fund list and daily NAV source

Status: accepted (2026-09-14).

## Context

Phase 5 needs a list of TEFAS funds to search and a daily NAV for each fund a
user holds. TEFAS's `.aspx` pages sit behind a JavaScript bot challenge, so
scraping them is out. Its own JSON API under `tefas.gov.tr/api/funds/` is
reachable with plain HTTP, needs no key and no login, and is the only source
used.

## Decision

TEFAS is the source of both the fund list and the daily NAV, and nothing else
is used. TEFAS is the official Takasbank and SPK platform for this data, so
KAP or a broker mirror would be a worse copy of the same thing.

### Fund list

`POST /api/funds/fonGetiriBazliBilgiGetir` returns every fund of one type in a
single request: code, name, category and an active flag. Only `fonTipi=YAT`
(regular mutual funds) is read. Pension and exchange-traded funds are a
different product. The request must be sent with its full payload: dropping
the period flags makes the endpoint answer `200` with a null result and no
error. The payload is in `backend/cmd/symbols/tefas.go`.

The list is fetched by `go run ./cmd/symbols -market tefas` from `backend/`,
which writes the committed seed `tefas_symbols.csv` of active funds, currency
`TRY`. The server does not call the list endpoint at run time. The seed only
fills a fresh database.

### Daily NAV

`POST /api/funds/fonFiyatBilgiGetir` takes a fund code and a lookback window
`periyod` in months, one of `1, 3, 6, 12, 36, 60`, and returns one row per
trading day. There is no all-funds-for-one-date call, so the fetcher makes one
request per **tracked** fund (one with a transaction), never the full catalog.

- A null result list is the API's error shape, returned with HTTP `200`. Any
  other `periyod`, or date parameters, produce it.
- A row with `fiyat` 0 is today's placeholder before publication, so it is
  dropped. Weekends are absent.
- `fiyat` is a JSON number with up to 6 decimals. It is decoded as
  `json.Number`, never `float64`, and stored as decimal text with
  `source = 'tefas'`.

NAV for day D is published around 13:30 Europe/Istanbul on D itself. The
scheduler's `tefas` source runs once a day at 14:00 by default, and the time
is a per-source setting (ADR 0010).

### Catch-up and backfill

Each daily run asks for the smallest `periyod` that reaches the requested date,
which re-reads the last month, so a missed day needs no special handling.
Saving a transaction fetches that fund's NAVs from the transaction date in one
request, and the startup backfill does the same. That fetch is skipped when
the stored prices already reach back to within 14 days of the start (longer
than the longest holiday stretch), and the window is clamped to 60 months. So
a newly tracked fund gets its NAVs within the request that saved it, and an
older transaction costs one extra request. This replaced the earlier
`-missing-only` cron run (ADR 0010).

### robots.txt

TEFAS's robots.txt disallows `/api/`, and both endpoints live there. The
maintainer accepted that for the shipped source, with these limits. The `tefas`
source is **off by default**: the server fetches nothing from TEFAS until an
admin turns it on, where the admin area says the endpoint is disallowed, so
the operator who enables it makes the call. While it is off, fund prices are
entered by hand, and a user's manual price wins over a fetched NAV for the
same fund and date. While it is on, the fetcher identifies itself honestly as
`gelirgider/1.0 (+https://github.com/dogauzun/gelirgider)`, fetches tracked
funds only, runs once a day, and keeps a pause between requests. TEFAS
publishes no rate limit, and a spot check of 8 rapid requests saw none.

### Fund discovery and renames

A fund's name changes while its code stays, so every fetch sets a tracked
fund's `instruments.name` to the `fonUnvan` of its newest NAV row when that
differs. The seed is not rewritten, and the next fetch corrects a stale name.

Funds outside the `YAT` list still return NAVs from `fonFiyatBilgiGetir`, so
such a fund enters in one of two ways: the statement import's discovery (ADR
0006), which asks TEFAS whether a code has NAVs, or the admin instrument form.
From then on the fetcher prices it like any tracked fund.

## Risks

- The endpoints are not a documented API and have already moved once. A
  future move is handled with a new spike, and manual entry stays the fallback.
- TEFAS publishes no rate limit. The tracked-funds-only rule keeps the volume
  low.
