import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { signedOutTitle } from '../components/SignedOutFrame'
import { paste } from '../test/paste'
import { expectSignedOutFrame } from '../test/signedOutFrame'
import { LoginPage } from './LoginPage'
import { SetupConfigurePage } from './SetupConfigurePage'
import { SetupGate, SetupPage } from './SetupPage'

function renderAt(path: string) {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={[path]}>
        <SetupGate>
          <Routes>
            <Route path="/setup" element={<SetupPage />} />
            <Route path="/setup/configure" element={<SetupConfigurePage />} />
            <Route path="/login" element={<LoginPage />} />
            <Route path="/" element={<div>home page</div>} />
          </Routes>
        </SetupGate>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const admin = { id: 1, email: 'a@b.com', reporting_currency: 'TRY', is_admin: true }

// Reads made by step 2 once the admin is logged in.
const configureReads: Record<string, unknown> = {
  '/api/me': admin,
  '/api/admin/providers': [],
  '/api/admin/sources': [
    {
      name: 'bist',
      enabled: true,
      at: '19:00',
      can_backfill: true,
      last_run: null,
      last_success_at: null,
    },
  ],
  '/api/admin/schedule': { catchup_enabled: true, backfill_enabled: true, backfill_from: '' },
  '/api/auth/config': { registration_open: true },
}

// needs_setup flips to false once the admin exists, like the server.
function mockServer(adminStatus: number, adminBody: object, needsSetup = true) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (url) => {
    if (String(url) === '/api/setup/status') {
      return new Response(JSON.stringify({ needs_setup: needsSetup }), { status: 200 })
    }
    if (String(url) === '/api/setup/admin') {
      if (adminStatus === 201) needsSetup = false
      return new Response(JSON.stringify(adminBody), { status: adminStatus })
    }
    if (!needsSetup && String(url) in configureReads) {
      return new Response(JSON.stringify(configureReads[String(url)]), { status: 200 })
    }
    return new Response(null, { status: 401 })
  })
}

function fillAndSubmit() {
  fireEvent.change(screen.getByLabelText('Kurulum anahtarı'), { target: { value: 'tok' } })
  fireEvent.change(screen.getByLabelText('E-posta'), { target: { value: 'a@b.com' } })
  fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'password123' } })
  fireEvent.change(screen.getByLabelText('Şifre (tekrar)'), { target: { value: 'password123' } })
  fireEvent.click(screen.getByRole('button', { name: 'Yönetici hesabını oluştur' }))
}

describe('SetupPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('redirects /login to /setup while setup is needed', async () => {
    mockServer(201, {})
    renderAt('/login')
    expect(await screen.findByLabelText('Kurulum anahtarı')).toBeInTheDocument()
  })

  // Setup promotes an existing account only on its stored password,
  // so both fields ask the password manager for the saved one.
  it('marks both setup password fields as current-password', async () => {
    mockServer(201, {})
    renderAt('/setup')
    expect(await screen.findByLabelText('Şifre')).toHaveAttribute(
      'autocomplete',
      'current-password',
    )
    expect(screen.getByLabelText('Şifre (tekrar)')).toHaveAttribute(
      'autocomplete',
      'current-password',
    )
  })

  it('redirects /setup home once an admin exists', async () => {
    mockServer(409, { error: 'setup already done' }, false)
    renderAt('/setup')
    expect(await screen.findByText('home page')).toBeInTheDocument()
  })

  it('shows the translated error for a wrong token', async () => {
    mockServer(403, { error: 'invalid setup token' })
    renderAt('/setup')
    await screen.findByLabelText('Kurulum anahtarı')
    fillAndSubmit()
    expect(await screen.findByText('Kurulum anahtarı hatalı.')).toBeInTheDocument()
  })

  // Editing a field drops the server error about the values sent.
  it('clears the server error once a field is edited', async () => {
    mockServer(403, { error: 'invalid setup token' })
    renderAt('/setup')
    await screen.findByLabelText('Kurulum anahtarı')
    fillAndSubmit()
    expect(await screen.findByText('Kurulum anahtarı hatalı.')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Kurulum anahtarı'), { target: { value: 'tok2' } })

    expect(screen.queryByText('Kurulum anahtarı hatalı.')).not.toBeInTheDocument()
  })

  // The same reveal toggle as on login and register.
  it('reveals and hides the step 1 password', async () => {
    mockServer(201, {})
    renderAt('/setup')
    const password = await screen.findByLabelText('Şifre')
    expect(password).toHaveAttribute('type', 'password')

    fireEvent.click(screen.getByRole('button', { name: 'Şifreyi göster' }))
    expect(password).toHaveAttribute('type', 'text')

    fireEvent.click(screen.getByRole('button', { name: 'Şifreyi gizle' }))
    expect(password).toHaveAttribute('type', 'password')
  })

  it('clears the mismatch error once the passwords are edited', async () => {
    mockServer(201, admin)
    renderAt('/setup')
    fireEvent.change(await screen.findByLabelText('E-posta'), { target: { value: 'a@b.com' } })
    fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'short' } })
    fireEvent.change(screen.getByLabelText('Şifre (tekrar)'), { target: { value: 'shrot' } })
    fireEvent.click(screen.getByRole('button', { name: 'Yönetici hesabını oluştur' }))
    expect(await screen.findByText('Şifreler eşleşmiyor.')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Şifre (tekrar)'), { target: { value: 'short' } })

    expect(screen.queryByText('Şifreler eşleşmiyor.')).not.toBeInTheDocument()
  })

  // A password over 72 bytes pasted into both fields is the same
  // password, so the server's length rule shows, not a mismatch.
  it('shows the 72-byte limit, not a mismatch, for a long pasted password', async () => {
    mockServer(400, { error: 'password must be at most 72 bytes' })
    renderAt('/setup')
    const long = 'Ab1-'.repeat(20)
    fireEvent.change(await screen.findByLabelText('Kurulum anahtarı'), {
      target: { value: 'tok' },
    })
    fireEvent.change(screen.getByLabelText('E-posta'), { target: { value: 'a@b.com' } })
    paste(screen.getByLabelText('Şifre'), long)
    paste(screen.getByLabelText('Şifre (tekrar)'), long)
    fireEvent.click(screen.getByRole('button', { name: 'Yönetici hesabını oluştur' }))

    expect(await screen.findByText('Şifre en fazla 72 bayt olmalı.')).toBeInTheDocument()
    expect(screen.queryByText('Şifreler eşleşmiyor.')).not.toBeInTheDocument()
  })

  // Chromium blurs a focused button the moment it is disabled, so the
  // running request marks the button pending instead; a second submit sends
  // nothing.
  it('keeps the submit button focused and pending while the admin is created', async () => {
    const fetchMock = vi
      .spyOn(globalThis, 'fetch')
      .mockImplementation((url) =>
        String(url) === '/api/setup/admin'
          ? new Promise<Response>(() => {})
          : Promise.resolve(new Response(JSON.stringify({ needs_setup: true }), { status: 200 })),
      )
    renderAt('/setup')
    await screen.findByLabelText('Kurulum anahtarı')
    const submit = screen.getByRole('button', { name: 'Yönetici hesabını oluştur' })
    submit.focus()
    fillAndSubmit()

    await waitFor(() => expect(submit).toHaveAttribute('aria-disabled', 'true'))
    expect(submit).toBeEnabled()
    expect(submit).toHaveFocus()
    fireEvent.click(submit)
    const posts = fetchMock.mock.calls.filter(([url]) => url === '/api/setup/admin')
    expect(posts).toHaveLength(1)
  })

  it('creates the admin and goes on to step 2', async () => {
    mockServer(201, admin)
    renderAt('/setup')
    await screen.findByLabelText('Kurulum anahtarı')
    fillAndSubmit()
    expect(await screen.findByText('Adım 2 / 2')).toBeInTheDocument()
    expect(globalThis.fetch).toHaveBeenCalledWith(
      '/api/setup/admin',
      expect.objectContaining({ method: 'POST' }),
    )
  })

  it('shows the three step 2 sections to an admin and skips home', async () => {
    mockServer(409, {}, false)
    renderAt('/setup/configure')
    expect(await screen.findByRole('heading', { name: 'Veri sağlayıcılar' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Kaynaklar' })).toBeInTheDocument()
    // Yönetim's sources table, one row per source.
    const table = await screen.findByRole('table')
    expect(within(table).getByRole('checkbox', { name: 'bist açık' })).toBeChecked()
    expect(screen.getByText('Herkese açık kayıt')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Şimdilik atla' }))
    expect(await screen.findByText('home page')).toBeInTheDocument()
  })

  // The signed-out pages share one frame and one title size.
  it('renders step 1 in the signed-out frame', async () => {
    mockServer(201, {})
    renderAt('/setup')
    expectSignedOutFrame(await screen.findByRole('heading', { level: 1, name: 'Kurulum' }))
  })

  // Step 2 is long, so "Atla" and "Bitir" sit in a bar fixed to the
  // bottom of the screen, and the page's bottom padding is the bar's height
  // so the bar never covers the end of the page.
  it('keeps the step 2 buttons in a bar fixed to the bottom', async () => {
    mockServer(409, {}, false)
    renderAt('/setup/configure')
    const heading = await screen.findByRole('heading', { level: 1, name: 'Kurulum' })
    expect(heading).toHaveClass(...signedOutTitle.split(' '))

    const bar = screen.getByRole('button', { name: 'Bitir' }).closest('.fixed')
    expect(bar).toHaveClass('bottom-0', 'h-16')
    expect(bar).toContainElement(screen.getByRole('button', { name: 'Şimdilik atla' }))
    expect(heading.closest('.pb-16')).not.toBeNull()
  })
})
