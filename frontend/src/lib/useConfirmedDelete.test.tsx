import { act, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { t } from '../i18n/strings'
import { clearToasts, flushToasts, useToasts } from './toast'
import { useConfirmedDelete } from './useConfirmedDelete'

const KEYS = { done: 'plans.deleted', failed: 'plans.deleteFailed' } as const

afterEach(async () => {
  await flushToasts()
  clearToasts()
})

function setup(
  mutation: Parameters<typeof useConfirmedDelete>[0],
  selectors: Partial<Parameters<typeof useConfirmedDelete>[1]> = {},
) {
  return renderHook(() => ({
    remove: useConfirmedDelete(mutation, { ...KEYS, ...selectors }),
    toasts: useToasts(),
  }))
}

it('does not delete when the confirm is cancelled', async () => {
  const mutate = vi.fn()
  const { result } = setup({ mutate })
  await act(() => result.current.remove([1, 2], 1, () => Promise.resolve(false)))
  expect(mutate).not.toHaveBeenCalled()
  expect(result.current.toasts).toHaveLength(0)
})

it('shows the done toast once the delete succeeded', async () => {
  const mutate = vi.fn((_id: number, options: { onSuccess(): void }) => options.onSuccess())
  const { result } = setup({ mutate })
  await act(() => result.current.remove([1, 2], 2, () => Promise.resolve(true)))
  expect(mutate).toHaveBeenCalledWith(2, expect.anything())
  expect(result.current.toasts.map((item) => item.message)).toEqual([t('plans.deleted')])
})

it('shows the failed toast with the error when the delete failed', async () => {
  const mutate = vi.fn((_id: number, options: { onError(error: Error): void }) =>
    options.onError(new Error('boom')),
  )
  const { result } = setup({ mutate })
  await act(() => result.current.remove([1], 1, () => Promise.resolve(true)))
  expect(result.current.toasts).toEqual([
    expect.objectContaining({ message: t('plans.deleteFailed', { error: 'boom' }), error: true }),
  ])
})

it('shows no failed toast when the caller shows the error itself', async () => {
  const mutate = vi.fn((_id: number, options: { onError(error: Error): void }) =>
    options.onError(new Error('boom')),
  )
  const { result } = renderHook(() => ({
    remove: useConfirmedDelete({ mutate }, { done: KEYS.done }),
    toasts: useToasts(),
  }))
  await act(() => result.current.remove([1], 1, () => Promise.resolve(true)))
  expect(mutate).toHaveBeenCalled()
  expect(result.current.toasts).toHaveLength(0)
})

// The row and fallback selectors default to the ListCard ones; a list with
// its own markup names them.
it('moves focus to the neighbour, else the fallback, found by the custom selectors', async () => {
  document.body.innerHTML =
    '<button data-row="1"></button><button data-row="2"></button><h2 id="heading" tabindex="-1"></h2>'
  const row = (id: number) => document.querySelector<HTMLElement>(`[data-row="${id}"]`)!
  const mutate = vi.fn((_id: number, options: { onSuccess(): void }) => options.onSuccess())
  const { result, rerender } = setup({ mutate }, { rowAttribute: 'data-row', fallback: '#heading' })

  row(1).focus()
  await act(() => result.current.remove([1, 2], 1, () => Promise.resolve(true)))
  row(1).remove()
  rerender()
  expect(document.activeElement).toBe(row(2))

  await act(() => result.current.remove([2], 2, () => Promise.resolve(true)))
  row(2).remove()
  rerender()
  expect(document.activeElement).toBe(document.getElementById('heading'))
  document.body.innerHTML = ''
})
