# ADR 0032: Watchlist with target prices

Status: accepted (2026-10-18).

## Context

Every instrument the app shows is one the user holds. There is no way to follow
an instrument before buying it: to see its last close and the day's move, or to
be told when it reaches a price the user has in mind. Today the user keeps that
list elsewhere and checks it by hand.

The alert has to stay in the app. Email alerts are a non-goal
([ROADMAP](../ROADMAP.md)), and the Dikkat card already reports things that
need a look without sending anything ([ADR 0030](0030-saving-plans.md)). A
reached target is one more such item. As with dividends and saving plans, the
app reports and the user acts ([ADR 0009](0009-dividends.md)); the watchlist
never writes a transaction.

## Decision

### One table, one row per user and instrument

Table `watchlist` holds `user_id`, `instrument_id`, a nullable `target_price`
as exact decimal text in the instrument's currency, a nullable `target_side`
(`above` or `below`), `note` (empty by default) and `created_at` (UTC). A unique
index on `(user_id, instrument_id)` allows one row per user and instrument; a
second add answers 409. Like the other per-user tables, every query filters by
`user_id`, and the rows go with the account.

The list belongs to the user, not to a portfolio. Deleting a portfolio leaves
it alone, and an instrument the user holds can be watched too. A `DEPOSIT`
instrument is refused, as every generic route refuses it
([ADR 0024](0024-term-deposits.md)).

### Routes

- `POST /api/watchlist` takes `instrument_id` and an optional `note`.
- `GET /api/watchlist` lists the caller's rows, sorted by symbol.
- `PUT /api/watchlist/{id}` sets or clears the target and the note.
- `DELETE /api/watchlist/{id}` removes a row.

A row that is not the caller's is a 404, the same as an unknown one.

### Price fields

A row carries the instrument and its prices in the instrument's currency; there
is no reporting-currency conversion.

- `last_price`, with `price_date` and `price_source`, is the winning close as of
  today, found as the summary values a position: the user's manual price over a
  fetched one on the same date, the newer date otherwise
  ([ADR 0019](0019-core-engine-and-auth.md)). Two users watching the same
  instrument can see different closes when one has a manual price.
- `prev_close`, with `prev_price_date`, is the latest close on or before the day
  before the last close's date, found as the summary finds its previous close.
- Both are scaled by the splits between their dates and today
  ([ADR 0008](0008-splits.md)), so a split between the two does not show as a
  fall.
- `day_change` is `last_price - prev_close`, to 2 decimals. The percent is the
  frontend's.

Each of these is null when the close is missing. An instrument that has never
been priced is a row with nulls, not an error.

### The target and its side

`PUT` takes `target_price` as a string, or null (or omitted) to clear it. A
target is a close, so it follows the manual price's rules: positive, at most 15
integer and 8 fraction digits. Omitting `note` keeps the stored one.

The side is fixed when the target is set, from the last close at that moment:
`above` when the target is greater than it, `below` when it is lower or equal.
A target with no close to compare with is refused. Clearing the target clears
the side.

`target_reached` is computed on every read from the current last close: `last >=
target` for `above`, `last <= target` for `below`. It is false without a
target or a close. It is a state, not an event: it stays true while the close
stays beyond the target, and nothing records when it first became so. Changing
the target sets the side again from the then-current close.

### Fetching a watched instrument

Adding a row fetches the instrument's price dated 14 days back on demand, the
fetch a transaction write already triggers, so the row has
two closes for the day's move before the next scheduled sync.

After that the daily syncs keep the price current. The per-instrument syncs
(US, TEFAS, crypto) fetch only what `instrument.Repo.Tracked` returns, so an
instrument counts as tracked when it appears in a transaction or in any user's
watchlist; an instrument that is both is one row. BIST comes from the whole
bulletin and gram gold from one series, and need nothing.

### Where it shows

A Watchlist page at `/watchlist`, in a block of the sidebar next to the admin
link. It lists the rows with the last close and its date, the day's change in
money and percent, the target with its side, a "target reached" pill and the
note. The user adds an instrument from the instrument search, edits the target
and note in a dialog, or removes the row.

The Dikkat card gets a group "İzleme listesi" with one item per reached target.
It is a data item like the due plans (ADR 0030): the attention rules of
[ADR 0023](0023-ui-design-system-v2.md) never hide it. It asks for no click, so
it does not count toward the card's header. Its action is a link to the page.
The group shows on the all-portfolios page only ([ADR 0014](0014-all-portfolios-view.md)),
since the list belongs to no portfolio; a portfolio page never requests the
list.

## Out of scope

- Email or push alerts.
- More than one target per instrument, a percentage target, a stop and a take
  profit together.
- Acknowledging or snoozing a reached target.
- Per-portfolio watchlists.
- Reporting-currency conversion of the prices.
- Charts or history of a watched instrument beyond what the position page
  shows for a holding.
- Writing a buy from the page; the user books a trade as usual.
