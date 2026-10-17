import type { UseQueryResult } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { t } from '../../i18n/strings'
import { LoadError } from './LoadError'
import { Skeleton } from './Skeleton'

type ChartQuery = Pick<
  UseQueryResult<unknown>,
  'data' | 'isError' | 'error' | 'refetch' | 'isFetching' | 'isPlaceholderData'
>

// The frame around a history query's chart: the failure with a
// Retry, a skeleton of the chart's `height` until the first data,
// then the chart, the previous range's lines dimmed while the new range loads.
// `errorTitle` names what failed when it is not a chart.
export function ChartFrame({
  query,
  height = 200,
  errorTitle = t('chart.error'),
  children,
}: {
  query: ChartQuery
  height?: number
  errorTitle?: string
  children: ReactNode
}) {
  if (query.isError)
    return (
      <LoadError
        title={errorTitle}
        error={query.error}
        onRetry={() => query.refetch()}
        retrying={query.isFetching}
      />
    )
  if (!query.data)
    return (
      <div style={{ height }}>
        <Skeleton className="h-full w-full" />
      </div>
    )
  return (
    <div
      // eslint-disable-next-line no-restricted-syntax -- the one aria-busy, see eslint.config.js
      aria-busy={query.isPlaceholderData}
      className={`transition-opacity ${query.isPlaceholderData ? 'opacity-50' : ''}`}
    >
      {children}
    </div>
  )
}
