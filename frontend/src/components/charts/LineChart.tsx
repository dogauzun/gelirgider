import { useEffect, useId, useMemo, useRef, useState } from 'react'
import {
  Area,
  CartesianGrid,
  ComposedChart,
  DefaultZIndexes,
  Line,
  ReferenceArea,
  ReferenceLine,
  XAxis,
  YAxis,
  ZIndexLayer,
  usePlotArea,
  useXAxisScale,
  useYAxisScale,
  type MouseHandlerDataParam,
  type XAxisTickContentProps,
} from 'recharts'
import { t } from '../../i18n/strings'
import {
  axisTicks,
  dateTicks,
  formatChartValue,
  formatDateLabel,
  formatPercentTicks,
  formatTicks,
  LEGEND_HEIGHT,
  pressWithoutFocus,
  type ChartMarker,
  type ChartPoint,
} from '../../lib/chart'
import { subDecimal } from '../../lib/decimal'
import { signedPercent } from '../../lib/format'
import { usePrivacy } from '../../lib/privacy'
import { excessReturn } from '../../lib/returns'
import { AnchoredTooltip, type TooltipProps } from './AnchoredTooltip'
import { ANIMATION_MS, AXIS_TICK, GRID, MARGIN, X_AXIS, Y_AXIS } from './chartStyle'
import { ChartTooltip, type ChartTooltipRow } from './ChartTooltip'

// A drag to zoom spans at least this many px sideways, so a wobbly click on a
// dense series, a point every 1.5 px, does not zoom (GLG-1032).
const MIN_ZOOM_DRAG_PX = 8

// The three series' strokes: dash pattern and color token. The benchmark is
// dotted so it never reads as the dashed cost line (GLG-491). Colors go
// through `style`: SVG presentation attributes do not take `var()`.
const STROKES = {
  value: { dash: undefined, color: 'var(--color-accent)' },
  cost: { dash: '4 3', color: 'var(--color-faint)' },
  benchmark: { dash: '1 3', color: 'var(--color-plum)' },
}

const MARKER_COLORS: Record<ChartMarker['kind'], string> = {
  buy: 'var(--color-gain)',
  sell: 'var(--color-loss)',
  dividend: 'var(--color-accent)',
  split: 'var(--color-plum)',
}
const MARKER_KINDS = Object.keys(MARKER_COLORS) as ChartMarker['kind'][]

// Each marker kind has its own shape as well as its colour, so the kinds tell
// apart without colour (GLG-807): buy points up, sell points down, a dividend
// is a dot, a split a diamond. r is the half-size around (cx, cy).
function MarkerShape({
  kind,
  cx,
  cy,
  r,
  ...rest
}: {
  kind: ChartMarker['kind']
  cx: number
  cy: number
  r: number
  'data-marker'?: string
}) {
  const style = { fill: MARKER_COLORS[kind], stroke: 'var(--color-surface)' }
  if (kind === 'dividend') {
    return <circle {...rest} cx={cx} cy={cy} r={r * 0.8} strokeWidth="1.5" style={style} />
  }
  const d = {
    buy: `M ${cx},${cy - r} L ${cx + r},${cy + r * 0.8} L ${cx - r},${cy + r * 0.8} Z`,
    sell: `M ${cx},${cy + r} L ${cx + r},${cy - r * 0.8} L ${cx - r},${cy - r * 0.8} Z`,
    split: `M ${cx},${cy - r} L ${cx + r},${cy} L ${cx},${cy + r} L ${cx - r},${cy} Z`,
  }[kind]
  return <path {...rest} d={d} strokeWidth="1.5" strokeLinejoin="round" style={style} />
}

// MarkerSwatch is a marker's shape at legend and tooltip size.
function MarkerSwatch({ kind }: { kind: ChartMarker['kind'] }) {
  return (
    <svg width="12" height="12" aria-hidden="true">
      <MarkerShape kind={kind} cx={6} cy={6} r={5} />
    </svg>
  )
}

// Swatch is the 16px line sample shown in the legend and in the tooltip rows.
function Swatch({ kind = 'value' }: { kind?: keyof typeof STROKES }) {
  const { dash, color } = STROKES[kind]
  return (
    <svg width="16" height="2" aria-hidden="true">
      <line
        x1="0"
        y1="1"
        x2="16"
        y2="1"
        strokeWidth="2"
        strokeDasharray={dash}
        strokeLinecap={kind === 'benchmark' ? 'round' : undefined}
        style={{ stroke: color }}
      />
    </svg>
  )
}

// LegendToggle is a legend entry that hides and shows its series. The label
// is the button's name and aria-pressed carries the state; a hidden entry
// keeps the muted label, struck through, with a faded swatch (GLG-922).
function LegendToggle({
  kind,
  label,
  shown,
  onToggle,
}: {
  kind: 'cost' | 'benchmark'
  label: string
  shown: boolean
  onToggle: () => void
}) {
  return (
    <button
      type="button"
      aria-pressed={shown}
      onClick={onToggle}
      className={`flex cursor-pointer items-center gap-1 rounded-sm ${shown ? '' : 'text-muted line-through'}`}
    >
      <span className={`flex ${shown ? '' : 'opacity-40'}`}>
        <Swatch kind={kind} />
      </span>
      {label}
    </button>
  )
}

// Markers draws the plotted markers on the value line with the chart's own
// scales, above the lines and below the cursor.
function Markers({
  markers,
}: {
  markers: { date: string; value: number; kind: ChartMarker['kind'] }[]
}) {
  const x = useXAxisScale()
  const y = useYAxisScale()
  if (!x || !y) return null
  return (
    <ZIndexLayer zIndex={DefaultZIndexes.scatter}>
      {markers.map((m, i) => {
        const cx = x(m.date)
        const cy = y(m.value)
        if (cx === undefined || cy === undefined) return null
        return <MarkerShape key={i} data-marker={m.kind} kind={m.kind} cx={cx} cy={cy} r={4.5} />
      })}
    </ZIndexLayer>
  )
}

// DateAxis is the x axis, labelling dateTicks of the plot's own width (from
// Recharts' plot area, so the y axis and margins are already taken off). The
// end labels sit inside the plot: the first starts at its point, the last
// ends at its point.
function DateAxis({ dates, multiYear }: { dates: string[]; multiYear: boolean }) {
  const ticks = dateTicks(dates, usePlotArea()?.width ?? 0, multiYear)
  function tick({ x, y, index }: XAxisTickContentProps) {
    const anchor =
      ticks.length === 1
        ? 'middle'
        : index === 0
          ? 'start'
          : index === ticks.length - 1
            ? 'end'
            : 'middle'
    return (
      <text x={x} y={y} dy="0.71em" fontSize={11} textAnchor={anchor} style={AXIS_TICK.style}>
        {formatDateLabel(ticks[index], multiYear)}
      </text>
    )
  }
  return <XAxis dataKey="date" ticks={ticks} interval={0} {...X_AXIS} tick={tick} />
}

// zoomSpan draws the span of a drag to zoom: shaded in the accent colour,
// with a line at each end.
function zoomSpan({
  x,
  y,
  width,
  height,
}: {
  x: number
  y: number
  width: number
  height: number
}) {
  const line = { stroke: STROKES.value.color }
  return (
    <g className="zoom-span">
      <rect
        x={x}
        y={y}
        width={width}
        height={height}
        style={{ fill: STROKES.value.color, fillOpacity: 0.12 }}
      />
      <line x1={x} y1={y} x2={x} y2={y + height} strokeWidth={1} style={line} />
      <line x1={x + width} y1={y} x2={x + width} y2={y + height} strokeWidth={1} style={line} />
    </g>
  )
}

const activeDate = (state: MouseHandlerDataParam) =>
  state.activeLabel == null ? null : String(state.activeLabel)

const toNumber = (v: string | null | undefined) =>
  v === null || v === undefined ? null : Number(v)

// LineChart draws total_value (accent, with a gradient area), cost_basis
// (dashed, faint) and, when any point has benchmark_value, a dotted benchmark
// line (ADR 0012), on Recharts (ADR 0023). Recharts draws and handles the
// pointer, touch, keyboard and animation; the ticks, the labels, privacy
// masking, the markers and the gaps are ours. Recharts gets Number() values
// for positions only: every amount it shows is formatted from the exact
// decimal string of `points`.
// benchmarkLabel is already translated; estimatedFrom (YYYY-MM-DD) marks the
// first day whose benchmark value is an estimate, flagged in the tooltip.
// markers are shapes on the value line at matching dates (others are dropped),
// listed in the tooltip and, by kind, in the legend; labels, already
// translated, rename the two lines. formatValue prints the tooltip's and the
// screen reader summary's amounts: whole numbers (formatChartValue) unless a
// page passes its own, as the price chart does for a unit price's decimals
// (GLG-918, GLG-927); it masks in privacy mode like formatChartValue. A null total_value or cost_basis is a gap in that
// line, as a missing benchmark_value is: nothing is drawn for that day
// (GLG-589). A series without any value is neither drawn nor in the legend.
// The cost and benchmark legend entries hide and show their series: a hidden
// series is not drawn, not in the tooltip and not in the y axis's range.
// height fixes the plot's height in px; without it the plot is 5:2 with a
// 200 px floor.
// onZoom turns on drag to zoom (GLG-949): with a mouse, press on the plot,
// drag sideways and release, and it gets the span's first and last dates, in
// order. A drag shorter than MIN_ZOOM_DRAG_PX, within one point or released
// outside the plot does nothing, and Escape cancels. Touch and pen keep scrubbing the tooltip.
// unit 'percent' (GLG-954) reads the same point fields as fractions, as the
// return card's percent mode passes its twr and benchmark_twr: percent ticks
// (formatPercentTicks), a solid zero line, no area fill, and the tooltip,
// the summary and formatValue's place taken by signed percentages, never
// masked; with both lines shown the tooltip adds the difference,
// excessReturn(value, benchmark), as the return card's pill computes it,
// named labels.excess ("fark" unless the caller passes "reel" for TÜFE).
// labels.valueHint is the value's legend entry's title.
export interface LineChartProps {
  points: ChartPoint[]
  height?: number
  benchmarkLabel?: string
  estimatedFrom?: string | null
  markers?: ChartMarker[]
  labels?: { value: string; cost?: string; valueHint?: string; excess?: string }
  formatValue?: (value: string) => string
  onZoom?: (from: string, to: string) => void
  unit?: 'money' | 'percent'
}

const percent = (v: string) => signedPercent(Number(v))

// useDragZoom is LineChart's drag to zoom, from Recharts' mouse events, which
// carry the date under the pointer (activeLabel; none off the plot). The
// pointerdown a browser sends before a mousedown names the pointer
// (onPointerDown, on the chart's wrapper), so a touch or pen press,
// whose taps also send mouse events, never starts a drag. Only a mouse
// press's default is prevented (pressWithoutFocus, for focus), so touch
// scrubbing and page scrolling stay Recharts' and the browser's. While a drag
// is on, window listeners cancel it on Escape and on a button released outside
// the chart (a release inside reaches the chart's handler first); they go
// when the drag ends. Without onZoom there are no handlers; span is the
// dragged span's first and last dates, in order, while it covers two or more.
function useDragZoom(onZoom: LineChartProps['onZoom']) {
  const [drag, setDrag] = useState<{ start: string; end: string } | null>(null)
  const pointerType = useRef('')
  const dragStartX = useRef(0)
  const dragging = drag !== null
  useEffect(() => {
    if (!dragging) return
    const cancel = (e: Event) => {
      if (e.type === 'mouseup' || (e as KeyboardEvent).key === 'Escape') setDrag(null)
    }
    window.addEventListener('mouseup', cancel)
    window.addEventListener('keydown', cancel)
    return () => {
      window.removeEventListener('mouseup', cancel)
      window.removeEventListener('keydown', cancel)
    }
  }, [dragging])
  const zoomHandlers = onZoom && {
    onMouseDown: (state: MouseHandlerDataParam, e: { button: number; clientX: number }) => {
      const at = activeDate(state)
      if (pointerType.current === 'mouse' && e.button === 0 && at !== null) {
        dragStartX.current = e.clientX
        setDrag({ start: at, end: at })
      }
    },
    onMouseMove: (state: MouseHandlerDataParam) => {
      const at = activeDate(state)
      if (at !== null) setDrag((d) => (d && d.end !== at ? { ...d, end: at } : d))
    },
    onMouseUp: (state: MouseHandlerDataParam, e: { clientX: number }) => {
      const at = activeDate(state)
      const wide = Math.abs(e.clientX - dragStartX.current) >= MIN_ZOOM_DRAG_PX
      if (drag && at !== null && at !== drag.start && wide) {
        const [from, to] = [drag.start, at].sort()
        onZoom(from, to)
      }
      setDrag(null)
    },
  }
  const span = drag && drag.start !== drag.end ? [drag.start, drag.end].sort() : null
  const onPointerDown = (e: { pointerType: string }) => (pointerType.current = e.pointerType)
  return { zoomHandlers, span, onPointerDown }
}

export function LineChart({
  points,
  benchmarkLabel,
  estimatedFrom,
  markers = [],
  labels,
  formatValue: moneyValue = formatChartValue,
  height,
  onZoom,
  unit = 'money',
}: LineChartProps) {
  const gradientId = useId()
  const inPercent = unit === 'percent'
  const formatValue = inPercent ? percent : moneyValue
  // Re-render when privacy flips, so ticks and tooltip mask at once.
  usePrivacy()
  // Hidden series live in this chart only, not saved. A new range keeps them
  // hidden. The benchmark is hidden by its label, and another benchmark, or
  // none, clears it: a newly chosen benchmark is always shown.
  const [costHidden, setCostHidden] = useState(false)
  const [hiddenBenchmark, setHiddenBenchmark] = useState<string | null>(null)
  const benchmarkKey = benchmarkLabel ?? ''
  if (hiddenBenchmark !== null && hiddenBenchmark !== benchmarkKey) setHiddenBenchmark(null)

  const { zoomHandlers, span, onPointerDown } = useDragZoom(onZoom)

  // Memoised: a drag-to-zoom re-renders on every pointer move, and the rows
  // only change with the points.
  const rows = useMemo(
    () =>
      points.map((p) => ({
        date: p.date,
        value: toNumber(p.total_value),
        cost: toNumber(p.cost_basis),
        benchmark: toNumber(p.benchmark_value),
      })),
    [points],
  )
  const values = rows.flatMap((r) =>
    [r.value, r.cost, r.benchmark].filter((v): v is number => v !== null),
  )
  // An all-zero money series is a portfolio with nothing in it; a 0%
  // return all the way (cash only) is still a line.
  const empty = inPercent ? values.length === 0 : !values.some((v) => v !== 0)
  const hasCost = rows.some((r) => r.cost !== null)
  const hasBenchmark = rows.some((r) => r.benchmark !== null)
  const costShown = hasCost && !costHidden
  const benchmarkShown = hasBenchmark && hiddenBenchmark === null
  // The tooltip anchor per date: the value, else the first shown series.
  const anchors = useMemo(
    () =>
      new Map(
        rows.map((r) => [
          r.date,
          r.value ?? (costShown ? r.cost : null) ?? (benchmarkShown ? r.benchmark : null),
        ]),
      ),
    [rows, costShown, benchmarkShown],
  )
  if (empty) {
    return <p className="text-sm text-muted">{t('chart.empty')}</p>
  }

  const hasValue = rows.some((r) => r.value !== null)
  const valueLabel = labels?.value ?? t('chart.totalValue')
  const costLabel = labels?.cost ?? t('chart.costBasis')

  // The y axis fits the visible series; with the value line all null and the
  // rest hidden there is nothing visible, and it keeps fitting every series.
  const visible = rows.flatMap((r) =>
    [r.value, costShown ? r.cost : null, benchmarkShown ? r.benchmark : null].filter(
      (v): v is number => v !== null,
    ),
  )
  const yTicks = axisTicks(visible.length > 0 ? visible : values)
  const tickLabels = new Map(
    (inPercent ? formatPercentTicks : formatTicks)(yTicks).map((label, i) => [yTicks[i], label]),
  )

  // The date labels carry the year when the range spans calendar years
  // (GLG-809); which dates are labelled depends on the plot's width
  // (DateAxis).
  const multiYear = points[0].date.slice(0, 4) !== points[points.length - 1].date.slice(0, 4)

  const indexByDate = new Map(points.map((p, i) => [p.date, i]))
  const plotted = markers.filter((m) => {
    const i = indexByDate.get(m.date)
    return i !== undefined && rows[i].value !== null
  })
  const markerKinds = MARKER_KINDS.filter((k) => plotted.some((m) => m.kind === k))

  // The screen reader summary: the date range and the value line's first and
  // last value with the change between them (GLG-807), subtracted exactly:
  // a float difference can round the other way at .5 (GLG-919). All three go
  // through formatValue, so the price chart's summary keeps the tooltip's
  // decimals (GLG-927). The change is formatted without its sign, then signed
  // unless it printed as zero, the way signDisplay 'exceptZero' would; a masked
  // value has no digit, so privacy mode leaks no sign.
  const valued = points.filter((p) => p.total_value !== null)
  const from = formatDateLabel(points[0].date, multiYear)
  const to = formatDateLabel(points[points.length - 1].date, multiYear)
  let summary = t('chart.line.range', { from, to })
  if (valued.length > 0) {
    const first = valued[0].total_value!
    const last = valued[valued.length - 1].total_value!
    const diff = subDecimal(last, first)
    const negative = diff.startsWith('-')
    let change: string
    if (inPercent) {
      // A percentage is signed already.
      change = formatValue(diff)
    } else {
      change = formatValue(negative ? diff.slice(1) : diff)
      change = (!/[1-9]/.test(change) ? '' : negative ? '-' : '+') + change
    }
    summary = t('chart.line.summary', {
      from,
      to,
      label: valueLabel,
      first: formatValue(first),
      last: formatValue(last),
      change,
    })
  }

  // The tooltip reads the active point's exact strings, never Recharts'
  // numeric payload.
  function tooltipContent({ active, activeIndex }: TooltipProps) {
    const p = active && activeIndex != null ? points[Number(activeIndex)] : undefined
    if (!p) return null
    const items: ChartTooltipRow[] = []
    if (p.total_value !== null) {
      items.push({
        key: 'value',
        swatch: <Swatch />,
        label: valueLabel,
        value: formatValue(p.total_value),
      })
    }
    if (costShown && p.cost_basis !== null) {
      items.push({
        key: 'cost',
        swatch: <Swatch kind="cost" />,
        label: costLabel,
        value: formatValue(p.cost_basis),
      })
    }
    if (benchmarkShown && p.benchmark_value !== undefined && p.benchmark_value !== null) {
      items.push({
        key: 'benchmark',
        swatch: <Swatch kind="benchmark" />,
        label: benchmarkLabel ?? '',
        value: formatValue(p.benchmark_value),
        note: estimatedFrom && p.date >= estimatedFrom ? t('chart.benchmark.estimated') : undefined,
      })
      const excess =
        inPercent && p.total_value !== null
          ? excessReturn(Number(p.total_value), Number(p.benchmark_value))
          : null
      if (excess !== null) {
        items.push({
          key: 'excess',
          swatch: <span className="w-4" />,
          label: labels?.excess ?? t('chart.benchmark.excess'),
          value: signedPercent(excess),
        })
      }
    }
    plotted
      .filter((m) => m.date === p.date)
      .forEach((m, i) =>
        items.push({
          key: `marker-${i}`,
          swatch: <MarkerSwatch kind={m.kind} />,
          label: t(`chart.marker.${m.kind}`),
          value: '',
        }),
      )
    return <ChartTooltip title={formatDateLabel(p.date, multiYear)} rows={items} />
  }

  return (
    <div>
      {/* A mouse press takes no focus (pressWithoutFocus, GLG-1034). */}
      <div onPointerDown={onPointerDown} onMouseDown={pressWithoutFocus}>
        <ComposedChart
          responsive
          data={rows}
          aria-label={summary}
          margin={MARGIN}
          // A drag selects no text.
          className={onZoom ? 'touch-pan-y select-none' : 'touch-pan-y'}
          {...zoomHandlers}
          style={
            height
              ? { width: '100%', height }
              : { width: '100%', aspectRatio: '5 / 2', minHeight: 200 }
          }
        >
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0" style={{ stopColor: STROKES.value.color, stopOpacity: 0.16 }} />
              <stop offset="1" style={{ stopColor: STROKES.value.color, stopOpacity: 0 }} />
            </linearGradient>
          </defs>
          <CartesianGrid {...GRID} />
          <DateAxis dates={points.map((p) => p.date)} multiYear={multiYear} />
          <YAxis
            {...Y_AXIS}
            ticks={yTicks}
            domain={[yTicks[0], yTicks[yTicks.length - 1]]}
            tickFormatter={(v: number) => tickLabels.get(v) ?? ''}
          />
          {inPercent && (
            <ReferenceLine
              className="zero-line"
              y={0}
              strokeWidth={1}
              style={{ stroke: 'var(--color-line-strong)' }}
            />
          )}
          {hasValue && (
            <Area
              className="series-value"
              dataKey="value"
              type="linear"
              baseValue="dataMin"
              strokeWidth={1.75}
              strokeLinejoin="round"
              strokeLinecap="round"
              // The style reaches the fill path too, so it carries the color
              // and only the line strokes with it (the fill has stroke="none").
              stroke="currentColor"
              style={{ color: STROKES.value.color }}
              fill={inPercent ? 'none' : `url(#${gradientId})`}
              fillOpacity={1}
              activeDot={{
                r: 4.5,
                strokeWidth: 2,
                style: { fill: 'var(--color-surface)', stroke: STROKES.value.color },
              }}
              animationDuration={ANIMATION_MS}
            />
          )}
          {hasCost && (
            <Line
              className="series-cost"
              dataKey="cost"
              hide={!costShown}
              type="linear"
              strokeWidth={1.25}
              strokeDasharray={STROKES.cost.dash}
              style={{ stroke: STROKES.cost.color }}
              dot={false}
              activeDot={false}
              animationDuration={ANIMATION_MS}
            />
          )}
          {hasBenchmark && (
            <Line
              className="series-benchmark"
              dataKey="benchmark"
              hide={!benchmarkShown}
              type="linear"
              strokeWidth={1.25}
              strokeDasharray={STROKES.benchmark.dash}
              strokeLinecap="round"
              style={{ stroke: STROKES.benchmark.color }}
              dot={false}
              activeDot={false}
              animationDuration={ANIMATION_MS}
            />
          )}
          {span && <ReferenceArea x1={span[0]} x2={span[1]} shape={zoomSpan} />}
          <Markers
            markers={plotted.map((m) => ({
              date: m.date,
              kind: m.kind,
              value: rows[indexByDate.get(m.date)!].value!,
            }))}
          />
          <AnchoredTooltip
            content={tooltipContent}
            cursor={{ strokeDasharray: '2 3', style: { stroke: 'var(--color-faint)' } }}
            anchors={anchors}
          />
        </ComposedChart>
      </div>

      <div
        data-chart-legend
        className={`mt-2 ${LEGEND_HEIGHT} flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted`}
      >
        {hasValue && (
          <span className="flex items-center gap-1" title={labels?.valueHint}>
            <Swatch />
            {valueLabel}
          </span>
        )}
        {hasCost && (
          <LegendToggle
            kind="cost"
            label={costLabel}
            shown={costShown}
            onToggle={() => setCostHidden(!costHidden)}
          />
        )}
        {hasBenchmark && (
          <LegendToggle
            kind="benchmark"
            label={benchmarkLabel ?? ''}
            shown={benchmarkShown}
            onToggle={() => setHiddenBenchmark(benchmarkShown ? benchmarkKey : null)}
          />
        )}
        {markerKinds.map((kind) => (
          <span key={kind} data-legend-marker={kind} className="flex items-center gap-1">
            <MarkerSwatch kind={kind} />
            {t(`chart.marker.${kind}`)}
          </span>
        ))}
      </div>
    </div>
  )
}
