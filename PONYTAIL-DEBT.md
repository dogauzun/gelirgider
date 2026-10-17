# Ponytail debt ledger

Every deliberate shortcut is marked in code with a `shortcut:` comment (older code says `ponytail:`) naming its ceiling and upgrade path. This ledger collects them so a deferral cannot quietly become permanent. Regenerate with `/ponytail-debt`.

Scan date: 2026-10-17, commit 3545f42.

Format: `<file>:<line>, <what was simplified>. ceiling: <the limit>. upgrade: <the trigger to revisit>.` A row tagged `no-trigger` names no upgrade path or trigger and is the kind that silently rots.

## backend/cmd/server/backfill.go

- backfill.go:248, startup backfill walk and on-demand backfill run unsynchronized. ceiling: a few duplicate downloads, upserts idempotent. upgrade: one shared backfill worker fed by a queue, if duplicates ever cost anything.

## backend/internal/auth/setup.go

- setup.go:92, one global lock on the setup endpoint. ceiling: once-per-install endpoint. upgrade: none named. `no-trigger`

## backend/internal/btcturk/btcturk.go

- btcturk.go:228, PAXG as the gold proxy, 24:00 UTC close, no bank spread. ceiling: proxy, not spot. upgrade: change the Source section of docs/adr/0017-gold.md first (policy pointer, no trigger). `no-trigger`

## backend/internal/ratelimit/ratelimit.go

- ratelimit.go:33, in-memory limiter. ceiling: single instance. upgrade: shared store (Redis) if the backend is scaled horizontally.

## backend/internal/scheduler/runlog.go

- runlog.go:28, no run-log pruning. ceiling: a few rows per source per day. upgrade: retention delete when table size matters.

## backend/internal/scheduler/scheduler.go

- scheduler.go:8, single-instance scheduler. ceiling: two replicas both fetch, harmless but racy logs. upgrade: DB lock row (`INSERT OR IGNORE` into a price_fetch_lock table keyed by date) when it matters.

## backend/internal/server/apidocs_test.go

- apidocs_test.go:29, route-pattern regexes recognize literals or `"METHOD "+Const` only. ceiling: other forms fail as unrecognized; path and method only, not schemas. upgrade: extend the regexes if such a pattern appears.

## backend/internal/server/cash_gaps.go

- cash_gaps.go:85, cash-gap POST not recomputed inside the write transaction. ceiling: two concurrent POSTs double-write. upgrade: recompute inside the write transaction if it ever matters.

## backend/internal/server/dividends.go

- dividends.go:23, fixed withholding rates per market in code. ceiling: no per-instrument rate for ETFs or funds; a law change is a code change. upgrade: move to a table if either matters.

## backend/internal/server/history.go

- history.go:509, benchmark carried close not split-adjusted. ceiling: a split inside the range shows as a drop in the benchmark. upgrade: scale the shadow units by splitsOf[id] on each ex-date.
- history.go:538, history sweep is O(txs + days × instruments). ceiling: per-day snapshot term. upgrade: portfolio_snapshots table if that term ever matters.
- history.go:559, whole first-price jump removed, including gain on in-range buys before the first price. ceiling: slight understatement. upgrade: none; would need a price at each buy. `no-trigger`
- history.go:684, loads every stored CPI month. ceiling: a few hundred rows since 2003. upgrade: none named. `no-trigger`

## backend/internal/server/scope.go

- scope.go:38, one List query per portfolio. ceiling: a handful of portfolios. upgrade: one UNION query if portfolio counts grow.

## backend/internal/server/server.go

- server.go:413, one backfill worker and one pending entry per source name. ceiling: serial walk per source. upgrade: parallel workers per source if a single walk is ever too slow.

## backend/internal/split/split.go

- split.go:92, one IN list, no chunking. ceiling: SQLITE_MAX_VARIABLE_NUMBER 32766. upgrade: chunk when a portfolio nears it.

## backend/internal/tefas/tefas.go

- tefas.go:226, a transaction dated before the fund existed refetches the fund on every call. ceiling: one request each. upgrade: remember the attempt in empty_fetches if it shows up in the logs.

## backend/internal/transaction/transaction.go

- transaction.go:362, any zero-price buy in the window suppresses split detection. ceiling: a hand-entered gift next to a split is hidden. upgrade: explicit marker on imported bonus rows (path only, no trigger). `no-trigger`

## backend/internal/us/us.go

- us.go:300, weekends only, no US holiday calendar. ceiling: the close before a holiday is stored under the holiday's date, same value the forward carry gives. upgrade: none named. `no-trigger`
- us.go:461, a ticker Tiingo does not know (404) is re-requested on every call. ceiling: one wasted request per such symbol per save/startup. upgrade: remember misses if the free tier's hourly limit ever bites.
- us.go:520, one Tiingo request per tracked symbol per run. ceiling: ~50 tracked US symbols against the free tier's 50 requests/hour, then 429s (logged, retried next day). upgrade: spread the run over the hour or move to a paid tier when it happens.

## frontend/src/components/LazyChart.tsx

- LazyChart.tsx:16, a failed chunk fetch stays failed until reload. ceiling: no in-page retry. upgrade: none; React and the browser both keep the failure. `no-trigger`

## frontend/src/components/PositionPriceChart.tsx

- PositionPriceChart.tsx:52, trade markers cover the loaded transactions only. ceiling: 100 rows per page; older markers appear after "load more". upgrade: none named. `no-trigger`

## frontend/src/components/TransactionDialog.tsx

- TransactionDialog.tsx:337, a new row's type defaults to the day's type. ceiling: a day of mixed types guesses wrong. upgrade: none named. `no-trigger`

## frontend/src/components/import/queue.ts

- queue.ts:85, distinct names past maxNames are dropped. ceiling: a year of statements prints a few dozen. upgrade: none named. `no-trigger`

## frontend/src/components/import/rows.ts

- rows.ts:99, Number arithmetic for the preview amount. ceiling: preview only, the stored figure is the server's. upgrade: none named. `no-trigger`

## frontend/src/components/import/useLeaveGuard.ts

- useLeaveGuard.ts:49, leave guard without a data router. ceiling: no useBlocker. upgrade: move to useBlocker if the app ever adopts a data router.

## frontend/src/components/transactionForm.ts

- transactionForm.ts:320, Number arithmetic for the preview figure. ceiling: a hint only. upgrade: a decimal library if the figure ever becomes more than a hint.

## frontend/src/i18n/strings.ts

- strings.ts:75, locale in localStorage only. ceiling: per device. upgrade: a `locale` column next to `reporting_currency` and `PUT /api/me/locale` if multi-device sync is ever wanted.

## frontend/src/lib/chart.ts

- chart.ts:168, the tooltip card may run past the plot's edge. ceiling: a card too big for the room on every side of the point. upgrade: none named. `no-trigger`

## frontend/src/lib/privacy.ts

- privacy.ts:8, privacy toggle per device, in the browser only. ceiling: per device. upgrade: none named (theme and locale name theirs). `no-trigger`

## frontend/src/pages/ImportPage.tsx

- ImportPage.tsx:34, import queue in module memory. ceiling: gone on a full reload. upgrade: history state, which would have to structured-clone the files and ApiErrors (path only, no trigger). `no-trigger`
- ImportPage.tsx:160, no client upload concurrency cap. ceiling: the browser queues past ~6 per host. upgrade: add one if a year of statements ever strains the server.

## frontend/src/theme.ts

- theme.ts:10, theme per device, in the browser only. ceiling: per device. upgrade: a `theme` column on the user row if multi-device sync is ever wanted.

## Totals

34 markers, 14 with no trigger.

33 of 34 still use the old `ponytail:` prefix. A grep for `shortcut:` alone misses them.
