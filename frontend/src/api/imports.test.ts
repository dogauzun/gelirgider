import { renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useParseStatements } from './imports'

afterEach(() => vi.restoreAllMocks())

const file = (name: string) => new File(['x'], name)

// Answers each parse with a preview whose broker is the file's name, after a
// pause, so overlapping calls show in `peak`.
function mockParse(failing: string) {
  const state = { inFlight: 0, peak: 0 }
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
    const name = ((init?.body as FormData).get('file') as File).name
    state.peak = Math.max(state.peak, ++state.inFlight)
    await new Promise((resolve) => setTimeout(resolve, name === 'a.pdf' ? 25 : 5))
    state.inFlight--
    if (name === failing) return new Response('{"error":"unreadable"}', { status: 422 })
    return new Response(JSON.stringify({ broker: name }), { status: 200 })
  })
  return state
}

describe('useParseStatements', () => {
  it('keeps file order, settles a rejected file alone and runs two at a time', async () => {
    const state = mockParse('c.pdf')
    const { result } = renderHook(() => useParseStatements(7))

    const results = await result.current(
      'auto',
      ['a.pdf', 'b.pdf', 'c.pdf', 'd.pdf', 'e.pdf'].map(file),
    )

    expect(results.map((r) => r.status)).toEqual([
      'fulfilled',
      'fulfilled',
      'rejected',
      'fulfilled',
      'fulfilled',
    ])
    // a.pdf is the slowest, yet comes back first.
    expect(results.map((r) => r.status === 'fulfilled' && r.value.broker)).toEqual([
      'a.pdf',
      'b.pdf',
      false,
      'd.pdf',
      'e.pdf',
    ])
    expect(state.peak).toBe(2)
  })
})
