import { fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CashFlowBucket } from '../api/cashflows'
import { t } from '../i18n/strings'
import { mockChartSize } from '../test/chartSize'
import { CashFlowCard } from './CashFlowCard'

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
  // As in BarChart.test: reduced motion draws the final bars at once.
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query === '(prefers-reduced-motion: reduce)',
    addEventListener() {},
    removeEventListener() {},
  }))
})
afterEach(() => {
  restoreSize()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

const bucket = (period: string, deposits = '1000.00', withdrawals = '250.00'): CashFlowBucket => ({
  period,
  deposits,
  withdrawals,
  net: (Number(deposits) - Number(withdrawals)).toFixed(2),
  dividends: '85.00',
  interest: '12.40',
  fees: '7.50',
})

// 30 months, 2024-05 .. 2026-10.
const months30 = Array.from({ length: 30 }, (_, i) =>
  bucket(new Date(Date.UTC(2024, 4 + i, 1)).toISOString().slice(0, 7)),
)

function renderCard(months: CashFlowBucket[], missing_rates: string[] = []) {
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(async () =>
    Response.json({
      currency: 'TRY',
      months,
      totals: bucket('', '0.00', '0.00'),
      missing_rates,
    }),
  )
  const { container } = render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <CashFlowCard portfolioId={1} />
    </QueryClientProvider>,
  )
  return { fetch, container }
}

const groups = (container: HTMLElement, series: string) =>
  container.querySelectorAll(`.series-${series} path`).length
const windowOption = (name: string) =>
  within(screen.getByRole('radiogroup', { name: t('cashflow.window') })).getByRole('radio', {
    name,
  })

describe('CashFlowCard', () => {
  it('opens on the last 24 months and cuts or widens them with the switch', async () => {
    const { fetch, container } = renderCard(months30)

    expect(screen.getByRole('heading', { name: 'Nakit akışı' })).toBeInTheDocument()
    expect(await screen.findByRole('application')).toBeInTheDocument()
    expect(String(fetch.mock.calls[0][0])).toBe('/api/portfolios/1/cashflows')
    expect(windowOption('24 ay')).toHaveAttribute('aria-checked', 'true')
    expect(groups(container, 'deposits')).toBe(24)
    expect(groups(container, 'withdrawals')).toBe(24)

    fireEvent.click(windowOption('12 ay'))
    expect(groups(container, 'deposits')).toBe(12)

    fireEvent.click(windowOption('Tümü'))
    expect(groups(container, 'deposits')).toBe(30)
  })

  it('draws deposits up and withdrawals down, and the tooltip lists the six fields', async () => {
    const { container } = renderCard([bucket('2026-09', '5000.00', '500.00')])

    const chart = await screen.findByRole('application')
    const zero = Number(
      container.querySelector('.recharts-reference-line line')!.getAttribute('y1'),
    )
    // A negative bar has a negative height, drawn from its far end.
    const edges = (series: string) => {
      const path = container.querySelector(`.series-${series} path`)!
      const y = Number(path.getAttribute('y'))
      const height = Number(path.getAttribute('height'))
      return { top: Math.min(y, y + height), bottom: Math.max(y, y + height) }
    }
    expect(edges('deposits').bottom).toBeCloseTo(zero, 0)
    expect(edges('deposits').top).toBeLessThan(zero - 1)
    expect(edges('withdrawals').top).toBeCloseTo(zero, 0)
    expect(edges('withdrawals').bottom).toBeGreaterThan(zero + 1)

    fireEvent.keyDown(chart, { key: 'ArrowRight' })
    expect(document.querySelector('[role="tooltip"]')?.textContent).toBe(
      'Eylül 2026Giriş₺5.000,00Çıkış₺500,00Net₺4.500,00Temettü₺85,00Faiz₺12,40Komisyon₺7,50',
    )
  })

  it('shows the streak and the longest run, and sums the shown window', async () => {
    vi.useFakeTimers({ toFake: ['Date'], now: new Date('2026-10-15T09:00:00Z') })
    renderCard([
      bucket('2026-05', '100.00', '0.00'),
      bucket('2026-06', '0.00', '0.00'),
      bucket('2026-07', '50.00', '0.00'),
      bucket('2026-08', '50.00', '0.00'),
      bucket('2026-09', '50.00', '0.00'),
      bucket('2026-10', '0.00', '0.00'),
    ])

    expect(await screen.findByTestId('cashflow-figures')).toHaveTextContent(
      'Üst üste3 ayEn uzun3 ay',
    )
    expect(screen.getByTestId('cashflow-totals')).toHaveTextContent(
      '24 ay: giriş ₺250,00, çıkış ₺0,00, net ₺250,00, temettü ve faiz ₺584,40, komisyon ₺45,00',
    )
    vi.useRealTimers()
  })

  it('changes the totals line with the window', async () => {
    renderCard(months30)

    expect(await screen.findByTestId('cashflow-totals')).toHaveTextContent(
      '24 ay: giriş ₺24.000,00, çıkış ₺6.000,00, net ₺18.000,00',
    )
    fireEvent.click(windowOption('12 ay'))
    expect(screen.getByTestId('cashflow-totals')).toHaveTextContent(
      '12 ay: giriş ₺12.000,00, çıkış ₺3.000,00, net ₺9.000,00',
    )
  })

  it('shows no figures or totals when there are no months', async () => {
    renderCard([])

    await screen.findByText(t('transactions.empty'))
    expect(screen.queryByTestId('cashflow-figures')).not.toBeInTheDocument()
    expect(screen.queryByTestId('cashflow-totals')).not.toBeInTheDocument()
  })

  it('says there are no transactions yet when there are no months', async () => {
    renderCard([])

    expect(await screen.findByText(t('transactions.empty'))).toBeInTheDocument()
    expect(screen.queryByRole('application')).not.toBeInTheDocument()
  })

  it('names the currencies without a rate', async () => {
    renderCard(months30, ['USD'])

    expect(await screen.findByText('USD için kur yok, o satırlar sayılmadı')).toBeInTheDocument()
  })

  it('shows no note when every row had a rate', async () => {
    renderCard(months30)

    await screen.findByRole('application')
    expect(screen.queryByText(/kur yok/)).not.toBeInTheDocument()
  })

  it('shows a load error with a retry', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      Response.json({ error: 'internal' }, { status: 500 }),
    )
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <CashFlowCard portfolioId={1} />
      </QueryClientProvider>,
    )

    expect(await screen.findByText(t('cashflow.error'))).toBeInTheDocument()
  })
})
