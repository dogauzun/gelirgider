import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AccountMenu } from './AccountMenu'
import { toast } from '../lib/toast'

function renderMenu() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <AccountMenu />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

function mockMe() {
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    if (String(input) === '/api/me') {
      return Response.json({
        id: '1',
        email: 'ada@example.com',
        reporting_currency: 'TRY',
        is_admin: false,
      })
    }
    return new Response(null, { status: 204 })
  })
}

describe('AccountMenu', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('opens the account menu and closes it on Escape', async () => {
    mockMe()
    renderMenu()

    const button = await screen.findByRole('button', { name: 'Hesap menüsü' })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    // Language lives on the account and auth pages, not in the menu.
    expect(screen.queryByRole('button', { name: 'English' })).not.toBeInTheDocument()

    fireEvent.click(button)

    const menu = await screen.findByRole('menu')
    expect(screen.getByRole('menuitem', { name: 'Hesap' })).toHaveAttribute('href', '/account')

    fireEvent.keyDown(menu, { key: 'Escape' })

    await waitFor(() => expect(screen.queryByRole('menu')).not.toBeInTheDocument())
  })

  // A client-side navigation leaves the page in place, so the menu has to
  // close itself; Base UI's LinkItem does not by default.
  it('closes the menu when a link in it is followed', async () => {
    mockMe()
    renderMenu()

    fireEvent.click(await screen.findByRole('button', { name: 'Hesap menüsü' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Hesap' }))

    await waitFor(() => expect(screen.queryByRole('menu')).not.toBeInTheDocument())
  })

  it('posts the logout request from the menu', async () => {
    const fetchMock = mockMe()
    renderMenu()

    fireEvent.click(await screen.findByRole('button', { name: 'Hesap menüsü' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Çıkış yap' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/auth/logout', expect.anything()),
    )
  })

  // A delete waiting behind its undo toast goes before the session
  // ends, or it would be refused after logout.
  it('runs pending toast commits before the logout request', async () => {
    const fetchMock = mockMe()
    let finishCommit = () => {}
    const commit = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          finishCommit = resolve
        }),
    )
    renderMenu()

    toast('Silindi', { undo: vi.fn(), commit })
    fireEvent.click(await screen.findByRole('button', { name: 'Hesap menüsü' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Çıkış yap' }))

    await waitFor(() => expect(commit).toHaveBeenCalledOnce())
    expect(fetchMock).not.toHaveBeenCalledWith('/api/auth/logout', expect.anything())
    finishCommit()
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/auth/logout', expect.anything()),
    )
  })
})
