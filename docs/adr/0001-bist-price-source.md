# ADR 0001: BIST end-of-day price source

Status: accepted (2026-09-13).

## Context

Phase 3 needs a daily closing price for every BIST instrument a user can hold.
The source has to be free, keyless, usable without a login, and stable enough
to run unattended. Its robots.txt has to allow the requests.

## Decision

Daily BIST closes come from the **Borsa Istanbul daily equity bulletin**: one
public zip per trading day, `https://www.borsaistanbul.com/data/thb/{YYYY}/{MM}/thb{YYYY}{MM}{DD}1.zip`,
holding one semicolon-separated CSV with every listed instrument. The host's
robots.txt allows it. It is official data, needs no key, and is one request
per day for all symbols. Bulletins before 2015-11-30 are an older `.xls` format
the fetcher does not read, so history starts there.

What the fetcher takes from the bulletin:

- The trade date, the series code, the bulletin name and the closing price,
  matched by header name, not column position, so a layout change fails loudly
  instead of storing wrong data.
- Rows in the equity, ETF and Darphane gold certificate groups, and only when
  the series code ends in the suffix of its group (`.E`, `.F`, `.S1`). That
  drops the default-settlement series such as `OYAKC.TE`. The suffix is
  stripped to get `instruments.symbol`.
- Closes are raw, not adjusted for splits or bonus issues. The holdings engine
  scales quantity on the ex-date instead (ADR 0008). The bulletin also flags
  capital-event days in a column the fetcher does not read.
- A row with an empty or zero close (a suspended instrument) is not stored as
  a price. The symbol list seeding still reads it, because a suspended stock
  can be held.
- A kept symbol with no `instruments` row is inserted as a BIST/TRY instrument
  named from the bulletin, so a company listed after the seed list was made is
  searchable and priceable the day it first trades. A backfill over old
  bulletins also adds companies delisted since, which is wanted: a user can
  hold one historically.

Fetched rows are stored in `prices` with `source = 'bist_bulletin'`.

A `404` means a non-trading day or a bulletin not yet published. The fetcher
reports that as "nothing", not as an error.

Manual price entry stays the permanent fallback. It writes the caller's own
`manual_prices` row, never `prices`, and a manual close wins over the fetched
one for the same instrument and date.

### When it runs

The bulletin appears in the evening, after the 18:00 close. The scheduler runs
the `bist` source once a day, 19:00 Europe/Istanbul by default (the time is a
per-source setting, ADR 0010), and retries hourly on an empty result until
23:00. A backend started late, or after downtime, catches up: it walks back at
most seven calendar days from today until a bulletin yields rows or the date
is already stored, so weekends and holidays are skipped without a calendar.

A backfill walks weekdays from the start date forward, one request per day with
a short pause, skips dates already stored, and stops on the first failure so a
rerun resumes. A date with no bulletin is remembered as a holiday only once a
later date in the same walk returned data; empty dates at the end of the range
may just be unpublished and stay unmarked. A BIST transaction saved on
the day triggers the same backfill for its instrument in the background (see
ADR 0003).

### Why not the others

- Yahoo `v8/finance/chart`: missed a normal trading day on every symbol
  checked, serves split-adjusted floats, needs one request per symbol, and its
  robots.txt is `Disallow: /`.
- İş Yatırım `HisseTekil`: an undocumented broker endpoint with closes adjusted
  for capital increases, and `/_layouts/` is disallowed in its robots.txt.
- Twelve Data and Finnhub cover BIST only on paid plans.

Neither Yahoo nor İş Yatırım may be used as a fallback. If the bulletin dies,
manual entry is the only fallback until a new spike finds a source whose
robots.txt allows it.

## Risks

- The URL pattern and CSV layout are not a documented API. Header-name matching
  and a loud failure are the mitigation.
- The publish time is observed, not promised.
- The zip is about 2 MB and covers instruments we do not track; the fetcher
  streams it and keeps only the tracked groups, which is the only thing keeping
  junk out of `instruments`.
- The rate limit is unknown, so backfill keeps a pause between requests.
