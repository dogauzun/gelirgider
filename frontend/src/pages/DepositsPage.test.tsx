import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import type { Deposit } from '../api/deposits'
import { t } from '../i18n/strings'
import { formatMoney } from '../lib/format'
import { DepositsPage } from './DepositsPage'

// jsdom has no showModal() on <dialog>; both methods flip the open state, and
// close() hands focus back to what had it before showModal(), as browsers do.
let focusBeforeDialog: Element | null = null
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    focusBeforeDialog = document.activeElement
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
    if (focusBeforeDialog instanceof HTMLElement) focusBeforeDialog.focus()
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
  renewed_from: null,
  value: '103254.80',
  accrued_interest: '3254.80',
  net_interest_at_maturity: '3254.80',
  payout: '103254.80',
  status: 'matured',
  ...fields,
})

// Answers the list with `deposits` and records the deleted ids; a DELETE drops
// the deposit from the list.
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
      deposits.splice(
        deposits.findIndex((item) => url === `/api/deposits/${item.id}`),
        1,
      )
      return new Response(null, { status: 204 })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return deleted
}

const deleteName = (item: Deposit) =>
  t('deposits.delete', {
    principal: formatMoney(item.principal, item.currency),
    currency: item.currency,
  })

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
  expect(within(row).queryByRole('button', { name: /mevduatını sil/ })).toBeNull()
})

it('offers Yenile on a due or matured deposit until another one renews it', async () => {
  mockApi([
    deposit({ id: 1, status: 'matured' }),
    deposit({ id: 2, status: 'due', renewed_from: 1 }),
    deposit({ id: 3, status: 'active' }),
  ])
  renderPage()

  // Deposit 1 is renewed by 2 and 3 is still running: only 2 offers it.
  const buttons = await screen.findAllByRole('button', { name: /mevduatını yenile/ })
  expect(buttons).toHaveLength(1)
  fireEvent.click(buttons[0])
  expect(screen.getByRole('dialog', { name: t('deposits.renewDialog.title') })).toBeInTheDocument()
})

it('deletes an active deposit after a confirm', async () => {
  const deleted = mockApi([deposit({ id: 7, status: 'active' })])
  renderPage()

  fireEvent.click(await screen.findByRole('button', { name: /mevduatını sil/ }))
  const confirm = await screen.findByRole('dialog', { name: t('deposits.deleteConfirm') })
  fireEvent.click(within(confirm).getByRole('button', { name: t('deposits.deleteButton') }))

  await waitFor(() => expect(deleted).toEqual(['/api/deposits/7']))
})

it('moves focus to the next deletable deposit after a delete, then to the add button', async () => {
  const deposits = [
    deposit({ id: 1, status: 'active', principal: '1000' }),
    deposit({ id: 2, status: 'matured', principal: '2000' }),
    deposit({ id: 3, status: 'active', principal: '3000' }),
  ]
  mockApi(deposits)
  renderPage()

  // The matured deposit has no delete button: the next active one takes over.
  const first = await screen.findByRole('button', { name: deleteName(deposits[0]) })
  first.focus()
  fireEvent.click(first)
  let confirm = await screen.findByRole('dialog', { name: t('deposits.deleteConfirm') })
  fireEvent.click(within(confirm).getByRole('button', { name: t('deposits.deleteButton') }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: deleteName(deposits[1]) })).toHaveFocus(),
  )

  // The only deletable row left (the matured one stays): the add button.
  const last = screen.getByRole('button', { name: deleteName(deposits[1]) })
  fireEvent.click(last)
  confirm = await screen.findByRole('dialog', { name: t('deposits.deleteConfirm') })
  fireEvent.click(within(confirm).getByRole('button', { name: t('deposits.deleteButton') }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: t('deposits.open') })).toHaveFocus(),
  )
})

it('offers to open the first deposit when there is none', async () => {
  mockApi([])
  renderPage()

  expect(await screen.findByText(t('deposits.empty'))).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: t('deposits.open') }))
  expect(screen.getByRole('dialog', { name: t('deposits.dialog.title') })).toBeInTheDocument()
})

it('positions the table card so the sr-only actions header cannot widen the page', async () => {
  mockApi([deposit({})])
  renderPage()

  await screen.findByText(t('deposits.status.matured'))
  // The card scrolls the table sideways; without `relative` the absolutely
  // positioned header span escapes it and the page scrolls sideways at 375 px.
  expect(screen.getByRole('table').parentElement).toHaveClass('relative', 'overflow-x-auto')
})
