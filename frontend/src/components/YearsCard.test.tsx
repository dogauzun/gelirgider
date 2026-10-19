import { fireEvent, render, screen, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Benchmark, ReturnMethod } from '../api/history'
import type { YearRow, YearsResponse } from '../api/years'
import { t } from '../i18n/strings'
import { MISSING, formatMoney, signedMoney, signedPercent } from '../lib/format'
import { excessReturn } from '../lib/returns'
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
  best: null,
  worst: null,
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
    price: null,
    closing: '1100.00',
    returns: { simple: '0.200000', twr: '0.100000', mwr: null },
  }),
  row({
    opening: '1100.00',
    net_flow: '500.00',
    gain: '120.00',
    price: '90.00',
    income: '20.00',
    fx: '-5.00',
    cash_fx: '15.00',
    closing: '1720.00',
    returns: { simple: '0.075000', twr: '0.075000', mwr: null },
  }),
]

const response = (
  years: YearRow[],
  missing: string[] = [],
  {
    currency = 'TRY',
    estimatedFrom = null,
  }: { currency?: string; estimatedFrom?: string | null } = {},
): YearsResponse => ({
  currency,
  years,
  missing_rates: missing,
  inflation_estimated_from: estimatedFrom,
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
      <YearsCard scope={1} benchmark={benchmark} benchmarkLabel="Dolar" method={method} />
    </QueryClientProvider>,
  )
  return fetch
}

const pct = (v: number) => signedPercent(v, { minimumFractionDigits: 1 })
const excessPct = (p: number, b: number) => signedPercent(excessReturn(p, b) ?? 0)
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
      MISSING,
      MISSING,
      MISSING,
      MISSING,
    ])
    expect(cells(row2024)).toEqual([
      formatMoney('1000.00', 'TRY'),
      signedMoney('0.00', 'TRY'),
      signedMoney('100.00', 'TRY'),
      formatMoney('1100.00', 'TRY'),
      pct(0.1),
      MISSING,
      MISSING,
      MISSING,
      MISSING,
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

  it('splits the gain into price, income and currency in the split view', async () => {
    renderCard(async () => Response.json(response(rows)))

    await screen.findByRole('table')
    expect(screen.getByText(t('years.hint'))).toBeInTheDocument()
    fireEvent.click(screen.getByRole('radio', { name: t('years.view.split') }))

    const table = screen.getByRole('table')
    expect(within(table).getByRole('columnheader', { name: t('years.price') })).toBeInTheDocument()
    expect(within(table).queryByRole('columnheader', { name: t('years.opening') })).toBeNull()
    const all = within(table).getAllByRole('row')
    expect(cells(all[1])).toEqual([
      signedMoney('90.00', 'TRY'),
      signedMoney('20.00', 'TRY'),
      signedMoney('-5.00', 'TRY'),
      signedMoney('15.00', 'TRY'),
      signedMoney('120.00', 'TRY'),
      pct(0.075),
      MISSING,
      MISSING,
      MISSING,
      MISSING,
    ])
    // The 2024 row has no price: a dash, and its gain still prints.
    expect(cells(all[2])[0]).toBe(MISSING)
    expect(cells(all[2])[4]).toBe(signedMoney('100.00', 'TRY'))
    expect(screen.getByText(t('years.splitHint'))).toBeInTheDocument()
    expect(screen.queryByText(t('years.hint'))).toBeNull()
  })

  it('offers no view switch without rows', async () => {
    renderCard(async () => Response.json(response([])))

    await screen.findByText(t('transactions.empty'))
    expect(screen.queryByRole('radiogroup')).toBeNull()
  })
  it('names the best and the worst position of each year', async () => {
    const best = {
      instrument_id: 1,
      symbol: 'XXX',
      market: 'BIST',
      currency: 'TRY',
      total: '120.00',
    }
    const worst = {
      instrument_id: 2,
      symbol: 'DEP',
      market: 'DEPOSIT',
      currency: 'TRY',
      total: '-50.00',
    }
    renderCard(async () => Response.json(response([row({ best, worst })])))

    const table = await screen.findByRole('table')
    const [bestCell, worstCell] = cells(within(table).getAllByRole('row')[1]).slice(-2)
    expect(bestCell).toBe(`XXX${signedMoney('120.00', 'TRY')}`)
    expect(worstCell).toBe(
      `${t('deposits.labelShort', { currency: 'TRY' })}${signedMoney('-50.00', 'TRY')}`,
    )
    expect(within(table).getByRole('columnheader', { name: t('years.best') })).toBeInTheDocument()
    expect(within(table).getByRole('columnheader', { name: t('years.worst') })).toBeInTheDocument()
  })

  it('prints a dash for a year without a best and a worst', async () => {
    const full = row({
      opening: '1.00',
      closing: '2.00',
      net_flow: '0.00',
      gain: '1.00',
      returns: { simple: '0.1', twr: '0.1', mwr: '0.1' },
      benchmark: { simple: '0.1', twr: '0.1', mwr: '0.1' },
      inflation: '0.2',
    })
    renderCard(async () => Response.json(response([full])), { benchmark: 'USD' })

    const table = await screen.findByRole('table')
    expect(cells(within(table).getAllByRole('row')[1]).filter((c) => c === MISSING)).toHaveLength(2)
  })

  describe('benchmark and inflation columns', () => {
    const withBench = [
      row({
        year: '2024',
        to: '2024-12-31',
        returns: { simple: null, twr: '0.100000', mwr: null },
        benchmark: { simple: null, twr: '0.050000', mwr: null },
        inflation: null,
      }),
      row({
        returns: { simple: null, twr: '0.100000', mwr: null },
        benchmark: { simple: null, twr: '0.050000', mwr: null },
        inflation: '0.250000',
      }),
    ]
    const headers = (table: HTMLElement) =>
      within(table)
        .getAllByRole('columnheader')
        .map((h) => h.textContent)

    it('prints the benchmark and the excess as a pill', async () => {
      renderCard(async () => Response.json(response(withBench, [], { currency: 'USD' })), {
        benchmark: 'USD',
      })

      const table = await screen.findByRole('table')
      expect(headers(table).slice(-5, -2)).toEqual([
        t('years.return'),
        'Dolar',
        t('performance.periods.excess'),
      ])
      const last2025 = cells(within(table).getAllByRole('row')[1]).slice(-4, -2)
      expect(last2025).toEqual([signedPercent(0.05), excessPct(0.1, 0.05)])
      expect(within(table).queryByRole('columnheader', { name: 'TÜFE' })).toBeNull()
    })

    it('prints TÜFE and the real return in TRY, estimated when the series is', async () => {
      renderCard(async () =>
        Response.json(response(withBench, [], { estimatedFrom: '2025-12-01' })),
      )

      const table = await screen.findByRole('table')
      const [r2025, r2024] = within(table).getAllByRole('row').slice(1)
      expect(cells(r2025).slice(-4, -2)).toEqual([
        signedPercent(0.25),
        `${excessPct(0.1, 0.25)} (${t('chart.benchmark.estimated')})`,
      ])
      // No TÜFE change for the year: a dash in both.
      expect(cells(r2024).slice(-4, -2)).toEqual([MISSING, MISSING])
      expect(within(table).getByTitle(t('chart.return.realHint'))).toBeInTheDocument()
    })

    it('shows the inflation columns alone when TÜFE is the benchmark in TRY', async () => {
      renderCard(async () => Response.json(response(withBench)), { benchmark: 'TUFE' })

      const table = await screen.findByRole('table')
      expect(headers(table).slice(-5, -2)).toEqual([
        t('years.return'),
        'TÜFE',
        t('performance.periods.real'),
      ])
    })

    it('shows the pair with the real header for TÜFE outside TRY', async () => {
      renderCard(async () => Response.json(response(withBench, [], { currency: 'USD' })), {
        benchmark: 'TUFE',
      })

      const table = await screen.findByRole('table')
      expect(headers(table).slice(-5, -2)).toEqual([
        t('years.return'),
        'Dolar',
        t('performance.periods.real'),
      ])
      expect(within(table).queryByRole('columnheader', { name: 'TÜFE' })).toBeNull()
    })

    it('shows no extra column without a benchmark in a foreign currency', async () => {
      renderCard(async () => Response.json(response(withBench, [], { currency: 'USD' })))

      const table = await screen.findByRole('table')
      expect(headers(table).at(-3)).toBe(t('years.return'))
    })

    it('keeps the columns in the split view', async () => {
      renderCard(async () => Response.json(response(withBench)), { benchmark: 'USD' })

      await screen.findByRole('table')
      fireEvent.click(screen.getByRole('radio', { name: t('years.view.split') }))
      const table = screen.getByRole('table')
      expect(headers(table).slice(-7, -2)).toEqual([
        t('years.return'),
        'Dolar',
        t('performance.periods.excess'),
        'TÜFE',
        t('performance.periods.real'),
      ])
      expect(cells(within(table).getAllByRole('row')[1]).slice(-6, -2)).toEqual([
        signedPercent(0.05),
        excessPct(0.1, 0.05),
        signedPercent(0.25),
        excessPct(0.1, 0.25),
      ])
    })
  })
})
