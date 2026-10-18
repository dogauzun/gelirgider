import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Projection } from '../api/projection'
import { t } from '../i18n/strings'
import { mockChartSize } from '../test/chartSize'
import { ProjectionCard } from './ProjectionCard'

let restoreSize: () => void
beforeEach(() => {
  restoreSize = mockChartSize()
})
afterEach(() => {
  restoreSize()
  vi.restoreAllMocks()
})

const months = [
  { month: '2027-01', low: 900, mid: 1000, high: 1100 },
  { month: '2027-02', low: 950, mid: 1100, high: 1250 },
]

const projection = (over: Partial<Projection>): Projection => ({
  unit: 'nominal',
  currency: 'TRY',
  amount: '2000',
  target_date: '2099-12-31',
  months,
  reached_at: null,
  monthly_gap: null,
  progress: 0.42,
  history_months: 12,
  ...over,
})

// The goal route answers a goal and the projection route `body`.
function renderCard(body: Projection) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) =>
    Response.json(
      String(input).endsWith('/goal')
        ? { amount: '2000', unit: 'nominal', target_date: '2027-12-31' }
        : body,
    ),
  )
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <ProjectionCard portfolioId={1} />
    </QueryClientProvider>,
  )
}

describe('ProjectionCard', () => {
  it('names the month the goal is reached, with the progress and the chart', async () => {
    renderCard(projection({ reached_at: '2027-10', progress: 0.42 }))

    expect(
      await screen.findByText(t('projection.reached', { month: 'Ekim 2027' })),
    ).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Hedef' })).toBeInTheDocument()
    expect(screen.getByText('%42')).toBeInTheDocument()
    expect(await screen.findByRole('application')).toBeInTheDocument()
  })

  it('names the extra monthly amount when the goal is not reached', async () => {
    renderCard(projection({ monthly_gap: 1250 }))

    expect(
      await screen.findByText(t('projection.gap', { amount: '₺1.250,00' })),
    ).toBeInTheDocument()
  })

  it('says there is too little history, and draws no chart, when there are no months', async () => {
    renderCard(projection({ months: [], history_months: 3 }))

    expect(await screen.findByText(t('projection.tooLittleHistory'))).toBeInTheDocument()
    expect(screen.queryByRole('application')).not.toBeInTheDocument()
  })

  it('says the target date has passed, and draws no chart', async () => {
    renderCard(projection({ months: [], history_months: 36, target_date: '2020-01-31' }))

    expect(await screen.findByText(t('projection.targetPassed'))).toBeInTheDocument()
    expect(screen.queryByRole('application')).not.toBeInTheDocument()
  })

  it('a passed target date wins over too little history', async () => {
    renderCard(projection({ months: [], history_months: 3, target_date: '2020-01-31' }))

    expect(await screen.findByText(t('projection.targetPassed'))).toBeInTheDocument()
    expect(screen.queryByText(t('projection.tooLittleHistory'))).not.toBeInTheDocument()
  })
})
