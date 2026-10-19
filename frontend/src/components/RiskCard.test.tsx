import { fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Benchmark } from '../api/history'
import { t } from '../i18n/strings'
import { MISSING } from '../lib/format'
import { RiskCard } from './RiskCard'

afterEach(() => {
  vi.restoreAllMocks()
})

const addDays = (date: string, days: number) =>
  new Date(Date.parse(date) + days * 86_400_000).toISOString().slice(0, 10)

const alternate = (i: number) => (i % 2 === 0 ? '0.000000' : '0.010000')

// `count` consecutive days from 2024-01-01 whose twr alternates 0, 0.01, 0, ...
// (the returns +1% and -0.99%), or `twr` on every day.
const history = (count: number, twr: string | null | 'alternate' = 'alternate') => ({
  currency: 'TRY',
  points: Array.from({ length: count }, (_, i) => ({
    date: addDays('2024-01-01', i),
    total_value: '1000.00',
    cash: '0.00',
    cost_basis: '1000.00',
    twr: twr === 'alternate' ? alternate(i) : twr,
  })),
  missing_rates: [],
  missing_prices: [],
  net_flow: '0.00',
  returns: { simple: null, twr: null, mwr: null },
  benchmark: null,
})

// The history answers `response`; the cash gaps behind a null twr one gap.
function renderCard(response: object, benchmark: Benchmark | null = null, label?: string) {
  const fetch = vi
    .spyOn(globalThis, 'fetch')
    .mockImplementation(async (input) =>
      Response.json(String(input).endsWith('/cash-gaps') ? gaps : response),
    )
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <RiskCard portfolioId={1} benchmark={benchmark} benchmarkLabel={label} />
    </QueryClientProvider>,
  )
  return fetch
}

// The history with the benchmark's twr on every day: half of the portfolio's,
// or `benchmark_twr` as given.
const withBenchmark = (benchmark_twr: 'half' | null) => {
  const base = history(40)
  return {
    ...base,
    points: base.points.map((p) => ({
      ...p,
      benchmark_twr: benchmark_twr === null ? null : String(Number(p.twr) / 2),
    })),
  }
}

const gaps = [{ portfolio_id: 1, date: '2024-01-02', currency: 'TRY', amount: '500' }]

const cell = (label: string) => screen.getByText(label).closest('div') as HTMLElement

describe('RiskCard', () => {
  it('shows the volatility, the downside deviation, the best and worst day and the count', async () => {
    renderCard(history(40))

    expect(await screen.findByText(t('risk.days', { count: 39 }))).toBeInTheDocument()
    expect(cell(t('risk.volatility'))).not.toHaveTextContent(MISSING)
    expect(cell(t('risk.downside'))).not.toHaveTextContent(MISSING)
    expect(cell(t('risk.bestDay'))).toHaveTextContent('+%1,00')
    expect(cell(t('risk.worstDay'))).toHaveTextContent('-%0,99')
  })

  it('says there is not enough data under 30 daily returns', async () => {
    renderCard(history(10))

    expect(await screen.findByText(t('risk.tooShort'))).toBeInTheDocument()
    expect(screen.queryByText(t('risk.volatility'))).not.toBeInTheDocument()
  })

  it('cuts the history to 1Y and offers all of it', async () => {
    renderCard(history(400))

    expect(await screen.findByText(t('risk.days', { count: 365 }))).toBeInTheDocument()
    fireEvent.click(screen.getByRole('radio', { name: t('risk.window.all') }))
    expect(await screen.findByText(t('risk.days', { count: 399 }))).toBeInTheDocument()
  })

  it('reads the all-time history with the benchmark', async () => {
    const fetch = renderCard(history(40), 'USD')

    await screen.findByText(t('risk.days', { count: 39 }))
    expect(String(fetch.mock.calls[0][0])).toMatch(
      /^\/api\/portfolios\/1\/history\?to=\d{4}-\d{2}-\d{2}&benchmark=USD$/,
    )
  })

  it('shows beta, correlation and the benchmark volatility with a benchmark', async () => {
    renderCard(withBenchmark('half'), 'XU100', 'BIST 100')

    expect(await screen.findByText(t('risk.beta'))).toBeInTheDocument()
    expect(cell(t('risk.beta'))).toHaveTextContent('2,00')
    expect(cell(t('risk.correlation'))).toHaveTextContent('1,00')
    expect(cell(t('risk.benchmarkVolatility', { benchmark: 'BIST 100' }))).not.toHaveTextContent(
      MISSING,
    )
  })

  it('asks for a benchmark without one', async () => {
    renderCard(history(40))

    expect(await screen.findByText(t('risk.noBenchmark'))).toBeInTheDocument()
    expect(screen.queryByText(t('risk.beta'))).not.toBeInTheDocument()
  })

  it('says the benchmark has gaps when it has no returns', async () => {
    renderCard(withBenchmark(null), 'USD', 'USD')

    expect(
      await screen.findByText(t('risk.benchmarkNoData', { benchmark: 'USD' })),
    ).toBeInTheDocument()
    expect(screen.queryByText(t('risk.beta'))).not.toBeInTheDocument()
  })

  it('explains an undefined return instead of the figures', async () => {
    renderCard(history(40, null))

    expect(
      await screen.findByRole('button', { name: t('performance.cashGaps.add') }),
    ).toBeInTheDocument()
    expect(screen.queryByText(t('risk.tooShort'))).not.toBeInTheDocument()
  })
})
