import { neighbourOf, useFocusLater } from './useFocusLater'

// For deleting a row of a ListCard tab (Mevduat, Planlar): call
// `focusAfterDelete(ids, id)` before the delete, with the ids of the rows that have a delete button in display
// order, and call what it returns once the delete succeeded. The row's delete
// button leaves with it and drops focus to <body>: focus moves, after the
// render that follows, to the next row's delete button, else the previous
// one's, else the add button -- unless the user has already moved it
// elsewhere. A row's delete button carries `data-delete-row`, the add buttons
// `data-list-add`. The add button waits for the render because the empty state
// brings its own.
export function useFocusAfterDelete() {
  const focusLater = useFocusLater()
  return (ids: number[], id: number) => {
    const neighbour = neighbourOf(ids, id)
    const target = neighbour !== undefined ? `[data-delete-row="${neighbour}"]` : '[data-list-add]'
    return () => focusLater([target], document.querySelector(`[data-delete-row="${id}"]`))
  }
}
