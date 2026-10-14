import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { ImportDone } from './ImportDone'
import { viewImportClass, type QueueItem } from './shared'

const file = (name: string) => new File(['x'], name, { type: 'text/csv' })

function renderDone(items: QueueItem[], onRestart = vi.fn()) {
  render(
    <MemoryRouter>
      <ImportDone items={items} portfolioId={7} onRestart={onRestart} />
    </MemoryRouter>,
  )
  return onRestart
}

describe('ImportDone', () => {
  it('lists each imported statement with its rows and a link to its filter, and each skipped or failed file with its reason', () => {
    renderDone([
      { file: file('ocak.csv'), status: 'imported', id: 41, count: 12 },
      { file: file('subat.csv'), status: 'imported', id: 42, count: 1 },
      { file: file('ocak-kopya.csv'), status: 'skipped', reason: 'ocak.csv ile aynı dosya' },
      { file: file('bozuk.csv'), status: 'failed', error: new Error('Satır 3 okunamadı') },
    ])

    expect(screen.getByRole('heading', { name: 'Bitti: 2 ekstre içe aktarıldı.' })).toBeVisible()
    const items = screen.getAllByRole('listitem')
    expect(items).toHaveLength(4)

    expect(items[0]).toHaveTextContent('ocak.csv')
    expect(items[0]).toHaveTextContent('12 işlem')
    expect(within(items[0]).getByRole('link', { name: 'İşlemlerde gör' })).toHaveAttribute(
      'href',
      '/portfolios/7/transactions?import=41',
    )
    expect(within(items[1]).getByRole('link', { name: 'İşlemlerde gör' })).toHaveAttribute(
      'href',
      '/portfolios/7/transactions?import=42',
    )

    expect(items[2]).toHaveTextContent('ocak-kopya.csv')
    expect(items[2]).toHaveTextContent('Atlandı')
    expect(items[2]).toHaveTextContent('ocak.csv ile aynı dosya')
    expect(within(items[2]).queryByRole('link')).toBeNull()

    expect(items[3]).toHaveTextContent('bozuk.csv')
    expect(items[3]).toHaveTextContent('Aktarılmadı')
    expect(items[3]).toHaveTextContent('Satır 3 okunamadı')
    expect(within(items[3]).queryByRole('link')).toBeNull()
  })

  // The same button-styled link as the history card under it (GLG-1056).
  it('styles the view link like the history card does', () => {
    renderDone([{ file: file('ocak.csv'), status: 'imported', id: 41, count: 12 }])

    expect(screen.getByRole('link', { name: 'İşlemlerde gör' }).className).toBe(viewImportClass)
  })

  it('shows the reasons and no success line when nothing was imported', () => {
    renderDone([
      { file: file('ocak.csv'), status: 'skipped', reason: 'Zaten içe aktarılmış' },
      { file: file('bozuk.csv'), status: 'failed', error: new Error('Satır 3 okunamadı') },
    ])

    expect(screen.getByRole('heading', { name: 'Hiçbir ekstre içe aktarılmadı.' })).toBeVisible()
    expect(screen.queryByText(/Bitti/)).toBeNull()
    expect(screen.queryByRole('link')).toBeNull()
    expect(screen.getByText(/Zaten içe aktarılmış/)).toBeVisible()
    expect(screen.getByText(/Satır 3 okunamadı/)).toBeVisible()
  })

  it('returns to step 1 on "Başka ekstre yükle"', () => {
    const onRestart = renderDone([
      { file: file('ocak.csv'), status: 'imported', id: 41, count: 12 },
    ])

    fireEvent.click(screen.getByRole('button', { name: 'Başka ekstre yükle' }))

    expect(onRestart).toHaveBeenCalledOnce()
  })
})
