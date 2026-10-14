import { useLayoutEffect, useRef, useState, type ComponentProps, type ReactNode } from 'react'
import { Tooltip, useActiveTooltipLabel, usePlotArea, useXAxisScale, useYAxisScale } from 'recharts'
import { placeTooltip } from '../../lib/chart'

// Gap between the active point and the tooltip card's near edge (GLG-779).
const TOOLTIP_GAP = 12

export type TooltipProps = { active?: boolean; activeIndex?: unknown }

// TooltipCard is the tooltip's content: it renders the chart's card and
// reports its size before paint, so the card is placed from its real size.
// Its type never changes, so the aria-live card stays one node while the
// active point moves.
function TooltipCard({
  render,
  onSize,
  ...props
}: TooltipProps & {
  render: (props: TooltipProps) => ReactNode
  onSize: (size: { width: number; height: number }) => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    if (ref.current) onSize({ width: ref.current.offsetWidth, height: ref.current.offsetHeight })
  })
  const card = render(props)
  return card && <div ref={ref}>{card}</div>
}

// AnchoredTooltip is Recharts' Tooltip placed by the active category's point,
// not by the pointer, so mouse, touch and keyboard place it alike
// (placeTooltip). The point's x is the category's, in the middle of its band
// on a bar chart; anchors holds each category's y value, and one without
// anchors on the plot's middle. centred puts the card's middle on the point's
// x whenever placeTooltip put it above or below the point, so a bar's card
// sits over its own slot (GLG-924). filterNull is off, or Recharts would hide
// the card on a category without any value.
export function AnchoredTooltip({
  anchors,
  content,
  cursor,
  centred = false,
}: {
  anchors: Map<string, number | null>
  content: (props: TooltipProps) => ReactNode
  cursor: ComponentProps<typeof Tooltip>['cursor']
  centred?: boolean
}) {
  const x = useXAxisScale()
  const y = useYAxisScale()
  const plot = usePlotArea()
  const label = useActiveTooltipLabel()
  const [card, setCard] = useState({ width: 0, height: 0 })
  // Same size: keep the state, so the measure does not render again.
  const onSize = (size: typeof card) =>
    setCard((old) => (old.width === size.width && old.height === size.height ? old : size))

  const px = label == null ? undefined : x?.(label, { position: 'middle' })
  const anchor = label == null ? undefined : anchors.get(String(label))
  const py = anchor == null ? undefined : y?.(anchor)
  let position: { x: number; y: number } | undefined
  if (px !== undefined && plot) {
    const point = { x: px, y: py ?? plot.y + plot.height / 2 }
    position = placeTooltip(point, card, plot, TOOLTIP_GAP)
    if (centred && (position.y + card.height <= point.y || position.y >= point.y)) {
      const left = Math.min(point.x - card.width / 2, plot.x + plot.width - card.width)
      position = { x: Math.max(plot.x, left), y: position.y }
    }
  }
  return (
    <Tooltip
      content={<TooltipCard render={content} onSize={onSize} />}
      position={position}
      filterNull={false}
      cursor={cursor}
      isAnimationActive={false}
    />
  )
}
