import { useState } from 'react'
import { Navigate, useParams } from 'react-router'
import {
  canBookPlan,
  useBookPlan,
  useDeletePlan,
  useDuePlansOf,
  usePlans,
  useSkipPlan,
  type DuePlan,
  type Plan,
} from '../api/plans'
import { PlanDialog } from '../components/PlanDialog'
import { ListCard, PortfolioListFrame } from '../components/PortfolioListFrame'
import { Badge } from '../components/ui/Badge'
import { Button } from '../components/ui/Button'
import { Icon } from '../components/ui/Icon'
import { Pill } from '../components/ui/Pill'
import { ROW_44, TD, TD_NUM } from '../components/ui/table'
import { useConfirm } from '../components/ui/useConfirm'
import { t, type StringKey } from '../i18n/strings'
import { formatQuantity } from '../lib/decimal'
import { formatMarketDate } from '../lib/date'
import { formatMoney, formatUnitPrice, MISSING } from '../lib/format'
import { useConfirmedDelete } from '../lib/useConfirmedDelete'
import { useAction } from '../lib/useOneAtATime'
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
// there is no close or the amount buys no unit. A deposit plan books its
// amount, which the Amount column shows.
function Proposal({ due }: { due: DuePlan }) {
  if (due.instrument_id === null) return null
  let text = t('plans.noPrice')
  if (due.quantity !== null && due.price !== null) {
    text = canBookPlan(due)
      ? t('plans.proposal', {
          quantity: formatQuantity(due.quantity),
          price: formatUnitPrice(due.price, due.currency),
        })
      : t('plans.noUnit')
  }
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
  const bookable = due !== undefined && canBookPlan(due)
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
          data-delete-row={plan.id}
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
  const query = usePlans(portfolioId)
  const { data } = query
  const dueList = useDuePlansOf([portfolioId])[0].data
  const bookMutation = useBookPlan()
  const skipMutation = useSkipPlan()
  const runAction = useAction()
  const { confirm, dialog } = useConfirm()
  const removePlan = useConfirmedDelete(useDeletePlan(portfolioId), {
    done: 'plans.deleted',
    failed: 'plans.deleteFailed',
  })
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Plan | null>(null)
  const isEmpty = data !== undefined && data.length === 0

  const remove = (plan: Plan) =>
    removePlan(
      (data ?? []).map((item) => item.id),
      plan.id,
      () =>
        confirm({
          title: t('plans.deleteConfirm'),
          body: t('plans.deleteConfirmBody', { summary: summary(plan) }),
          confirmLabel: t('plans.deleteButton'),
          danger: true,
        }),
    )

  // Books or skips the occurrence the plan has come to.
  function settle(mutation: typeof bookMutation, id: number, doneKey: StringKey) {
    runAction(
      () => mutation.mutateAsync(id),
      t(doneKey),
      (error) => t('plans.actionFailed', { error }),
    )
  }

  let body = null
  if (isEmpty) {
    body = (
      <section className="mb-6 flex flex-col items-center gap-3 card px-6 py-12 text-center">
        <p className="text-[16px] font-semibold text-ink">{t('plans.empty')}</p>
        <Button data-list-add onClick={() => setAdding(true)}>
          {t('plans.add')}
        </Button>
      </section>
    )
  } else if (data) {
    body = (
      <ListCard
        title={t('plans.title')}
        addLabel={t('plans.add')}
        onAdd={() => setAdding(true)}
        columns={COLUMNS}
      >
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
      </ListCard>
    )
  }

  return (
    <PortfolioListFrame
      portfolioId={portfolioId}
      query={query}
      loadingLabel={t('plans.loading')}
      errorTitle={t('plans.loadError')}
    >
      {adding && <PlanDialog portfolioId={portfolioId} onClose={() => setAdding(false)} />}
      {editing && (
        <PlanDialog portfolioId={portfolioId} plan={editing} onClose={() => setEditing(null)} />
      )}
      {body}
      {dialog}
    </PortfolioListFrame>
  )
}
