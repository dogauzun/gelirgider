import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { describe, expect, it } from 'vitest'
import { expectSignedOutFrame } from '../test/signedOutFrame'
import { NotFoundPage, SignedOutNotFoundPage } from './NotFoundPage'

describe('SignedOutNotFoundPage', () => {
  // GLG-970: a visitor's unknown URL gets the brand and the language switch
  // like the other signed-out pages. The signed-in one sits in the shell
  // (App.test).
  it('renders in the signed-out frame', () => {
    render(
      <MemoryRouter>
        <SignedOutNotFoundPage />
      </MemoryRouter>,
    )

    expectSignedOutFrame(screen.getByRole('heading', { level: 1 }))
  })

  // GLG-1058: the title has no full stop like the frame's other titles, and
  // the action goes to the login page rather than to / (which only redirects
  // a visitor there).
  it('titles without a full stop and links to the login page', () => {
    render(
      <MemoryRouter>
        <SignedOutNotFoundPage />
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(/^Sayfa bulunamadı$/)
    expect(screen.getByRole('link', { name: 'Giriş yap' })).toHaveAttribute('href', '/login')
    expect(screen.queryByRole('link', { name: 'Portföylere dön' })).not.toBeInTheDocument()
  })
})

describe('NotFoundPage', () => {
  // GLG-1058: the signed-in page keeps its title and its way back.
  it('keeps the title with a full stop and the link to the portfolios', () => {
    render(
      <MemoryRouter>
        <NotFoundPage />
      </MemoryRouter>,
    )

    expect(screen.getByRole('heading', { level: 1, name: 'Sayfa bulunamadı.' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Portföylere dön' })).toHaveAttribute('href', '/')
  })
})
