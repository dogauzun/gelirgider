import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { DepositDialog } from './DepositDialog'

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

// Answers the POST with `reply` and returns the bodies it received.
function mockPost(reply: Response = Response.json({ id: 1 }, { status: 201 })) {
  const bodies: unknown[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    if (String(input) !== '/api/portfolios/1/deposits' || init?.method !== 'POST') {
      throw new Error(`unexpected request: ${String(input)}`)
    }
    bodies.push(JSON.parse(init.body as string))
    return reply
  })
  return bodies
}

function fill(values: Record<string, string>) {
  for (const [label, value] of Object.entries(values)) {
    fireEvent.change(screen.getByLabelText(label), { target: { value } })
  }
}

function renderDialog(onClose = vi.fn()) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <DepositDialog portfolioId={1} onClose={onClose} />
    </QueryClientProvider>,
  )
  return onClose
}

const terms = {
  [t('deposits.principal')]: '100000',
  [t('deposits.dialog.rate')]: '45',
  [t('deposits.dialog.stopaj')]: '17.5',
  [t('deposits.start')]: '2026-01-01',
  [t('deposits.maturity')]: '2026-02-02',
}
const submit = () =>
  fireEvent.click(screen.getByRole('button', { name: t('deposits.dialog.submit') }))

// Rate and stopaj go out as fractions through a decimal shift, and the cash
// deposit is asked for only when the box is ticked.
it('sends the terms as fractions and no fund by default', async () => {
  const bodies = mockPost()
  const onClose = renderDialog()

  fill(terms)
  submit()

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(bodies).toEqual([
    {
      currency: 'TRY',
      principal: '100000',
      rate: '0.45',
      stopaj_rate: '0.175',
      start_date: '2026-01-01',
      maturity_date: '2026-02-02',
    },
  ])
})

it('sends fund true when the box is ticked', async () => {
  const bodies = mockPost()
  const onClose = renderDialog()

  fill(terms)
  fireEvent.click(screen.getByRole('checkbox', { name: t('deposits.fund') }))
  submit()

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(bodies).toEqual([expect.objectContaining({ rate: '0.45', fund: true })])
})

it('leaves the stopaj empty and refuses a form without it', () => {
  const bodies = mockPost()
  renderDialog()

  expect(screen.getByLabelText(t('deposits.dialog.stopaj'))).toHaveValue('')
  fill({ ...terms, [t('deposits.dialog.stopaj')]: '' })
  submit()

  expect(screen.getByRole('alert')).toHaveTextContent(t('deposits.error.stopaj'))
  expect(bodies).toEqual([])
})

it('shows the server message inline and stays open', async () => {
  mockPost(
    Response.json(
      { error: 'maturity_date: must be at most 10 years after start_date' },
      { status: 400 },
    ),
  )
  const onClose = renderDialog()

  fill(terms)
  submit()

  expect(await screen.findByRole('alert')).toHaveTextContent(t('deposits.error.maturityDate'))
  expect(onClose).not.toHaveBeenCalled()
})
