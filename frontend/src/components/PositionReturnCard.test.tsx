import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { PositionHistoryResponse } from '../api/positions'
import { chooseOption } from '../test/select'
import { PositionReturnCard } from './PositionReturnCard'

const history = (
  returns: Partial<PositionHistoryResponse['returns']>,
  from: string,
  to: string,
): { data: PositionHistoryResponse; isError: false } => ({
  isError: false,
  data: {
    currency: 'TRY',
    returns: { simple: '0.125', twr: '0.05', mwr: '0.090909', ...returns },
    returns_from: from,
    points: [from, to].map((date) => ({
      date,
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
