import { t, type StringKey } from '../i18n/strings'
import { toast } from './toast'
import { neighbourOf, useFocusLater } from './useFocusLater'

// For deleting a row of a ListCard tab (Mevduat, Planlar, İzleme listesi):
// `remove(ids, id, confirm)` asks `confirm` (the page's own useConfirm dialog,
// which lib/ may not import), then deletes, with the `done` toast or the
// `failed` toast (which takes `{error}`). `ids` are the rows that have a delete
// button, in display order. The row's delete button leaves with it and drops
// focus to <body>: focus moves, after the render that follows, to the next
// row's delete button, else the previous one's, else the add button -- unless
// the user has already moved it elsewhere. A row's delete button carries
// `data-delete-row`, the add buttons `data-list-add`. The add button waits for
// the render because the empty state brings its own.
export function useConfirmedDelete(
  mutation: {
    mutate(id: number, options: { onSuccess(): void; onError(error: Error): void }): void
  },
  { done, failed }: { done: StringKey; failed: StringKey },
) {
  const focusLater = useFocusLater()
  return async (ids: number[], id: number, confirm: () => Promise<boolean>) => {
    if (!(await confirm())) return
    const neighbour = neighbourOf(ids, id)
    const target = neighbour !== undefined ? `[data-delete-row="${neighbour}"]` : '[data-list-add]'
    const from = document.querySelector(`[data-delete-row="${id}"]`)
    mutation.mutate(id, {
      onSuccess: () => {
        toast(t(done))
        focusLater([target], from)
      },
      onError: (error) => toast(t(failed, { error: error.message }), { error: true }),
    })
  }
}
