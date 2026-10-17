import { render, screen, within } from '@testing-library/react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { App } from '../App'
import { TABLE } from '../components/ui/table'

// jsdom has no matchMedia; the users section picks its table by viewport.
beforeAll(() => {
  window.matchMedia = (query) =>
    ({
      matches: true,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
})

function mockMe(isAdmin: boolean) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    if (String(input) === '/api/me') {
      return new Response(
        JSON.stringify({
          id: 1,
          email: 'ada@example.com',
          reporting_currency: 'TRY',
          is_admin: isAdmin,
        }),
        { status: 200 },
      )
    }
    return new Response('[]', { status: 200 })
  })
}

describe('AdminPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    window.history.pushState({}, '', '/')
  })

  it('shows the nav entry and the sections to an admin', async () => {
    mockMe(true)
    window.history.pushState({}, '', '/admin')
    render(<App />)

    expect(await screen.findByRole('heading', { name: 'Yönetim' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Yönetim' })).toHaveAttribute('href', '/admin')
    expect(screen.getByRole('heading', { name: 'Veri sağlayıcılar' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Kaynaklar' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Kullanıcılar' })).toBeInTheDocument()
  })

  it('links each section from the in-page nav', async () => {
    mockMe(true)
    window.history.pushState({}, '', '/admin')
    render(<App />)

    const nav = await screen.findByRole('navigation', { name: 'Bölümler' })
    const links = within(nav).getAllByRole('link')
    expect(links.map((link) => link.textContent)).toEqual([
      'Veri sağlayıcılar',
      'Kaynaklar',
      'Enstrüman ekle',
      'Kullanıcılar',
    ])
    for (const link of links) {
      const id = link.getAttribute('href')!.slice(1)
      expect(document.getElementById(id)).toBeInstanceOf(HTMLElement)
    }

    // One row that scrolls sideways when it does not fit, 44 px targets.
    expect(nav).toHaveClass('flex', 'overflow-x-auto')
    expect(nav).not.toHaveClass('flex-wrap')
    for (const link of links) expect(link).toHaveClass('h-11', 'whitespace-nowrap')
  })

  // PageHeader in the portfolio pages' wrapper: left-aligned 1,000 px
  // column, 24 px gutter.
  it('sits in the portfolio pages wrapper under the shared page header', async () => {
    mockMe(true)
    window.history.pushState({}, '', '/admin')
    render(<App />)

    const nav = await screen.findByRole('navigation', { name: 'Bölümler' })
    expect(nav.parentElement).toHaveClass('mx-auto', 'max-w-6xl', 'px-6', 'pt-6', 'pb-12')
    const heading = screen.getByRole('heading', { level: 1, name: 'Yönetim' })
    expect(heading).toHaveClass('text-[20px]', 'max-lg:sr-only')
    expect(nav.parentElement).toContainElement(heading)
  })

  it('titles the cards at 14 px and gives the users table the table classes', async () => {
    mockMe(true)
    window.history.pushState({}, '', '/admin')
    render(<App />)

    for (const name of ['Veri sağlayıcılar', 'Kaynaklar', 'Enstrüman ekle', 'Kullanıcılar']) {
      expect(await screen.findByRole('heading', { name })).toHaveClass('text-[14px]')
    }
    const users = document.getElementById('admin-users')!
    expect(await within(users).findByRole('table')).toHaveClass(TABLE)
  })

  it('shows NotFound and no nav entry to a non-admin', async () => {
    mockMe(false)
    window.history.pushState({}, '', '/admin')
    render(<App />)

    expect(await screen.findByText('Sayfa bulunamadı.')).toBeInTheDocument()
    const nav = screen.getByRole('navigation', { name: 'Ana menü' })
    expect(within(nav).getByRole('link', { name: 'Tüm portföyler' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Yönetim' })).not.toBeInTheDocument()
  })
})
