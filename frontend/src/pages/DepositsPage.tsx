import { useState } from 'react'
import { Navigate, useParams } from 'react-router'
import { useDeleteDeposit, useDeposits, type Deposit } from '../api/deposits'
import { DepositDialog } from '../components/DepositDialog'
import { ListCard, PortfolioListFrame } from '../components/PortfolioListFrame'
import { RenewDepositDialog } from '../components/RenewDepositDialog'
import { Button } from '../components/ui/Button'
import { EmptyCard } from '../components/ui/EmptyCard'
import { Icon } from '../components/ui/Icon'
import { Pill } from '../components/ui/Pill'
import { ROW_44, TD, TD_NUM } from '../components/ui/table'
import { useConfirm } from '../components/ui/useConfirm'
import { t, type StringKey } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { formatMoney, formatNumber } from '../lib/format'
import { useConfirmedDelete } from '../lib/useConfirmedDelete'
import { usePortfolioTitle } from '../lib/useTitle'

const STATUS: Record<Deposit['status'], { labelKey: StringKey; tone: 'gain' | 'warn' | 'cash' }> = {
  active: { labelKey: 'deposits.status.active', tone: 'gain' },
  due: { labelKey: 'deposits.status.due', tone: 'warn' },
  matured: { labelKey: 'deposits.status.matured', tone: 'cash' },
}

// The rate and the stopaj arrive as fractions: 0.175 -> "%17,5". Intl formats
// the string as an exact decimal.
const percent = (fraction: string) =>
  formatNumber(fraction, { style: 'percent', maximumFractionDigits: 7 })

const date = (iso: string) =>
  formatMarketDate(iso, { day: '2-digit', month: 'short', year: 'numeric' })

// Left-aligned text columns, then the figures. Value, accrued and payout come
// from the server at 2 decimals.
const COLUMNS: { labelKey: StringKey; className: string }[] = [
  { labelKey: 'transactions.currency', className: '' },
  { labelKey: 'deposits.principal', className: 'text-right' },
  { labelKey: 'deposits.rate', className: 'text-right' },
  { labelKey: 'deposits.stopaj', className: 'text-right' },
  { labelKey: 'deposits.start', className: 'text-right' },
  { labelKey: 'deposits.maturity', className: 'text-right' },
  { labelKey: 'deposits.value', className: 'text-right' },
  { labelKey: 'deposits.accrued', className: 'text-right' },
  { labelKey: 'deposits.payout', className: 'text-right' },
  { labelKey: 'deposits.status', className: '' },
]

function DepositRow({
  deposit,
  onRenew,
  onDelete,
}: {
  deposit: Deposit
  // Set only on a due or matured deposit that has not been renewed.
  onRenew?: () => void
  onDelete: () => void
}) {
  const { currency } = deposit
  const status = STATUS[deposit.status]
  return (
    <tr className={ROW_44}>
      <td className={TD}>{currency}</td>
      <td className={TD_NUM}>{formatMoney(deposit.principal, currency)}</td>
      <td className={TD_NUM}>{percent(deposit.rate)}</td>
      <td className={TD_NUM}>{percent(deposit.stopaj_rate)}</td>
      <td className={TD_NUM}>{date(deposit.start_date)}</td>
      <td className={TD_NUM}>{date(deposit.maturity_date)}</td>
      <td className={TD_NUM}>{formatMoney(deposit.value, currency)}</td>
      <td className={TD_NUM}>{formatMoney(deposit.accrued_interest, currency)}</td>
      <td className={`${TD_NUM} font-medium`}>{formatMoney(deposit.payout, currency)}</td>
      <td className={TD}>
        <Pill tone={status.tone}>{t(status.labelKey)}</Pill>
      </td>
      <td className={`${TD} w-0 whitespace-nowrap`}>
        {onRenew && (
          <Button
            variant="ghost"
            aria-label={t('deposits.renewLabel', {
              principal: formatMoney(deposit.principal, currency),
              currency,
            })}
            onClick={onRenew}
          >
            {t('deposits.renew')}
          </Button>
        )}
        {/* A matured deposit has booked its sell and its interest: the
            server refuses to delete it. */}
        {deposit.status !== 'matured' && (
          <Button
            variant="ghost"
            iconOnly
            aria-label={t('deposits.delete', {
              principal: formatMoney(deposit.principal, currency),
              currency,
            })}
            data-delete-row={deposit.id}
            onClick={onDelete}
          >
            <Icon name="trash" />
          </Button>
        )}
      </td>
    </tr>
  )
}

// The Mevduat tab: the portfolio's term deposits and the dialog that opens
// one. Per portfolio only, like İşlemler; the `all` scope goes back to its
// summary.
export function DepositsPage() {
  const params = useParams()
  if (params.id === 'all') return <Navigate to="/portfolios/all" replace />
  return <DepositsBody portfolioId={Number(params.id)} />
}

function DepositsBody({ portfolioId }: { portfolioId: number }) {
  usePortfolioTitle(t('deposits.title'), portfolioId)
  const query = useDeposits(portfolioId)
  const { data } = query
  const { confirm, dialog } = useConfirm()
  const removeDeposit = useConfirmedDelete(useDeleteDeposit(portfolioId), {
    done: 'deposits.deleted',
    failed: 'deposits.deleteFailed',
  })
  const [opening, setOpening] = useState(false)
  const [renewing, setRenewing] = useState<Deposit | null>(null)
  const isEmpty = data !== undefined && data.length === 0
  // A deposit is renewed once: the server answers 409 to a second renewal.
  const renewed = new Set(data?.map((deposit) => deposit.renewed_from))

  // A matured deposit has no delete button to take focus.
  const remove = (deposit: Deposit) =>
    removeDeposit(
      (data ?? []).filter((item) => item.status !== 'matured').map((item) => item.id),
      deposit.id,
      () =>
        confirm({
          title: t('deposits.deleteConfirm'),
          body: t('deposits.deleteConfirmBody', {
            summary: `${formatMoney(deposit.principal, deposit.currency)} · ${date(deposit.maturity_date)}`,
          }),
          confirmLabel: t('deposits.deleteButton'),
          danger: true,
        }),
    )

  let body = null
  if (isEmpty) {
    body = (
      <EmptyCard title={t('deposits.empty')} className="mb-6">
        <Button data-list-add onClick={() => setOpening(true)}>
          {t('deposits.open')}
        </Button>
      </EmptyCard>
    )
  } else if (data) {
    body = (
      <ListCard
        title={t('deposits.title')}
        addLabel={t('deposits.open')}
        onAdd={() => setOpening(true)}
        columns={COLUMNS}
      >
        {data.map((deposit) => (
          <DepositRow
            key={deposit.id}
            deposit={deposit}
            onRenew={
              deposit.status !== 'active' && !renewed.has(deposit.id)
                ? () => setRenewing(deposit)
                : undefined
            }
            onDelete={() => void remove(deposit)}
          />
        ))}
      </ListCard>
    )
  }

  return (
    <PortfolioListFrame
      portfolioId={portfolioId}
      query={query}
      loadingLabel={t('deposits.loading')}
      errorTitle={t('deposits.loadError')}
    >
      {opening && <DepositDialog portfolioId={portfolioId} onClose={() => setOpening(false)} />}
      {renewing && <RenewDepositDialog deposit={renewing} onClose={() => setRenewing(null)} />}
      {body}
      {dialog}
    </PortfolioListFrame>
  )
}
