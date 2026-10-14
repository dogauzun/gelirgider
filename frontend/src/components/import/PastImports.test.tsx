import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import { importsQueryKey, type Import } from '../../api/imports'
import { PastImports } from './PastImports'
import { viewImportClass } from './shared'

const imported: Import = {
  id: 41,
  broker: 'midas',
  portfolio_id: 7,
  period_start: '2026-06-01',
  period_end: '2026-06-30',
  currency: 'TRY',
  transaction_count: 12,
  created_at: '2026-07-02T10:00:00Z',
}

// jsdom implements neither method on <dialog>.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})

afterEach(() => vi.restoreAllMocks())

function renderPast(imports: Import[] = [imported]) {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } })
  client.setQueryData(importsQueryKey, imports)
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <PastImports portfolioId={7} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// "İşlemlerde gör" looked different here and on the Bitti list, and sat
// ~4 px from "Geri al" (GLG-1056).
it('styles the view link as on the Bitti list and keeps the usual gap to undo', () => {
  renderPast()

  const link = screen.getByRole('link', { name: 'İşlemlerde gör' })
  expect(link).toHaveAttribute('href', '/portfolios/7/transactions?import=41')
  expect(link.className).toBe(viewImportClass)
  expect(link.parentElement).toHaveClass('gap-2')
  expect(link.parentElement).toContainElement(screen.getByRole('button', { name: 'Geri al' }))
})

// Undoing one import put every row's "Geri al" into the pending state, so
// nothing showed which import was going (GLG-1074).
it('shows only the undone row as pending', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}))
  renderPast([
    imported,
    { ...imported, id: 42, period_start: '2026-07-01', period_end: '2026-07-31' },
  ])

  const [first, second] = screen.getAllByRole('button', { name: 'Geri al' })
  fireEvent.click(second)
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Geri al' }))

  await waitFor(() => expect(second).toHaveAttribute('aria-disabled', 'true'))
  expect(first).not.toHaveAttribute('aria-disabled')
})

// With pending only on the undone row, the other rows' "Geri al" stayed
// clickable, and a second undo took the mutation from the first one, whose
// toast, focus move and error were then lost (GLG-1074).
it('starts no second undo while one is running', async () => {
  const fetch = vi.spyOn(globalThis, 'fetch').mockImplementation(() => new Promise(() => {}))
  renderPast([
    imported,
    { ...imported, id: 42, period_start: '2026-07-01', period_end: '2026-07-31' },
  ])

  const [first, second] = screen.getAllByRole('button', { name: 'Geri al' })
  fireEvent.click(second)
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Geri al' }))
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1))

  fireEvent.click(first)
  const dialog = screen.queryByRole('dialog')
  if (dialog) fireEvent.click(within(dialog).getByRole('button', { name: 'Geri al' }))
  await new Promise((resolve) => setTimeout(resolve, 50))
  expect(fetch).toHaveBeenCalledTimes(1)
})
