import '@testing-library/jest-dom/vitest'
import { cleanup, configure } from '@testing-library/react'
import { afterEach } from 'vitest'

// findBy*/waitFor give up after 1s by default. A cold render of the whole app
// takes longer than that when other test runs share the CPU (GLG-397).
// vi.waitFor does not read this, so a wait on a lazily loaded chart uses
// waitFor (GLG-983).
configure({ asyncUtilTimeout: 5000 })

afterEach(cleanup)

// jsdom's UA stylesheet hides [popover] until it is :popover-open, but jsdom
// has no showPopover to open one, so a popover would never be accessible in
// a test. Show them all, as the browser does once they are opened (GLG-440).
// !important: without it jsdom still applies its UA rule.
const popoverStyle = document.createElement('style')
popoverStyle.textContent = '[popover] { display: block !important; }'
document.head.append(popoverStyle)
