# ADR 0004: TEFAS fund list and daily NAV source

Status: accepted (GLG-71, 2026-09-14); amended 2026-09-14 (GLG-109,
robots.txt, same-day publish time, the crawler as shipped); amended
2026-09-14 (GLG-103, manual entry writes `manual_prices`); amended
2026-09-15 (GLG-183, `-missing-only` run); amended 2026-09-23 (GLG-255,
14-day backfill rule); amended 2026-09-23 (GLG-299, the crawler moved into
the backend as `internal/tefas`; see "Update 2026-09-23" near the end);
superseded in part by [ADR 0010](0010-setup-and-admin.md) (GLG-299,
GLG-303, GLG-305, 2026-09-23): the fetcher is the backend `tefas` scheduler
source at its `source.tefas.at` time, and `tools/tefas` with its
`-missing-only` cron line is gone; amended 2026-10-01 (GLG-694, a tracked fund's name follows `fonUnvan`, and
funds outside the `fonTipi=YAT` list enter through import discovery or the
admin instrument form; see "Update 2026-10-01" at the end); amended
2026-10-07 (GLG-1115, the robots.txt decision is that the `tefas` source is
off by default and the operator who enables it decides); amended 2026-10-13
(pre-publication review, personal disclosure removed)

## Decision

Fund list and daily NAV both come from **`tefas.gov.tr`'s own JSON API**, but
not the endpoint the ticket named: `api/DB/BindHistoryInfo` and its siblings
are gone. TEFAS quietly moved to a new set of endpoints under `api/funds/`,
almost certainly alongside a front-end rewrite (the `.aspx` pages are now
served through a JS bot-challenge, see below). The two calls needed:

- **Fund list**: `POST /api/funds/fonGetiriBazliBilgiGetir` — one request
  returns every fund of a given type (code, name, category, active flag).
  1065 funds for `fonTipi=YAT` on 2026-09-14, all `tefasDurum: true`.
- **Daily NAV**: `POST /api/funds/fonFiyatBilgiGetir` — **per fund**, not
  per date. Takes a fund code and a months-back window (`periyod`, enum
  `{1,3,6,12,36,60}`), returns one row per trading day in that window. There
  is no BIST-bulletin-style "all funds, one date" call for prices — only the
  fund-list endpoint is all-funds-in-one-request; NAV is one request per
  tracked fund, same shape as the Yahoo fallback in ADR 0001.

Manual price entry (the same mechanism GLG-32 built for BIST) stays available
as the fallback. Since GLG-103 it writes the caller's own `manual_prices` row,
which wins over a fetched NAV for the same fund and date. A fetcher (GLG-87)
would write `prices` with `source = 'tefas'`.

GLG-183 added a frequent `-missing-only` cron line (every 15 minutes) that
only ever queries funds with no `prices` row yet, so a newly tracked fund
gets a NAV within minutes; the tracked-funds-once-a-day request volume
accepted below does not grow except by one extra request per newly tracked
fund. GLG-255 widened it to funds whose prices start more than 14 days after
their earliest transaction (within the 60-month window), one extra request
when an older transaction is added.

> Superseded 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-299, GLG-305): the
> `-missing-only` flag and its 15-minute cron line went away with
> `tools/tefas`. A newly tracked fund now gets its NAVs from the on-demand
> fetch after its transaction is saved, and the 14-day rule survives as
> `backfillSlackDays` in `backend/internal/tefas/tefas.go`, used by that
> fetch and by the startup backfill.

tefas.gov.tr's robots.txt disallows the `/api/` path these requests use, so
the `tefas` source is off by default — see "Decision on robots.txt" below.

## How the new endpoint was found, and why it's trusted

A plain re-test of the ticket's endpoint (below) confirmed it's still dead.
An existing third-party crawler was looked at and not used; the project
talks to TEFAS directly, and only what curl returned from `tefas.gov.tr` is
recorded below as fact.

## Verified with curl (2026-09-14)

**1. Legacy endpoint — confirmed still dead:**

```
$ curl -s -X POST https://www.tefas.gov.tr/api/DB/BindHistoryInfo \
    -d "fontip=YAT&fonkod=&bastarih=01.09.2026&bittarih=14.09.2026"
-> HTTP 404
{
  "fault": {
    "faultCode": "ERR-006",
    "faultString": "Method not found or disabled!",
    "faultStatusCode": "404"
  }
}
```

Adding a browser `User-Agent`, `Referer: https://www.tefas.gov.tr/TarihselVeriler.aspx`,
`X-Requested-With: XMLHttpRequest`, and cookies from a prior `GET` of the page
made no difference — same 404.

**2. The `.aspx` pages are now behind a JS bot-challenge**, which is a
separate problem from the dead endpoint. `GET /TarihselVeriler.aspx` and
`GET /FonKarsilastirma.aspx` both return a 6 KB stub, not the page:

```
$ curl -s https://www.tefas.gov.tr/FonKarsilastirma.aspx | wc -c
6079
$ curl -s https://www.tefas.gov.tr/FonKarsilastirma.aspx | grep -o 'TSPD\|Please enable JavaScript'
TSPD
TSPD
Please enable JavaScript to view the page content.
```

`TSPD` is an F5 Distributed Cloud / Shape anti-automation challenge cookie.
No amount of header tweaking gets past it with curl — it requires executing
the page's JS. This explains why the old `.aspx`-driven `api/DB/*` endpoints
look dead: the front end that called them no longer exists in a
curl-reachable form. It also means **the fund list and NAV cannot come from
scraping the `.aspx` pages**, only from whatever JSON API the new front end
actually calls — which turned out to still be curl-reachable directly (see
below), unlike the pages.

**3. New fund list endpoint — works, no bot-challenge, no auth:**

```
$ curl -s -X POST https://www.tefas.gov.tr/api/funds/fonGetiriBazliBilgiGetir \
    -H "Content-Type: application/json" -H "Accept: application/json, text/plain, */*" \
    -d '{"dil":"TR","fonTipi":"YAT","islem":1,"calismaTipi":2,
         "kurucuKodu":null,"sfonTurKod":null,"fonTurAciklama":null,"fonTurKod":null,"fonGrubu":null,
         "donemGetiri1a":"1","donemGetiri3a":"1","donemGetiri6a":"1","donemGetiri1y":"1",
         "donemGetiriyb":"1","donemGetiri3y":"1","donemGetiri5y":"1",
         "basTarih":null,"bitTarih":null,"getiriOrani":"1"}'
-> HTTP 200, 331 KB, resultList: 1065 entries
{
  "fonKodu": "AU1",
  "fonUnvan": "A1 CAPİTAL PORTFÖY ALTIN FONU",
  "fonTurAciklama": "Kıymetli Madenler Şemsiye Fonu",
  "tefasDurum": true,
  "getiri1a": 0.5893, "getiri3a": 9.2181, ...  (period returns, not needed for import)
}
```

`islem`, `calismaTipi`, and the long list of `donemGetiriX` flags all had to
be present with these exact values — dropping most of them (as in the
minimal payload the crawler repo showed for the _price_ endpoint) made this
one return `resultList: null` with no error. `fonTipi` is an enum:
`YAT` (regular mutual funds — the ones in scope for GLG-83), `EMK` (pension
funds), `BYF` (exchange-traded funds). Only `YAT` was queried; `EMK`/`BYF`
are a different product and out of scope unless a future ticket asks for
them. All 1065 `YAT` funds returned had `tefasDurum: true`; 9 distinct
`fonTurAciklama` categories (equity, debt instruments, money market,
precious metals, variable, fund-of-funds, free, mixed, participation).

**4. New NAV endpoint — per fund, months-back window, not per-date:**

```
$ curl -s -X POST https://www.tefas.gov.tr/api/funds/fonFiyatBilgiGetir \
    -H "Content-Type: application/json" -H "Accept: application/json, text/plain, */*" \
    -d '{"fonKodu":"YAC","dil":"TR","periyod":1}'
-> HTTP 200, 22 rows
{"fonKodu":"YAC","fonUnvan":"YAPI KREDİ PORTFÖY İKİNCİ FON SEPETİ FONU",
 "kategoriDerece":83,"kategoriFonSay":101,"tarih":"2026-08-14","fiyat":14.556637}
...
{"tarih":"2026-09-11","fiyat":14.934257}   <- last real price (Friday)
{"tarih":"2026-09-14","fiyat":0}            <- today (Monday), not published yet
```

Notes on that response:

- Weekend dates (2026-09-12/13) are absent entirely — non-trading days are
  omitted, same as the BIST bulletin, not returned as zero.
- Today's row exists but `fiyat: 0` — **NAV for the current day is a
  placeholder until it's published**. The spike ran mid-session on
  2026-09-14 (Monday) and Friday 09-11 was the latest real value at that
  point. Correction from GLG-87 later the same day: day D's NAV was
  observed published by 13:27 Europe/Istanbul on day D, not D+1 as first
  expected. The crawler drops `fiyat == 0` rows, and the daily run is
  scheduled for 20:00 (GLG-109), well after the observed publish time.

  > Superseded 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-303): the backend
  > `tefas` source runs daily at its `source.tefas.at` setting, default
  > 14:00, which is still after the observed publish time.

- `periyod` only accepts `{1, 3, 6, 12, 36, 60}` (months). Anything else, or
  passing `bastarih`/`bittarih` instead, returns `HTTP 200` with
  `{"errorCode":null,"errorMessage":"Sistem Hatası!!","resultList":null}` —
  a 200 with a null result is the API's error shape, not an HTTP error code.
- `fiyat` is a **JSON number**, e.g. `14.556637` (6 decimals observed) — not
  a string. Decoding straight into `float64` before converting to
  `decimal.Decimal` reintroduces the float bug this project's `decimal`
  rule exists to avoid; GLG-87 must decode it as `json.Number` (or into a
  struct field typed `json.Number`) and build the decimal from its string
  form, the same way `ValidClose` in `backend/internal/price/price.go`
  expects a decimal string, not a float.

**5. No bulk-by-date NAV call exists.** The price endpoint is fund-scoped
only; getting NAV for N tracked funds is N requests. 8 rapid sequential
requests all returned `HTTP 200` with no throttling observed — but that's a
2026-09-14 spot-check, not a documented limit, and TEFAS publishes none. A
fetcher should stay conservative anyway (small delay between requests, only
tracked funds — never the full 1065-fund catalog on a schedule).

## robots.txt and terms

```
$ curl -s https://www.tefas.gov.tr/robots.txt
User-Agent: *
Allow: /
Disallow: /api/
...
```

`robots.txt` disallows `/api/` for every user agent. Both endpoints this ADR
uses live under `/api/`.

**Decision on robots.txt**: fetching from TEFAS is the decision of the
operator who enables it, so the `tefas` source is off by default (GLG-1114).
The server fetches nothing from TEFAS until an admin turns the source on in
Yönetim / Administration, where the `tefas` row says the endpoint is
disallowed by robots.txt. With the source off, fund NAVs are entered by hand
(manual entry writes `manual_prices`, GLG-103; see above). With it on, the
crawler identifies itself with the honest User-Agent
`gelirgider/1.0 (+https://github.com/dogauzun/gelirgider)`, fetches only
funds the user actually tracks (those with a transaction, never the
1065-fund catalog), runs once a day, and re-fetches the last month on every
run so a missed day needs no special handling. As first shipped
(2026-09-14) it ran outside the server as a separate tool under
`tools/tefas`, from host cron (GLG-109).

> Superseded 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-299, GLG-305): the
> fetcher no longer runs as `tools/tefas` from host cron but inside the
> backend as the `tefas` scheduler source (`backend/internal/tefas`), with
> the same User-Agent, tracked funds only and the one-month re-fetch; it
> also fetches on demand after a transaction is saved and in the backfill.

## Precision and quantities — no schema change needed

NAV values observed with up to 6 fraction digits (`14.556637`), matching the
ticket's expectation. `backend/internal/price/price.go`'s `decimalPattern`
already accepts up to 8 fraction digits, and
`backend/internal/transaction/handlers.go` already has the comment
"8 fraction digits covers TEFAS (6) and fractional quantities" — this was
anticipated when those validators were written (GLG-48) and needs no
change.

> Amended 2026-09-21 (GLG-214): the transaction parser now accepts 9
> fraction digits, and its comment reads "9 fraction digits covers TEFAS (6)
> and Midas fractional shares"; `decimalPattern` in `price.go` still accepts 8.

## Alternatives considered

| Source                                                    | Fund list                                                         | Daily NAV, all funds one date | Per-fund history            | Free / no login                 | Verdict                                                                                                                                              |
| --------------------------------------------------------- | ----------------------------------------------------------------- | ----------------------------- | --------------------------- | ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `tefas.gov.tr/api/funds/*` (new)                          | yes, 1065 in one call                                             | no — per-fund only            | yes, up to 5y via `periyod` | yes                             | **chosen**                                                                                                                                           |
| `tefas.gov.tr/api/DB/*` (legacy, ticket's starting point) | —                                                                 | —                             | —                           | —                               | dead: `404 Method not found or disabled!`                                                                                                            |
| `.aspx` pages scraped directly                            | —                                                                 | —                             | —                           | blocked by F5/TSPD JS challenge | rejected, not curl-reachable                                                                                                                         |
| Is Yatirim (worked for BIST in ADR 0001)                  | untested — no known TEFAS-mirror path found in the time available | —                             | —                           | —                               | not pursued: no working endpoint found, unlike BIST                                                                                                  |
| SPK / KAP                                                 | —                                                                 | —                             | —                           | —                               | not pursued: TEFAS _is_ the official Takasbank/SPK platform for this data, so these would be a worse copy of the same source, not an independent one |
| Manual CSV / entry fallback                               | n/a                                                               | n/a                           | n/a                         | yes                             | kept as permanent fallback (GLG-32 pattern), same as BIST                                                                                            |

## Consequences

**GLG-83 (fund import)**: call `fonGetiriBazliBilgiGetir` with `fonTipi=YAT`
once, filter to `tefasDurum: true`, and import `fonKodu` -> `instruments.symbol`,
`fonUnvan` -> `name`, market `'TEFAS'`, currency always `'TRY'`.
`fonTurAciklama` can be stored if the schema ever wants a fund-category
column; it isn't required for MVP import. ~1065 candidate funds as of
2026-09-14 — given the known instrument-lookup gap from Phase 2, consider
import-on-search rather than pre-seeding all 1065 rows, the same open
question BIST already has, not a new one this ticket needs to resolve.

> Resolved 2026-09-14 (GLG-83): all 1065 funds are pre-seeded from
> `backend/internal/instrument/tefas_symbols.csv`, not imported on search.

**GLG-87 (NAV fetcher)**, as shipped: a standalone Go module at
`tools/tefas` (not `internal/tefas`, not wired into the server's scheduler,
no `-fetch-tefas` flag) — one `fonFiyatBilgiGetir` call per tracked TEFAS
instrument (not all 1065 funds), `-periyod` months back with the default
`1` and a cap of `60` (5 years) for backfill; `source = 'tefas'`; rows with
`fiyat == 0` (unpublished) are dropped; `fiyat` is decoded as `json.Number`,
never `float64`. Scheduling is host cron via `deploy/tefas.sh` (GLG-109),
daily at 20:00, after the observed 13:27 publish time.

> Superseded 2026-09-23 by [ADR 0010](0010-setup-and-admin.md) (GLG-299, GLG-305): the
> fetcher is now `backend/internal/tefas`, wired into the scheduler as the
> `tefas` source with a `-fetch-tefas` flag and a default time of 14:00;
> `tools/tefas` and `deploy/tefas.sh` were removed. See "Update 2026-09-23"
> below.

**Update 2026-09-23** (GLG-296): the crawler moved into the backend as
`internal/tefas` (GLG-299), a normal scheduler source named `tefas` with a
`-fetch-tefas` flag and backfill steps. `tools/tefas`, `deploy/tefas.sh`,
`deploy/tefas.Dockerfile`, the compose `tefas` service and the host cron
were removed in GLG-305.

> Superseded 2026-10-06 (GLG-1087): here and in the note above, the
> per-source `-fetch-*` flags are one `-fetch <source>` flag with `-date`
> (#1050), so TEFAS runs as `-fetch tefas`; see `deploy/RUNBOOK.md` §5.

**Update 2026-10-01** (GLG-694, Phase 22): TEFAS fund names change while
the code stays, so the fetcher updates a tracked fund's `instruments.name`
from the `fonUnvan` of its `fonFiyatBilgiGetir` response on every fetch
(GLG-698). The seed CSV is not rewritten; it only seeds a fresh database,
and the next fetch corrects any stale name in it.

Funds outside the `fonTipi=YAT` list still return NAVs from
`fonFiyatBilgiGetir`. GJH and GNP, two Garanti-only funds, are not in the
list but were priced that way on 2026-10-01. Such a fund enters through the
statement import's discovery (ADR 0006, GLG-699) or the admin instrument
form, and from then on the fetcher prices it like any tracked fund.
