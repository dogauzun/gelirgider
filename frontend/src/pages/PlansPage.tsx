import { useState } from 'react'
import { Navigate, useParams } from 'react-router'
import {
  useBookPlan,
  useDeletePlan,
  useDuePlansOf,
  usePlans,
  useSkipPlan,
  type DuePlan,
  type Plan,
} from '../api/plans'
import { PlanDialog } from '../components/PlanDialog'
import { PortfolioHeader } from '../components/PortfolioHeader'
import { ActionsTh } from '../components/ui/ActionsTh'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { Icon } from '../components/ui/Icon'
import { LoadError } from '../components/ui/LoadError'
import { Pill } from '../components/ui/Pill'
import { SkeletonRows } from '../components/ui/Skeleton'
import { ROW_44, TABLE, TD, TD_NUM, TH } from '../components/ui/table'
import { useConfirm } from '../components/ui/useConfirm'
import { t, type StringKey } from '../i18n/strings'
import { formatQuantity } from '../lib/decimal'
import { formatMarketDate } from '../lib/date'
import { formatMoney, formatUnitPrice, MISSING } from '../lib/format'
import { toast } from '../lib/toast'
import { usePortfolioTitle } from '../lib/useTitle'

const STATUS: Record<Plan['status'], { labelKey: StringKey; tone: 'gain' | 'warn' | 'cash' }> = {
  active: { labelKey: 'plans.status.active', tone: 'gain' },
  due: { labelKey: 'plans.status.due', tone: 'warn' },
  ended: { labelKey: 'plans.status.ended', tone: 'cash' },
}

const date = (iso: string) =>
  formatMarketDate(iso, { day: '2-digit', month: 'short', year: 'numeric' })

const COLUMNS: { labelKey: StringKey; className: string }[] = [
  { labelKey: 'transactions.instrument', className: '' },
  { labelKey: 'plans.amount', className: 'text-right' },
  { labelKey: 'plans.day', className: '' },
  { labelKey: 'plans.next', className: 'text-right' },
  { labelKey: 'plans.end', className: 'text-right' },
  { labelKey: 'plans.status', className: '' },
]

// What booking a due buy plan would write: the units and the close, or that
// there is no close to price them at. A deposit plan books its amount, which
// the Amount column shows.
function Proposal({ due }: { due: DuePlan }) {
  if (due.instrument_id === null) return null
  const text =
    due.quantity !== null && due.price !== null
      ? t('plans.proposal', {
          quantity: formatQuantity(due.quantity),
          price: formatUnitPrice(due.price, due.currency),
        })
      : t('plans.noPrice')
  return <span className="mt-1 block text-[12px] text-muted">{text}</span>
}

const summary = (plan: Plan) =>
  `${plan.symbol ?? t('plans.cash')} · ${formatMoney(plan.amount, plan.currency)}`

function PlanRow({
  plan,
  due,
  busy,
  onBook,
  onSkip,
  onEdit,
  onDelete,
}: {
  plan: Plan
  // Set when the due list has the plan.
  due?: DuePlan
  busy: boolean
  onBook: () => void
  onSkip: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const status = STATUS[plan.status]
  // A buy needs a close to price its units at; a deposit books its amount.
  const bookable = due !== undefined && (due.instrument_id === null || due.quantity !== null)
  return (
    <tr className={ROW_44}>
      <td className={TD}>
        {plan.symbol ? (
          <span className="inline-flex items-center gap-2">
            {plan.symbol}
            {plan.market && <Badge>{plan.market}</Badge>}
          </span>
        ) : (
          t('plans.cash')
        )}
      </td>
      <td className={TD_NUM}>{formatMoney(plan.amount, plan.currency)}</td>
      <td className={TD}>{t('plans.dayOfMonth', { day: plan.day_of_month })}</td>
      <td className={TD_NUM}>{date(plan.next_date)}</td>
      <td className={TD_NUM}>{plan.end_date ? date(plan.end_date) : MISSING}</td>
      <td className={TD}>
        <Pill tone={status.tone}>{t(status.labelKey)}</Pill>
        {due && <Proposal due={due} />}
      </td>
      <td className={`${TD} w-0 whitespace-nowrap`}>
        {plan.status === 'due' && (
          <>
            <Button variant="ghost" disabled={!bookable} pending={busy} onClick={onBook}>
              {t('plans.book')}
            </Button>
            <Button variant="ghost" pending={busy} onClick={onSkip}>
              {t('plans.skip')}
            </Button>
          </>
        )}
        <Button variant="ghost" onClick={onEdit}>
          {t('plans.edit')}
        </Button>
        <Button
          variant="ghost"
          iconOnly
          aria-label={t('plans.delete', { summary: summary(plan) })}
          onClick={onDelete}
        >
          <Icon name="trash" />
        </Button>
      </td>
    </tr>
  )
}

// The Planlar tab: the portfolio's monthly saving plans, the due ones with
// what booking them would write, and the dialog that adds or edits one. Per
// portfolio only, like Mevduat; the `all` scope goes back to its summary.
export function PlansPage() {
  const params = useParams()
  if (params.id === 'all') return <Navigate to="/portfolios/all" replace />
  return <PlansBody portfolioId={Number(params.id)} />
}

function PlansBody({ portfolioId }: { portfolioId: number }) {
  usePortfolioTitle(t('plans.title'), portfolioId)
  const { data, isLoading, isError, error, refetch, isFetching } = usePlans(portfolioId)
  const dueList = useDuePlansOf([portfolioId])[0].data
  const deleteMutation = useDeletePlan(portfolioId)
  const bookMutation = useBookPlan()
  const skipMutation = useSkipPlan()
  const { confirm, dialog } = useConfirm()
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Plan | null>(null)
  const isEmpty = data !== undefined && data.length === 0

  async function remove(plan: Plan) {
    const confirmed = await confirm({
      title: t('plans.deleteConfirm'),
      body: t('plans.deleteConfirmBody', { summary: summary(plan) }),
      confirmLabel: t('plans.deleteButton'),
      danger: true,
    })
    if (!confirmed) return
    deleteMutation.mutate(plan.id, {
      onSuccess: () => toast(t('plans.deleted')),
      onError: (mutationError) =>
        toast(t('plans.deleteFailed', { error: mutationError.message }), { error: true }),
    })
  }

  // Books or skips the occurrence the plan has come to.
  function settle(mutation: typeof bookMutation, id: number, doneKey: StringKey) {
    mutation.mutate(id, {
      onSuccess: () => toast(t(doneKey)),
      onError: (mutationError) =>
        toast(t('plans.actionFailed', { error: mutationError.message }), { error: true }),
    })
  }

  let body = null
  if (isEmpty) {
    body = (
      <section className="mb-6 flex flex-col items-center gap-3 card px-6 py-12 text-center">
        <p className="text-[16px] font-semibold text-ink">{t('plans.empty')}</p>
        <Button onClick={() => setAdding(true)}>{t('plans.add')}</Button>
      </section>
    )
  } else if (data) {
    body = (
      <section className="mb-6 overflow-x-auto card">
        <div className="flex items-center justify-between gap-3 border-b border-line px-4 py-3">
          <h2 className="text-[14px] font-semibold text-ink">{t('plans.title')}</h2>
          <Button onClick={() => setAdding(true)}>
            <Icon name="plus" />
            {t('plans.add')}
          </Button>
        </div>
        <table className={TABLE}>
          <thead>
            <tr>
              {COLUMNS.map(({ labelKey, className }) => (
                <th key={labelKey} className={`${TH} ${className}`}>
                  {t(labelKey)}
                </th>
              ))}
              <ActionsTh />
            </tr>
          </thead>
          <tbody>
            {data.map((plan) => (
              <PlanRow
                key={plan.id}
                plan={plan}
                due={dueList?.find((item) => item.plan_id === plan.id)}
                busy={bookMutation.isPending || skipMutation.isPending}
                onBook={() => settle(bookMutation, plan.id, 'plans.booked')}
                onSkip={() => settle(skipMutation, plan.id, 'plans.skipped')}
                onEdit={() => setEditing(plan)}
                onDelete={() => void remove(plan)}
              />
            ))}
          </tbody>
        </table>
      </section>
    )
  }

  return (
    // The page's own button leads, so the header's add-transaction one is quiet.
    <PortfolioHeader portfolioId={portfolioId} add="quiet">
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        {adding && <PlanDialog portfolioId={portfolioId} onClose={() => setAdding(false)} />}
        {editing && (
          <PlanDialog portfolioId={portfolioId} plan={editing} onClose={() => setEditing(null)} />
        )}
        {isLoading && <SkeletonRows label={t('plans.loading')} />}
        {isError && (
          <LoadError
            title={t('plans.loadError')}
            error={error}
            onRetry={() => refetch()}
            retrying={isFetching}
            className="mb-6"
          />
        )}
        {body}
        {dialog}
      </div>
    </PortfolioHeader>
  )
}
