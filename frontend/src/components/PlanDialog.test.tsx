import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { PlanDialog } from './PlanDialog'

// jsdom implements neither method on <dialog>; both only flip the open state.
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

const xyz = {
  id: 7,
  symbol: 'XYZ',
  market: 'BIST',
  name: 'XYZ Holding',
  currency: 'TRY',
  sector_override: null,
}

// Answers the instrument search and the plan POST; returns the POST bodies.
function mockApi() {
  const bodies: unknown[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url.startsWith('/api/instruments?')) return Response.json([xyz])
    if (url === '/api/portfolios/1/plans' && init?.method === 'POST') {
      bodies.push(JSON.parse(init.body as string))
      return Response.json({ id: 1 }, { status: 201 })
    }
    throw new Error(`unexpected request: ${url}`)
  })
  return bodies
}

async function fillBuyPlan() {
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <PlanDialog portfolioId={1} onClose={onClose} />
    </QueryClientProvider>,
  )
  fireEvent.change(screen.getByLabelText(t('transactions.instrument')), {
    target: { value: 'xyz' },
  })
  fireEvent.click(await screen.findByRole('option', { name: /XYZ Holding/ }))
  fireEvent.change(screen.getByLabelText(`${t('plans.amount')} · TRY`), {
    target: { value: '1000' },
  })
  fireEvent.change(screen.getByLabelText(t('plans.dialog.dayField')), { target: { value: '5' } })
  fireEvent.change(screen.getByLabelText(t('plans.start')), { target: { value: '2026-11-01' } })
  return onClose
}

const submit = () => fireEvent.click(screen.getByRole('button', { name: t('plans.dialog.submit') }))

// A buy plan names no currency (the instrument has it) and an unticked box
// sends no fund: a user who already recorded the cash must not get it twice.
it('posts a buy plan with no currency and no fund', async () => {
  const bodies = mockApi()
  const onClose = await fillBuyPlan()

  submit()

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(bodies).toEqual([
    {
      instrument_id: 7,
      amount: '1000',
      day_of_month: 5,
      start_date: '2026-11-01',
      note: 'Düzenli alım',
    },
  ])
})

it('posts fund true with the box ticked', async () => {
  const bodies = mockApi()
  const onClose = await fillBuyPlan()

  fireEvent.click(screen.getByRole('checkbox', { name: t('plans.dialog.fund') }))
  submit()

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(bodies).toEqual([expect.objectContaining({ instrument_id: 7, fund: true })])
})

it('shows the day error under the field for a missing day and posts nothing', async () => {
  const bodies = mockApi()
  await fillBuyPlan()

  fireEvent.change(screen.getByLabelText(t('plans.dialog.dayField')), { target: { value: '' } })
  submit()

  expect(await screen.findByText(t('error.field.day_of_month'))).toBeInTheDocument()
  expect(bodies).toEqual([])
})
