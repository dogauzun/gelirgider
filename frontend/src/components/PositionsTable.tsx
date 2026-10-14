import { useRef, useState, type HTMLAttributes } from 'react'
import { Link, useLocation } from 'react-router'
import { usePortfolios } from '../api/portfolios'
import type { PortfolioScope, Position, Summary } from '../api/summary'
import { getLocale, t, type StringKey } from '../i18n/strings'
import { buildAiPrompt } from '../lib/aiPrompt'
import { formatMarketDate, formatShortMarketDate, isStale, todayIstanbul } from '../lib/date'
import { formatQuantity } from '../lib/decimal'
import {
  formatMoney,
  formatNumber,
  formatUnitPrice,
  MISSING,
  plTone,
  signedMoney,
  signedPercent,
} from '../lib/format'
import {
  dayRatio,
  returnPercent,
  returnRatio,
  unrealized,
  weightOf,
  weightTotal,
} from '../lib/positions'
import { useIsDesktop } from '../lib/useIsDesktop'
import { rangeSearch, useRange } from '../lib/useRange'
import { toast } from '../lib/toast'
import { ActionsTh } from './ui/ActionsTh'
import { Badge } from './ui/Badge'
import { Button } from './ui/Button'
import { inputClass } from './ui/inputClass'
import { Icon } from './ui/Icon'
import { Menu, MenuItem } from './ui/Menu'
import { Pill } from './ui/Pill'
import { PositionDialog } from './PositionDialog'
import { Segmented } from './ui/Segmented'
import { SortHeader } from './ui/SortHeader'
import { nextSort, TABLE, TD, TD_NUM, ROW_40, type Sort } from './ui/table'
import { TAP_Y } from './ui/tap'
import { Toggletip } from './ui/Toggletip'
import { WeightBar } from './ui/WeightBar'

export type TradeType = 'buy' | 'sell'

// The move as the change in the position's own currency, its percent, and
// the two dates.
function dayChange(
  position: Position,
): { money: string; percent: string | null; title: string } | null {
  const { day_change: change, prev_price_date: from, price_date: to } = position
  if (change == null || from == null || to == null) return null
  const ratio = dayRatio(position)
  return {
    money: signedMoney(change, position.currency),
    percent: ratio === null ? null : signedPercent(ratio),
    title: t('summary.dayChangeTitle', {
      from: formatShortMarketDate(from),
      to: formatShortMarketDate(to),
    }),
  }
}

// One line: the amount, then its percent.
function plText(amount: string | null, percent: string | null, currency: string): string {
  if (amount === null) return MISSING
  return percent ? `${signedMoney(amount, currency)} · ${percent}` : signedMoney(amount, currency)
}

// The sortable columns, in order, with what each sorts by; numbers go in as
// Number(), which is only for ordering. A missing value sorts last either way.
type SortKey =
  | 'symbol'
  | 'weight'
  | 'quantity'
  | 'avgCost'
  | 'price'
  | 'day'
  | 'value'
  | 'pl'
  | 'realized'
  | 'realizedPercent'
type Column = { key: SortKey; label: StringKey; value: (p: Position) => string | null }
const COLUMNS: Column[] = [
  { key: 'symbol', label: 'summary.symbol', value: (p) => p.symbol },
  { key: 'weight', label: 'summary.weight', value: (p) => p.market_value_reporting },
  { key: 'quantity', label: 'summary.quantity', value: (p) => p.quantity },
  { key: 'avgCost', label: 'summary.avgCost', value: (p) => p.avg_cost },
  { key: 'price', label: 'summary.lastPrice', value: (p) => (p.has_price ? p.last_price : null) },
  // The percent, as the cell shows it: amounts would rank a large holding's
  // small move above a small holding's large one.
  {
    key: 'day',
    label: 'summary.dayChange',
    value: (p) => {
      const ratio = dayRatio(p)
      return ratio === null ? null : String(ratio)
    },
  },
  { key: 'value', label: 'summary.value', value: (p) => p.market_value_reporting },
  { key: 'pl', label: 'summary.pl', value: (p) => unrealized(p).amount },
]

// The closed view's columns (GLG-1101). Realized P/L sorts by its reporting
// amount, as K/Z does: the cells are in each position's own currency.
const CLOSED_COLUMNS: Column[] = [
  { key: 'symbol', label: 'summary.symbol', value: (p) => p.symbol },
  { key: 'realized', label: 'summary.realizedPl', value: (p) => p.realized_pl_reporting },
  {
    key: 'realizedPercent',
    label: 'summary.realizedPlPercent',
    value: (p) => {
      const ratio = realizedRatio(p)
      return ratio === null ? null : String(ratio)
    },
  },
]

function sortPositions(positions: Position[], columns: Column[], sort: Sort<SortKey>): Position[] {
  const pick = columns.find((c) => c.key === sort.key)!.value
  const sign = sort.dir === 'asc' ? 1 : -1
  return [...positions].sort((a, b) => {
    const x = pick(a)
    const y = pick(b)
    if (x === null || y === null) return x === y ? 0 : x === null ? 1 : -1
    return sort.key === 'symbol'
      ? sign * x.localeCompare(y, getLocale())
      : sign * (Number(x) - Number(y))
  })
}

// A manual price (GLG-134) or a fetched one too old for its market (GLG-557),
// as a small pill after the price with the explanation in its toggletip.
function pricePill(position: Position) {
  const date = position.price_date
  if (date === null) return null
  if (position.price_source === 'manual') {
    return (
      <Toggletip tip={t('prices.manualTitle', { date: formatMarketDate(date) })}>
        <Pill tone="warn">{t('prices.manual')}</Pill>
      </Toggletip>
    )
  }
  if (!isStale(position.market, date, todayIstanbul())) return null
  return (
    <Toggletip tip={t('prices.staleTitle', { date: formatMarketDate(date) })}>
      <Pill tone="warn">{t('prices.stale')}</Pill>
    </Toggletip>
  )
}

// The symbol opens the position page (GLG-549) with the chart's range; the
// read-only `all` scope has no position page, so there it is plain text (ADR
// 0014). A phone row is plain text too: the row opens the position dialog,
// whose title links to the page (GLG-1105).
function SymbolLink({
  position,
  portfolioId,
  plain = false,
}: {
  position: Position
  portfolioId: PortfolioScope
  plain?: boolean
}) {
  const { search } = useLocation()
  if (plain || portfolioId === 'all') {
    return <span className="shrink-0 font-semibold text-ink">{position.symbol}</span>
  }
  return (
    <Link
      to={`/portfolios/${portfolioId}/positions/${position.instrument_id}${rangeSearch(search)}`}
      className="shrink-0 font-semibold text-ink hover:underline"
    >
      {position.symbol}
    </Link>
  )
}

// What makes a row open the position dialog (GLG-1105): a click or tap
// anywhere but its links and buttons (the symbol, a toggletip, the ⋮
// trigger), and Enter on the focused row. The ⋮ menu's popup is portaled out
// of the row, so its cell stops its clicks instead.
function opener(onOpen: () => void): HTMLAttributes<HTMLElement> {
  return {
    tabIndex: 0,
    onClick: (event) => {
      if (!(event.target as Element).closest('a, button')) onOpen()
    },
    onKeyDown: (event) => {
      if (event.key === 'Enter' && event.target === event.currentTarget) onOpen()
    },
  }
}

const OPENER =
  'cursor-pointer outline-accent hover:bg-raised-hover focus-visible:outline-2 focus-visible:-outline-offset-2'

// The badges after a name: the currency when it is not the reporting one, a
// split (GLG-276), and a negative quantity (GLG-25 keeps those visible).
function Tags({ position, reporting }: { position: Position; reporting: string }) {
  const splits = position.splits ?? []
  const splitLabel =
    splits.length > 0
      ? t('summary.splitLabel', {
          splits: splits
            .map(
              (s) =>
                `${formatMarketDate(s.date)} · ×${formatNumber(s.ratio, { maximumFractionDigits: 20 })}`,
            )
            .join(', '),
        })
      : null
  return (
    <>
      {position.currency !== reporting && <Badge>{position.currency}</Badge>}
      {splitLabel && <Badge label={splitLabel}>{t('summary.split')}</Badge>}
      {Number(position.quantity) < 0 && <Pill tone="loss">{t('summary.negativeQuantity')}</Pill>}
    </>
  )
}

// Symbol, name and tags on one line; the name gives up its width first and
// shows in full on hover.
function Instrument({
  position,
  portfolioId,
  reporting,
}: {
  position: Position
  portfolioId: PortfolioScope
  reporting: string
}) {
  return (
    <div className="flex min-w-0 items-center gap-1.5">
      <SymbolLink position={position} portfolioId={portfolioId} />
      <span className="min-w-0 truncate text-[12px] text-muted" title={position.name}>
        {position.name}
      </span>
      <Tags position={position} reporting={reporting} />
    </div>
  )
}

type Handlers = {
  onTrade?: (type: TradeType, position: Position) => void
  onEditPrice?: (position: Position, row: HTMLElement | null) => void
  onRemovePrice?: (position: Position) => void
}

type Opens = { onOpen: (position: Position) => void }

function PositionRow({
  position,
  portfolioId,
  reporting,
  total,
  largest,
  onTrade,
  onEditPrice,
  onRemovePrice,
  onOpen,
}: {
  position: Position
  portfolioId: PortfolioScope
  reporting: string
  total: number
  // The largest weight in the table: its bar is full, the others scale to it.
  largest: number
} & Handlers &
  Opens) {
  const rowRef = useRef<HTMLTableRowElement>(null)
  const weight = weightOf(position, total)
  const day = dayChange(position)
  const pl = unrealized(position)
  const foreign = position.currency !== reporting
  return (
    <tr ref={rowRef} className={`${ROW_40} ${OPENER}`} {...opener(() => onOpen(position))}>
      {/* Takes the width the figures leave, so the name truncates rather
          than push the table past the card at 1280px. */}
      <td className={`${TD} w-full max-w-0`}>
        <Instrument position={position} portfolioId={portfolioId} reporting={reporting} />
      </td>
      <td className={TD_NUM}>
        {weight === null ? MISSING : <WeightBar share={weight} largest={largest} />}
      </td>
      <td className={TD_NUM}>{formatQuantity(position.quantity)}</td>
      <td className={TD_NUM}>{formatUnitPrice(position.avg_cost, position.currency, 6)}</td>
      <td className={TD_NUM}>
        {position.has_price ? (
          <span className="inline-flex items-center gap-1.5">
            {formatUnitPrice(position.last_price as string, position.currency)}
            {pricePill(position)}
          </span>
        ) : (
          MISSING
        )}
      </td>
      <td className={`${TD_NUM} ${plTone(position.day_change)}`}>
        {day && (
          <Toggletip tip={`${day.money} · ${day.title}`}>{day.percent ?? day.money}</Toggletip>
        )}
      </td>
      <td
        className={`${TD_NUM} font-medium`}
        title={foreign ? formatMoney(position.market_value, position.currency) : undefined}
      >
        {position.market_value_reporting !== null
          ? formatMoney(position.market_value_reporting, reporting)
          : MISSING}
      </td>
      <td className={`${TD_NUM} ${plTone(pl.amount)}`}>
        {plText(pl.amount, pl.percent, reporting)}
      </td>
      {portfolioId !== 'all' && (
        <td className={`${TD} w-0`} onClick={(event) => event.stopPropagation()}>
          <Menu
            trigger={
              <Button
                variant="ghost"
                iconOnly
                aria-label={t('summary.rowMenu', { symbol: position.symbol })}
              >
                <Icon name="dots" />
              </Button>
            }
          >
            <MenuItem onSelect={() => onTrade?.('buy', position)}>{t('summary.buy')}</MenuItem>
            <MenuItem onSelect={() => onTrade?.('sell', position)}>{t('summary.sell')}</MenuItem>
            <MenuItem onSelect={() => onEditPrice?.(position, rowRef.current)}>
              {t('prices.enter')}
            </MenuItem>
            {position.price_source === 'manual' && (
              <MenuItem tone="danger" onSelect={() => onRemovePrice?.(position)}>
                {t('prices.removeManual')}
              </MenuItem>
            )}
          </Menu>
        </td>
      )}
    </tr>
  )
}

// A closed position's realized P/L over the cost of the shares it sold
// (GLG-1100), by the one return rule.
function realizedRatio(position: Position): number | null {
  return returnRatio(position.realized_pl, position.realized_cost)
}

// A closed position (GLG-218): only the realized P/L it left behind, and its
// percent.
function ClosedRow({
  position,
  portfolioId,
  reporting,
  onOpen,
}: {
  position: Position
  portfolioId: PortfolioScope
  reporting: string
} & Opens) {
  return (
    <tr className={`${ROW_40} ${OPENER}`} {...opener(() => onOpen(position))}>
      <td className={`${TD} w-full max-w-0`}>
        <Instrument position={position} portfolioId={portfolioId} reporting={reporting} />
      </td>
      <td className={`${TD_NUM} ${plTone(position.realized_pl)}`}>
        {signedMoney(position.realized_pl, position.currency)}
      </td>
      <td className={`${TD_NUM} ${plTone(position.realized_pl)}`}>
        {returnPercent(position.realized_pl, position.realized_cost) ?? MISSING}
      </td>
    </tr>
  )
}

// The left side of a phone row, open or closed: symbol, tags and name.
function ListItemName({
  position,
  portfolioId,
  reporting,
}: {
  position: Position
  portfolioId: PortfolioScope
  reporting: string
}) {
  return (
    <div className="min-w-0 flex-1">
      <div className="flex flex-wrap items-center gap-1.5 text-[13px]">
        <SymbolLink position={position} portfolioId={portfolioId} plain />
        <Tags position={position} reporting={reporting} />
      </div>
      <p className="truncate text-[12px] text-muted" title={position.name}>
        {position.name}
      </p>
    </div>
  )
}

// The phone row: the table has no honest layout under md, so it is a list
// rather than a horizontally scrolling table (ADR 0005). The symbol and name
// on the left, value, P/L and day change on the right; the whole row opens
// the position dialog (GLG-1105), which carries the rest.
function PositionListItem({
  position,
  portfolioId,
  reporting,
  onOpen,
}: {
  position: Position
  portfolioId: PortfolioScope
  reporting: string
} & Opens) {
  const pl = unrealized(position)
  const day = dayChange(position)
  return (
    <li
      className={`flex items-start justify-between gap-3 border-b border-line px-4 py-2 last:border-0 ${OPENER}`}
      {...opener(() => onOpen(position))}
    >
      <ListItemName position={position} portfolioId={portfolioId} reporting={reporting} />
      <div className="shrink-0 text-right tabular-nums">
        <div className="font-medium text-ink">
          {position.market_value_reporting !== null
            ? formatMoney(position.market_value_reporting, reporting)
            : MISSING}
        </div>
        <div className={`text-[12px] ${plTone(pl.amount)}`}>
          {pl.amount !== null ? signedMoney(pl.amount, reporting) : MISSING}
          {pl.amount !== null && pl.percent && <span className="ml-1">{pl.percent}</span>}
        </div>
        {day && (
          <Toggletip tip={day.title} className={`text-[12px] ${plTone(position.day_change)}`}>
            {`${t('summary.dayChange')} ${day.money}${day.percent ? ` (${day.percent})` : ''}`}
          </Toggletip>
        )}
      </div>
    </li>
  )
}

function ClosedListItem({
  position,
  portfolioId,
  reporting,
  onOpen,
}: {
  position: Position
  portfolioId: PortfolioScope
  reporting: string
} & Opens) {
  const percent = returnPercent(position.realized_pl, position.realized_cost)
  return (
    <li
      className={`flex items-center justify-between gap-3 border-b border-line px-4 py-2 last:border-0 ${OPENER}`}
      {...opener(() => onOpen(position))}
    >
      <ListItemName position={position} portfolioId={portfolioId} reporting={reporting} />
      <span className={`shrink-0 text-[13px] tabular-nums ${plTone(position.realized_pl)}`}>
        {signedMoney(position.realized_pl, position.currency)}
        {percent && <span className="ml-1">{percent}</span>}
      </span>
    </li>
  )
}

const VIEWS = ['open', 'closed'] as const
type View = (typeof VIEWS)[number]

// Turkish letters to ASCII, both cases, as the server's instrument search
// folds them (turkishFold in backend/internal/instrument). Lowercasing in a
// locale cannot do it: "BIMAS" becomes "bımas" in tr and "HİSSE" "hi̇sse"
// (with a combining dot) in en, which an ASCII query never matches.
const TURKISH_ASCII: Record<string, string> = {
  ç: 'c',
  Ç: 'C',
  ğ: 'g',
  Ğ: 'G',
  ı: 'i',
  İ: 'I',
  ö: 'o',
  Ö: 'O',
  ş: 's',
  Ş: 'S',
  ü: 'u',
  Ü: 'U',
}

function searchFold(text: string): string {
  return text.replace(/[çÇğĞıİöÖşŞüÜ]/g, (c) => TURKISH_ASCII[c]).toLowerCase()
}

// The positions card of Özet and Tüm portföyler (GLG-933): 40px rows sorted
// by a header click, Değer descending first (Kapalı: Gerçekleşmiş K/Z,
// GLG-1101); a row menu with Al, Sat, Fiyat gir and, for a manual price, Elle
// fiyatı kaldır; an Açık | Kapalı switch; a search on symbol and name
// (GLG-1102); a row opens the position dialog (GLG-1105), which flips
// through the rows as shown.
// The page owns what the menu and the dialog's buttons open, through the
// handlers. The `all` scope is read-only: no menu, symbols as plain text
// (ADR 0014). Realized P/L and the currency gain per position are on the
// position page.
export function PositionsTable({
  summary,
  portfolioId,
  onTrade,
  onEditPrice,
  onRemovePrice,
}: {
  summary: Summary
  portfolioId: PortfolioScope
} & Handlers) {
  // Each view keeps its own sort, so a closed-only key never reaches Açık.
  const [sorts, setSorts] = useState<Record<View, Sort<SortKey>>>({
    open: { key: 'value', dir: 'desc' },
    closed: { key: 'realized', dir: 'desc' },
  })
  const [view, setView] = useState<View>('open')
  const [search, setSearch] = useState('')
  // By id, so the dialog shows the position's figures after a refetch.
  const [openId, setOpenId] = useState<number | null>(null)
  const [range] = useRange()
  const desktop = useIsDesktop()
  const reporting = summary.currency
  // A quantity of 0 alone is not closed (GLG-827).
  const open = sortPositions(
    summary.positions.filter((p) => !p.closed),
    COLUMNS,
    sorts.open,
  )
  const closed = sortPositions(
    summary.positions.filter((p) => p.closed),
    CLOSED_COLUMNS,
    sorts.closed,
  )
  // Both sides folded to ASCII, so "bimas" finds BIMAS and "hisse" finds
  // "HİSSE SENEDİ" in either locale (GLG-1143). The filter only narrows the
  // rows shown: the switch, the weight bars and the AI copy still go by every
  // position.
  const query = searchFold(search.trim())
  const matches = (p: Position) =>
    searchFold(p.symbol).includes(query) || searchFold(p.name).includes(query)
  const openRows = query ? open.filter(matches) : open
  const closedRows = query ? closed.filter(matches) : closed
  // The total row is the server's figure for every open position (GLG-976),
  // which would read as the sum of the matching rows, so it hides meanwhile.
  const showTotal = open.length > 0 && !query
  const total = weightTotal(summary)
  const largest = Math.max(...open.map((p) => weightOf(p, total) ?? 0))

  // The server's own totals (GLG-976): the rows are rounded one by one, so
  // their sum can be a cent off the KPI cards. With no row in the reporting
  // currency the value total is unknown, not the server's zero.
  const totalValue = open.some((p) => p.market_value_reporting !== null)
    ? summary.positions_value
    : null
  const totalPercent = returnPercent(summary.unrealized_pl, summary.cost_basis)
  const totalLabel = `${t('summary.total')} · ${t('summary.positionsCount', {
    count: open.length,
    currency: reporting,
  })}`

  // PortfolioHeader already loads the list, so this reads its cache.
  const { data: portfolios } = usePortfolios()
  const portfolioName =
    portfolioId === 'all'
      ? t('portfolio.all')
      : (portfolios?.find((p) => p.id === portfolioId)?.name ?? '')
  // Awaited inside try: `navigator.clipboard` is undefined outside a secure
  // context, which throws before any promise exists.
  async function copyForAi() {
    try {
      await navigator.clipboard.writeText(buildAiPrompt(summary, portfolioName, todayIstanbul()))
      toast(t('summary.copyAi.copied'))
    } catch {
      toast(t('summary.copyAi.failed'), { error: true })
    }
  }

  const showClosed = view === 'closed' && closed.length > 0
  const readOnly = portfolioId === 'all'

  // The view on screen: Kapalı with no closed rows shows the open table.
  const shown: View = showClosed ? 'closed' : 'open'
  const sort = sorts[shown]
  const shownRows = shown === 'closed' ? closedRows : openRows
  const opened = summary.positions.find((p) => p.instrument_id === openId)
  const onOpen = (position: Position) => setOpenId(position.instrument_id)

  function sortBy(key: SortKey) {
    setSorts((all) => ({ ...all, [shown]: nextSort(all[shown], key) }))
  }

  const headers = (columns: Column[]) =>
    columns.map(({ key, label }) => (
      <SortHeader
        key={key}
        column={key}
        sort={sort}
        onSort={sortBy}
        className={key === 'symbol' ? '' : 'text-right'}
      >
        {t(label)}
      </SortHeader>
    ))

  const table = showClosed ? (
    <table className={TABLE}>
      <thead>
        <tr>{headers(CLOSED_COLUMNS)}</tr>
      </thead>
      <tbody>
        {closedRows.length === 0 ? (
          <tr className={ROW_40}>
            <td colSpan={CLOSED_COLUMNS.length} className={`${TD} text-muted`}>
              {t('summary.noMatchingPositions')}
            </td>
          </tr>
        ) : (
          closedRows.map((position) => (
            <ClosedRow
              key={position.instrument_id}
              position={position}
              portfolioId={portfolioId}
              reporting={reporting}
              onOpen={onOpen}
            />
          ))
        )}
      </tbody>
    </table>
  ) : (
    <table className={TABLE}>
      <thead>
        <tr>
          {headers(COLUMNS)}
          {!readOnly && <ActionsTh />}
        </tr>
      </thead>
      <tbody>
        {openRows.length === 0 ? (
          <tr className={ROW_40}>
            <td colSpan={COLUMNS.length + 1} className={`${TD} text-muted`}>
              {t(open.length === 0 ? 'summary.noOpenPositions' : 'summary.noMatchingPositions')}
            </td>
          </tr>
        ) : (
          openRows.map((position) => (
            <PositionRow
              key={position.instrument_id}
              position={position}
              portfolioId={portfolioId}
              reporting={reporting}
              total={total}
              largest={largest}
              onTrade={onTrade}
              onEditPrice={onEditPrice}
              onRemovePrice={onRemovePrice}
              onOpen={onOpen}
            />
          ))
        )}
      </tbody>
      {showTotal && (
        <tfoot>
          <tr className={`${ROW_40} border-t border-line [&>td]:border-0`}>
            <td colSpan={6} className={`${TD} text-[12px] text-muted`}>
              {totalLabel}
            </td>
            <td className={`${TD_NUM} font-medium`}>
              {totalValue !== null ? formatMoney(totalValue, reporting) : MISSING}
            </td>
            <td className={`${TD_NUM} ${plTone(summary.unrealized_pl)}`}>
              {plText(summary.unrealized_pl, totalPercent, reporting)}
            </td>
            {!readOnly && <td className={TD} />}
          </tr>
        </tfoot>
      )}
    </table>
  )

  return (
    <section className="overflow-hidden card">
      {opened && (
        <PositionDialog
          scope={portfolioId}
          summary={summary}
          positions={shownRows}
          position={opened}
          range={range}
          onChange={onOpen}
          onTrade={onTrade}
          onEditPrice={onEditPrice}
          onClose={() => setOpenId(null)}
        />
      )}
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 border-b border-line px-4 py-3">
        <h2 className="text-[14px] font-semibold text-ink">{t('summary.positions')}</h2>
        {/* On a phone a row of its own under the title and the controls. */}
        {summary.positions.length > 0 && (
          <input
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('summary.searchPositions')}
            aria-label={t('summary.searchPositions')}
            className={`${inputClass({ size: 'sm' })} order-last text-[14px] md:order-none md:ml-auto md:w-48`}
          />
        )}
        <div className="flex flex-wrap items-center gap-2">
          {closed.length > 0 && (
            <Segmented
              options={VIEWS}
              value={view}
              onChange={setView}
              label={(option) => t(`summary.${option}`)}
              aria-label={t('summary.positionsView')}
              itemClassName={TAP_Y}
            />
          )}
          {open.length > 0 && (
            <Button variant="secondary" onClick={copyForAi}>
              {t('summary.copyAi.button')}
            </Button>
          )}
        </div>
      </div>

      {/* Only the layout matching the viewport is built (GLG-460). The table
          scrolls sideways at tablet widths, where it is wider than the card
          (GLG-322); at 1280px it fits. relative: the actions header's sr-only
          label is absolute, so without a positioned scroller it escapes the
          clip and scrolls the whole page (GLG-679). */}
      {desktop ? (
        <div className="relative overflow-x-auto">{table}</div>
      ) : (
        // Room under the last row for the floating add button (GLG-933).
        <div className="pb-14">
          <ul>
            {showClosed && closedRows.length > 0 ? (
              closedRows.map((position) => (
                <ClosedListItem
                  key={position.instrument_id}
                  position={position}
                  portfolioId={portfolioId}
                  reporting={reporting}
                  onOpen={onOpen}
                />
              ))
            ) : showClosed || openRows.length === 0 ? (
              <li className="px-4 py-3 text-[13px] text-muted">
                {t(
                  !showClosed && open.length === 0
                    ? 'summary.noOpenPositions'
                    : 'summary.noMatchingPositions',
                )}
              </li>
            ) : (
              openRows.map((position) => (
                <PositionListItem
                  key={position.instrument_id}
                  position={position}
                  portfolioId={portfolioId}
                  reporting={reporting}
                  onOpen={onOpen}
                />
              ))
            )}
          </ul>
          {!showClosed && showTotal && (
            <div className="flex items-center justify-between gap-3 border-t border-line px-4 py-3">
              <p className="text-[12px] text-muted">{totalLabel}</p>
              <div className="shrink-0 text-right tabular-nums">
                <div className="font-medium text-ink">
                  {totalValue !== null ? formatMoney(totalValue, reporting) : MISSING}
                </div>
                <div className={`text-[12px] ${plTone(summary.unrealized_pl)}`}>
                  {plText(summary.unrealized_pl, totalPercent, reporting)}
                </div>
              </div>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
