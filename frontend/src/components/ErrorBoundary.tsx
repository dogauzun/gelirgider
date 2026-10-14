import { Component, type ErrorInfo, type ReactNode } from 'react'
import { t } from '../i18n/strings'

interface Props {
  children: ReactNode
  // A change clears a caught error and renders the children again, without
  // remounting them when there was none (GLG-875: the shell passes the
  // pathname). A portfolio page still starts over on a portfolio switch: App
  // keys it by the portfolio id (GLG-1062).
  resetKey?: string
  // Rendered instead of the full-page fallback, e.g. inside a chart card.
  fallback?: ReactNode
}

interface State {
  hasError: boolean
  resetKey?: string
}

// Class component because React has no hook equivalent of
// componentDidCatch/getDerivedStateFromError.
export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false, resetKey: this.props.resetKey }

  static getDerivedStateFromError(): Partial<State> {
    return { hasError: true }
  }

  static getDerivedStateFromProps(props: Props, state: State): Partial<State> | null {
    return props.resetKey === state.resetKey ? null : { hasError: false, resetKey: props.resetKey }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(error, info)
  }

  render() {
    if (this.state.hasError && this.props.fallback !== undefined) return this.props.fallback
    if (this.state.hasError) {
      // The boundary wraps the routed pages, which each own the <main>, so
      // the fallback that replaces them brings its own <main> and h1. The
      // alert role sits on a span: on the h1 it would drop the heading role.
      return (
        <main id="main" className="flex flex-col items-center gap-2 px-6 py-12 text-center">
          <h1 className="text-sm text-muted">
            <span role="alert">{t('error.render')}</span>
          </h1>
          <a href="/" className="text-sm font-medium text-ink underline underline-offset-2">
            {t('error.render.home')}
          </a>
        </main>
      )
    }

    return this.props.children
  }
}
