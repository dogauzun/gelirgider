import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import type { Deposit } from '../api/deposits'
import { t } from '../i18n/strings'
import { DepositsPage } from './DepositsPage'

// jsdom has no showModal() on <dialog>; this only flips the open state.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const deposit = (fields: Partial<Deposit>): Deposit => ({
  id: 1,
  instrument_id: 10,
  currency: 'TRY',
  principal: '100000',
  rate: '0.45',
  stopaj_rate: '0.175',
  start_date: '2026-01-01',
  maturity_date: '2026-02-02',
  value: '103254.80',
  accrued_interest: '3254.80',
  net_interest_at_maturity: '3254.80',
  payout: '103254.80',
  status: 'matured',
  ...fields,
})

// Answers the list with `deposits` and records the deleted ids.
function mockApi(deposits: Deposit[]) {
  const deleted: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/portfolios') {
      return Response.json([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }])
    }
    if (url === '/api/portfolios/1/deposits') return Response.json(deposits)
    if (url.startsWith('/api/deposits/') && init?.method === 'DELETE') {
      deleted.push(url)
      return new Response(null, { status: 204 })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return deleted
}

function renderPage() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={['/portfolios/1/deposits']}>
        <Routes>
          <Route path="/portfolios/:id/deposits" element={<DepositsPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

it('shows a matured deposit with its payout and the Kapandı badge, and no delete', async () => {
  mockApi([deposit({})])
  renderPage()

  const row = (await screen.findByText(t('deposits.status.matured'))).closest('tr') as HTMLElement
  // The payout is the ninth cell, after the value and the accrued interest.
  expect(within(row).getAllByRole('cell')[8]).toHaveTextContent('103.254,80')
  expect(within(row).getByText('%45')).toBeInTheDocument()
  expect(within(row).getByText('%17,5')).toBeInTheDocument()
  expect(within(row).queryByRole('button')).toBeNull()
})

it('deletes an active deposit after a confirm', async () => {
  const deleted = mockApi([deposit({ id: 7, status: 'active' })])
  renderPage()

  fireEvent.click(await screen.findByRole('button', { name: /mevduatını sil/ }))
  const confirm = await screen.findByRole('dialog', { name: t('deposits.deleteConfirm') })
  fireEvent.click(within(confirm).getByRole('button', { name: t('deposits.deleteButton') }))

  await waitFor(() => expect(deleted).toEqual(['/api/deposits/7']))
})

it('offers to open the first deposit when there is none', async () => {
  mockApi([])
  renderPage()

  expect(await screen.findByText(t('deposits.empty'))).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: t('deposits.open') }))
  expect(screen.getByRole('dialog', { name: t('deposits.dialog.title') })).toBeInTheDocument()
})
