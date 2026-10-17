import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { GuestRoute } from '../components/GuestRoute'
import { ProtectedRoute } from '../components/ProtectedRoute'
import { LoginPage } from './LoginPage'
import { expectSignedOutFrame } from '../test/signedOutFrame'
import { RegisterPage } from './RegisterPage'

function renderAndSubmit() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={['/register']}>
        <Routes>
          <Route path="/register" element={<RegisterPage />} />
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<div>home page</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  fireEvent.change(screen.getByLabelText(/e-posta/i), { target: { value: 'a@b.com' } })
  // Exact: the reveal toggle's aria-label also contains "şifre".
  fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'password123' } })
  fireEvent.click(screen.getByRole('button', { name: /kayıt ol/i }))
}

const me = () => new Response(JSON.stringify({ id: 1, email: 'a@b.com' }), { status: 200 })

describe('RegisterPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('registers, logs in and redirects to home', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () => me())

    renderAndSubmit()

    expect(await screen.findByText('home page')).toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/auth/register',
      expect.objectContaining({ method: 'POST' }),
    )
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/auth/login',
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('falls back to the login page with a notice when auto-login fails', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (url) =>
      String(url) === '/api/auth/login'
        ? new Response(JSON.stringify({ error: 'boom' }), { status: 500 })
        : me(),
    )

    renderAndSubmit()

    expect(await screen.findByText('Kayıt tamamlandı, giriş yap')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /giriş yap/i })).toBeInTheDocument()
  })

  // isPending reaches the button a tick after the click, so a double
  // click's second click came before pending and registered twice.
  it('registers once on a double click', async () => {
    const fetchSpy = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation(() => new Promise<Response>(() => {}))

    renderAndSubmit()
    const submit = screen.getByRole('button', { name: /kayıt ol/i })
    fireEvent.click(submit)

    await waitFor(() => expect(submit).toHaveAttribute('aria-disabled', 'true'))
    expect(fetchSpy.mock.calls.filter(([url]) => url === '/api/auth/register')).toHaveLength(1)
  })

  // Editing the email drops the error about the one already sent.
  it('clears the server error once the email is edited', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (url) =>
      String(url) === '/api/auth/register'
        ? new Response(JSON.stringify({ error: 'email already registered' }), { status: 409 })
        : me(),
    )

    renderAndSubmit()
    expect(await screen.findByText('Bu e-posta zaten kayıtlı.')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText(/e-posta/i), { target: { value: 'c@d.com' } })

    // The reset reaches the page on the query client's next tick.
    await waitFor(() =>
      expect(screen.queryByText('Bu e-posta zaten kayıtlı.')).not.toBeInTheDocument(),
    )
  })

  // A deep link that bounced to login survives the detour through
  // register, both when auto-login works and via the login fallback.
  it.each([true, false])(
    'returns to the protected deep link after register (auto-login ok: %s)',
    async (autoLoginOk) => {
      let loggedIn = false
      let loginCalls = 0
      vi.spyOn(globalThis, 'fetch').mockImplementation(async (url) => {
        if (String(url) === '/api/auth/login') {
          loginCalls++
          if (!autoLoginOk && loginCalls === 1) {
            return new Response(JSON.stringify({ error: 'boom' }), { status: 500 })
          }
          loggedIn = true
        }
        if (String(url) === '/api/me' && !loggedIn) return new Response(null, { status: 401 })
        return me()
      })

      function Where() {
        const { pathname, search, hash } = useLocation()
        return <div>deep page {pathname + search + hash}</div>
      }
      render(
        <QueryClientProvider
          client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
        >
          <MemoryRouter initialEntries={['/portfolios/7/transactions?page=2#last']}>
            <Routes>
              <Route
                path="/login"
                element={
                  <GuestRoute>
                    <LoginPage />
                  </GuestRoute>
                }
              />
              <Route
                path="/register"
                element={
                  <GuestRoute>
                    <RegisterPage />
                  </GuestRoute>
                }
              />
              <Route path="/" element={<div>home page</div>} />
              <Route
                path="/portfolios/:id/transactions"
                element={
                  <ProtectedRoute>
                    <Where />
                  </ProtectedRoute>
                }
              />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )

      fireEvent.click(await screen.findByRole('link', { name: 'Hesabın yok mu? Kayıt ol' }))
      fireEvent.change(await screen.findByLabelText(/e-posta/i), { target: { value: 'a@b.com' } })
      fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'password123' } })
      fireEvent.click(screen.getByRole('button', { name: /kayıt ol/i }))

      if (!autoLoginOk) {
        expect(await screen.findByText('Kayıt tamamlandı, giriş yap')).toBeInTheDocument()
        fireEvent.change(screen.getByLabelText(/e-posta/i), { target: { value: 'a@b.com' } })
        fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'password123' } })
        fireEvent.click(screen.getByRole('button', { name: /giriş yap/i }))
      }

      expect(
        await screen.findByText('deep page /portfolios/7/transactions?page=2#last'),
      ).toBeInTheDocument()
      expect(screen.queryByText('home page')).not.toBeInTheDocument()
    },
  )

  it('shows a closed message instead of the form when registration is closed', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () => new Response(JSON.stringify({ registration_open: false }), { status: 200 }),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/register']}>
          <RegisterPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('Kayıt kapalı, yöneticine başvur.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /kayıt ol/i })).not.toBeInTheDocument()
    expectSignedOutFrame(screen.getByRole('heading', { level: 1, name: 'Kayıt ol' }))
  })

  // The signed-out pages share one frame and one title size.
  it('renders the form in the signed-out frame', () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () => new Response(JSON.stringify({ registration_open: true }), { status: 200 }),
    )
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/register']}>
          <RegisterPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expectSignedOutFrame(screen.getByRole('heading', { level: 1, name: 'Kayıt ol' }))
  })
})
