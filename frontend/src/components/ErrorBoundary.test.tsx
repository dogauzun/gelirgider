import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { ErrorBoundary } from './ErrorBoundary'

function Bomb(): never {
  throw new Error('boom')
}

describe('ErrorBoundary', () => {
  it('announces the fallback message as the page h1 inside a main when a child throws', () => {
    // React logs the error to the console; keep test output clean.
    vi.spyOn(console, 'error').mockImplementation(() => {})

    render(
      <ErrorBoundary>
        <Bomb />
      </ErrorBoundary>,
    )

    expect(screen.getByRole('alert')).toHaveTextContent(t('error.render'))
    expect(screen.getByRole('main')).toBeInTheDocument()
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(t('error.render'))
  })

  // A chart card brings its own small fallback, not a whole page.
  it('renders the fallback prop instead of the page fallback', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})

    render(
      <ErrorBoundary fallback={<p>small</p>}>
        <Bomb />
      </ErrorBoundary>,
    )

    expect(screen.getByText('small')).toBeInTheDocument()
    expect(screen.queryByRole('main')).not.toBeInTheDocument()
  })

  // The app shell passes the pathname.
  it('renders the children again when resetKey changes', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const { rerender } = render(
      <ErrorBoundary resetKey="/a">
        <Bomb />
      </ErrorBoundary>,
    )
    expect(screen.getByRole('alert')).toBeInTheDocument()

    rerender(
      <ErrorBoundary resetKey="/b">
        <p>page b</p>
      </ErrorBoundary>,
    )
    expect(screen.getByText('page b')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
