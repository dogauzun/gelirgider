import { useEffect, useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import {
  useAddExpectedDividend,
  useDismissExpectedDividend,
  useExpectedDividendsOf,
  type ExpectedDividend,
} from '../api/dividends'
import { useAllocationTargets } from '../api/allocationTargets'
import { useAttentionRules } from '../api/auth'
import { useDueDepositsOf, useMatureDeposit } from '../api/deposits'
import { usePortfolioHistory } from '../api/history'
import { useBookPlan, useDuePlansOf, useSkipPlan } from '../api/plans'
import { usePortfolios } from '../api/portfolios'
import { useDeletePrice } from '../api/prices'
import {
  usePortfolioSummaries,
  usePortfolioSummary,
  type PortfolioScope,
  type Position,
  type Summary,
  type SummaryStats,
} from '../api/summary'
import { useInvalidatePortfolios } from '../api/transactions'
import { useWatchlist } from '../api/watchlist'
import { Allocation } from '../components/Allocation'
import { ContributionCard } from '../components/ContributionCard'
import { AttentionCard } from '../components/AttentionCard'
import { CreatePortfolioDialog } from '../components/CreatePortfolioDialog'
import { expectedDividendForm } from '../components/expectedDividendForm'
import { Mark } from '../components/Mark'
import { MissingRatesMark } from '../components/MissingRatesMark'
import { PortfolioHeader } from '../components/PortfolioHeader'
import { PortfoliosTable } from '../components/PortfoliosTable'
import { PositionsTable } from '../components/PositionsTable'
import { PriceDialog } from '../components/PriceDialog'
import { ProjectionCard } from '../components/ProjectionCard'
import { ReturnCard } from '../components/ReturnCard'
import { ReturnSplitCard } from '../components/ReturnSplitCard'
import { toInput } from '../components/transactionForm'
import { Button } from '../components/ui/Button'
import { FitFigure, KPI_FIGURE, KpiCell, KpiStrip } from '../components/ui/Kpi'
import { LoadError } from '../components/ui/LoadError'
import { Skeleton } from '../components/ui/Skeleton'
import { usePositionActions } from '../components/usePositionActions'
import { t, type StringKey } from '../i18n/strings'
import {
  attentionItems,
  driftItems,
  mergeAttention,
  moverCount,
  watchTargetItems,
} from '../lib/attention'
import { formatMarketDate, formatShortMarketDate, monthsBetween, todayIstanbul } from '../lib/date'
import { formatMoney, MISSING, plTone, signedMoney } from '../lib/format'
import { useNavState } from '../lib/navState'
import { returnPercent } from '../lib/positions'
import { resolveRange } from '../lib/ranges'
import { useRange } from '../lib/useRange'
import { useBenchmark, useReturnMethod } from '../lib/useReturnSettings'
import { usePortfolioTitle } from '../lib/useTitle'
import { toast } from '../lib/toast'
import { focusIfLost } from '../lib/useFocusLater'
import { useAction } from '../lib/useOneAtATime'

// Özet's KPI strip (layout A): one row of five from lg, the total a
// little wider; two columns on a phone with the total across both.
const KPI_COLUMNS = 'grid-cols-2 lg:grid-cols-[1.3fr_1fr_1fr_1fr_1fr]'

// One money figure that never breaks inside: the browser may otherwise
// wrap between a "+" and a "₺". Lines wrap at the spaces and
// " · " separators around it instead.
function Fig({ children }: { children: ReactNode }) {
  return <span className="whitespace-nowrap tabular-nums">{children}</span>
}

// t() with each variable rendered as a Fig, for sub-lines like
// "Maliyet {amount}" whose label may wrap but whose amount may not.
function tFigures(key: StringKey, vars: Record<string, string>): ReactNode {
  const placeholders = Object.fromEntries(Object.keys(vars).map((k) => [k, `{${k}}`]))
  return t(key, placeholders)
    .split(/\{(\w+)\}/)
    .map((part, i) => (i % 2 ? <Fig key={i}>{vars[part]}</Fig> : part))
}

// The loading shape of the return and attention cards and the KPI strip, so
// the page does not jump when the data lands.
function SummarySkeleton() {
  return (
    <>
      <div className="mb-4 grid gap-4 lg:grid-cols-[2fr_1fr]">
        <Skeleton className="h-[340px] w-full rounded-card" />
        <Skeleton className="h-[140px] w-full rounded-card" />
      </div>
      <KpiStrip columns={KPI_COLUMNS}>
        {[0, 1, 2, 3, 4].map((cell) => (
          <div
            key={cell}
            className={`bg-surface p-5 ${cell === 0 ? 'col-span-2 lg:col-span-1' : ''}`}
          >
            <Skeleton className="h-3 w-24" />
            <Skeleton className="mt-3 h-6 w-28" />
          </div>
        ))}
      </KpiStrip>
    </>
  )
}

// One portfolio's positions table with what its row menu opens: the
// transaction dialog prefilled from the row, the price dialog
// anchored to the row, and the removal of a manual price. The
// `all` scope renders the table alone: these need a numeric portfolio id
// (ADR 0014).
function PortfolioPositions({ summary, portfolioId }: { summary: Summary; portfolioId: number }) {
  const { openTrade, openPrice, dialogs } = usePositionActions(portfolioId)
  const removal = useDeletePrice(portfolioId)
  return (
    <>
      {dialogs}
      <PositionsTable
        summary={summary}
        portfolioId={portfolioId}
        onTrade={openTrade}
        onEditPrice={openPrice}
        onRemovePrice={(position) =>
          removal.mutate(
            { instrumentId: position.instrument_id, date: position.price_date as string },
            { onError: (error) => toast(error.message, { error: true }) },
          )
        }
      />
    </>
  )
}

// The "Dikkat" card with what its buttons do: Ekle books the
// expected dividend as the Temettü tab's "Tümünü ekle" does, one row; Yoksay
// dismisses it; Fiyat gir and Güncelle open the price editor, centred, for
// that position. On one portfolio's Özet `portfolios` is that one, unnamed,
// and `linkTo` its id for the movers' links. On Tüm portföyler it is every
// portfolio: each one's items come from its own summary and expected
// dividends, named, as expected dividends are per portfolio.
function Attention({
  portfolios,
  linkTo,
  estimatedFrom,
}: {
  portfolios: { id: number; name?: string }[]
  linkTo?: number
  estimatedFrom: string | null
}) {
  const ids = portfolios.map((p) => p.id)
  const summaries = usePortfolioSummaries(ids)
  // The drift rule reads the page's own scope: the portfolio, or every portfolio together.
  const scope = linkTo ?? 'all'
  const scopeSummary = usePortfolioSummary(scope).data
  const targets = useAllocationTargets(scope).data
  const expected = useExpectedDividendsOf(ids)
  const dueDeposits = useDueDepositsOf(ids)
  const duePlans = useDuePlansOf(ids)
  // The list belongs to no portfolio: only Tüm portföyler asks for it.
  const watched = useWatchlist(linkTo === undefined).data ?? []
  const invalidatePortfolios = useInvalidatePortfolios()
  // Undefined while loading or on a failure: attentionItems then uses the defaults.
  const rules = useAttentionRules().data
  const [pricing, setPricing] = useState<{ position: Position; portfolioId: number } | null>(null)
  // On Tüm portföyler a saved price refetches the other portfolios after the
  // editor closes; the card keeps its focus target until they have.
  const [refreshing, setRefreshing] = useState(false)
  const today = todayIstanbul()
  // Which portfolio each dividend item came from, for Ekle and Yoksay.
  const owners = new Map<ExpectedDividend, number>()
  const drift = scopeSummary ? driftItems(scopeSummary, targets, rules) : []
  const items = mergeAttention(
    [
      ...portfolios.map((p, i) => {
        const summary = summaries[i].data
        if (!summary) return []
        const dividends = expected[i].data ?? []
        for (const d of dividends) owners.set(d, p.id)
        return attentionItems({
          summary,
          expected: dividends,
          dueDeposits: dueDeposits[i].data,
          duePlans: duePlans[i].data,
          today,
          portfolioName: p.name,
          rules,
        })
      }),
      drift,
      ...(linkTo === undefined ? [watchTargetItems(watched)] : []),
    ],
    estimatedFrom,
    moverCount(rules),
  )

  // Busy until the expected list has refetched (useAddExpectedDividend).
  const add = useAddExpectedDividend()
  const dismissal = useDismissExpectedDividend()
  const maturity = useMatureDeposit()
  const booking = useBookPlan()
  const skipping = useSkipPlan()
  const busy =
    add.isPending ||
    dismissal.isPending ||
    maturity.isPending ||
    booking.isPending ||
    skipping.isPending
  const runAction = useAction()
  function run(
    dividend: ExpectedDividend,
    action: (portfolioId: number) => Promise<unknown>,
    done: string,
  ) {
    runAction(() => action(owners.get(dividend) as number), done)
  }

  return (
    <>
      {pricing && (
        <PriceDialog
          position={pricing.position}
          portfolioId={pricing.portfolioId}
          anchor={null}
          onClose={() => {
            setPricing(null)
            // A price is the user's, not the portfolio's: on Tüm portföyler
            // every portfolio holding it and the sum change with it.
            if (linkTo === undefined) {
              setRefreshing(true)
              void invalidatePortfolios().finally(() => setRefreshing(false))
            }
          }}
        />
      )}
      <AttentionCard
        items={items}
        busy={busy}
        editingPrice={pricing !== null || refreshing}
        portfolioId={linkTo}
        onAddDividend={(dividend) =>
          run(
            dividend,
            (portfolioId) =>
              add.mutateAsync({ portfolioId, input: toInput(expectedDividendForm(dividend)) }),
            t('dividends.expected.added', { count: '1' }),
          )
        }
        onDismissDividend={(dividend) =>
          run(
            dividend,
            (portfolioId) => dismissal.mutateAsync({ portfolioId, dividend }),
            t('dividends.expected.dismissed'),
          )
        }
        onCloseDeposit={(deposit) =>
          runAction(() => maturity.mutateAsync(deposit.id), t('attention.deposit.closed'))
        }
        onBookPlan={(plan) => runAction(() => booking.mutateAsync(plan.plan_id), t('plans.booked'))}
        onSkipPlan={(plan) =>
          runAction(() => skipping.mutateAsync(plan.plan_id), t('plans.skipped'))
        }
        onEditPrice={(instrumentId) => {
          for (const [i, p] of portfolios.entries()) {
            const position = summaries[i].data?.positions.find(
              (q) => q.instrument_id === instrumentId,
            )
            if (position) return setPricing({ position, portfolioId: p.id })
          }
        }}
      />
    </>
  )
}

// How long ago the first transaction was, as "2 yıl 3 ay", from today in
// Europe/Istanbul. A zero part is left out; under a month reads "bu ay".
function elapsedLabel(first: string): string {
  const months = monthsBetween(first, todayIstanbul())
  if (months < 1) return t('summary.stats.thisMonth')
  const years = Math.floor(months / 12)
  const parts = []
  if (years > 0) parts.push(t('summary.stats.years', { count: years }))
  if (months % 12 > 0) parts.push(t('summary.stats.months', { count: months % 12 }))
  return parts.join(' ')
}

// The summary's stats: net contributions, dividends net of stopaj,
// interest, fees and the first transaction. Without reporting totals (a
// currency with no rate) the money cells show MISSING; the attention card
// already says why.
function StatsCard({
  stats,
  first,
  currency,
}: {
  stats: SummaryStats
  first: string
  currency: string
}) {
  const r = stats.reporting
  const money = (value: number | string) => formatMoney(value, currency)
  const cells: { label: string; figure: string; detail: ReactNode }[] = [
    {
      label: t('summary.stats.netContribution'),
      figure: r ? money(r.net_contribution) : MISSING,
      detail:
        r &&
        tFigures('summary.stats.netContributionDetail', {
          deposits: money(r.deposits),
          withdrawals: money(r.withdrawals),
        }),
    },
    {
      label: t('summary.stats.dividends'),
      figure: r ? money(r.dividends_net) : MISSING,
      detail:
        r &&
        tFigures('summary.stats.dividendsDetail', {
          gross: money(r.dividends_gross),
          tax: money(r.dividends_tax),
        }),
    },
    { label: t('summary.stats.interest'), figure: r ? money(r.interest) : MISSING, detail: null },
    { label: t('summary.stats.fees'), figure: r ? money(r.fees) : MISSING, detail: null },
    {
      label: t('summary.stats.firstTransaction'),
      figure: formatMarketDate(first),
      detail: elapsedLabel(first),
    },
  ]

  return (
    // A full-width strip under the main KPI strip, visible without scrolling
    // (it sat under Dağılım and Katkı before). The cells name
    // themselves, as in the KPI strip above; the heading is for screen
    // readers. The fifth cell spans two columns below lg, so the grid has no
    // hole.
    <KpiStrip columns="grid-cols-2 sm:grid-cols-3 lg:grid-cols-5">
      <h2 className="sr-only">{t('summary.stats.title')}</h2>
      {cells.map((cell, i) => (
        <KpiCell
          key={cell.label}
          label={cell.label}
          className={i === 4 ? 'col-span-2 lg:col-span-1' : ''}
        >
          <FitFigure className={`${KPI_FIGURE} text-ink`} text={cell.figure} />
          {cell.detail && <p className="mt-1 text-[12px] text-muted">{cell.detail}</p>}
        </KpiCell>
      ))}
    </KpiStrip>
  )
}

// Özet, layout A: the return card beside the attention card, one
// KPI strip, İstatistikler as a strip under it, the
// positions table, then Dağılım beside Katkı. Each
// number is on the page once; what used to be a sub-line is a cell's hint.
export function SummaryPage() {
  const params = useParams()
  const portfolioId: PortfolioScope = params.id === 'all' ? 'all' : Number(params.id)
  usePortfolioTitle(t('summary.title'), portfolioId)
  const navigate = useNavigate()
  const { data, isLoading, isError, error, refetch, isFetching } = usePortfolioSummary(portfolioId)
  const [range, setRange] = useRange()
  const { from, to } = resolveRange(range, todayIstanbul())
  const { benchmark, options: benchmarkOptions, setBenchmark } = useBenchmark(data)
  const history = usePortfolioHistory(portfolioId, from, to, benchmark)
  const [method, setMethod] = useReturnMethod()

  // Closed positions come down the same array but are held apart:
  // they are not holdings, so they stay out of the allocation, the total
  // value and the position count. A quantity of 0 alone is not closed.
  // The positions table sorts its own rows.
  const positions = (data?.positions ?? []).filter((p) => !p.closed)
  const closed = (data?.positions ?? []).filter((p) => p.closed)
  const cashByCurrency = data
    ? Object.entries(data.cash_by_currency).filter(([, amount]) => Number(amount) !== 0)
    : []
  // data.cash is the reporting-currency total, which silently excludes any
  // currency missing an FX rate (see missing_rates) -- checking it alone
  // would call a portfolio "empty" while it still holds untotaled cash.
  // cash_by_currency carries every currency's raw amount, rate or no rate.
  const isEmpty =
    data && positions.length === 0 && closed.length === 0 && cashByCurrency.length === 0

  // Same exclusion, one step further: data.cash and data.total_value are
  // summed by skipping every currency in missing_rates, so when
  // nothing is left to convert they read 0,00 next to real holdings. That is
  // a wrong number, not an incomplete one -- show MISSING instead, the same way a
  // position with no reporting value does. The cash cell's hint then carries
  // the actual balance, so it is there whenever any cash sits in a currency
  // other than the reporting one, not only when there are several.
  const missingRates = data?.missing_rates ?? []
  const convertedCash = cashByCurrency.some(([currency]) => !missingRates.includes(currency))
  const cashKnown = cashByCurrency.length === 0 || convertedCash
  const totalValueKnown = convertedCash || positions.some((p) => p.market_value_reporting !== null)
  const cashHint = cashByCurrency.some(([currency]) => currency !== data?.currency)
    ? cashByCurrency.map(([currency, amount]) => formatMoney(amount, currency)).join(' · ')
    : undefined

  // The reporting-currency cost the unrealized P/L was earned on, as the
  // server sums it: total_value less cash less the gain, each rounded on its
  // own, can be a cent off the rows and the chart. Without a
  // percent (lib/positions returnPercent) the cost is dropped from the hint
  // too.
  const unrealized = data?.unrealized_pl ?? null
  const unrealizedPercent = data ? returnPercent(unrealized, data.cost_basis) : null
  const unrealizedHint =
    data && unrealizedPercent !== null
      ? `${unrealizedPercent} ${t('summary.vsCost')} · ${t('summary.cost', {
          amount: formatMoney(data.cost_basis, data.currency),
        })}`
      : undefined

  // The open positions' day change, dated by the newest price it covers.
  const dayTotal = data?.day_change ?? null
  const dayDate = positions.reduce<string>((latest, p) => {
    if (p.day_change_reporting == null) return latest
    const date = p.price_date ?? ''
    return date > latest ? date : latest
  }, '')

  // Tüm portföyler: the same page summed over every portfolio, with
  // a row per portfolio. Its figures leave out what any portfolio left out,
  // so they carry the mark the sidebar's total carries; the
  // attention card names the currencies, per portfolio.
  const isAll = portfolioId === 'all'
  const { data: portfolios } = usePortfolios()
  const mark = isAll && (
    <MissingRatesMark currencies={missingRates} currency={data?.currency ?? ''} />
  )
  const portfoliosTable = isAll && portfolios && portfolios.length > 0 && (
    <PortfoliosTable portfolios={portfolios} range={range} method={method} />
  )

  // A portfolio deleted from its own page lands here with its header, and
  // the focus it held, gone: focus the first remaining row, else the new
  // portfolio button, unless focus has moved on; then clear the state so a
  // remount does not do it again.
  const { state, clear } = useNavState()
  const focusAfterDelete = Boolean(state?.focusAfterDelete)
  useEffect(() => {
    if (!focusAfterDelete || !portfolios || !data) return
    focusIfLost(
      document.querySelector<HTMLElement>('[data-portfolio-row]') ??
        document.querySelector<HTMLElement>('[data-new-portfolio]'),
    )
    clear()
  }, [focusAfterDelete, portfolios, data, clear])

  return (
    // An empty portfolio's one primary action is the card's own.
    <PortfolioHeader portfolioId={portfolioId} add={isEmpty ? 'quiet' : 'float'}>
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        {isAll && portfolios?.length === 0 ? (
          <NoPortfolio />
        ) : (
          <>
            {isLoading && <SummarySkeleton />}
            {isError && (
              <LoadError
                title={t('summary.error')}
                error={error}
                onRetry={() => refetch()}
                retrying={isFetching}
              />
            )}

            {data &&
              (isEmpty ? (
                <div className="flex flex-col gap-4">
                  <section className="flex flex-col items-center gap-3 card px-6 py-12 text-center">
                    <Mark size={32} />
                    <p className="text-[16px] font-semibold text-ink">{t('summary.empty')}</p>
                    {/* `all` has no İşlemler or İçe aktar to send the user to
                        (ADR 0014); its rows below open the portfolios. */}
                    {!isAll && (
                      <>
                        <Button onClick={() => navigate(`/portfolios/${portfolioId}/transactions`)}>
                          {t('summary.emptyLink')}
                        </Button>
                        {/* Import is the faster start for a broker user, so it
                            sits under the one primary action as a link. */}
                        <Link
                          to={`/portfolios/${portfolioId}/import`}
                          className="inline-flex h-11 items-center px-2 text-[14px] font-medium text-ink-2 underline underline-offset-2 hover:text-ink sm:h-9"
                        >
                          {t('summary.emptyImport')}
                        </Link>
                      </>
                    )}
                  </section>
                  {portfoliosTable}
                </div>
              ) : (
                <div className="flex flex-col gap-4">
                  {/* Tracks with a zero minimum (grid-cols-1 is minmax(0,
                      1fr)): a long attention line, as one naming its
                      portfolio, truncates instead of widening the page
                      below lg or taking the return card's room from lg. */}
                  <div className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)] lg:items-start">
                    <div className="flex flex-col gap-4">
                      <ReturnCard
                        currency={data.currency}
                        query={history}
                        range={range}
                        onRange={setRange}
                        firstDate={data.stats?.first_transaction_date ?? undefined}
                        benchmark={benchmark}
                        benchmarks={benchmarkOptions}
                        onBenchmark={setBenchmark}
                        method={method}
                        onMethod={setMethod}
                      />
                      <ReturnSplitCard scope={portfolioId} range={range} />
                    </div>
                    {(!isAll || portfolios) && (
                      <Attention
                        portfolios={isAll ? (portfolios ?? []) : [{ id: portfolioId }]}
                        linkTo={isAll ? undefined : portfolioId}
                        estimatedFrom={history.data?.benchmark?.estimated_from ?? null}
                      />
                    )}
                  </div>

                  <KpiStrip columns={KPI_COLUMNS}>
                    <KpiCell
                      label={t('summary.totalValue')}
                      className="col-span-2 lg:col-span-1"
                      hint={
                        data.fx_pl !== null
                          ? `${t('summary.fxPl')} ${signedMoney(data.fx_pl, data.currency)}`
                          : undefined
                      }
                    >
                      <FitFigure
                        className={`${KPI_FIGURE} text-ink`}
                        text={
                          totalValueKnown ? formatMoney(data.total_value, data.currency) : MISSING
                        }
                        mark={mark}
                      />
                    </KpiCell>
                    <KpiCell
                      label={t('summary.dayChange')}
                      hint={
                        dayTotal !== null && dayDate
                          ? t('summary.dayAsOf', { date: formatShortMarketDate(dayDate) })
                          : undefined
                      }
                    >
                      <FitFigure
                        className={`${KPI_FIGURE} ${plTone(dayTotal)}`}
                        text={signedMoney(dayTotal, data.currency)}
                      />
                    </KpiCell>
                    <KpiCell label={t('summary.cash')} hint={cashHint}>
                      <FitFigure
                        className={`${KPI_FIGURE} text-ink`}
                        text={cashKnown ? formatMoney(data.cash, data.currency) : MISSING}
                        mark={mark}
                      />
                    </KpiCell>
                    <KpiCell label={t('summary.unrealizedPl')} hint={unrealizedHint}>
                      <FitFigure
                        className={`${KPI_FIGURE} ${plTone(unrealized)}`}
                        text={signedMoney(unrealized, data.currency)}
                        mark={mark}
                      />
                    </KpiCell>
                    <KpiCell label={t('summary.realizedPl')}>
                      <FitFigure
                        className={`${KPI_FIGURE} ${plTone(data.realized_pl)}`}
                        text={signedMoney(data.realized_pl, data.currency)}
                      />
                    </KpiCell>
                  </KpiStrip>

                  {/* An older backend sends no stats. */}
                  {data.stats?.first_transaction_date && (
                    <StatsCard
                      stats={data.stats}
                      first={data.stats.first_transaction_date}
                      currency={data.currency}
                    />
                  )}

                  {portfoliosTable}

                  {isAll ? (
                    <PositionsTable summary={data} portfolioId="all" />
                  ) : (
                    <PortfolioPositions summary={data} portfolioId={portfolioId} />
                  )}

                  {/* Side by side with equal heights from lg, stacked below. */}
                  <div className="grid gap-4 lg:grid-cols-2">
                    <Allocation
                      portfolioId={portfolioId}
                      positions={positions}
                      cash={data.cash}
                      cashByCurrency={data.cash_by_currency}
                      cashByCurrencyReporting={data.cash_by_currency_reporting}
                      missingRates={data.missing_rates}
                      currency={data.currency}
                    />
                    <ContributionCard
                      positions={positions}
                      currency={data.currency}
                      portfolioId={portfolioId}
                      dayChange={data.day_change ?? null}
                    />
                  </div>

                  <ProjectionCard portfolioId={portfolioId} />
                </div>
              ))}
          </>
        )}
      </div>
    </PortfolioHeader>
  )
}

// Tüm portföyler with no portfolio at all: the first one to create, as the
// home page offered before it went.
function NoPortfolio() {
  const [creating, setCreating] = useState(false)
  return (
    <section className="flex flex-col items-center gap-3 card px-6 py-12 text-center">
      <Mark size={32} />
      <p className="text-[16px] font-semibold text-ink">{t('portfolios.emptyTitle')}</p>
      <p className="text-[13px] text-muted">{t('portfolios.emptyBody')}</p>
      <Button onClick={() => setCreating(true)}>{t('portfolios.emptyCta')}</Button>
      {creating && <CreatePortfolioDialog onClose={() => setCreating(false)} />}
    </section>
  )
}
