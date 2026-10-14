// The look the four Recharts wrappers share: props objects spread
// onto Recharts' own components, nothing more (ADR 0023).

// Series draw in on first render and when the data changes. Left on
// Recharts' default isAnimationActive="auto", so reduced motion turns it off.
export const ANIMATION_MS = 300

export const AXIS_TICK = { fontSize: 11, style: { fill: 'var(--color-faint)' } }

export const MARGIN = { top: 12, right: 12, bottom: 0, left: 0 }

export const GRID = {
  vertical: false,
  strokeDasharray: '3 4',
  style: { stroke: 'var(--color-line)' },
}

export const X_AXIS = { axisLine: false, tickLine: false, tick: AXIS_TICK }

// Every y tick is labelled. The y axis is as wide as a compact tick with a
// minus sign, like `-100 Mn`.
export const Y_AXIS = { ...X_AXIS, interval: 0, width: 56 }

// BarChart's and ReturnBars' frame: a bar is 60% of its slot; Recharts'
// default fills 80%.
export const BAR_FRAME = {
  responsive: true,
  margin: MARGIN,
  barCategoryGap: '20%',
  className: 'touch-pan-y tabular-nums',
}
