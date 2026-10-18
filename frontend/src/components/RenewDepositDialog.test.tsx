import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import type { Deposit } from '../api/deposits'
import { t } from '../i18n/strings'
import { RenewDepositDialog } from './RenewDepositDialog'

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

const old: Deposit = {
  id: 7,
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
  status: 'due',
}

// Answers the POST with `reply` and returns the bodies it received.
function mockPost(reply: Response) {
  const bodies: unknown[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    if (String(input) !== '/api/deposits/7/renew' || init?.method !== 'POST') {
      throw new Error(`unexpected request: ${String(input)}`)
    }
    bodies.push(JSON.parse(init.body as string))
    return reply
  })
  return bodies
}

// Renders the dialog, fills the three terms and submits.
function renderAndSubmit() {
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <RenewDepositDialog deposit={old} onClose={onClose} />
    </QueryClientProvider>,
  )
  const terms = {
    [t('deposits.dialog.rate')]: '40',
    [t('deposits.dialog.stopaj')]: '15',
    [t('deposits.maturity')]: '2026-06-02',
  }
  for (const [label, value] of Object.entries(terms)) {
    fireEvent.change(screen.getByLabelText(label), { target: { value } })
  }
  fireEvent.click(screen.getByRole('button', { name: t('deposits.renewDialog.submit') }))
  return onClose
}

it('shows the payout as the new principal and posts the terms as fractions once', async () => {
  const bodies = mockPost(Response.json({ id: 8 }, { status: 201 }))
  const onClose = renderAndSubmit()

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  // A decimal shift on the string: 40 % is "0.4", the same number as "0.40".
  expect(bodies).toEqual([{ rate: '0.4', stopaj_rate: '0.15', maturity_date: '2026-06-02' }])
  expect(screen.getByLabelText(t('deposits.renewDialog.principal'))).toHaveValue('₺103.254,80')
})

it('shows the 409 message inline and stays open', async () => {
  mockPost(Response.json({ error: 'deposit is already renewed' }, { status: 409 }))
  const onClose = renderAndSubmit()

  expect(await screen.findByRole('alert')).toHaveTextContent(t('deposits.error.renewed'))
  expect(onClose).not.toHaveBeenCalled()
})
