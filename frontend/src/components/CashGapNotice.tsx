import { useId, useState } from 'react'
import { useCashGaps, type CashGap } from '../api/history'
import { usePortfolios } from '../api/portfolios'
import type { PortfolioScope } from '../api/summary'
import { useFillCashGaps } from '../api/transactions'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { decimalUnits, unitsToDecimal } from '../lib/decimal'
import { formatMoney } from '../lib/format'
import { useOneAtATime } from '../lib/useOneAtATime'
import { Button } from './ui/Button'
import { Notice } from './ui/Notice'
import { Skeleton } from './ui/Skeleton'
import { TABLE, TD, TD_NUM, TD_WRAP, TH } from './ui/table'
import { useModal } from './ui/useModal'
import { Modal } from './ui/Modal'
import { sheetPanel } from './ui/sheetPanel'

// Why every return of the scope is null (ADR 0011), on the Performans tab's
// periodic returns and drawdown cards (GLG-1110). With cash gaps, the first
// day each portfolio's cash went below zero and its shortfall per currency,
// and a button that records them as deposits after a confirm. Both cards
// share the gaps query, and the fill invalidates every portfolio query, so
// one confirm fixes both. Without gaps the range starts with no value.
export function CashGapNotice({ scope }: { scope: PortfolioScope }) {
  const query = useCashGaps(scope)
  const { data: portfolios } = usePortfolios()
  const [open, setOpen] = useState(false)

  // A failed load still has a reason to give: the old text names both.
  if (query.isError) return <Text>{t('performance.periods.undefined')}</Text>
  if (!query.data) return <Skeleton className="h-10 w-full" />
  const gaps = query.data
  if (gaps.length === 0) return <Text>{t('performance.cashGaps.noValue')}</Text>

  // The server orders the gaps by portfolio, date, currency, so a
  // portfolio's first gap is its first date.
  const byPortfolio = new Map<number, CashGap[]>()
  for (const gap of gaps) {
    const list = byPortfolio.get(gap.portfolio_id)
    if (list) list.push(gap)
    else byPortfolio.set(gap.portfolio_id, [gap])
  }
  const name = (id: number) => portfolios?.find((p) => p.id === id)?.name ?? ''

  return (
    <div className="flex flex-col items-start gap-2 text-[13px] text-ink-2">
      <ul className="flex flex-col gap-1">
        {[...byPortfolio].map(([id, rows]) => (
          <li key={id} className="wrap-anywhere">
            {scope === 'all' && <b className="font-semibold text-ink">{name(id)}: </b>}
            {t('performance.cashGaps.line', {
              date: formatMarketDate(rows[0].date),
              amounts: shortfalls(rows).join(', '),
            })}
          </li>
        ))}
      </ul>
      <p className="text-muted">{t('performance.cashGaps.cause')}</p>
      <Button variant="secondary" onClick={() => setOpen(true)}>
        {t('performance.cashGaps.add')}
      </Button>
      {open && (
        <CashGapDialog scope={scope} gaps={gaps} name={name} onClose={() => setOpen(false)} />
      )}
    </div>
  )
}

function Text({ children }: { children: string }) {
  return <p className="text-[13px] text-muted">{children}</p>
}

// One portfolio's shortfall per currency, the exact sum of that currency's
// gaps, in the order the currencies first fall short.
function shortfalls(rows: CashGap[]): string[] {
  const sums = new Map<string, bigint>()
  for (const { currency, amount } of rows) {
    sums.set(currency, (sums.get(currency) ?? 0n) + decimalUnits(amount))
  }
  return [...sums].map(([currency, units]) => formatMoney(unitsToDecimal(units), currency))
}

// Lists the deposits the server will write; confirm sends only the note, as
// the server recomputes the gaps (GLG-1109). Mounted only while open, so a
// failed post's error is gone on the next open.
function CashGapDialog({
  scope,
  gaps,
  name,
  onClose,
}: {
  scope: PortfolioScope
  gaps: CashGap[]
  name: (id: number) => string
  onClose: () => void
}) {
  const dialogRef = useModal()
  const titleId = useId()
  const fill = useFillCashGaps(scope)
  const once = useOneAtATime()
  const all = scope === 'all'

  return (
    <Modal ref={dialogRef} aria-labelledby={titleId} onClose={onClose}>
      <div className={sheetPanel('p-5 md:w-[30rem]')}>
        <h2 id={titleId} className="text-[15px] font-semibold text-ink">
          {t('performance.cashGaps.add')}
        </h2>
        <p className="mt-2 text-[13px] text-ink-2">{t('performance.cashGaps.body')}</p>
        <div className="mt-3 max-h-[50vh] overflow-y-auto">
          <table className={TABLE}>
            <thead>
              <tr>
                <th className={TH}>{t('transactions.date')}</th>
                {all && <th className={TH}>{t('performance.periods.portfolio')}</th>}
                <th className={TH}>{t('transactions.currency')}</th>
                <th className={`${TH} text-right`}>{t('transactions.amount')}</th>
              </tr>
            </thead>
            <tbody>
              {gaps.map((gap) => (
                <tr key={`${gap.portfolio_id}-${gap.date}-${gap.currency}`} className="h-9">
                  <td className={`${TD} tabular-nums`}>{formatMarketDate(gap.date)}</td>
                  {all && <td className={TD_WRAP}>{name(gap.portfolio_id)}</td>}
                  <td className={TD}>{gap.currency}</td>
                  <td className={TD_NUM}>{formatMoney(gap.amount, gap.currency)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {fill.isError && (
          <div className="mt-3">
            <Notice role="alert">{fill.error.message}</Notice>
          </div>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            {t('confirm.cancel')}
          </Button>
          <Button
            pending={fill.isPending}
            onClick={() =>
              once(() => fill.mutateAsync(t('performance.cashGaps.note'), { onSuccess: onClose }))
            }
          >
            {t('performance.cashGaps.confirm')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
