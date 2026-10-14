import { t } from '../i18n/strings'

// A value that silently drops the holdings it could not convert is worse than
// no value at all, so mark it. Reuses summary.missingRates rather
// than adding a portfolios.* twin, so the wording -- "excluded for
// lack of a rate", never "these currencies have no rate" -- lives in one
// place. `currencies` is the excluded set; on the total line it is the union
// across portfolios.
export function MissingRatesMark({
  currencies,
  currency,
}: {
  currencies: string[]
  currency: string
}) {
  if (currencies.length === 0) {
    return null
  }
  const label = t('summary.missingRates', { currency, currencies: currencies.join(', ') })
  return (
    <span role="img" aria-label={label} title={label} className="ml-0.5 cursor-help text-warn">
      *
    </span>
  )
}
