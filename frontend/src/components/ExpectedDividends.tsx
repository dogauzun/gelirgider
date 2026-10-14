import { useEffect, useRef, useState } from 'react'
import {
  useAddExpectedDividends,
  useDismissExpectedDividend,
  useExpectedDividends,
  type ExpectedDividend,
} from '../api/dividends'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { formatQuantity, decimalUnits, unitsToDecimal } from '../lib/decimal'
import { formatMoney, formatUnitPrice } from '../lib/format'
import { useIsDesktop } from '../lib/useIsDesktop'
import { useOneAtATime } from '../lib/useOneAtATime'
import { toast } from '../lib/toast'
import { focusIfLost, neighbourOf } from '../lib/useFocusLater'
import { TransactionDialog } from './TransactionDialog'
import { expectedDividendForm } from './expectedDividendForm'
import { toInput } from './transactionForm'
import { ActionsTh } from './ui/ActionsTh'
import { Badge } from './ui/Badge'
import { Button } from './ui/Button'
import { Notice } from './ui/Notice'
import { ROW_44, TABLE, TD, TD_NUM, TH } from './ui/table'

function SymbolLabel({ row }: { row: ExpectedDividend }) {
  return (
    <span className="flex items-center gap-1.5">
      <span className="text-[13px] font-semibold text-ink">{row.symbol}</span>
      <Badge>{row.market}</Badge>
    </span>
  )
}

const rowKey = (row: ExpectedDividend) => `${row.instrument_id}-${row.date}`

// The rows' net total, added exactly per currency: "₺1.264,79", or
// "₺1.264,79 + $12,34" when BIST and US dividends mix, since the panel has no
// FX rate to put them in one currency.
function netTotal(rows: ExpectedDividend[]): string {
  const totals = new Map<string, bigint>()
  for (const row of rows) {
    totals.set(row.currency, (totals.get(row.currency) ?? 0n) + decimalUnits(row.net))
  }
  return [...totals]
    .map(([currency, units]) => formatMoney(unitsToDecimal(units), currency))
    .join(' + ')
}

// Dividends to confirm, the first card on the Temettü tab (GLG-292, GLG-605,
// GLG-939). Renders nothing while the list is empty, which is most of the
// time; the header carries the count and the net total (GLG-568).
export function ExpectedDividends({ portfolioId }: { portfolioId: number }) {
  const { data } = useExpectedDividends(portfolioId)
  const addAllMutation = useAddExpectedDividends()
  const dismissal = useDismissExpectedDividend()
  const [adding, setAdding] = useState<ExpectedDividend | null>(null)
  const busy = addAllMutation.isPending || dismissal.isPending
  const [error, setError] = useState<string | null>(null)
  const desktop = useIsDesktop()
  const rows = data ?? []
  // The suggestion last acted on (added in the dialog or dismissed), with the
  // row order at the time. "Tümünü ekle" records no rows, so focus goes to
  // "İşlem ekle" once the panel empties (GLG-459).
  const openedRef = useRef<{ key: string; keys: string[] } | null>(null)

  // Adding a suggestion books it, so the refetch removes its row, and the
  // dialog hands focus back to that row's button, now gone: focus drops to
  // <body> (GLG-442). "Yoksay" and "Tümünü ekle" remove the focused button
  // the same way (GLG-459). Once the row is gone and the buttons are enabled
  // again, move focus to the next suggestion's add button, else the previous
  // one's, else the page's "İşlem ekle" -- unless the user has already moved
  // it elsewhere. A failed action clears the record: its row stays.
  useEffect(() => {
    const opened = openedRef.current
    if (adding || busy || !opened || !data) return
    const keys = data.map(rowKey)
    if (keys.includes(opened.key)) return
    openedRef.current = null
    const neighbour = neighbourOf(opened.keys, opened.key, (key) => keys.includes(key))
    focusIfLost(
      document.querySelector<HTMLElement>(
        neighbour ? `[data-add-dividend="${neighbour}"]` : '[data-add-transaction]',
      ),
    )
  }, [adding, busy, data])

  const once = useOneAtATime()
  function run(action: () => Promise<void>) {
    once(async () => {
      setError(null)
      try {
        await action()
      } catch (actionError) {
        openedRef.current = null
        setError((actionError as Error).message)
      }
    })
  }

  // One request after another, so a failure stops the rest and the toast
  // counts only what was saved. The error names the failed row and how many
  // rows were not added.
  function addAll() {
    openedRef.current = { key: '', keys: [] }
    run(async () => {
      const { saved, error } = await addAllMutation.mutateAsync({
        portfolioId,
        inputs: rows.map((row) => toInput(expectedDividendForm(row))),
      })
      if (saved > 0) toast(t('dividends.expected.added', { count: saved }))
      if (error) {
        const row = rows[saved]
        throw new Error(
          t('dividends.expected.addAllFailed', {
            symbol: row.symbol,
            date: formatMarketDate(row.date),
            count: rows.length - saved,
            error: error.message,
          }),
          { cause: error },
        )
      }
    })
  }

  function dismiss(row: ExpectedDividend) {
    openedRef.current = { key: rowKey(row), keys: rows.map(rowKey) }
    run(async () => {
      await dismissal.mutateAsync({ portfolioId, dividend: row })
      toast(t('dividends.expected.dismissed'))
    })
  }

  function actions(row: ExpectedDividend) {
    const vars = { symbol: row.symbol, date: formatMarketDate(row.date) }
    return (
      <>
        <Button
          variant="secondary"
          aria-label={t('dividends.expected.addLabel', vars)}
          data-add-dividend={rowKey(row)}
          onClick={() => {
            openedRef.current = { key: rowKey(row), keys: rows.map(rowKey) }
            setAdding(row)
          }}
          pending={busy}
        >
          {t('dividends.expected.add')}
        </Button>
        <Button
          variant="ghost"
          aria-label={t('dividends.expected.dismissLabel', vars)}
          onClick={() => dismiss(row)}
          pending={busy}
        >
          {t('dividends.expected.dismiss')}
        </Button>
      </>
    )
  }

  return (
    <>
      {/* Outside the empty check: adding the last row empties the list while
          the dialog is still closing. */}
      {adding && (
        <TransactionDialog
          portfolioId={portfolioId}
          editing={null}
          initial={expectedDividendForm(adding)}
          onClose={() => setAdding(null)}
        />
      )}
      {rows.length > 0 && (
        <section className="mb-6 card ring-1 ring-accent">
          <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 border-b border-line px-4 py-2.5">
            <div className="flex flex-wrap items-baseline gap-x-2">
              <h2 className="text-[14px] font-semibold text-plum">
                {t('dividends.expected.title')}
              </h2>
              <span className="text-[13px] text-muted">
                {t('dividends.expected.summary', {
                  count: rows.length,
                  net: netTotal(rows),
                })}
              </span>
            </div>
            <Button variant="secondary" onClick={addAll} pending={busy}>
              {t('dividends.expected.addAll')}
            </Button>
          </div>
          {error && (
            <div className="px-4 pt-3">
              <Notice role="alert">{error}</Notice>
            </div>
          )}

          {/* Eight columns and two buttons are wider than the card at tablet
              widths in English (GLG-1369): the table scrolls in its own box,
              as on Pozisyonlar. relative keeps the actions header's absolute
              sr-only label inside the clip (GLG-679). */}
          {desktop ? (
            <div className="relative overflow-x-auto">
              <table className={TABLE}>
                <thead>
                  <tr>
                    <th className={TH}>{t('transactions.instrument')}</th>
                    <th className={TH}>{t('dividends.expected.exDate')}</th>
                    <th className={`${TH} text-right`}>{t('transactions.quantity')}</th>
                    <th className={`${TH} text-right`}>{t('dividends.expected.perShare')}</th>
                    <th className={`${TH} text-right`}>{t('dividends.expected.gross')}</th>
                    <th className={`${TH} text-right`}>{t('transactions.withholdingTax')}</th>
                    <th className={`${TH} text-right`}>{t('dividends.expected.net')}</th>
                    <ActionsTh />
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => (
                    <tr key={rowKey(row)} className={ROW_44}>
                      <td className={TD}>
                        <SymbolLabel row={row} />
                      </td>
                      <td className={TD}>{formatMarketDate(row.date)}</td>
                      <td className={TD_NUM}>{formatQuantity(row.quantity)}</td>
                      <td className={TD_NUM}>{formatUnitPrice(row.per_share, row.currency)}</td>
                      <td className={TD_NUM}>{formatMoney(row.gross, row.currency)}</td>
                      <td className={TD_NUM}>{formatMoney(row.stopaj, row.currency)}</td>
                      <td className={`${TD_NUM} font-medium text-plum`}>
                        {formatMoney(row.net, row.currency)}
                      </td>
                      <td className={TD}>
                        <div className="flex justify-end gap-1">{actions(row)}</div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            // Phone: stacked rows like the transaction list (ADR 0005).
            <ul>
              {rows.map((row) => (
                <li key={rowKey(row)} className="border-b border-line px-4 py-2.5 last:border-0">
                  <div className="flex items-start justify-between gap-3">
                    <span className="min-w-0">
                      <SymbolLabel row={row} />
                      <span className="mt-0.5 block truncate text-[12px] text-muted">
                        {`${formatMarketDate(row.date)} · ${formatQuantity(row.quantity)} × ${formatUnitPrice(row.per_share, row.currency)}`}
                      </span>
                    </span>
                    <span className="shrink-0 text-right text-[12px] text-muted">
                      <span className="block text-[14px] font-medium text-plum">
                        {formatMoney(row.net, row.currency)}
                      </span>
                      <span className="block">
                        {`${t('dividends.expected.gross')} ${formatMoney(row.gross, row.currency)}`}
                      </span>
                      <span className="block">
                        {`${t('transactions.withholdingTax')} ${formatMoney(row.stopaj, row.currency)}`}
                      </span>
                    </span>
                  </div>
                  <div className="mt-2 flex justify-end gap-2">{actions(row)}</div>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
    </>
  )
}
