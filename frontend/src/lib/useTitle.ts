import { useEffect } from 'react'
import { usePortfolios } from '../api/portfolios'
import type { PortfolioScope } from '../api/summary'
import { t } from '../i18n/strings'

// Sets the tab title to the given translated parts, most specific first, then
// the app name: "Özet · My portfolio · gelirgider". Parts not known yet (a
// portfolio name still loading) are skipped.
export function useTitle(...parts: (string | undefined)[]) {
  const title = [...parts.filter(Boolean), t('home.title')].join(' · ')
  useEffect(() => {
    document.title = title
  }, [title])
}

// useTitle for a page under a portfolio: the page's title, then the
// portfolio's name. PortfolioHeader loads the same list, so this reads its
// cache. An id the loaded list does not have is the header's not-found
// state, so the title is the 404 page's.
export function usePortfolioTitle(page: string, portfolioId: PortfolioScope) {
  const { data } = usePortfolios()
  const portfolio = portfolioId === 'all' ? undefined : data?.find((p) => p.id === portfolioId)
  const missing = portfolioId !== 'all' && data !== undefined && !portfolio
  const name = portfolioId === 'all' ? t('portfolio.all') : portfolio?.name
  useTitle(missing ? t('notFound.title') : page, missing ? undefined : name)
}
