import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import type { DuePlan, Plan } from '../api/plans'
import { t } from '../i18n/strings'
import { PlansPage } from './PlansPage'

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

// Answers the lists and records the book POSTs.
function mockApi(dues: DuePlan[]) {
  const booked: string[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/portfolios') {
      return Response.json([{ id: 1, name: 'Emeklilik', created_at: '2026-01-01T00:00:00Z' }])
    }
    if (url === '/api/portfolios/1/plans') return Response.json([plan])
    if (url === '/api/portfolios/1/plans/due') return Response.json(dues)
    if (url === '/api/plans/3/book' && init?.method === 'POST') {
      booked.push(url)
      return Response.json({ ...plan, status: 'active' })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return booked
}

function renderPage() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={['/portfolios/1/plans']}>
        <Routes>
          <Route path="/portfolios/:id/plans" element={<PlansPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

it('shows a due plan with its proposed booking and books it once', async () => {
  const booked = mockApi([due])
  renderPage()

  const row = (await screen.findByText(t('plans.status.due'))).closest('tr') as HTMLElement
  expect(within(row).getByText('XYZ')).toBeInTheDocument()
  expect(await within(row).findByText('8 adet × ₺123,45')).toBeInTheDocument()

  fireEvent.click(within(row).getByRole('button', { name: t('plans.book') }))

  await waitFor(() => expect(booked).toEqual(['/api/plans/3/book']))
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
