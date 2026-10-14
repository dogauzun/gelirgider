import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LoginPage } from '../pages/LoginPage'
import { RegisterPage } from '../pages/RegisterPage'
import { GuestRoute } from './GuestRoute'

// Same wiring as main.tsx, so the test covers the routes users actually hit.
function renderAt(path: string) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[path]}>
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
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('GuestRoute', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it.each(['/login', '/register'])('sends a logged-in user from %s to home', async (path) => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ id: '1', email: 'a@b.com' }), { status: 200 }),
    )

    renderAt(path)

    expect(await screen.findByText('home page')).toBeInTheDocument()
    expect(screen.queryByLabelText(/e-posta/i)).not.toBeInTheDocument()
  })

  // The query keeps `isLoading` true for the whole retry window, so
  // without this the page is blank for about seven seconds.
  it('says the server is unreachable while a failed /api/me retries', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new TypeError('Failed to fetch'))

    renderAt('/login')

    expect(await screen.findByText('Sunucuya ulaşılamadı')).toBeInTheDocument()
  })

  it('renders the form for a logged-out visitor', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 401 }))

    renderAt('/login')

    expect(await screen.findByRole('button', { name: /giriş yap/i })).toBeInTheDocument()
  })
})
