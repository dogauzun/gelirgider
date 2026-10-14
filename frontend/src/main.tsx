import { CSPProvider } from '@base-ui/react/csp-provider'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { Toaster } from './components/ui/Toast'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    {/* The CSP (deploy/nginx.conf) blocks <style> elements; the one rule
        Base UI would inject is in index.css (ADR 0023). */}
    <CSPProvider disableStyleElements>
      <App />
      <Toaster />
    </CSPProvider>
  </StrictMode>,
)
