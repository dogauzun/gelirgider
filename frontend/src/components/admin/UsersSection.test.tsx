import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { Toaster } from '../ui/Toast'
import { UsersSection } from './UsersSection'

// jsdom implements neither method on <dialog>; both flip the open state, and
// close() hands focus back to what had it before showModal(), as browsers do.
// Nor matchMedia: the section renders the table from md up and the phone list
// below (GLG-1166), so tests get the table unless they clear `desktop`.
let desktop = true
let focusBeforeDialog: Element | null = null
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    focusBeforeDialog = document.activeElement
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
    if (focusBeforeDialog instanceof HTMLElement) focusBeforeDialog.focus()
  }
  window.matchMedia = (query) =>
    ({
      matches: desktop,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
})

beforeEach(() => {
  desktop = true
})

const ada = { id: 1, email: 'ada@example.com', is_admin: true, created_at: '2026-09-01T10:00:00Z' }
const bob = { id: 2, email: 'bob@example.com', is_admin: false, created_at: '2026-09-20T10:00:00Z' }

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

// UsersSection on /admin, a stub home page, and the Toaster the app shell
// normally mounts.
function renderAtAdmin() {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter initialEntries={['/admin']}>
        <Routes>
          <Route path="/admin" element={<UsersSection />} />
          <Route path="/" element={<p>home page</p>} />
        </Routes>
        <Toaster />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('UsersSection', () => {
  afterEach(() => vi.restoreAllMocks())

  it('refetches the list after adding a user and translates the last admin error', async () => {
    let users = [ada]
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url === '/api/me') return json({ id: 1, email: ada.email, is_admin: true })
      if (url === '/api/auth/config') return json({ registration_open: true })
      if (url === '/api/admin/users' && method === 'GET') return json(users)
      if (url === '/api/admin/users' && method === 'POST') {
        users = [...users, bob]
        return json(bob, 201)
      }
      if (url === '/api/admin/users/1/admin') return json({ error: 'last admin' }, 409)
      return json({ error: 'unexpected' }, 500)
    })

    renderAtAdmin()

    expect(await screen.findByText('ada@example.com')).toBeInTheDocument()
    expect(screen.queryByText('bob@example.com')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Kullanıcı ekle' }))
    // Named by its heading, not just any dialog (GLG-452).
    const dialog = screen.getByRole('dialog', { name: 'Kullanıcı ekle' })
    fireEvent.change(within(dialog).getByLabelText('E-posta'), {
      target: { value: bob.email },
    })
    fireEvent.change(within(dialog).getByLabelText('Şifre'), { target: { value: 'hunter22!' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Kullanıcı ekle' }))

    expect(await screen.findByText('bob@example.com')).toBeInTheDocument()

    fireEvent.click(
      screen.getByRole('button', { name: 'ada@example.com kullanıcısını yöneticilikten çıkar' }),
    )
    fireEvent.click(
      within(await screen.findByRole('dialog')).getByRole('button', {
        name: 'Yöneticilikten çıkar',
      }),
    )
    expect(
      await screen.findByText(
        'Son yönetici kaldırılamaz. Önce başka bir kullanıcıyı yönetici yap.',
      ),
    ).toBeInTheDocument()
  })

  // GLG-1386: the table scrolls sideways inside its box, never the page.
  it('scrolls the table sideways inside its box', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) =>
      String(input) === '/api/admin/users' ? json([ada, bob]) : json({}),
    )
    renderAtAdmin()
    const table = await screen.findByRole('table')
    expect(table.parentElement).toHaveClass('relative', 'overflow-x-auto')
  })

  // GLG-917: Chromium blurs a focused control the moment it is disabled, so a
  // keyboard user lost focus to <body> while the toggle or the admin button
  // saved. jsdom does not blur on disable, so the test checks both stay
  // enabled during the save as well as focused.
  it('keeps the registration toggle and the admin button enabled and focused while they save', async () => {
    let release!: () => void
    const held = new Promise<void>((resolve) => (release = resolve))
    const puts: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (init?.method === 'PUT') {
        puts.push(url)
        await held
        return new Response(null, { status: 204 })
      }
      if (url === '/api/me') return json({ id: 1, email: ada.email, is_admin: true })
      if (url === '/api/auth/config') return json({ registration_open: true })
      if (url === '/api/admin/users') return json([ada, bob])
      return json({ error: 'unexpected' }, 500)
    })

    renderAtAdmin()

    const toggle = await screen.findByRole('checkbox')
    await vi.waitFor(() => expect(toggle).toBeChecked())
    const makeAdmin = await screen.findByRole('button', {
      name: 'bob@example.com kullanıcısını yönetici yap',
    })
    for (const [i, control] of [toggle, makeAdmin].entries()) {
      control.focus()
      fireEvent.click(control)
      // Pending once the PUT is out; it is held until release().
      await vi.waitFor(() => expect(puts).toHaveLength(i + 1))
      await vi.waitFor(() => expect(control).toHaveAttribute('aria-disabled', 'true'))
      expect(control).toBeEnabled()
      expect(control).toHaveFocus()
      // A second click while the save runs sends nothing.
      fireEvent.click(control)
    }
    expect(puts).toEqual(['/api/admin/registration', '/api/admin/users/2/admin'])

    release()
    await vi.waitFor(() => expect(makeAdmin).not.toHaveAttribute('aria-disabled'))
    expect(makeAdmin).toHaveFocus()
  })

  // GLG-994: the same for a dialog's submit button while its request runs.
  it('keeps the add user submit button focused and pending while it saves', async () => {
    let release!: () => void
    const held = new Promise<void>((resolve) => (release = resolve))
    const posts: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (init?.method === 'POST') {
        posts.push(url)
        await held
        return json(bob, 201)
      }
      if (url === '/api/me') return json({ id: 1, email: ada.email, is_admin: true })
      if (url === '/api/auth/config') return json({ registration_open: true })
      if (url === '/api/admin/users') return json([ada])
      return json({ error: 'unexpected' }, 500)
    })

    renderAtAdmin()

    fireEvent.click(await screen.findByRole('button', { name: 'Kullanıcı ekle' }))
    const dialog = screen.getByRole('dialog', { name: 'Kullanıcı ekle' })
    fireEvent.change(within(dialog).getByLabelText('E-posta'), {
      target: { value: bob.email },
    })
    fireEvent.change(within(dialog).getByLabelText('Şifre'), { target: { value: 'hunter22!' } })
    const submit = within(dialog).getByRole('button', { name: 'Kullanıcı ekle' })
    submit.focus()
    fireEvent.click(submit)
    await vi.waitFor(() => expect(submit).toHaveAttribute('aria-disabled', 'true'))
    expect(submit).toBeEnabled()
    expect(submit).toHaveFocus()
    // A second click (or Enter in a field) while the save runs sends nothing.
    fireEvent.click(submit)
    expect(posts).toEqual(['/api/admin/users'])
    release()
  })

  // GLG-1268: isPending reaches the controls a tick after the click, so a
  // double click's second click came before pending and sent the request twice.
  it('sends one request per double click on every control', async () => {
    const sent: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (init?.method) {
        sent.push(`${init.method} ${url}`)
        return new Promise<Response>(() => {})
      }
      if (url === '/api/me') return json({ id: 1, email: ada.email, is_admin: true })
      if (url === '/api/auth/config') return json({ registration_open: true })
      if (url === '/api/admin/users') return json([ada, bob])
      return json({ error: 'unexpected' }, 500)
    })
    renderAtAdmin()
    async function doubleClick(control: HTMLElement) {
      fireEvent.click(control)
      fireEvent.click(control)
      await vi.waitFor(() => expect(control).toHaveAttribute('aria-disabled', 'true'))
    }

    const toggle = await screen.findByRole('checkbox')
    await vi.waitFor(() => expect(toggle).toBeChecked())
    await doubleClick(toggle)
    await doubleClick(
      await screen.findByRole('button', { name: 'bob@example.com kullanıcısını yönetici yap' }),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Kullanıcı ekle' }))
    let dialog = screen.getByRole('dialog', { name: 'Kullanıcı ekle' })
    fireEvent.change(within(dialog).getByLabelText('E-posta'), { target: { value: 'c@d.com' } })
    fireEvent.change(within(dialog).getByLabelText('Şifre'), { target: { value: 'hunter22!' } })
    await doubleClick(within(dialog).getByRole('button', { name: 'Kullanıcı ekle' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Vazgeç' }))

    fireEvent.click(screen.getByRole('button', { name: 'bob@example.com için şifre sıfırla' }))
    dialog = screen.getByRole('dialog', { name: 'Şifre sıfırla' })
    fireEvent.change(within(dialog).getByLabelText('Yeni şifre'), {
      target: { value: 'hunter22!' },
    })
    await doubleClick(within(dialog).getByRole('button', { name: 'Şifre sıfırla' }))

    expect(sent).toEqual([
      'PUT /api/admin/registration',
      'PUT /api/admin/users/2/admin',
      'POST /api/admin/users',
      'POST /api/admin/users/2/password',
    ])
  })

  it('asks before an admin demotes themselves, then leaves /admin for home (GLG-347)', async () => {
    let meIsAdmin = true
    const puts: string[] = []
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/me') return json({ id: 1, email: ada.email, is_admin: meIsAdmin })
      if (url === '/api/auth/config') return json({ registration_open: true })
      if (url === '/api/admin/users') return json([ada, { ...bob, is_admin: true }])
      if (url === '/api/admin/users/1/admin' && init?.method === 'PUT') {
        puts.push(String(init.body))
        meIsAdmin = false
        return new Response(null, { status: 204 })
      }
      return json({ error: 'unexpected' }, 500)
    })

    renderAtAdmin()

    expect(await screen.findByText('bob@example.com')).toBeInTheDocument()
    fireEvent.click(
      screen.getByRole('button', { name: 'ada@example.com kullanıcısını yöneticilikten çıkar' }),
    )

    const dialog = await screen.findByRole('dialog')
    expect(
      within(dialog).getByText(
        'Kendini yöneticilikten çıkarırsan bu sayfaya bir daha giremezsin. Emin misin?',
      ),
    ).toBeInTheDocument()
    expect(puts).toHaveLength(0)

    fireEvent.click(within(dialog).getByRole('button', { name: 'Yöneticilikten çıkar' }))

    expect(await screen.findByText('home page')).toBeInTheDocument()
    expect(screen.getByText('Artık yönetici değilsin')).toBeInTheDocument()
    expect(puts).toEqual([JSON.stringify({ is_admin: false })])
  })

  // The table's cells do not wrap; a long email must, or it widens the table.
  it('lets a long email wrap in the users table', async () => {
    const long = { ...bob, email: `${'a'.repeat(80)}@example.com` }
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/me') return json({ id: 1, email: ada.email, is_admin: true })
      if (url === '/api/auth/config') return json({ registration_open: true })
      if (url === '/api/admin/users') return json([ada, long])
      return json({ error: 'unexpected' }, 500)
    })
    renderAtAdmin()

    const table = await screen.findByRole('table')
    expect(await within(table).findByText(long.email)).toHaveClass('whitespace-normal', 'break-all')
  })

  // GLG-1057: the admin toggle holds both labels, the other one hidden, so it
  // is equally wide on every row, and the caller's row, which has no delete
  // button, keeps an empty slot of the delete button's width.
  it('keeps the row actions the same width on every row', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/me') return json({ id: 1, email: ada.email, is_admin: true })
      if (url === '/api/auth/config') return json({ registration_open: true })
      if (url === '/api/admin/users') return json([ada, bob])
      return json({ error: 'unexpected' }, 500)
    })

    renderAtAdmin()

    const table = await screen.findByRole('table')
    await within(table).findByText(bob.email)
    const shown = (button: HTMLElement) =>
      [...button.querySelectorAll('span span')].map((label) => [
        label.textContent,
        !label.classList.contains('invisible'),
      ])
    const row = (email: string) => within(within(table).getByText(email).closest('tr')!)
    expect(shown(row(ada.email).getByRole('button', { name: /yöneticilikten çıkar$/ }))).toEqual([
      ['Yöneticilikten çıkar', true],
      ['Yönetici yap', false],
    ])
    expect(shown(row(bob.email).getByRole('button', { name: /yönetici yap$/ }))).toEqual([
      ['Yöneticilikten çıkar', false],
      ['Yönetici yap', true],
    ])
    // The caller cannot be deleted; an empty slot holds the delete button's place.
    expect(row(ada.email).queryByRole('button', { name: /sil$/ })).toBeNull()
    expect(
      within(table).getByText(ada.email).closest('tr')!.querySelector('span.w-9'),
    ).not.toBeNull()
    // The phone list moves delete beside the email, out of the button row,
    // so the row fits one line at 375 px.
    cleanup()
    desktop = false
    renderAtAdmin()
    const item = within((await screen.findByText(bob.email)).closest('li')!)
    expect(screen.queryByRole('table')).toBeNull()
    expect(item.getByRole('button', { name: /sil$/ }).parentElement).not.toBe(
      item.getByRole('button', { name: /şifre sıfırla$/ }).parentElement,
    )
  })
  // GLG-1146: deleting a user removed the focused delete button with its row,
  // dropping a keyboard user to <body>. Focus moves to the next user's delete
  // button, else the previous one's, else the section heading.
  it('moves focus to a neighbouring delete button, else the heading, after a delete', async () => {
    const carol = { ...bob, id: 3, email: 'carol@example.com' }
    let users = [ada, bob, carol]
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/me') return json({ id: 1, email: ada.email, is_admin: true })
      if (url === '/api/auth/config') return json({ registration_open: true })
      if (url === '/api/admin/users') return json(users)
      const deleted = /^\/api\/admin\/users\/(\d+)$/.exec(url)
      if (deleted && init?.method === 'DELETE') {
        users = users.filter((user) => user.id !== Number(deleted[1]))
        return new Response(null, { status: 204 })
      }
      return json({ error: 'unexpected' }, 500)
    })

    renderAtAdmin()

    async function remove(email: string) {
      const trigger = await screen.findByRole('button', { name: `${email} kullanıcısını sil` })
      trigger.focus()
      fireEvent.click(trigger)
      const dialog = screen.getByRole('dialog', { name: `${email} silinsin mi?` })
      fireEvent.change(within(dialog).getByLabelText('E-posta'), { target: { value: email } })
      fireEvent.click(within(dialog).getByRole('button', { name: 'Kullanıcıyı ve verilerini sil' }))
      await vi.waitFor(() => expect(screen.queryByText(email)).toBeNull())
    }

    await remove(bob.email)
    expect(document.activeElement).toBe(
      screen.getByRole('button', { name: 'carol@example.com kullanıcısını sil' }),
    )

    // ada is the caller and has no delete button, so the heading takes focus.
    await remove(carol.email)
    expect(document.activeElement).toBe(screen.getByRole('heading', { name: 'Kullanıcılar' }))
  })

  // GLG-1207: the delete confirm was unmounted while still open on cancel,
  // which drops focus to <body>; close() is what hands it back to the opener.
  it('returns focus to the delete button when the delete confirm is cancelled', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url === '/api/me') return json({ id: 1, email: ada.email, is_admin: true })
      if (url === '/api/auth/config') return json({ registration_open: true })
      if (url === '/api/admin/users') return json([ada, bob])
      return json({ error: 'unexpected' }, 500)
    })

    renderAtAdmin()

    const trigger = await screen.findByRole('button', { name: 'bob@example.com kullanıcısını sil' })
    const cancels: [string, (dialog: HTMLElement) => void][] = [
      // Escape on a modal <dialog> fires `cancel`; jsdom does not map the key.
      ['Escape', (dialog) => fireEvent(dialog, new Event('cancel', { cancelable: true }))],
      [
        'Vazgeç',
        (dialog) => fireEvent.click(within(dialog).getByRole('button', { name: 'Vazgeç' })),
      ],
      ['backdrop', (dialog) => fireEvent.click(dialog)],
    ]
    for (const [how, cancel] of cancels) {
      trigger.focus()
      fireEvent.click(trigger)
      const dialog = screen.getByRole('dialog', { name: 'bob@example.com silinsin mi?' })
      // The typed email starts empty on every open.
      expect(within(dialog).getByLabelText('E-posta'), how).toHaveValue('')
      fireEvent.change(within(dialog).getByLabelText('E-posta'), { target: { value: 'bob@' } })
      cancel(dialog)
      expect(document.activeElement, how).toBe(trigger)
    }
  })
})
