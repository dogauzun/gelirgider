import { Suspense, type ReactNode } from 'react'
import { t } from '../i18n/strings'
import { DONUT_SIZE } from '../lib/allocation'
import { LEGEND_HEIGHT } from '../lib/chart'
import { lazyNamed } from '../lib/lazyNamed'
import type { AllocationDonutProps } from './charts/AllocationDonut'
import type { BarChartProps } from './charts/BarChart'
import type { DrawdownChartProps } from './charts/DrawdownChart'
import type { LineChartProps } from './charts/LineChart'
import type { ReturnBarsProps } from './charts/ReturnBars'
import { ErrorBoundary } from './ErrorBoundary'
import { Skeleton } from './ui/Skeleton'

// The only way pages get a chart (GLG-915). Recharts and both charts are one
// chunk, `charts/index.ts`, fetched when a page first renders a chart.
// ponytail: a failed fetch stays failed until a reload. React keeps a
// rejected lazy component, and the browser keeps a failed module fetch for
// the document, so an in-page retry would only get the same error.
const LazyLineChart = lazyNamed(() => import('./charts'), 'LineChart')
const LazyBarChart = lazyNamed(() => import('./charts'), 'BarChart')
const LazyDrawdownChart = lazyNamed(() => import('./charts'), 'DrawdownChart')
const LazyReturnBars = lazyNamed(() => import('./charts'), 'ReturnBars')
const LazyAllocationDonut = lazyNamed(() => import('./charts'), 'AllocationDonut')

// The default fallback is built on each render, so `t()` reads the locale
// active then, not the one at import (GLG-1254).
function Loaded({
  placeholder,
  fallback = (
    <p role="alert" className="text-sm text-muted">
      {t('chart.error')}
    </p>
  ),
  children,
}: {
  placeholder: ReactNode
  fallback?: ReactNode
  children: ReactNode
}) {
  return (
    <ErrorBoundary fallback={fallback}>
      <Suspense fallback={placeholder}>{children}</Suspense>
    </ErrorBoundary>
  )
}

// The placeholders take the chart's own height, so nothing moves when it
// arrives: the line chart is its `height`, or 5:2 with a 200px floor, plus
// its legend (mt-2, LEGEND_HEIGHT).
export function LineChart(props: LineChartProps) {
  return (
    <Loaded
      placeholder={
        <div data-chart-placeholder>
          {props.height ? (
            <div style={{ height: props.height }}>
              <Skeleton className="h-full w-full" />
            </div>
          ) : (
            <Skeleton className="aspect-[5/2] min-h-[200px] w-full" />
          )}
          <div className={`mt-2 ${LEGEND_HEIGHT}`} />
        </div>
      }
    >
      <LazyLineChart {...props} />
    </Loaded>
  )
}

export function BarChart(props: BarChartProps) {
  return (
    <Loaded
      placeholder={
        <div data-chart-placeholder style={{ height: props.height ?? 200 }}>
          <Skeleton className="h-full w-full" />
        </div>
      }
    >
      <LazyBarChart {...props} />
    </Loaded>
  )
}

export function DrawdownChart(props: DrawdownChartProps) {
  return (
    <Loaded
      placeholder={
        <div data-chart-placeholder style={{ height: props.height ?? 140 }}>
          <Skeleton className="h-full w-full" />
        </div>
      }
    >
      <LazyDrawdownChart {...props} />
    </Loaded>
  )
}

// The return bars' placeholder is their height plus their one-line legend.
export function ReturnBars(props: ReturnBarsProps) {
  return (
    <Loaded
      placeholder={
        <div data-chart-placeholder>
          <div style={{ height: props.height ?? 200 }}>
            <Skeleton className="h-full w-full" />
          </div>
          <div className="mt-2 min-h-4" />
        </div>
      }
    >
      <LazyReturnBars {...props} />
    </Loaded>
  )
}

// The donut's placeholder is a ring of its size. On a failed chunk nothing
// renders: the allocation legend beside it carries every number.
export function AllocationDonut(props: AllocationDonutProps) {
  return (
    <Loaded
      placeholder={
        <div
          data-chart-placeholder
          className="shrink-0 animate-pulse rounded-full border-16 border-line"
          style={{ width: DONUT_SIZE, height: DONUT_SIZE }}
        />
      }
      fallback={null}
    >
      <LazyAllocationDonut {...props} />
    </Loaded>
  )
}
