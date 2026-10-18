import type { UseQueryResult } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { t, type StringKey } from '../i18n/strings'
import { PortfolioHeader } from './PortfolioHeader'
import { ActionsTh } from './ui/ActionsTh'
import { Button } from './ui/Button'
import { Icon } from './ui/Icon'
import { LoadError } from './ui/LoadError'
import { SkeletonRows } from './ui/Skeleton'
import { TABLE, TH } from './ui/table'

// The frame of a portfolio tab that lists one query's rows (Mevduat,
// Planlar): the header, then the loading skeleton or the load error, then the
// page's own content as children.
export function PortfolioListFrame({
  portfolioId,
  query,
  loadingLabel,
  errorTitle,
  children,
}: {
  portfolioId: number
  query: UseQueryResult
  loadingLabel: string
  errorTitle: string
  children: ReactNode
}) {
  return (
    // The page's own button leads, so the header's add-transaction one is quiet.
    <PortfolioHeader portfolioId={portfolioId} add="quiet">
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        {query.isLoading && <SkeletonRows label={loadingLabel} />}
        {query.isError && (
          <LoadError
            title={errorTitle}
            error={query.error}
            onRetry={() => query.refetch()}
            retrying={query.isFetching}
            className="mb-6"
          />
        )}
        {children}
      </div>
    </PortfolioHeader>
  )
}

// The card of such a tab: a title with the add button, then the table with
// `columns` as its header and the rows as children.
export function ListCard({
  title,
  addLabel,
  onAdd,
  columns,
  children,
}: {
  title: string
  addLabel: string
  onAdd: () => void
  columns: { labelKey: StringKey; className: string }[]
  children: ReactNode
}) {
  return (
    <section className="relative mb-6 overflow-x-auto card">
      <div className="flex items-center justify-between gap-3 border-b border-line px-4 py-3">
        <h2 className="text-[14px] font-semibold text-ink">{title}</h2>
        <Button onClick={onAdd}>
          <Icon name="plus" />
          {addLabel}
        </Button>
      </div>
      <table className={TABLE}>
        <thead>
          <tr>
            {columns.map(({ labelKey, className }) => (
              <th key={labelKey} className={`${TH} ${className}`}>
                {t(labelKey)}
              </th>
            ))}
            <ActionsTh />
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </section>
  )
}
