import { Fragment } from 'react'
import type { CashReconciliation, Holding, Reconciliation } from '../../api/imports'
import { t } from '../../i18n/strings'
import { formatQuantity, subDecimal } from '../../lib/decimal'
import { formatMoney, formatNumber, formatUnitPrice, MISSING, signedMoney } from '../../lib/format'
import { Icon } from '../ui/Icon'

// The statement's period-end snapshot next to what the portfolio computes
// with the preview's checked rows applied. A line the server marked
// as matching gets a check, everything else a warning: a position the
// statement never mentions has no statement quantity at all. The last line
// compares the statement's cash balance in its own currency. The
// page hands it the lines moved by the rows the user would commit.
export function HoldingsPanel({
  holdings,
  reconciliation,
  cash,
  desktop,
}: {
  holdings: Holding[]
  reconciliation: Reconciliation[]
  cash: CashReconciliation | null
  desktop: boolean
}) {
  if (reconciliation.length === 0 && !cash) return null
  const bySymbol = new Map(holdings.map((holding) => [holding.symbol, holding]))
  // A table with the difference per line, so a mismatch reads at a glance
  // rather than from two numbers 900px from their symbol.
  const lines = reconciliation.map((entry) => {
    const holding = bySymbol.get(entry.symbol)
    return {
      key: entry.symbol,
      label: (
        <>
          <span className="flex items-center gap-2">
            <MatchIcon match={entry.match} />
            <span className="font-semibold text-ink">{entry.symbol}</span>
          </span>
          {/* Garanti and Garanti Yatırım print no average cost, so their
              holdings carry "0". */}
          {holding && Number(holding.avg_cost) !== 0 && (
            <span className="mt-0.5 block pl-6 text-[12px] text-muted">
              {`${t('import.avgCost')} ${formatUnitPrice(holding.avg_cost, holding.currency)}`}
            </span>
          )}
        </>
      ),
      statement: entry.statement_quantity ? formatQuantity(entry.statement_quantity) : MISSING,
      portfolio: formatQuantity(entry.portfolio_quantity),
      difference: (
        <DifferenceCell
          statement={entry.statement_quantity ?? '0'}
          portfolio={entry.portfolio_quantity}
          match={entry.match}
          format={(value) =>
            formatNumber(value, { maximumFractionDigits: 9, signDisplay: 'exceptZero' })
          }
        />
      ),
    }
  })
  if (cash) {
    lines.push({
      key: 'cash',
      label: (
        <span className="flex items-center gap-2">
          <MatchIcon match={cash.match} />
          <span className="font-medium text-ink">{t('import.reconcile.cash')}</span>
        </span>
      ),
      statement: formatMoney(cash.statement_amount, cash.currency),
      portfolio: formatMoney(cash.portfolio_amount, cash.currency),
      difference: (
        <DifferenceCell
          statement={cash.statement_amount}
          portfolio={cash.portfolio_amount}
          match={cash.match}
          format={(value) => signedMoney(value, cash.currency)}
        />
      ),
    })
  }
  const cell = 'px-4 py-2 text-right align-top whitespace-nowrap'
  const numbers = (line: (typeof lines)[number]) => (
    <>
      <td className={cell}>{line.statement}</td>
      <td className={cell}>{line.portfolio}</td>
      {line.difference}
    </>
  )
  return (
    <section className="mt-4 overflow-hidden card">
      <h3 className="border-b border-line px-4 py-2 text-[12px] font-medium text-muted uppercase">
        {t('import.holdings')}
      </h3>
      <div className="overflow-x-auto">
        {/* Content width on desktop, so the numbers sit next to their
            symbol instead of across the card. A phone has no room for four
            columns of 9-decimal quantities, so there the symbol takes a
            row of its own above its numbers. */}
        {/* eslint-disable-next-line no-restricted-syntax -- content width, which TABLE's w-full would undo */}
        <table className={`text-[13px] text-ink-2 ${desktop ? '' : 'w-full'}`}>
          <thead>
            <tr className="border-b border-line text-[12px] text-muted">
              {desktop && (
                <th className="py-2 pr-8 pl-4 text-left font-medium">
                  {t('import.reconcile.symbol')}
                </th>
              )}
              <th className={`${cell} font-medium`}>{t('import.reconcile.statementColumn')}</th>
              <th className={`${cell} font-medium`}>{t('import.reconcile.portfolioColumn')}</th>
              <th className={`${cell} font-medium`}>{t('import.reconcile.difference')}</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((line) =>
              desktop ? (
                <tr key={line.key} className="border-b border-line last:border-0">
                  <td className="py-2 pr-8 pl-4 align-top">{line.label}</td>
                  {numbers(line)}
                </tr>
              ) : (
                <Fragment key={line.key}>
                  <tr>
                    <th scope="colgroup" colSpan={3} className="px-4 pt-2 text-left font-normal">
                      {line.label}
                    </th>
                  </tr>
                  <tr className="border-b border-line last:border-0">{numbers(line)}</tr>
                </Fragment>
              ),
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function MatchIcon({ match }: { match: boolean }) {
  return (
    <Icon
      name={match ? 'check' : 'warning'}
      label={t(match ? 'import.reconcile.match' : 'import.reconcile.mismatch')}
      className={`shrink-0 ${match ? 'text-gain' : 'text-warn'}`}
    />
  )
}

// Statement minus portfolio, exact: both are decimal strings, counted in the
// API's 1e-9 units, so no float rounding. `format` signs it: a
// surplus on the statement side gets a plus, and a difference that prints as
// zero, a sub-cent cash gap, gets no sign. Highlighted when the
// line does not match, the server's call: the cash line allows a cent of
// rounding, so a non-zero difference can still match, next to a
// check.
function DifferenceCell({
  statement,
  portfolio,
  match,
  format,
}: {
  statement: string
  portfolio: string
  match: boolean
  format: (value: string) => string
}) {
  const text = format(subDecimal(statement, portfolio))
  return (
    <td
      className={`px-4 py-2 text-right align-top whitespace-nowrap ${
        match ? 'text-muted' : 'font-medium text-warn'
      }`}
    >
      {text}
    </td>
  )
}
