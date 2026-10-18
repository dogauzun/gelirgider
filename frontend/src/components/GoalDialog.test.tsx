import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { todayIstanbul } from '../lib/date'
import { GoalDialog } from './GoalDialog'

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

// A portfolio with no goal in `currency`; returns the PUT bodies received.
function mockApi(currency: string) {
  const bodies: unknown[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/portfolios/1/summary') return Response.json({ currency })
    if (url !== '/api/portfolios/1/goal') throw new Error(`unexpected request: ${url}`)
    if (init?.method === 'PUT') {
      bodies.push(JSON.parse(init.body as string))
      return Response.json(bodies[0])
    }
    return Response.json({ error: 'goal not found' }, { status: 404 })
  })
  return bodies
}

async function renderDialog(currency: string) {
  const bodies = mockApi(currency)
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <GoalDialog portfolioId={1} onClose={onClose} />
    </QueryClientProvider>,
  )
  await screen.findByLabelText(t('goal.amount'))
  return { bodies, onClose }
}

const save = () => screen.getByRole('button', { name: t('goal.save') })
const fill = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } })

// The first day of next month: the earliest target date the server accepts.
function nextMonth() {
  const [year, month] = todayIstanbul().split('-').map(Number)
  return new Date(Date.UTC(year, month, 1)).toISOString().slice(0, 10)
}

it('keeps Save disabled for an empty amount and for a date in the current month', async () => {
  await renderDialog('TRY')
  expect(save()).toBeDisabled()

  fill(t('goal.date'), nextMonth())
  expect(save()).toBeDisabled()

  fill(t('goal.amount'), '500000')
  expect(save()).toBeEnabled()

  fill(t('goal.date'), `${todayIstanbul().slice(0, 8)}28`)
  expect(save()).toBeDisabled()

  fill(t('goal.date'), todayIstanbul())
  expect(save()).toBeDisabled()
})

it('refuses a date more than 50 years ahead and takes the same month 50 years ahead', async () => {
  await renderDialog('TRY')
  fill(t('goal.amount'), '500000')
  const today = todayIstanbul()
  const capMonth = `${Number(today.slice(0, 4)) + 50}${today.slice(4, 7)}`

  fill(t('goal.date'), `${capMonth}-28`)
  expect(save()).toBeEnabled()

  fill(t('goal.date'), '5000-12-01')
  expect(save()).toBeDisabled()
  expect(screen.getByText(t('goal.error.targetDateFar'))).toBeInTheDocument()
})

it('offers no usd unit for a USD reporting currency, and real only for TRY', async () => {
  await renderDialog('USD')
  fireEvent.click(screen.getByRole('combobox', { name: t('goal.unit') }))
  const options = await screen.findAllByRole('option')
  expect(options.map((o) => o.textContent)).toEqual(['USD'])
  cleanup()

  await renderDialog('TRY')
  fireEvent.click(screen.getByRole('combobox', { name: t('goal.unit') }))
  const tryOptions = await screen.findAllByRole('option')
  expect(tryOptions.map((o) => o.textContent)).toEqual([
    'TRY',
    t('goal.unit.real'),
    t('goal.unit.usd'),
  ])
})

it('sends the typed goal', async () => {
  const { bodies, onClose } = await renderDialog('TRY')

  fill(t('goal.amount'), '1.500.000,50')
  fill(t('goal.date'), nextMonth())
  fireEvent.click(save())

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(bodies).toEqual([{ amount: '1500000.50', unit: 'nominal', target_date: nextMonth() }])
})
