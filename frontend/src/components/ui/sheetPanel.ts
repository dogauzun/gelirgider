// The panel inside a ui/Modal: a bottom sheet on phones, a centred card from
// md up. `classes` carries what differs per dialog: the padding,
// the md width and any extra such as a scroll cap. It lives apart from Modal
// because react-refresh wants component files to export only components.
export function sheetPanel(classes: string) {
  // eslint-disable-next-line no-restricted-syntax -- the one sheet panel
  return `absolute inset-x-0 bottom-0 w-full rounded-t-xl bg-surface shadow-pop md:inset-x-auto md:top-1/2 md:bottom-auto md:left-1/2 md:max-w-[calc(100%-2rem)] md:-translate-x-1/2 md:-translate-y-1/2 md:rounded-card ${classes}`
}
