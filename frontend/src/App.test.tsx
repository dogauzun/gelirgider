import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it, onTestFinished, vi } from 'vitest'
import { App } from './App'
import { Toaster } from './components/ui/Toast'
import { toast } from './lib/toast'
import { setLocale } from './i18n/strings'
import { setPrivacy } from './lib/privacy'
import { chartDrag, mockChartSize } from './test/chartSize'

describe('App', () => {
  // jsdom has no matchMedia. The dividends and admin pages pick their table
  // by viewport; a test that wants the phone layout stubs its own.
  beforeAll(() => {
    window.matchMedia = (query) =>
      ({
        matches: true,
        media: query,
        addEventListener() {},
        removeEventListener() {},
      }) as unknown as MediaQueryList
  })

  afterEach(() => {
    // Unmount first. The locale and privacy resets below re-render a tree
    // that is still mounted, and with fetch restored its queries would fail and
    // keep retrying into the next test through the shared QueryClient.
    cleanup()
    vi.restoreAllMocks()
    // The other tests assert Turkish copy.
    setLocale('tr')
    setPrivacy(false)
  })

  // GLG-379. Runs first: App's QueryClient is shared by every test here, and
  // the next one leaves a logged-out me in it.
  it('sends the user to the login page when a data query finds the session gone', async () => {
    let signedIn = true
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (!signedIn) return Response.json({ error: 'unauthorized' }, { status: 401 })
      if (url === '/api/me') {
        return Response.json({
          id: '1',
          email: 'a@b.co',
          reporting_currency: 'TRY',
          is_admin: false,
        })
      }
      if (url === '/api/portfolios') {
        return Response.json([
          {
            id: 1,
            name: 'Ana',
            created_at: '2026-01-01T00:00:00Z',
            total_value: '0',
            cash: '0',
            unrealized_pl: null,
            currency: 'TRY',
            missing_rates: [],
          },
        ])
      }
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    render(
      <>
        <App />
        <Toaster />
      </>,
    )
    // The sidebar's row, named with its value.
    const link = await screen.findByRole('link', { name: /^Ana/ })
    // GLG-849: neither outlives the session, and the held-back delete is
    // dropped, not sent without it.
    const commit = vi.fn()
    act(() => toast('İşlem silinemedi', { error: true }))
    act(() => toast('İşlem silindi', { undo: vi.fn(), commit }))

    // Signed out elsewhere; opening the portfolio queries its summary.
    signedIn = false
    fireEvent.click(link)

    expect(await screen.findByRole('button', { name: 'Giriş yap' })).toBeInTheDocument()
    expect(window.location.pathname).toBe('/login')
    // GLG-801: the login page says why the user is there.
    const notice = screen.getByText('Oturumunun süresi doldu, tekrar giriş yap')
    expect(notice).toHaveAttribute('role', 'status')
    // The toast regions are empty.
    expect(screen.getByRole('alert')).toBeEmptyDOMElement()
    const toasts = screen.getAllByRole('status').filter((el) => el !== notice)
    expect(toasts).toHaveLength(1)
    expect(toasts[0]).toBeEmptyDOMElement()
    expect(commit).not.toHaveBeenCalled()
  })

  it('re-renders the mounted tree in English after the language switch', async () => {
    // Logged out, so "/" redirects to the login page, which carries the switch.
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 401 }))

    render(<App />)

    expect(await screen.findByRole('button', { name: 'Giriş yap' })).toBeInTheDocument()

    const english = screen.getByRole('button', { name: 'English' })
    english.focus()
    fireEvent.click(english)

    expect(await screen.findByRole('button', { name: 'Log in' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'English' })).toHaveAttribute('aria-pressed', 'true')
    // The remount replaced the pressed button; focus follows it (GLG-681).
    expect(screen.getByRole('button', { name: 'English' })).not.toBe(english)
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'English' }))
    expect(screen.getByRole('button', { name: 'Türkçe' })).toHaveAttribute('aria-pressed', 'false')
  })

  // GLG-389. Last: it leaves user B logged in on the shared QueryClient.
  it('never shows the previous user its cached data after logout and login', async () => {
    const portfolio = (name: string) => ({
      id: 1,
      name,
      created_at: '2026-01-01T00:00:00Z',
      total_value: '0',
      cash: '0',
      unrealized_pl: null,
      currency: 'TRY',
      missing_rates: [],
    })
    const userA = { id: '1', email: 'a@b.co', reporting_currency: 'TRY', is_admin: false }
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    let me: typeof userA | null = userA
    // B's list stays in flight until the assertion below has run.
    let releaseB = () => {}
    const bList = new Promise<void>((resolve) => (releaseB = resolve))
    // What is sent from the logout on (GLG-999).
    let sent: string[] | null = null
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      sent?.push(url)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/auth/config') return Response.json({ registration_open: true })
      if (url === '/api/auth/logout') {
        me = null
        return new Response(null, { status: 204 })
      }
      if (url === '/api/auth/login') {
        me = userB
        return Response.json(userB)
      }
      if (!me) {
        // A slow 401 keeps the post-logout /api/me refetch in flight long
        // enough to catch a session-expired notice on the login page.
        await new Promise((r) => setTimeout(r, 300))
        return Response.json({ error: 'unauthorized' }, { status: 401 })
      }
      if (url === '/api/me') return Response.json(me)
      if (url === '/api/portfolios') {
        if (me === userA) return Response.json([portfolio('Ana')])
        await bList
        return Response.json([portfolio('Bora')])
      }
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    // The tests above leave /login, with a `from` pointing elsewhere.
    window.history.replaceState(null, '', '/')
    render(<App />)
    expect(await screen.findByRole('link', { name: /^Ana/ })).toBeInTheDocument()

    // jsdom has no stylesheet, so the sidebar and the small-screen top bar
    // both render; the sidebar's menu stands for both.
    const sidebar = screen.getByRole('navigation', { name: 'Ana menü' })
    fireEvent.click(within(sidebar).getByRole('button', { name: 'Hesap menüsü' }))
    const logout = await screen.findByRole('menuitem', { name: 'Çıkış yap' })
    sent = []
    fireEvent.click(logout)
    const submit = await screen.findByRole('button', { name: 'Giriş yap' })
    // A deliberate logout is not an expired session (GLG-801).
    expect(screen.queryByText('Oturumunun süresi doldu, tekrar giriş yap')).not.toBeInTheDocument()
    // The page left behind sends nothing more; the login page asks only for
    // the session and the public config (GLG-999).
    const expected = ['/api/auth/logout', '/api/me', '/api/auth/config', '/api/setup/status']
    expect(sent.filter((url) => !expected.includes(url))).toEqual([])
    fireEvent.change(screen.getByLabelText('E-posta'), { target: { value: 'b@b.co' } })
    fireEvent.change(screen.getByLabelText('Şifre'), { target: { value: 'secret123' } })
    fireEvent.click(submit)

    // B's Tüm portföyler page is up while its list is still pending.
    await waitFor(() => expect(window.location.pathname).toBe('/portfolios/all'))
    await waitFor(() =>
      expect(
        within(screen.getByRole('navigation', { name: 'Ana menü' })).getByText('b@b.co'),
      ).toBeInTheDocument(),
    )
    expect(screen.queryByText('Ana')).not.toBeInTheDocument()

    // B's portfolio shows first in the sidebar. Found by text there, not by
    // role over the whole page: Tüm portföyler renders a large tree as the
    // list lands, and a role query over it can outrun the default 1 s
    // window under load. A's never appears.
    releaseB()
    const nav = screen.getByRole('navigation', { name: 'Ana menü' })
    expect(await within(nav).findByText('Bora', {}, { timeout: 5000 })).toBeInTheDocument()
    expect(screen.queryByText('Ana')).not.toBeInTheDocument()
  })

  // GLG-681. No remount since GLG-1031: the pressed button stays, focused.
  // Runs after the test above, which leaves user B signed in.
  it('keeps focus on the privacy button when it is pressed', async () => {
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') return Response.json([])
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    render(<App />)
    const sidebar = () => screen.getByRole('navigation', { name: 'Ana menü' })
    const hide = await within(sidebar()).findByRole('button', { name: 'Tutarları gizle' })
    hide.focus()
    fireEvent.click(hide)

    const show = await within(sidebar()).findByRole('button', { name: 'Tutarları göster' })
    expect(show).toBe(hide)
    expect(document.activeElement).toBe(show)
  })

  // Moved from the deleted Header tests (GLG-875). Runs after the test above,
  // which leaves user B signed in.
  it('offers a skip link to main first and names the logo links', async () => {
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') return Response.json([])
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    render(<App />)
    await screen.findByRole('navigation', { name: 'Ana menü' })

    const skip = screen.getByRole('link', { name: 'İçeriğe geç' })
    expect(skip).toHaveAttribute('href', '#main')
    expect(document.querySelector('a[href], button')).toBe(skip)
    // The shell adds no <main>; the page's is the skip link's target.
    expect(document.querySelectorAll('main')).toHaveLength(1)
    expect(screen.getByRole('main')).toHaveAttribute('id', 'main')
    // Named by an attribute, so the name holds even where the text is hidden.
    // They open Tüm portföyler, the home page since GLG-935.
    const logos = document.querySelectorAll('a[aria-label="gelirgider"]')
    expect(logos.length).toBeGreaterThan(0)
    logos.forEach((logo) => expect(logo).toHaveAttribute('href', '/portfolios/all'))
  })

  // GLG-935: the home page is gone; / lands on Tüm portföyler, which offers
  // the first portfolio when there is none. Runs after the tests above,
  // which leave user B signed in.
  it('redirects / to Tüm portföyler', async () => {
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') return Response.json([])
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    window.history.replaceState(null, '', '/')
    render(<App />)

    expect(await screen.findByText('Henüz portföyün yok')).toBeInTheDocument()
    expect(window.location.pathname).toBe('/portfolios/all')
    expect(screen.getByRole('heading', { level: 1, name: 'Tüm portföyler' })).toBeInTheDocument()
  })

  // GLG-955: the Performans tab is a route of its own. Runs after the tests
  // above, which leave user B signed in.
  it('routes the Performans tab', async () => {
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') return Response.json([])
      if (url === '/api/portfolios/all/returns/periods') {
        return Response.json({ currency: 'TRY', months: [], years: [] })
      }
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    window.history.replaceState(null, '', '/portfolios/all/performance')
    render(<App />)

    expect(
      await screen.findByRole('heading', { level: 2, name: 'Dönemsel getiri' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Performans' })).toHaveAttribute('aria-current', 'page')
    window.history.replaceState(null, '', '/')
  })

  // GLG-1262: each page renders its own tab row, so a tab change unmounts
  // the focused tab with the page and dropped focus to <body>.
  it('keeps focus on the tab row when a tab is activated from the keyboard', async () => {
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') {
        return Response.json([
          {
            id: 1,
            name: 'Ana',
            created_at: '2026-01-01T00:00:00Z',
            total_value: '0',
            cash: '0',
            unrealized_pl: null,
            currency: 'TRY',
            missing_rates: [],
          },
        ])
      }
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    window.history.replaceState(null, '', '/portfolios/1')
    render(<App />)

    const tabs = await screen.findByRole('navigation', { name: 'Portföy sekmeleri' })
    const tab = within(tabs).getByRole('link', { name: 'İşlemler' })
    tab.focus()
    fireEvent.click(tab)

    await waitFor(() => expect(window.location.pathname).toBe('/portfolios/1/transactions'))
    const active = await screen.findByRole('link', { name: 'İşlemler', current: 'page' })
    await waitFor(() => expect(document.activeElement).toBe(active))
    window.history.replaceState(null, '', '/')
  })

  // GLG-882: a signed-in user's unknown URL is a page like any other, inside
  // the shell. Runs after the tests above, which leave user B signed in.
  it('shows the not-found page inside the shell when signed in', async () => {
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') return Response.json([])
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    window.history.replaceState(null, '', '/bu-sayfa-yok')
    render(<App />)

    expect(await screen.findByRole('navigation', { name: 'Ana menü' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1, name: 'Sayfa bulunamadı.' })).toBeInTheDocument()
    expect(within(screen.getByRole('banner')).getByText('Sayfa bulunamadı')).toBeInTheDocument()
    expect(document.querySelectorAll('main')).toHaveLength(1)
    expect(window.location.pathname).toBe('/bu-sayfa-yok')
    window.history.replaceState(null, '', '/')
  })

  // GLG-1339: the portfolio pages share one /portfolios/:id layout route; a
  // sub-path none of its children names is still unknown, not a portfolio
  // page. Runs after the tests above, which leave user B signed in.
  it('shows the not-found page for an unknown portfolio sub-path', async () => {
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') {
        return Response.json([
          {
            id: 1,
            name: 'Ana',
            created_at: '2026-01-01T00:00:00Z',
            total_value: '0',
            cash: '0',
            unrealized_pl: null,
            currency: 'TRY',
            missing_rates: [],
          },
        ])
      }
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    window.history.replaceState(null, '', '/portfolios/1/bu-sayfa-yok')
    render(<App />)

    expect(await screen.findByRole('navigation', { name: 'Ana menü' })).toBeInTheDocument()
    expect(
      await within(screen.getByRole('main')).findByRole('heading', {
        level: 1,
        name: 'Sayfa bulunamadı.',
      }),
    ).toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: 'Portföy sekmeleri' })).not.toBeInTheDocument()
    expect(window.location.pathname).toBe('/portfolios/1/bu-sayfa-yok')
    window.history.replaceState(null, '', '/')
  })

  // GLG-1077: setup step 2 is for the admin; a signed-in non-admin gets the
  // same not-found page as any unknown URL, inside the shell with its link
  // home, not a bare page.
  it('shows a non-admin the not-found page inside the shell at /setup/configure', async () => {
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') return Response.json([])
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    window.history.replaceState(null, '', '/setup/configure')
    render(<App />)

    expect(await screen.findByRole('navigation', { name: 'Ana menü' })).toBeInTheDocument()
    const main = screen.getByRole('main')
    expect(
      within(main).getByRole('heading', { level: 1, name: 'Sayfa bulunamadı.' }),
    ).toBeInTheDocument()
    expect(within(main).getByRole('link', { name: 'Portföylere dön' })).toHaveAttribute('href', '/')
    expect(within(screen.getByRole('banner')).getByText('Sayfa bulunamadı')).toBeInTheDocument()
    expect(document.querySelectorAll('main')).toHaveLength(1)
    window.history.replaceState(null, '', '/')
  })

  // GLG-1062: the sidebar keeps the İçe aktar tab on a portfolio switch, and
  // a preview opened for A must not stay on screen to be committed into B.
  // Runs after the tests above, which leave user B signed in.
  it('starts the import page over when the portfolio changes', async () => {
    const portfolio = (id: number, name: string) => ({
      id,
      name,
      created_at: '2026-01-01T00:00:00Z',
      total_value: '0',
      cash: '0',
      unrealized_pl: null,
      currency: 'TRY',
      missing_rates: [],
    })
    // jsdom has no matchMedia; the preview picks its table by viewport.
    vi.stubGlobal('matchMedia', (media: string) => ({
      matches: true,
      media,
      addEventListener() {},
      removeEventListener() {},
    }))
    onTestFinished(() => {
      vi.unstubAllGlobals()
    })
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios')
        return Response.json([portfolio(1, 'Ana'), portfolio(2, 'Yan')])
      if (url === '/api/imports' && (init?.method ?? 'GET') === 'GET') return Response.json([])
      if (url === '/api/imports/parse') {
        return Response.json({
          broker: 'midas',
          sha256: 'abc',
          period_start: '2025-12-01',
          period_end: '2025-12-31',
          currency: 'USD',
          warnings: [],
          rows: [],
          holdings: [],
          reconciliation: [],
        })
      }
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    window.history.replaceState(null, '', '/portfolios/1/import')
    render(<App />)

    const input = await waitFor(() => {
      const found = document.querySelector<HTMLInputElement>('input[type="file"]')
      expect(found).not.toBeNull()
      return found!
    })
    const file = new File(['%PDF-1.4'], 'ekstre.pdf', { type: 'application/pdf' })
    fireEvent.change(input, { target: { files: [file] } })
    expect(await screen.findByRole('heading', { name: 'ekstre.pdf' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('link', { name: /^Yan/ }))

    await waitFor(() => expect(window.location.pathname).toBe('/portfolios/2/import'))
    const current = screen.getAllByRole('listitem').find((item) => item.ariaCurrent)
    expect(current).toHaveTextContent('Dosya seç')
    expect(screen.queryByRole('heading', { name: 'ekstre.pdf' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'İçe aktar' })).not.toBeInTheDocument()
    window.history.replaceState(null, '', '/')
  })

  // GLG-1062 review: a modal blocks the sidebar but not Back. A dialog
  // opened for A must not stay open on B's page to book into B.
  it('closes a portfolio page dialog when Back leads to another portfolio', async () => {
    // jsdom has no showModal.
    HTMLDialogElement.prototype.showModal = function () {
      this.open = true
    }
    const portfolio = (id: number, name: string) => ({
      id,
      name,
      created_at: '2026-01-01T00:00:00Z',
      total_value: '0',
      cash: '0',
      unrealized_pl: null,
      currency: 'TRY',
      missing_rates: [],
    })
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios')
        return Response.json([portfolio(1, 'Ana'), portfolio(2, 'Yan')])
      if (url.endsWith('/dividends/timeline')) {
        return Response.json({
          currency: 'TRY',
          months: [],
          years: [],
          by_instrument: [],
          missing_rates: [],
        })
      }
      if (url.endsWith('/dividends/expected')) return Response.json([])
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    window.history.replaceState(null, '', '/portfolios/2/dividends')
    render(<App />)
    await screen.findByRole('button', { name: 'Temettü ekle' })

    // The sidebar keeps the tab: A's Temettüler, its add dialog open.
    fireEvent.click(screen.getByRole('link', { name: /^Ana/ }))
    await waitFor(() => expect(window.location.pathname).toBe('/portfolios/1/dividends'))
    fireEvent.click(await screen.findByRole('button', { name: 'Temettü ekle' }))
    expect(document.querySelector('dialog[open]')).not.toBeNull()

    act(() => window.history.back())

    await waitFor(() => expect(window.location.pathname).toBe('/portfolios/2/dividends'))
    // The router renders Back in a transition, after the URL has changed, so
    // wait for that render to remount the page (GLG-1098).
    await waitFor(() => expect(document.querySelector('dialog[open]')).toBeNull())
    window.history.replaceState(null, '', '/')
  })

  // GLG-876. The privacy toggle in the drawer leaves the drawer open with
  // focus on the toggle; it no longer remounts the app (GLG-1031). Runs after
  // the tests above, which leave user B signed in.
  it('keeps the drawer open and the toggle focused when privacy is toggled from it', async () => {
    // jsdom has no showModal.
    HTMLDialogElement.prototype.showModal = function () {
      this.open = true
    }
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') return Response.json([])
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    render(<App />)
    const open = await screen.findByRole('button', { name: 'Menüyü aç' })
    open.focus()
    fireEvent.click(open)
    const hide = within(screen.getByRole('dialog')).getByRole('button', { name: 'Tutarları gizle' })
    hide.focus()
    fireEvent.click(hide)

    const show = await within(screen.getByRole('dialog')).findByRole('button', {
      name: 'Tutarları göster',
    })
    expect(show).toBe(hide)
    expect(document.activeElement).toBe(show)
  })

  // GLG-1031. Privacy re-renders the amounts in place: the range before a
  // zoom and the Katkı card's choice survive it. Runs after the tests above,
  // which leave user B signed in.
  it('keeps the range before a zoom and the Katkı choice across a privacy toggle', async () => {
    const restoreSize = mockChartSize()
    // Desktop layout, and reduced motion so the chart draws at once.
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: true,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }))
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: false }
    const position = {
      instrument_id: 1,
      symbol: 'THYAO',
      name: 'THY',
      currency: 'TRY',
      quantity: '10',
      avg_cost: '100.00',
      cost_basis: '1000.00',
      last_price: '110.00',
      has_price: true,
      price_source: 'yahoo',
      price_date: '2026-10-01',
      prev_close: '100.00',
      prev_price_date: '2026-09-30',
      day_change: '100.00',
      day_change_reporting: '100.00',
      market_value: '1100.00',
      unrealized_pl: '100.00',
      realized_pl: '0.00',
      market_value_reporting: '1100.00',
      unrealized_pl_reporting: '100.00',
      realized_pl_reporting: '0.00',
    }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') {
        return Response.json([
          {
            id: 1,
            name: 'Ana',
            created_at: '2026-01-01T00:00:00Z',
            total_value: '1100.00',
            cash: '0',
            unrealized_pl: '100.00',
            currency: 'TRY',
            missing_rates: [],
          },
        ])
      }
      if (url === '/api/portfolios/1/summary') {
        return Response.json({
          currency: 'TRY',
          cash: '0.00',
          cash_by_currency: {},
          cash_by_currency_reporting: {},
          total_value: '1100.00',
          positions_value: '1100.00',
          cost_basis: '1000.00',
          unrealized_pl: '100.00',
          realized_pl: '0.00',
          fx_pl: null,
          missing_rates: [],
          positions: [position],
        })
      }
      if (url.startsWith('/api/portfolios/1/history?')) {
        return Response.json({
          currency: 'TRY',
          points: [
            { date: '2026-07-01', total_value: '1000.00', cash: '0', cost_basis: '1000.00' },
            { date: '2026-10-01', total_value: '1100.00', cash: '0', cost_basis: '1000.00' },
          ],
          missing_rates: [],
          missing_prices: [],
          net_flow: '0.00',
          returns: { simple: '0.100000', twr: '0.100000', mwr: '0.100000' },
          benchmark: null,
        })
      }
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    try {
      window.history.replaceState(null, '', '/portfolios/1?range=1y')
      render(<App />)
      const katki = within(
        (await screen.findByRole('radiogroup', { name: 'Katkı ölçüsü' })).closest('section')!,
      )
      fireEvent.click(katki.getByRole('radio', { name: 'Bugün' }))

      // At 600 px, x 60 is the first point and 580 the last (LineChart.test.tsx).
      const chart = await waitFor(() => {
        const el = document.querySelector('.recharts-wrapper')
        if (!el) throw new Error('no chart yet')
        return el
      })
      await chartDrag(chart, 580, 60)
      expect(screen.getByRole('radio', { name: 'Özel' })).toHaveAttribute('aria-checked', 'true')

      expect(screen.getAllByText('₺1.100,00').length).toBeGreaterThan(0)
      const sidebar = screen.getByRole('navigation', { name: 'Ana menü' })
      fireEvent.click(within(sidebar).getByRole('button', { name: 'Tutarları gizle' }))
      await waitFor(() =>
        within(screen.getByRole('navigation', { name: 'Ana menü' })).getByRole('button', {
          name: 'Tutarları göster',
        }),
      )
      // The amounts mask at once.
      expect(screen.queryByText('₺1.100,00')).not.toBeInTheDocument()

      fireEvent.click(screen.getByRole('button', { name: 'Özel aralığı temizle' }))
      expect(screen.getByRole('radio', { name: '1Y' })).toHaveAttribute('aria-checked', 'true')
      expect(window.location.search).toBe('?range=1y')
      expect(
        within(
          screen.getByRole('radiogroup', { name: 'Katkı ölçüsü' }).closest('section')!,
        ).getByRole('radio', { name: 'Bugün' }),
      ).toHaveAttribute('aria-checked', 'true')
    } finally {
      restoreSize()
      vi.unstubAllGlobals()
      window.history.replaceState(null, '', '/')
    }
  })

  // GLG-1023. Another admin removed this user's admin role while Yönetim was
  // open. Runs after the tests above, which leave user B signed in.
  it('says the user is no longer an admin and drops the admin page on a 403', async () => {
    const userB = { id: '2', email: 'b@b.co', reporting_currency: 'TRY', is_admin: true }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/setup/status') return Response.json({ needs_setup: false })
      if (url === '/api/me') return Response.json(userB)
      if (url === '/api/portfolios') return Response.json([])
      if (url === '/api/auth/config') return Response.json({ registration_open: false })
      if (url.startsWith('/api/admin/') && !userB.is_admin) {
        return Response.json({ error: 'admin only' }, { status: 403 })
      }
      return Response.json({ error: 'not found' }, { status: 404 })
    })

    window.history.replaceState(null, '', '/admin')
    render(
      <>
        <App />
        <Toaster />
      </>,
    )
    const nav = await screen.findByRole('navigation', { name: 'Ana menü' })
    const toggle = await screen.findByRole('checkbox', { name: /Herkese açık kayıt/ })
    await waitFor(() => expect(toggle).toBeEnabled())
    expect(within(nav).getByRole('link', { name: 'Yönetim' })).toBeInTheDocument()

    userB.is_admin = false
    fireEvent.click(toggle)

    expect(await screen.findByText('Artık yönetici değilsin.')).toBeInTheDocument()
    await waitFor(() =>
      expect(within(nav).queryByRole('link', { name: 'Yönetim' })).not.toBeInTheDocument(),
    )
    expect(screen.queryByRole('checkbox', { name: /Herkese açık kayıt/ })).not.toBeInTheDocument()
    // The toast outlives the page that showed the error line.
    expect(screen.getByText('Artık yönetici değilsin.')).toBeInTheDocument()
    expect(screen.queryByText('Bir hata oluştu. Lütfen tekrar dene.')).not.toBeInTheDocument()
    window.history.replaceState(null, '', '/')
  })
})
