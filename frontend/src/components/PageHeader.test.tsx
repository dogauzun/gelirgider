import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { PageHeader } from './PageHeader'

// Below lg the top bar shows the title, so the page's own h1 is for screen
// readers only there; from lg it is the 20 px heading.
it('hides its title below lg only', () => {
  render(<PageHeader title="Yönetim" />)
  const heading = screen.getByRole('heading', { level: 1, name: 'Yönetim' })
  expect(heading).toHaveClass('max-lg:sr-only', 'text-[20px]')
  expect(heading).not.toHaveClass('sr-only')
})

it('shows the description and the actions', () => {
  render(
    <PageHeader
      title="İçe aktar"
      description="Aracı kurum ekstrelerini yükle."
      actions={<button type="button">Yeni</button>}
    />,
  )
  expect(screen.getByText('Aracı kurum ekstrelerini yükle.')).toHaveClass('text-muted')
  expect(screen.getByRole('button', { name: 'Yeni' })).toBeInTheDocument()
})
