// Touch areas of at least 44px (Apple HIG) on phones, below `sm`, drawn as an
// invisible ::before so the control keeps its compact look. The
// element becomes the containing block; a parent with overflow other than
// visible clips the extension, so it needs the room as padding.

// A 26px segmented-control chip: 9px above and below, none sideways, so
// neighbouring chips never take each other's taps.
export const TAP_Y =
  'max-sm:relative max-sm:before:absolute max-sm:before:inset-x-0 max-sm:before:-inset-y-[9px]'

// A 36px icon button: 4px on every side. Neighbours sit at least 8px away.
export const TAP_ICON = 'max-sm:relative max-sm:before:absolute max-sm:before:-inset-1'
