import { Fragment, useEffect, useRef, type ReactNode } from 'react'
import { Link } from 'react-router'
import type { DueDeposit } from '../api/deposits'
import type { ExpectedDividend } from '../api/dividends'
import { canBookPlan, type DuePlan } from '../api/plans'
import { t, type StringKey } from '../i18n/strings'
import { DRIFT_FORMAT, SHARE_FORMAT } from '../lib/allocation'
import type { AttentionItem } from '../lib/attention'
import { formatMarketDate, formatShortMarketDate } from '../lib/date'
import { formatQuantity } from '../lib/decimal'
import {
  formatMoney,
  formatNumber,
  formatUnitPrice,
  plTone,
  signedMoney,
  signedPercent,
} from '../lib/format'
import { focusIfLost, neighbourOf } from '../lib/useFocusLater'
import { usePositionPath } from '../lib/useRange'
import { Button } from './ui/Button'
import { buttonClass } from './ui/buttonClass'
import { Pill } from './ui/Pill'

const GROUPS: [StringKey, AttentionItem['kind'][]][] = [
  [
    'attention.group.data',
    ['missing-rate', 'missing-price', 'stale-manual-prices', 'estimated-benchmark'],
  ],
  ['attention.group.dividends', ['dividend']],
  ['attention.group.deposits', ['due-deposit']],
  ['attention.group.plans', ['due-plan']],
  ['attention.group.watchlist', ['watch-target']],
  ['attention.group.today', ['mover']],
  [
    'attention.group.concentration',
    ['concentration', 'try-cash-share', 'no-hedge', 'allocation-drift'],
  ],
]

function Dot({ tone }: { tone: 'warn' | 'accent' }) {
  return (
    <i
      aria-hidden
      className={`mr-2 inline-block size-[7px] shrink-0 rounded-full ${tone === 'warn' ? 'bg-warn' : 'bg-accent'}`}
    />
  )
}

function Row({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <li className="flex items-center justify-between gap-2 border-t border-line py-1 text-[13px] text-ink first:border-t-0">
      {children}
      {action}
    </li>
  )
}

// A price item's key (Fiyat gir or Güncelle). On Tüm portföyler the same
// instrument can lack a price in two portfolios; the name tells them apart.
const priceKey = (item: AttentionItem & { instrument_id: number }) =>
  `${item.portfolio ?? ''} ${item.instrument_id}`

// A due deposit's key. Deposit ids are global, so the id is enough.
const depositKey = (item: AttentionItem & { kind: 'due-deposit' }) => String(item.deposit.id)

// A due plan's key. Plan ids are global, so the id is enough.
const planKey = (item: AttentionItem & { kind: 'due-plan' }) => String(item.plan.plan_id)

// A dividend item's key. On Tüm portföyler the same dividend can be due in
// two portfolios; their names are unique per user, so the name tells them apart.
const dividendKey = (item: AttentionItem & { kind: 'dividend' }) =>
  `${item.portfolio ?? ''} ${item.dividend.instrument_id} ${item.dividend.date}`

// An action's accessible name. On Tüm portföyler the same symbol's action
// can appear for two portfolios, so the name leads with the portfolio.
const named = (item: AttentionItem, label: string) =>
  item.portfolio ? t('attention.inPortfolio', { portfolio: item.portfolio, label }) : label

function concentrationLabel(item: AttentionItem & { kind: 'concentration' }) {
  if (item.subject === 'cash') return t('summary.cash')
  if (item.subject === 'currency') return t('attention.currencyAssets', { currency: item.label })
  return item.label
}

// The text of the items that are one sentence and have no action.
function noticeText(
  item: AttentionItem & { kind: 'try-cash-share' | 'no-hedge' | 'allocation-drift' },
) {
  if (item.kind === 'try-cash-share') {
    return t('attention.tryCashShare', { share: formatNumber(item.share, SHARE_FORMAT) })
  }
  if (item.kind === 'no-hedge') return t('attention.noHedge')
  return t('attention.allocationDrift', {
    assetClass: item.assetClass,
    drift: formatNumber(item.drift, DRIFT_FORMAT),
  })
}

// The line of one item: its text, under the portfolio's name on Tüm
// portföyler. The name has a line of its own, so a long one never hides the
// item's own facts; it truncates and carries its full text as a title.
function Label({ item, children }: { item: AttentionItem; children: ReactNode }) {
  const text = <span className="min-w-0 truncate">{children}</span>
  return (
    <span className="flex min-w-0 items-center">
      {item.kind !== 'mover' &&
        item.kind !== 'concentration' &&
        item.kind !== 'try-cash-share' &&
        item.kind !== 'no-hedge' &&
        item.kind !== 'allocation-drift' && (
          <Dot
            tone={
              item.kind === 'dividend' ||
              item.kind === 'due-deposit' ||
              item.kind === 'due-plan' ||
              item.kind === 'watch-target'
                ? 'accent'
                : 'warn'
            }
          />
        )}
      {item.portfolio ? (
        <span className="flex min-w-0 flex-col">
          <span className="truncate text-[11px] text-muted" title={item.portfolio}>
            {item.portfolio}
          </span>
          {text}
        </span>
      ) : (
        text
      )}
    </span>
  )
}

// A due deposit's line: its net, with Vadeyi kapat.
function DepositLine({
  item,
  busy,
  onClose,
}: {
  item: AttentionItem & { kind: 'due-deposit' }
  busy: boolean
  onClose: () => void
}) {
  const d = item.deposit
  const principal = formatMoney(d.principal, d.currency)
  const date = formatMarketDate(d.maturity_date)
  return (
    <Row
      action={
        <Button
          variant="secondary"
          size="sm"
          className="shrink-0"
          aria-label={named(item, t('attention.deposit.closeLabel', { principal, date }))}
          pending={busy}
          data-attention-action={`close ${depositKey(item)}`}
          onClick={onClose}
        >
          {t('attention.deposit.close')}
        </Button>
      }
    >
      <Label item={item}>
        <span className="font-semibold">{formatMoney(d.net, d.currency)}</span>{' '}
        {t('attention.deposit.detail', {
          gross: formatMoney(d.gross, d.currency),
          stopaj: formatMoney(d.stopaj, d.currency),
        })}{' '}
        · {formatShortMarketDate(d.maturity_date)}
      </Label>
    </Row>
  )
}

// A due plan's line: Kaydet books it (a buy with no close or no unit cannot be booked),
// Atla moves the plan on without booking.
function PlanLine({
  item,
  busy,
  onSettle,
}: {
  item: AttentionItem & { kind: 'due-plan' }
  busy: boolean
  onSettle: (action: 'book' | 'skip') => void
}) {
  const d = item.plan
  const symbol = d.symbol ?? t('plans.cash')
  const amount = formatMoney(d.amount, d.currency)
  const label = { symbol, amount, date: formatMarketDate(d.date) }
  let detail = t('attention.plan.noPrice')
  if (d.quantity !== null && d.price !== null) {
    detail = canBookPlan(d)
      ? t('plans.proposal', {
          quantity: formatQuantity(d.quantity),
          price: formatUnitPrice(d.price, d.currency),
        })
      : t('attention.plan.noUnit')
  }
  return (
    <Row
      action={
        <span className="flex shrink-0 gap-1">
          <Button
            variant="secondary"
            size="sm"
            className="shrink-0"
            aria-label={named(item, t('attention.plan.bookLabel', label))}
            pending={busy}
            disabled={!canBookPlan(d)}
            data-attention-action={`book ${planKey(item)}`}
            onClick={() => onSettle('book')}
          >
            {t('plans.book')}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            className="shrink-0"
            aria-label={named(item, t('attention.plan.skipLabel', label))}
            pending={busy}
            data-attention-action={`skip ${planKey(item)}`}
            onClick={() => onSettle('skip')}
          >
            {t('plans.skip')}
          </Button>
        </span>
      }
    >
      <Label item={item}>
        <span className="font-semibold">{symbol}</span> · {amount} · {formatShortMarketDate(d.date)}
        {d.instrument_id !== null && ` · ${detail}`}
        {d.missed > 1 && ` · ${t('attention.plan.missed', { count: d.missed })}`}
      </Label>
    </Row>
  )
}

// A reached watchlist target's line: the symbol, which side of its target the
// close is on, and a link to the list. It asks for no click, so it stays out of `asking`.
function WatchLine({ item }: { item: AttentionItem & { kind: 'watch-target' } }) {
  const w = item.item
  return (
    <Row
      action={
        <Link
          to="/watchlist"
          className={buttonClass({ variant: 'secondary', size: 'sm', className: 'shrink-0' })}
        >
          {t('attention.watch.open')}
        </Link>
      }
    >
      <Label item={item}>
        <span className="font-semibold">{w.symbol}</span>{' '}
        {t(w.target_side === 'below' ? 'attention.watch.below' : 'attention.watch.above', {
          target: formatUnitPrice(w.target_price!, w.currency),
          last: formatUnitPrice(w.last_price!, w.currency),
        })}
      </Label>
    </Row>
  )
}

// AttentionCard renders the "Dikkat" list that attentionItems builds; it
// decides nothing itself. The header counts the items that ask for
// something (Veri, Temettü onayı, Vadesi gelen mevduat and Planlı alımlar). A mover's symbol links to its position
// when `portfolioId` is given; Tüm portföyler leaves it out. `busy` is a
// dividend, deposit or plan request in flight: Ekle, Yoksay, Vadeyi kapat, Kaydet and Atla ignore clicks
// meanwhile, so a second click cannot book the same one twice. They stay enabled, as
// Button's `pending` does, so focus stays on them. `editingPrice`
// is the price editor that Fiyat gir or Güncelle opened, still open.
export function AttentionCard({
  items,
  portfolioId,
  busy = false,
  editingPrice = false,
  onAddDividend,
  onDismissDividend,
  onCloseDeposit,
  onBookPlan,
  onSkipPlan,
  onEditPrice,
}: {
  items: AttentionItem[]
  portfolioId?: number
  busy?: boolean
  editingPrice?: boolean
  onAddDividend: (dividend: ExpectedDividend) => void
  onDismissDividend: (dividend: ExpectedDividend) => void
  onCloseDeposit: (deposit: DueDeposit) => void
  onBookPlan: (plan: DuePlan) => void
  onSkipPlan: (plan: DuePlan) => void
  onEditPrice: (instrumentId: number) => void
}) {
  const positionPath = usePositionPath()
  // The item last acted on, which button, and that kind's item order then.
  const actedRef = useRef<{
    key: string
    action: 'add' | 'dismiss' | 'price' | 'close' | 'book' | 'skip'
    keys: string[]
  } | null>(null)
  const dividendKeys = items.flatMap((item) =>
    item.kind === 'dividend' ? [dividendKey(item)] : [],
  )
  const depositKeys = items.flatMap((item) =>
    item.kind === 'due-deposit' ? [depositKey(item)] : [],
  )
  const planKeys = items.flatMap((item) => (item.kind === 'due-plan' ? [planKey(item)] : []))
  const priceKeys = items.flatMap((item) =>
    item.kind === 'missing-price' || item.kind === 'stale-manual-prices' ? [priceKey(item)] : [],
  )

  // Ekle and Yoksay remove the focused button once the expected list has
  // refetched, dropping focus to <body>; a price saved from Fiyat
  // gir or Güncelle does the same once the summary has. Then, as
  // the Temettü tab does, move focus to the same button of the
  // next item, else the previous one's, else the card's heading -- unless
  // the user has already moved it elsewhere. A failed request or a cancelled
  // editor leaves the item: forget it.
  useEffect(() => {
    const acted = actedRef.current
    if (busy || editingPrice || !acted) return
    actedRef.current = null
    const keys = {
      price: priceKeys,
      close: depositKeys,
      add: dividendKeys,
      dismiss: dividendKeys,
      book: planKeys,
      skip: planKeys,
    }[acted.action]
    if (keys.includes(acted.key)) return
    const neighbour = neighbourOf(acted.keys, acted.key, (key) => keys.includes(key))
    // Compared, not in a selector: a portfolio's name may hold a quote.
    focusIfLost(
      neighbour
        ? [...document.querySelectorAll<HTMLElement>('[data-attention-action]')].find(
            (el) => el.dataset.attentionAction === `${acted.action} ${neighbour}`,
          )
        : document.querySelector<HTMLElement>('[data-attention-heading]'),
    )
  })

  const act = (item: AttentionItem & { kind: 'dividend' }, action: 'add' | 'dismiss') => {
    actedRef.current = { key: dividendKey(item), action, keys: dividendKeys }
    if (action === 'add') onAddDividend(item.dividend)
    else onDismissDividend(item.dividend)
  }
  const close = (item: AttentionItem & { kind: 'due-deposit' }) => {
    actedRef.current = { key: depositKey(item), action: 'close', keys: depositKeys }
    onCloseDeposit(item.deposit)
  }
  const settle = (item: AttentionItem & { kind: 'due-plan' }, action: 'book' | 'skip') => {
    actedRef.current = { key: planKey(item), action, keys: planKeys }
    if (action === 'book') onBookPlan(item.plan)
    else onSkipPlan(item.plan)
  }
  const editPrice = (item: AttentionItem & { instrument_id: number }) => {
    actedRef.current = { key: priceKey(item), action: 'price', keys: priceKeys }
    onEditPrice(item.instrument_id)
  }
  const asking = items.filter(
    (item) =>
      GROUPS[0][1].includes(item.kind) ||
      item.kind === 'dividend' ||
      item.kind === 'due-deposit' ||
      item.kind === 'due-plan',
  ).length

  const line = (item: AttentionItem): ReactNode => {
    switch (item.kind) {
      case 'missing-rate':
        return (
          <Row>
            <Label item={item}>
              {item.positions > 0
                ? t('attention.missingRatePositions', {
                    currency: item.currency,
                    count: item.positions,
                  })
                : t('attention.missingRate', { currency: item.currency })}
            </Label>
          </Row>
        )
      case 'missing-price':
        return (
          <Row
            action={
              <Button
                variant="secondary"
                size="sm"
                className="shrink-0"
                aria-label={named(item, t('prices.enterLabel', { symbol: item.symbol }))}
                data-attention-action={`price ${priceKey(item)}`}
                onClick={() => editPrice(item)}
              >
                {t('prices.enter')}
              </Button>
            }
          >
            <Label item={item}>{t('attention.missingPrice', { symbol: item.symbol })}</Label>
          </Row>
        )
      case 'stale-manual-prices':
        return (
          <Row
            action={
              <Button
                variant="secondary"
                size="sm"
                className="shrink-0"
                aria-label={named(item, t('attention.updateLabel', { symbol: item.symbol }))}
                data-attention-action={`price ${priceKey(item)}`}
                onClick={() => editPrice(item)}
              >
                {t('attention.update')}
              </Button>
            }
          >
            <Label item={item}>
              {t('attention.staleManual', {
                count: item.count,
                days: item.days,
              })}
            </Label>
          </Row>
        )
      case 'estimated-benchmark':
        return (
          <Row>
            <Label item={item}>
              {t('attention.estimatedBenchmark', { date: formatMarketDate(item.from) })}
            </Label>
          </Row>
        )
      case 'dividend': {
        const d = item.dividend
        const date = formatMarketDate(d.date)
        return (
          <Row
            action={
              <span className="flex shrink-0 gap-1">
                <Button
                  variant="secondary"
                  size="sm"
                  className="shrink-0"
                  aria-label={named(
                    item,
                    t('dividends.expected.addLabel', { symbol: d.symbol, date }),
                  )}
                  pending={busy}
                  data-attention-action={`add ${dividendKey(item)}`}
                  onClick={() => act(item, 'add')}
                >
                  {t('dividends.expected.add')}
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  className="shrink-0"
                  aria-label={named(
                    item,
                    t('dividends.expected.dismissLabel', { symbol: d.symbol, date }),
                  )}
                  pending={busy}
                  data-attention-action={`dismiss ${dividendKey(item)}`}
                  onClick={() => act(item, 'dismiss')}
                >
                  {t('dividends.expected.dismiss')}
                </Button>
              </span>
            }
          >
            <Label item={item}>
              <span className="font-semibold">{d.symbol}</span> · {formatMoney(d.net, d.currency)} ·{' '}
              {formatShortMarketDate(d.date)}
            </Label>
          </Row>
        )
      }
      case 'due-deposit':
        return <DepositLine item={item} busy={busy} onClose={() => close(item)} />
      case 'due-plan':
        return <PlanLine item={item} busy={busy} onSettle={(action) => settle(item, action)} />
      case 'watch-target':
        return <WatchLine item={item} />
      case 'mover':
        return (
          <Row
            action={
              <span className={`shrink-0 whitespace-nowrap ${plTone(item.amount)}`}>
                {signedMoney(item.amount, item.currency)}
                {item.ratio !== null && ` · ${signedPercent(item.ratio)}`}
              </span>
            }
          >
            <Label item={item}>
              {portfolioId === undefined ? (
                <span className="font-semibold">{item.symbol}</span>
              ) : (
                <Link
                  to={positionPath(portfolioId, item.instrument_id)}
                  className="font-semibold hover:underline"
                >
                  {item.symbol}
                </Link>
              )}
            </Label>
          </Row>
        )
      case 'concentration':
        return (
          <Row
            action={
              <span className="shrink-0 text-muted">{formatNumber(item.share, SHARE_FORMAT)}</span>
            }
          >
            <Label item={item}>{concentrationLabel(item)}</Label>
          </Row>
        )
      case 'try-cash-share':
      case 'no-hedge':
      case 'allocation-drift':
        return (
          <Row>
            <Label item={item}>{noticeText(item)}</Label>
          </Row>
        )
    }
  }

  return (
    <section className="card p-4" aria-label={t('attention.title')}>
      <div className="flex items-center justify-between gap-2">
        {/* Where focus goes once an action removed the last item of its kind. */}
        <h2 data-attention-heading tabIndex={-1} className="text-sm font-medium text-ink">
          {t('attention.title')}
        </h2>
        {asking > 0 && (
          <span
            role="img"
            aria-label={t('attention.count', { count: asking })}
            title={t('attention.count', { count: asking })}
          >
            <Pill tone="warn">{asking}</Pill>
          </span>
        )}
      </div>
      {items.length === 0 ? (
        <p className="mt-2 text-[13px] text-muted">{t('attention.allClear')}</p>
      ) : (
        GROUPS.map(([heading, kinds]) => {
          const group = items.filter((item) => kinds.includes(item.kind))
          if (group.length === 0) return null
          return (
            <div key={heading}>
              <h3 className="mt-3 mb-1 text-[10.5px] font-semibold tracking-wide text-muted uppercase">
                {t(heading)}
              </h3>
              <ul>
                {/* A plan stays listed after Kaydet or Atla and may re-sort, so its row
                    is keyed by the plan to keep the focused button with it. */}
                {group.map((item, index) => (
                  <Fragment key={item.kind === 'due-plan' ? `plan ${planKey(item)}` : index}>
                    {line(item)}
                  </Fragment>
                ))}
              </ul>
            </div>
          )
        })
      )}
    </section>
  )
}
