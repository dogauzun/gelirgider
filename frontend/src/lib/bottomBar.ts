// Ref callback for a bar fixed or stuck to the bottom of a scrolling box (the
// page, or the transaction dialog's sheet). While the bar is mounted the box
// gets the bar's height as scroll-padding-bottom, so keyboard focus never
// scrolls a control under the bar (GLG-1067, WCAG 2.4.11); 4px more keeps the
// focus ring clear. Measured, as a bar wraps to a second line on a phone.
// React calls the returned cleanup when the bar unmounts.
export function bottomBar(bar: HTMLElement | null) {
  // jsdom has no ResizeObserver.
  if (!bar || typeof ResizeObserver === 'undefined') return
  let box = bar.parentElement
  while (box && !/auto|scroll/.test(getComputedStyle(box).overflowY)) box = box.parentElement
  const style = (box ?? document.documentElement).style
  const observer = new ResizeObserver(() => {
    style.scrollPaddingBottom = `${bar.offsetHeight + 4}px`
  })
  observer.observe(bar)
  return () => {
    observer.disconnect()
    style.scrollPaddingBottom = ''
  }
}
