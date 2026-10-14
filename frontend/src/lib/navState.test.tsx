import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter, useLocation } from 'react-router'
import { expect, it } from 'vitest'
import { useNavState } from './navState'

it('reads the state and clears it in place', () => {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <MemoryRouter
      initialEntries={[
        { pathname: '/portfolios/1/transactions', search: '?import=4', state: { add: true } },
      ]}
    >
      {children}
    </MemoryRouter>
  )
  const { result } = renderHook(() => ({ nav: useNavState(), location: useLocation() }), {
    wrapper,
  })
  expect(result.current.nav.state?.add).toBe(true)

  act(() => result.current.nav.clear())
  expect(result.current.location.pathname).toBe('/portfolios/1/transactions')
  expect(result.current.location.search).toBe('?import=4')
  expect(result.current.nav.state).toBeNull()
})
