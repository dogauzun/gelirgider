import { cleanup, render, screen } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { afterEach, expect, it } from 'vitest'
import type { PositionHistoryResponse } from '../api/positions'
import { chooseOption } from '../test/select'
import { PositionReturnCard as Card } from './PositionReturnCard'

const BENCHMARKS = [{ value: 'XU100', label: 'BIST 100' }]

// The card with no benchmark chosen unless a test passes one.
const PositionReturnCard = (
  props: Partial<ComponentProps<typeof Card>> &
    Pick<ComponentProps<typeof Card>, 'history' | 'currency'>,
) => <Card benchmark={null} benchmarks={BENCHMARKS} onBenchmark={() => {}} {...props} />

const history = (
  returns: Partial<PositionHistoryResponse['returns']>,
  from: string,
  to: string,
  benchmark: PositionHistoryResponse['benchmark'] = null,
): { data: PositionHistoryResponse; isError: false } => ({
  isError: false,
  data: {
    currency: 'TRY',
    returns: { simple: '0.125', twr: '0.05', mwr: '0.090909', ...returns },
    returns_from: from,
    returns_to: to,
    benchmark,
    points: [from, to].map((date) => ({
      date,
      benchmark_twr: benchmark ? '0.1' : null,
      close: '10',
      quantity: '1',
      avg_cost: '10',
      cost_basis: '10',
      market_value: '10',
    })),
  },
})

afterEach(() => {
  cleanup()
  localStorage.clear()
})

it('shows the stored method, and no annualized line within a year', () => {
  localStorage.setItem('returnMethod', 'mwr')
  render(<PositionReturnCard history={history({}, '2026-01-01', '2026-01-11')} currency="TRY" />)
  expect(screen.getByRole('heading', { name: 'Getiri' })).toBeInTheDocument()
  expect(screen.getByText('+%9,09')).toBeInTheDocument()
  expect(screen.queryByText(/yıllık/)).toBeNull()
  expect(screen.getByText('TRY cinsinden, temettüler dahil')).toBeInTheDocument()
})

it('annualizes from a year apart, a calendar year counting as one year', () => {
  localStorage.setItem('returnMethod', 'mwr')
  render(<PositionReturnCard history={history({}, '2025-01-01', '2025-12-31')} currency="TRY" />)
  expect(screen.getByText('yıllık +%9,09')).toBeInTheDocument()
})

it('annualizes and tags estimates only up to returns_to, not the range end', () => {
  localStorage.setItem('returnMethod', 'mwr')
  const closed = history({}, '2025-01-01', '2026-02-05', {
    code: 'XU100',
    returns: { simple: '0.1', twr: '0.1', mwr: '0.1' },
    estimated_from: '2025-06-01',
  })
  // Closed on 2025-01-31: 30 days of returns inside a 400-day range.
  closed.data.returns_to = '2025-01-31'
  render(<PositionReturnCard history={closed} currency="TRY" benchmark="XU100" />)
  expect(screen.getByText('+%9,09')).toBeInTheDocument()
  expect(screen.queryByText(/yıllık/)).toBeNull()
  expect(screen.queryByText(/tahmini/)).toBeNull()
})

it('shows a dash with the undefined title for a null return', () => {
  localStorage.setItem('returnMethod', 'mwr')
  render(
    <PositionReturnCard
      history={history({ mwr: null }, '2024-01-01', '2025-12-31')}
      currency="TRY"
    />,
  )
  expect(screen.getByText('—').closest('span[title]')).toHaveAttribute(
    'title',
    'Bu aralık için tanımsız',
  )
  expect(screen.queryByText(/yıllık/)).toBeNull()
})

it('shows twr after choosing it', async () => {
  localStorage.setItem('returnMethod', 'mwr')
  render(<PositionReturnCard history={history({}, '2026-01-01', '2026-01-11')} currency="TRY" />)
  await chooseOption(screen.getByRole('combobox', { name: 'Getiri yöntemi' }), 'Zaman ağırlıklı')
  expect(screen.getByText('+%5,00')).toBeInTheDocument()
})

it('shows nothing under the head on an error', () => {
  render(<PositionReturnCard history={{ data: undefined, isError: true }} currency="TRY" />)
  expect(screen.queryByText(/cinsinden/)).toBeNull()
})

const XU100 = (estimated_from: string | null = null) => ({
  code: 'XU100' as const,
  returns: { simple: '0.1', twr: '0.1', mwr: '0.100000' },
  estimated_from,
})

it('shows the benchmark pill: its return by the method and the excess over it', () => {
  localStorage.setItem('returnMethod', 'mwr')
  render(
    <PositionReturnCard
      history={history({}, '2026-01-01', '2026-01-11', XU100())}
      currency="TRY"
      benchmark="XU100"
    />,
  )
  expect(screen.getByText('BIST 100 +%10,00 · fark -%0,83')).toBeInTheDocument()
})

it('marks an estimated benchmark', () => {
  localStorage.setItem('returnMethod', 'mwr')
  render(
    <PositionReturnCard
      history={history({}, '2026-01-01', '2026-01-11', XU100('2026-01-05'))}
      currency="TRY"
      benchmark="XU100"
    />,
  )
  expect(screen.getByText(/BIST 100 \+%10,00 \(tahmini\)/)).toBeInTheDocument()
})

it('says so when the benchmark has no values in the range', () => {
  const h = history({}, '2026-01-01', '2026-01-11', XU100())
  h.data.points.forEach((p) => (p.benchmark_twr = null))
  render(<PositionReturnCard history={h} currency="TRY" benchmark="XU100" />)
  expect(screen.getByText('BIST 100 · bu aralık için veri yok')).toBeInTheDocument()
})

it('does not mark an undefined benchmark return as estimated', () => {
  localStorage.setItem('returnMethod', 'mwr')
  const b = XU100('2026-01-05')
  render(
    <PositionReturnCard
      history={history({}, '2026-01-01', '2026-01-11', {
        ...b,
        returns: { ...b.returns, mwr: null },
      })}
      currency="TRY"
      benchmark="XU100"
    />,
  )
  expect(screen.getByText('BIST 100 —')).toBeInTheDocument()
  expect(screen.queryByText(/tahmini/)).toBeNull()
})
