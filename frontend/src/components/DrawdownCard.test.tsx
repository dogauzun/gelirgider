import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { mockChartSize } from '../test/chartSize'
import { DrawdownCard } from './DrawdownCard'

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
})
afterEach(() => {
  restoreSize()
  vi.restoreAllMocks()
})

// Index 1, 1.25, 1, 1.125, 1.1875: a 20% fall from 02 Mar to 03 Mar 2025,
// then 5% below that peak on the last day.
const falling = ['0.000000', '0.250000', '0.000000', '0.125000', '0.187500']

const history = (twr: (string | null)[], value = '1000.00') => ({
  currency: 'TRY',
  points: twr.map((v, i) => ({
    date: `2025-03-0${i + 1}`,
    total_value: value,
    cash: '0.00',
    cost_basis: value,
    twr: v,
  })),
  missing_rates: [],
  missing_prices: [],
  net_flow: '0.00',
  returns: { simple: null, twr: null, mwr: null },
  benchmark: null,
})

// The history answers `response`; the cash gaps behind a null twr none.
function renderCard(response: () => Promise<Response>) {
  const fetch = vi
    .spyOn(globalThis, 'fetch')
    .mockImplementation(async (input) =>
      String(input).endsWith('/cash-gaps') ? Response.json([]) : response(),
    )
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <DrawdownCard portfolioId={1} />
    </QueryClientProvider>,
  )
  return fetch
}

describe('DrawdownCard', () => {
  it('reads the all-time history and shows the largest and current drawdown and the chart', async () => {
    const fetch = renderCard(async () => Response.json(history(falling)))

    expect(screen.getByRole('heading', { name: 'Zirveden düşüş' })).toBeInTheDocument()
    expect(
      await screen.findByRole('application', {
        name: '01 Mar – 05 Mar zirveden düşüş, en büyük -%20',
      }),
    ).toBeInTheDocument()
    expect(screen.getByTestId('drawdown-figures')).toHaveTextContent(
      'En büyük-%2002 Mar 2025 – 03 Mar 2025Şu an-%5',
    )
    // No `from`: the backend starts at the first transaction.
    expect(String(fetch.mock.calls[0][0])).toMatch(
      /^\/api\/portfolios\/1\/history\?to=\d{4}-\d{2}-\d{2}$/,
    )
  })

  it('shows zero and no dates when the series never falls', async () => {
    renderCard(async () => Response.json(history(['0.000000', '0.010000'])))

    expect(await screen.findByTestId('drawdown-figures')).toHaveTextContent('En büyük%0Şu an%0')
  })

  it('explains an undefined return instead of the figures and the chart', async () => {
    renderCard(async () => Response.json(history([null, null])))

    expect(await screen.findByText(t('performance.cashGaps.noValue'))).toBeInTheDocument()
    expect(screen.queryByTestId('drawdown-figures')).not.toBeInTheDocument()
    expect(screen.queryByRole('application')).not.toBeInTheDocument()
  })

  // A scope with no transactions answers one point, today, all zero and
  // with twr null: the empty state of the card above, not a
  // return that cannot be computed.
  it('says there are no transactions yet for an empty scope', async () => {
    renderCard(async () => Response.json(history([null], '0.00')))

    expect(await screen.findByText(t('transactions.empty'))).toBeInTheDocument()
    expect(screen.queryByText(t('performance.cashGaps.noValue'))).not.toBeInTheDocument()
  })

  // On the day of the first transaction the one point has twr null too.
  it('says there is too little data on the first day', async () => {
    renderCard(async () => Response.json(history([null])))

    expect(await screen.findByText(t('chart.empty'))).toBeInTheDocument()
    expect(screen.queryByText(t('performance.cashGaps.noValue'))).not.toBeInTheDocument()
  })

  it('shows a load error with a retry', async () => {
    renderCard(async () => Response.json({ error: 'internal' }, { status: 500 }))

    expect(await screen.findByText(t('drawdown.error'))).toBeInTheDocument()
  })
})
