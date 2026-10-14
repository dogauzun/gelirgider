import { Fragment, useEffect, useRef, type ReactNode } from 'react'
import { Link, useLocation } from 'react-router'
import type { ExpectedDividend } from '../api/dividends'
import { t, type StringKey } from '../i18n/strings'
import { SHARE_FORMAT } from '../lib/allocation'
import { ATTENTION, type AttentionItem } from '../lib/attention'
import { formatMarketDate, formatShortMarketDate } from '../lib/date'
import { formatMoney, formatNumber, plTone, signedMoney, signedPercent } from '../lib/format'
import { focusIfLost, neighbourOf } from '../lib/useFocusLater'
import { rangeSearch } from '../lib/useRange'
import { Button } from './ui/Button'
import { Pill } from './ui/Pill'

const GROUPS: [StringKey, AttentionItem['kind'][]][] = [
  [
    'attention.group.data',
    ['missing-rate', 'missing-price', 'stale-manual-prices', 'estimated-benchmark'],
  ],
  ['attention.group.dividends', ['dividend']],
  ['attention.group.today', ['mover']],
  ['attention.group.concentration', ['concentration']],
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

// A dividend item's key. On Tüm portföyler the same dividend can be due in
// two portfolios; their names are unique per user, so the name tells them apart.
const dividendKey = (item: AttentionItem & { kind: 'dividend' }) =>
  `${item.portfolio ?? ''} ${item.dividend.instrument_id} ${item.dividend.date}`

// An action's accessible name. On Tüm portföyler the same symbol's action
// can appear for two portfolios, so the name leads with the portfolio.
const named = (item: AttentionItem, label: string) =>
  item.portfolio ? t('attention.inPortfolio', { portfolio: item.portfolio, label }) : label

// The line of one item: its text, under the portfolio's name on Tüm
// portföyler. The name has a line of its own, so a long one never hides the
// item's own facts; it truncates and carries its full text as a title.
function Label({ item, children }: { item: AttentionItem; children: ReactNode }) {
  const text = <span className="min-w-0 truncate">{children}</span>
  return (
    <span className="flex min-w-0 items-center">
      {item.kind !== 'mover' && item.kind !== 'concentration' && (
        <Dot tone={item.kind === 'dividend' ? 'accent' : 'warn'} />
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

// AttentionCard renders the "Dikkat" list that attentionItems builds; it
// decides nothing itself. The header counts the items that ask for
// something (Veri and Temettü onayı). A mover's symbol links to its position
// when `portfolioId` is given; Tüm portföyler leaves it out. `busy` is a
// dividend request in flight: Ekle and Yoksay ignore clicks meanwhile, so a
// second click cannot book the same dividend twice. They stay enabled, as
// Button's `pending` does, so focus stays on them. `editingPrice`
// is the price editor that Fiyat gir or Güncelle opened, still open.
export function AttentionCard({
  items,
  portfolioId,
  busy = false,
  editingPrice = false,
  onAddDividend,
  onDismissDividend,
  onEditPrice,
}: {
  items: AttentionItem[]
  portfolioId?: number
  busy?: boolean
  editingPrice?: boolean
  onAddDividend: (dividend: ExpectedDividend) => void
  onDismissDividend: (dividend: ExpectedDividend) => void
  onEditPrice: (instrumentId: number) => void
}) {
  const { search } = useLocation()
  // The item last acted on, which button, and that kind's item order then.
  const actedRef = useRef<{
    key: string
    action: 'add' | 'dismiss' | 'price'
    keys: string[]
  } | null>(null)
  const dividendKeys = items.flatMap((item) =>
    item.kind === 'dividend' ? [dividendKey(item)] : [],
  )
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
    const keys = acted.action === 'price' ? priceKeys : dividendKeys
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
  const editPrice = (item: AttentionItem & { instrument_id: number }) => {
    actedRef.current = { key: priceKey(item), action: 'price', keys: priceKeys }
    onEditPrice(item.instrument_id)
  }
  const asking = items.filter(
    (item) => GROUPS[0][1].includes(item.kind) || item.kind === 'dividend',
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
                days: ATTENTION.staleManualDays,
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
                  to={`/portfolios/${portfolioId}/positions/${item.instrument_id}${rangeSearch(search)}`}
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
            <Label item={item}>
              {item.subject === 'cash'
                ? t('summary.cash')
                : item.subject === 'currency'
                  ? t('attention.currencyAssets', { currency: item.label })
                  : item.label}
            </Label>
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
                {group.map((item, index) => (
                  <Fragment key={index}>{line(item)}</Fragment>
                ))}
              </ul>
            </div>
          )
        })
      )}
    </section>
  )
}
