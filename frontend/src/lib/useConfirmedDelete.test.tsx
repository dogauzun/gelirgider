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

function setup(mutation: Parameters<typeof useConfirmedDelete>[0]) {
  return renderHook(() => ({ remove: useConfirmedDelete(mutation, KEYS), toasts: useToasts() }))
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
