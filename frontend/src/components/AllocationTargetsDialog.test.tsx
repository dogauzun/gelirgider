import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { AllocationTargetsDialog } from './AllocationTargetsDialog'

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

// Answers the PUT with `reply` and returns the bodies it got.
function mockApi(reply: () => Promise<Response> | Response) {
  const bodies: unknown[] = []
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    if (String(input) === '/api/portfolios/1/allocation-targets' && init?.method === 'PUT') {
      bodies.push(JSON.parse(init.body as string))
      return reply()
    }
    throw new Error(`unexpected request: ${String(input)}`)
  })
  return bodies
}

function renderDialog() {
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={new QueryClient()}>
      <AllocationTargetsDialog
        portfolioId={1}
        targets={[
          { asset_class: 'equity', weight: '60' },
          { asset_class: 'fund', weight: '40' },
        ]}
        onClose={onClose}
      />
    </QueryClientProvider>,
  )
  return onClose
}

const save = () => screen.getByRole('button', { name: t('transactions.save') })

it('saves the targets and closes', async () => {
  const bodies = mockApi(() => Response.json({ targets: [] }))
  const onClose = renderDialog()

  fireEvent.click(save())

  await waitFor(() => expect(onClose).toHaveBeenCalled())
  expect(bodies).toEqual([
    {
      targets: [
        { asset_class: 'equity', weight: '60' },
        { asset_class: 'fund', weight: '40' },
      ],
    },
  ])
})

// isPending reaches the button a tick after the click, so a double click
// sent the PUT twice.
it('sends the targets once on a double click of save', async () => {
  const bodies = mockApi(() => new Promise<Response>(() => {}))
  renderDialog()

  fireEvent.click(save())
  fireEvent.click(save())

  await waitFor(() => expect(save()).toHaveAttribute('aria-disabled', 'true'))
  expect(bodies).toHaveLength(1)
})

it('keeps the dialog open with the error when the save fails', async () => {
  mockApi(() => Response.json({ error: 'internal error' }, { status: 500 }))
  const onClose = renderDialog()

  fireEvent.click(save())

  expect(await screen.findByRole('alert')).toHaveTextContent(t('error.internal'))
  expect(onClose).not.toHaveBeenCalled()
})
