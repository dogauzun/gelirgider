import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import type { DuePlan, Plan } from '../api/plans'
import { Toaster } from '../components/ui/Toast'
import { t } from '../i18n/strings'
import { formatMoney } from '../lib/format'
import { PlansPage } from './PlansPage'

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

const plan: Plan = {
  id: 3,
  portfolio_id: 1,
  instrument_id: 7,
  symbol: 'XYZ',
  market: 'BIST',
  currency: 'TRY',
  amount: '1000',
  day_of_month: 5,
  start_date: '2026-01-05',
  end_date: null,
  next_date: '2026-10-05',
  fund: false,
  note: 'Düzenli alım',
  status: 'due',
}

const due: DuePlan = {
  plan_id: 3,
  instrument_id: 7,
  symbol: 'XYZ',
  market: 'BIST',
  currency: 'TRY',
  amount: '1000',
  date: '2026-10-05',
  missed: 1,
  price: '123.45',
  price_date: '2026-10-05',
  quantity: '8',
  booked_amount: '987.60',
}

// Answers the lists and records the book and skip POSTs; a DELETE drops the plan from
// the list.
function mockApi(dues: DuePlan[], plans: Plan[] = [plan]) {
  const posted: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/portfolios') {
      return Response.json([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }])
    }
    if (url === '/api/portfolios/1/plans') return Response.json(plans)
    if (url.startsWith('/api/plans/') && init?.method === 'DELETE') {
      plans.splice(
        plans.findIndex((item) => url === `/api/plans/${item.id}`),
        1,
      )
      return new Response(null, { status: 204 })
    }
    if (url === '/api/portfolios/1/plans/due') return Response.json(dues)
    if ((url === '/api/plans/3/book' || url === '/api/plans/3/skip') && init?.method === 'POST') {
      posted.push(url)
      return Response.json({ ...plan, status: 'active' })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return posted
}

function renderPage() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <Toaster />
      <MemoryRouter initialEntries={['/portfolios/1/plans']}>
        <Routes>
          <Route path="/portfolios/:id/plans" element={<PlansPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

it('shows a due plan with its proposed booking and books it once', async () => {
  const posted = mockApi([due])
  renderPage()

  const row = (await screen.findByText(t('plans.status.due'))).closest('tr') as HTMLElement
  expect(within(row).getByText('XYZ')).toBeInTheDocument()
  expect(await within(row).findByText('8 adet × ₺123,45')).toBeInTheDocument()

  fireEvent.click(within(row).getByRole('button', { name: t('plans.book') }))

  await waitFor(() => expect(posted).toEqual(['/api/plans/3/book']))
})

it.each([
  ['plans.book', 'plans.booked', '/api/plans/3/book'],
  ['plans.skip', 'plans.skipped', '/api/plans/3/skip'],
] as const)('sends one request when %s is double clicked', async (action, done, url) => {
  const posted = mockApi([due])
  renderPage()

  const row = (await screen.findByText(t('plans.status.due'))).closest('tr') as HTMLElement
  const button = await within(row).findByRole('button', { name: t(action) })
  fireEvent.click(button)
  fireEvent.click(button)

  expect(await screen.findByText(t(done))).toBeInTheDocument()
  expect(posted).toEqual([url])
})

it('disables Kaydet when the due plan has no close', async () => {
  mockApi([{ ...due, price: null, price_date: null, quantity: null, booked_amount: null }])
  renderPage()

  const row = (await screen.findByText(t('plans.status.due'))).closest('tr') as HTMLElement
  expect(await within(row).findByText(t('plans.noPrice'))).toBeInTheDocument()
  expect(within(row).getByRole('button', { name: t('plans.book') })).toBeDisabled()
  expect(within(row).getByRole('button', { name: t('plans.skip') })).toBeEnabled()
})

it('disables Kaydet when the due plan buys no unit', async () => {
  mockApi([{ ...due, price: '310', quantity: '0', booked_amount: '0.00' }])
  renderPage()

  const row = (await screen.findByText(t('plans.status.due'))).closest('tr') as HTMLElement
  expect(await within(row).findByText(t('plans.noUnit'))).toBeInTheDocument()
  expect(within(row).queryByText(/0 adet/)).toBeNull()
  expect(within(row).getByRole('button', { name: t('plans.book') })).toBeDisabled()
  expect(within(row).getByRole('button', { name: t('plans.skip') })).toBeEnabled()
})

const deleteName = (item: Plan) =>
  t('plans.delete', {
    summary: `${item.symbol ?? t('plans.cash')} · ${formatMoney(item.amount, item.currency)}`,
  })

async function deletePlan(item: Plan) {
  const trigger = await screen.findByRole('button', { name: deleteName(item) })
  trigger.focus()
  fireEvent.click(trigger)
  const confirm = await screen.findByRole('dialog', { name: t('plans.deleteConfirm') })
  return { trigger, confirm }
}

it('moves focus to the next plan after a delete, then to the previous, then to the add button', async () => {
  const plans = [
    { ...plan, id: 1, amount: '100' },
    { ...plan, id: 2, amount: '200' },
    { ...plan, id: 3, amount: '300' },
  ]
  mockApi([], plans)
  renderPage()

  let { confirm } = await deletePlan(plans[0])
  fireEvent.click(within(confirm).getByRole('button', { name: t('plans.deleteButton') }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: deleteName(plans[0]) })).toHaveFocus(),
  )
  expect(plans.map((item) => item.id)).toEqual([2, 3])

  // The last row: the previous one takes over.
  ;({ confirm } = await deletePlan(plans[1]))
  fireEvent.click(within(confirm).getByRole('button', { name: t('plans.deleteButton') }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: deleteName(plans[0]) })).toHaveFocus(),
  )

  // The only row: the empty card's add button.
  ;({ confirm } = await deletePlan(plans[0]))
  fireEvent.click(within(confirm).getByRole('button', { name: t('plans.deleteButton') }))
  await waitFor(() => expect(screen.getByRole('button', { name: t('plans.add') })).toHaveFocus())
})

it('keeps focus on the trash button when the delete is cancelled', async () => {
  mockApi(
    [],
    [
      { ...plan, id: 1 },
      { ...plan, id: 2, amount: '200' },
    ],
  )
  renderPage()

  const { trigger, confirm } = await deletePlan({ ...plan, id: 1 })
  fireEvent.click(within(confirm).getByRole('button', { name: t('confirm.cancel') }))

  await waitFor(() => expect(trigger).toHaveFocus())
})
