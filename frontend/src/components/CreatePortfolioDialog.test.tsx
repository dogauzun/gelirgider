import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { CreatePortfolioDialog } from './CreatePortfolioDialog'

// jsdom implements neither method on <dialog>.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})

afterEach(() => {
  vi.restoreAllMocks()
})

// A blank name used to be a silent no-op: no request and no error.
it('shows the name error for a blank name without a request', () => {
  const fetchSpy = vi.spyOn(globalThis, 'fetch')
  render(
    <QueryClientProvider client={new QueryClient()}>
      <CreatePortfolioDialog onClose={() => {}} onCreated={() => {}} />
    </QueryClientProvider>,
  )
  const input = screen.getByRole('textbox', { name: t('portfolios.name') })
  fireEvent.change(input, { target: { value: '   ' } })
  fireEvent.click(screen.getByRole('button', { name: t('portfolios.create') }))

  expect(screen.getByRole('alert')).toHaveTextContent(t('error.portfolioName'))
  expect(input).toHaveAttribute('aria-invalid', 'true')
  expect(fetchSpy).not.toHaveBeenCalled()
})

// The "name already used" error is about the name sent, so editing
// the name drops it, as on the signed-out forms.
it('clears the server error once the name is edited', async () => {
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(
    new Response(JSON.stringify({ error: 'name already used' }), { status: 409 }),
  )
  render(
    <QueryClientProvider client={new QueryClient()}>
      <CreatePortfolioDialog onClose={() => {}} onCreated={() => {}} />
    </QueryClientProvider>,
  )
  const input = screen.getByRole('textbox', { name: t('portfolios.name') })
  fireEvent.change(input, { target: { value: 'Ana Portföy' } })
  fireEvent.click(screen.getByRole('button', { name: t('portfolios.create') }))
  await waitFor(() =>
    expect(screen.getByRole('alert')).toHaveTextContent(t('error.portfolioNameTaken')),
  )
  expect(input).toHaveAttribute('aria-invalid', 'true')

  fireEvent.change(input, { target: { value: 'Ana Portföy 2' } })

  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  expect(input).not.toHaveAttribute('aria-invalid')
})

// isPending reaches the button a tick after the click, so a double
// click's second click came before pending and created the portfolio twice.
it('creates the portfolio once on a double click', async () => {
  const fetchSpy = vi
    .spyOn(globalThis, 'fetch')
    .mockImplementation(() => new Promise<Response>(() => {}))
  render(
    <QueryClientProvider client={new QueryClient()}>
      <CreatePortfolioDialog onClose={() => {}} onCreated={() => {}} />
    </QueryClientProvider>,
  )
  fireEvent.change(screen.getByRole('textbox', { name: t('portfolios.name') }), {
    target: { value: 'Ana Portföy' },
  })
  const create = screen.getByRole('button', { name: t('portfolios.create') })
  fireEvent.click(create)
  fireEvent.click(create)
  await waitFor(() => expect(create).toHaveAttribute('aria-disabled', 'true'))
  expect(fetchSpy).toHaveBeenCalledTimes(1)
})
