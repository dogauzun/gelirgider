import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { loginRedirectPath } from '../api/auth'
import { GuestRoute } from '../components/GuestRoute'
import { ProtectedRoute } from '../components/ProtectedRoute'
import { expectSignedOutFrame } from '../test/signedOutFrame'
import { LoginPage } from './LoginPage'

describe('LoginPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('logs in and redirects to home on a successful submit', async () => {
    // A fresh Response per call: the auth config query reads one too.
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () => new Response(JSON.stringify({ id: 1, email: 'a@b.com' }), { status: 200 }),
    )

    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/login']}>
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<div>home page</div>} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    fireEvent.change(screen.getByLabelText(/e-posta/i), { target: { value: 'a@b.com' } })
    // Exact: the reveal toggle's aria-label also contains "şifre".
    fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'password123' } })
    fireEvent.click(screen.getByRole('button', { name: /giriş yap/i }))

    expect(await screen.findByText('home page')).toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/auth/login',
      expect.objectContaining({ method: 'POST' }),
    )
  })

  // Password managers fill the login form, and a short password is
  // the server's to reject, not an untranslated browser tooltip.
  it('marks the login password as current and leaves its length to the server', () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () => new Response(JSON.stringify({}), { status: 200 }),
    )
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/login']}>
          <LoginPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(screen.getByLabelText(/e-posta/i)).toHaveAttribute('autocomplete', 'username')
    const password = screen.getByLabelText('Şifre')
    expect(password).toHaveAttribute('autocomplete', 'current-password')
    expect(password).toBeRequired()
    expect(password).not.toHaveAttribute('minlength')
  })

  // The signed-out pages share one frame and one title size.
  it('renders in the signed-out frame', () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () => new Response(JSON.stringify({}), { status: 200 }),
    )
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/login']}>
          <LoginPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expectSignedOutFrame(screen.getByRole('heading', { level: 1, name: 'Giriş yap' }))
  })

  // A failed submit is announced to screen readers.
  it('announces a failed login as an alert', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (url) =>
      String(url) === '/api/auth/login'
        ? new Response(JSON.stringify({ error: 'invalid email or password' }), { status: 401 })
        : new Response(JSON.stringify({}), { status: 200 }),
    )

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/login']}>
          <LoginPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    fireEvent.change(screen.getByLabelText(/e-posta/i), { target: { value: 'a@b.com' } })
    fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'wrong-password' } })
    fireEvent.click(screen.getByRole('button', { name: /giriş yap/i }))

    expect(await screen.findByRole('alert')).toHaveTextContent('E-posta veya şifre hatalı.')
  })

  // A server error is about the values sent, so editing a field
  // drops it.
  it('clears the server error once a field is edited', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (url) =>
      String(url) === '/api/auth/login'
        ? new Response(JSON.stringify({ error: 'invalid email or password' }), { status: 401 })
        : new Response(JSON.stringify({}), { status: 200 }),
    )
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/login']}>
          <LoginPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    fireEvent.change(screen.getByLabelText(/e-posta/i), { target: { value: 'a@b.com' } })
    fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'wrong-password' } })
    fireEvent.click(screen.getByRole('button', { name: /giriş yap/i }))
    expect(await screen.findByTestId('auth-error')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'right-password' } })

    // The reset reaches the page on the query client's next tick.
    await waitFor(() => expect(screen.queryByTestId('auth-error')).not.toBeInTheDocument())
  })

  // The session-expired notice is no success, and is announced.
  it('shows the session-expired notice as a neutral status', () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(
      async () => new Response(JSON.stringify({}), { status: 200 }),
    )
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter
          initialEntries={[{ pathname: '/login', state: { notice: 'auth.login.sessionExpired' } }]}
        >
          <LoginPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const notice = screen.getByRole('status')
    expect(notice).toHaveTextContent('Oturumunun süresi doldu, tekrar giriş yap')
    expect(notice).not.toHaveClass('text-gain')
  })

  // A deep link that bounced to the login page is where login lands.
  it('returns to the protected deep link after login', async () => {
    let loggedIn = false
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (url) => {
      if (String(url) === '/api/auth/login') loggedIn = true
      if (String(url) === '/api/me' && !loggedIn) return new Response(null, { status: 401 })
      return new Response(JSON.stringify({ id: 1, email: 'a@b.com' }), { status: 200 })
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

    fireEvent.change(await screen.findByLabelText(/e-posta/i), { target: { value: 'a@b.com' } })
    fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'password123' } })
    fireEvent.click(screen.getByRole('button', { name: /giriş yap/i }))

    expect(
      await screen.findByText('deep page /portfolios/7/transactions?page=2#last'),
    ).toBeInTheDocument()
    expect(screen.queryByText('home page')).not.toBeInTheDocument()

    // Anything that is not an in-app page goes home: no open redirect.
    for (const pathname of ['//evil.com', '/\\evil.com', 'https://evil.com', '/login']) {
      expect(loginRedirectPath({ from: { pathname } })).toBe('/')
    }
    expect(loginRedirectPath(null)).toBe('/')
  })

  it('reveals and hides the password', () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter initialEntries={['/login']}>
          <LoginPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    const password = screen.getByLabelText('Şifre')
    expect(password).toHaveAttribute('type', 'password')

    fireEvent.click(screen.getByRole('button', { name: 'Şifreyi göster' }))
    expect(password).toHaveAttribute('type', 'text')

    fireEvent.click(screen.getByRole('button', { name: 'Şifreyi gizle' }))
    expect(password).toHaveAttribute('type', 'password')
  })

  describe('two-step verification', () => {
    function mockLogin(totp: () => Response) {
      let signedIn = false
      const fetchMock = vi.spyOn(globalThis, 'fetch').mockImplementation(async (url) => {
        const path = String(url)
        if (path === '/api/auth/login') {
          return new Response(JSON.stringify({ totp_required: true, token: 'tok' }), {
            status: 200,
          })
        }
        if (path === '/api/auth/login/totp') {
          const res = totp()
          signedIn = res.ok
          return res
        }
        if (path === '/api/me' && !signedIn) return new Response(null, { status: 401 })
        return new Response(JSON.stringify({ id: 1, email: 'a@b.com' }), { status: 200 })
      })
      render(
        <QueryClientProvider
          client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
        >
          <MemoryRouter initialEntries={['/login']}>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route path="/" element={<div>home page</div>} />
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>,
      )
      fireEvent.change(screen.getByLabelText(/e-posta/i), { target: { value: 'a@b.com' } })
      fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'password123' } })
      fireEvent.click(screen.getByRole('button', { name: /giriş yap/i }))
      return fetchMock
    }

    const wrongCode = () => new Response(JSON.stringify({ error: 'invalid code' }), { status: 401 })

    async function submitCode(code: string) {
      fireEvent.change(await screen.findByLabelText('Doğrulama kodu'), { target: { value: code } })
      fireEvent.click(screen.getByRole('button', { name: 'Doğrula' }))
    }

    it('asks for the code instead of navigating when login answers totp_required', async () => {
      mockLogin(wrongCode)

      const code = await screen.findByLabelText('Doğrulama kodu')
      expect(code).toHaveAttribute('inputmode', 'numeric')
      expect(code).toHaveAttribute('autocomplete', 'one-time-code')
      expect(code).not.toHaveAttribute('maxlength')
      expect(screen.queryByLabelText('Şifre')).not.toBeInTheDocument()
      expect(screen.queryByText('home page')).not.toBeInTheDocument()
    })

    it('shows a wrong code as an error and keeps the field', async () => {
      mockLogin(wrongCode)

      await submitCode('000000')

      expect(await screen.findByRole('alert')).toHaveTextContent('Kod hatalı.')
      expect(screen.getByLabelText('Doğrulama kodu')).toHaveValue('000000')
      expect(screen.queryByLabelText('Şifre')).not.toBeInTheDocument()
    })

    it('posts the token and code, then navigates like a normal login', async () => {
      const fetchMock = mockLogin(
        () => new Response(JSON.stringify({ id: 1, email: 'a@b.com' }), { status: 200 }),
      )

      await submitCode('123456')

      expect(await screen.findByText('home page')).toBeInTheDocument()
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/auth/login/totp',
        expect.objectContaining({ body: JSON.stringify({ token: 'tok', code: '123456' }) }),
      )
    })

    it('returns to the password form when the login expired', async () => {
      mockLogin(() => new Response(JSON.stringify({ error: 'login expired' }), { status: 401 }))

      await submitCode('123456')

      expect(await screen.findByLabelText('Şifre')).toBeInTheDocument()
      expect(screen.getByRole('alert')).toHaveTextContent('Süre doldu, yeniden giriş yap.')
      expect(screen.queryByLabelText('Doğrulama kodu')).not.toBeInTheDocument()
    })
  })
})
