import { render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Benchmark, ReturnMethod } from '../api/history'
import type { YearRow, YearsResponse } from '../api/years'
import { t } from '../i18n/strings'
import { MISSING, formatMoney, signedMoney, signedPercent } from '../lib/format'
import { YearsCard } from './YearsCard'

afterEach(() => {
  vi.restoreAllMocks()
})

const row = (fields: Partial<YearRow>): YearRow => ({
  year: '2025',
  from: '2025-01-01',
  to: '2025-12-31',
  partial: false,
  opening: '0.00',
  closing: '0.00',
  net_flow: '0.00',
  gain: '0.00',
  price: '0.00',
  income: '0.00',
  fx: '0.00',
  cash_fx: '0.00',
  returns: { simple: null, twr: null, mwr: null },
  benchmark: null,
  inflation: null,
  ...fields,
})

const rows = [
  row({
    year: '2024',
    from: '2024-03-04',
    to: '2024-12-31',
    partial: true,
    opening: '1000.00',
    net_flow: '0.00',
    gain: '100.00',
    closing: '1100.00',
    returns: { simple: '0.200000', twr: '0.100000', mwr: null },
  }),
  row({
    opening: '1100.00',
    net_flow: '500.00',
    gain: '120.00',
    closing: '1720.00',
    returns: { simple: '0.075000', twr: '0.075000', mwr: null },
  }),
]

const response = (years: YearRow[], missing: string[] = []): YearsResponse => ({
  currency: 'TRY',
  years,
  missing_rates: missing,
  inflation_estimated_from: null,
})

function renderCard(
  answer: () => Promise<Response>,
  {
    method = 'twr',
    benchmark = null,
  }: { method?: ReturnMethod; benchmark?: Benchmark | null } = {},
) {
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(answer)
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <YearsCard scope={1} benchmark={benchmark} method={method} />
    </QueryClientProvider>,
  )
  return fetch
}

const pct = (v: number) => signedPercent(v, { minimumFractionDigits: 1 })
const cells = (tr: HTMLElement) =>
  within(tr)
    .getAllByRole('cell')
    .map((c) => c.textContent)

describe('YearsCard', () => {
  it('prints a row per year, newest first, and marks the partial one', async () => {
    renderCard(async () => Response.json(response(rows)))

    const table = await screen.findByRole('table', { name: t('years.title') })
    const all = within(table).getAllByRole('row')
    expect(all).toHaveLength(3)
    const [row2025, row2024] = [all[1], all[2]]
    expect(within(row2025).getByRole('rowheader')).toHaveTextContent('2025')
    expect(within(row2024).getByRole('rowheader')).toHaveTextContent('2024')
    expect(cells(row2025)).toEqual([
      formatMoney('1100.00', 'TRY'),
      signedMoney('500.00', 'TRY'),
      signedMoney('120.00', 'TRY'),
      formatMoney('1720.00', 'TRY'),
      pct(0.075),
    ])
    expect(cells(row2024)).toEqual([
      formatMoney('1000.00', 'TRY'),
      signedMoney('0.00', 'TRY'),
      signedMoney('100.00', 'TRY'),
      formatMoney('1100.00', 'TRY'),
      pct(0.1),
    ])
    expect(within(row2024).getByText('*')).toBeInTheDocument()
    expect(within(row2025).queryByText('*')).toBeNull()
  })

  it('prints the chosen method', async () => {
    renderCard(async () => Response.json(response(rows)), { method: 'simple' })

    const table = await screen.findByRole('table')
    expect(cells(within(table).getAllByRole('row')[2])[4]).toBe(pct(0.2))
  })

  it('shows a dash for a return that cannot be computed', async () => {
    renderCard(async () => Response.json(response(rows)), { method: 'mwr' })

    const table = await screen.findByRole('table')
    expect(cells(within(table).getAllByRole('row')[1])[4]).toBe(MISSING)
  })

  it('says there are no transactions yet for an empty scope', async () => {
    renderCard(async () => Response.json(response([])))

    expect(await screen.findByText(t('transactions.empty'))).toBeInTheDocument()
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('names the currencies left out of the totals', async () => {
    renderCard(async () => Response.json(response(rows, ['USD'])))

    expect(
      await screen.findByText(t('chart.missingRates', { currency: 'TRY', currencies: 'USD' })),
    ).toBeInTheDocument()
  })

  it('asks for the benchmark', async () => {
    const fetch = renderCard(async () => Response.json(response(rows)), { benchmark: 'USD' })

    await screen.findByRole('table')
    expect(String(fetch.mock.calls[0][0])).toBe('/api/portfolios/1/years?benchmark=USD')
  })

  it('shows a load error', async () => {
    renderCard(async () => Response.json({ error: 'internal' }, { status: 500 }))

    expect(await screen.findByText(t('years.error'))).toBeInTheDocument()
  })
})
