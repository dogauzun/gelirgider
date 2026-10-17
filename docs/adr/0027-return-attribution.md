# ADR 0027: Return attribution

Status: accepted (2026-10-18).

## Context

The history response says how much the portfolio gained over a range, but not
why. A USD stock held through a rising USD/TRY shows a gain in TRY with no
price move at all, and the user cannot tell the currency's share from the
stock's. Today's currency figures are two partial answers: `fx_pl` on the
summary is a residual (total value less the flows at their own dates' rates
and both P/Ls), and `fx_pl_reporting` per position is the gain since cost.
[ADR 0003](0003-multi-currency-and-market-data-sources.md) and
[ADR 0011](0011-portfolio-returns.md) record their limits.

The split needs a method that is exact, comes from the same inputs as the
chart, and adds up to the chart's own change in value.

## Decision

### Four buckets, computed per day

The history sweep splits each day's change in total value into four buckets,
for each day `d` against the day before, `prev`. `f` is the factor of a
currency into the reporting currency on that day (`holdings.Factor`).

- **fx**, per instrument: `MV_prev × (f_d − f_prev)`. `MV` is the native market
  value, or the cost basis for a position with no price yet, which is what
  `total_value` counts.
- **price**, per instrument: `(MV_d − MV_prev − tradeCash_d) × f_d`.
  `tradeCash` is buys (amount plus fee) less sells (amount less fee), so the
  fee sits inside price and a trade moves no bucket by itself.
- **income**: `net × f_d`, with `net` a dividend (amount less stopaj) or
  interest, converted at the pay-date rate.
- **cash_fx**: `Σ cash_prev × (f_d − f_prev)` over every currency of the
  scope's cash, not only the currencies of the positions.

The buckets of a range are the sums of its days. The response carries them per
position and for the portfolio as `total`.

### The identity

Over a range, the buckets sum to `V_last − V_first − flows`, where `flows` are
the ones `net_flow` counts (ADR 0011): converted deposits less withdrawals,
plus the first-price jump pseudo-flows. The sum is exact on unrounded
decimals. Each bucket is rounded to 2 decimals in the response, so the sum of
the rounded buckets is within 0.02 of the exact `V_last − V_first − flows`.
`net_flow` itself adds each flow day as a difference of rounded values, so it
can differ from the exact flows by up to one cent per flow day; against the
rounded change in value less `net_flow` the bound widens by that much.

### Owner decisions

- Buy and sell fees sit inside `price`. So do the other cash costs: a
  standalone fee row, and the fee on a deposit or a withdrawal. These have no
  instrument, so they are in `total.price` and in no position's `price`.
- Income is net of stopaj. Interest, a dividend with no instrument, is income
  too, and also appears in `total.income` only.
- A deposit instrument's price change (ADR 0024) counts as `income`, being
  interest; its currency move stays `fx`.
- A dividend is converted at its pay-date rate. Later moves of the currency
  that holds the cash go to `cash_fx`.
- First-price jumps are excluded as flows, as in ADR 0011: the jump when a
  priced position first gets a price is a flow, not a gain, so it is in no
  bucket.
- A missing rate gives `null` for the buckets that need it, plus the currency
  in `missing_rates`, as the rest of the history response does.
- On `all`, each portfolio's state is computed on its own and the buckets are
  summed, as in [ADR 0014](0014-all-portfolios-view.md).

### `fx_pl` and `fx_pl_reporting` stay

The summary's `fx_pl` and the per-position `fx_pl_reporting` are not replaced.
They keep meaning "since cost": the currency part of the gain since the buys,
not since the start of a range. The attribution's `fx` is the currency's share
of a chosen range.

### Worked example

10 shares of a USD stock at a flat 100 USD, no cash, USD/TRY going from 30 to
33 (up 10%) between day `prev` and day `d`, reporting currency TRY.

- `MV_prev = 10 × 100 = 1000` USD, `f_prev = 30`, `f_d = 33`.
- `fx = 1000 × (33 − 30) = 3000`.
- `price = (1000 − 1000 − 0) × 33 = 0`.
- `income = 0`, `cash_fx = 0`.
- The value goes from 30000 to 33000 TRY, no flows: `3000 = 3000 + 0 + 0 + 0`.

## Out of scope

- Converting realized P/L at the sell-date rate. Realized P/L and closed
  positions still convert at the valuation-date rate (ADR 0003), so their
  currency effect stays in the residual `fx_pl`.
