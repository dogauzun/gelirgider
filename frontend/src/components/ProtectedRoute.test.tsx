import { act, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { logout, meQueryKey } from '../api/auth'
import { ProtectedRoute } from './ProtectedRoute'

function LoginStub() {
  const state = useLocation().state as { notice?: string; from?: { pathname?: string } } | null
  return (
    <>
      <div>login page</div>
      <p>{state?.notice}</p>
      <p>from: {state?.from?.pathname}</p>
    </>
  )
}

function renderAt(
  path: string,
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
) {
  render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route
            path="/account"
            element={
              <ProtectedRoute>
                <div>account page</div>
              </ProtectedRoute>
            }
          />
          <Route path="/login" element={<LoginStub />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ProtectedRoute', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  // A failing backend is not a logout.
  it('shows the server error, not the login page, when /api/me fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 502 }))

    renderAt('/account')

    expect(await screen.findByText('Sunucuya ulaşılamadı')).toBeInTheDocument()
    expect(screen.queryByText('login page')).not.toBeInTheDocument()
  })

  // Once the retries end nothing refetches /api/me, so the error
  // screen offers a retry that renders the page when the backend is back.
  it('renders the page when Retry succeeds after /api/me failed', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response(null, { status: 502 }))

    renderAt('/account')
    const retry = await screen.findByRole('button', { name: 'Tekrar dene' })
    fetchMock.mockResolvedValue(
      Response.json({ id: 1, email: 'a@b.co', reporting_currency: 'TRY', is_admin: false }),
    )
    fireEvent.click(retry)

    expect(await screen.findByText('account page')).toBeInTheDocument()
  })

  // A pending /api/me is announced, not silent.
  it('announces the loading session while /api/me is pending', () => {
    vi.spyOn(globalThis, 'fetch').mockReturnValue(new Promise<Response>(() => {}))

    renderAt('/account')

    expect(screen.getByRole('status')).toHaveTextContent('Oturum yükleniyor...')
    expect(screen.queryByText('account page')).not.toBeInTheDocument()
  })

  it('sends a logged-out visitor to the login page', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 401 }))

    renderAt('/account')

    expect(await screen.findByText('login page')).toBeInTheDocument()
  })

  // The null me a logout leaves can reach the protected page before
  // the navigation to /login commits; that is no expired session.
  it('adds no session-expired notice after a logout the user chose', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) =>
      String(input) === '/api/me'
        ? Response.json({ id: 1, email: 'a@b.co', reporting_currency: 'TRY', is_admin: false })
        : new Response(null, { status: 204 }),
    )
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    renderAt('/account', client)
    expect(await screen.findByText('account page')).toBeInTheDocument()

    await act(async () => {
      await logout()
      client.setQueryData(meQueryKey, null)
    })

    expect(await screen.findByText('login page')).toBeInTheDocument()
    expect(screen.queryByText('auth.login.sessionExpired')).not.toBeInTheDocument()
    // Nothing to resume either: the next login, maybe another user's, starts
    // at home instead of the page left.
    expect(screen.getByText('from:')).toBeInTheDocument()
  })
})
