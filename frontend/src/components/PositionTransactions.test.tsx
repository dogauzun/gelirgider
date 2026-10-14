import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import type { Transaction, useTransactions } from '../api/transactions'
import { PositionTransactions } from './PositionTransactions'

const buy: Transaction = {
  id: 1,
  portfolio_id: 1,
  instrument_id: 7,
  instrument_symbol: 'THYAO',
  instrument_name: 'Türk Hava Yolları',
  type: 'buy',
  date: '2026-03-02',
  quantity: '10',
  price: '250.00',
  amount: '2500.00',
  fee: '0',
  currency: 'TRY',
  note: null,
  created_at: '2026-03-02T09:00:00Z',
  import_id: null,
}

// The table scrolls sideways inside its box, never the page.
it('scrolls the table sideways inside its box', () => {
  const query = { data: [buy] } as unknown as ReturnType<typeof useTransactions>
  render(<PositionTransactions transactions={query} />)
  expect(screen.getByRole('table').parentElement).toHaveClass('relative', 'overflow-x-auto')
})
