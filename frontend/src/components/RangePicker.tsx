import { t } from '../i18n/strings'
import { RANGES, resolveRange, type RangeValue } from '../lib/ranges'
import { Button } from './ui/Button'
import { inputClass } from './ui/inputClass'
import { Icon } from './ui/Icon'
import { Select } from './ui/Select'
import { Segmented } from './ui/Segmented'
import { todayIstanbul } from '../lib/date'

// The chart range picker shared by Özet, Tüm portföyler and the position
// page: RANGES, then up to three previous calendar years back to the
// portfolio's first transaction, then Özel, which reveals a from / to pair of
// date inputs. The page keeps the value in the URL (lib/useRange). From sm up
// the options are chips that wrap; below sm, where the chips do not
// fit the card, they are one Select. With onClear, Özel leads its dates with
// a clear button (drag to zoom); below sm it sits beside the Select,
// as the dates need the whole row at 320 px.
export function RangePicker({
  value,
  onChange,
  firstDate,
  onClear,
}: {
  value: RangeValue
  onChange: (value: RangeValue) => void
  firstDate?: string
  onClear?: () => void
}) {
  const today = todayIstanbul()
  const lastYear = Number(today.slice(0, 4)) - 1
  const years: string[] = []
  if (firstDate) {
    for (let y = lastYear; y >= Math.max(Number(firstDate.slice(0, 4)), lastYear - 2); y--) {
      years.push(String(y))
    }
  }
  // A year from the URL outside those (or before firstDate loads) is still
  // the range shown, so it is still an option.
  if (/^\d{4}$/.test(value.key) && !years.includes(value.key)) years.push(value.key)
  const keys = [...RANGES.map((r) => r.key), ...years, 'custom']
  const labels: Record<string, string> = Object.fromEntries(
    RANGES.map((r) => [r.key, t(r.labelKey)]),
  )
  labels.custom = t('chart.range.custom')

  // Özel starts from the range shown so far, so the chart does not jump; from
  // Tümü, that is the first transaction.
  const select = (key: string) => {
    if (key === value.key) return
    if (key === 'custom') {
      const { from, to } = resolveRange(value, today)
      onChange({ key, from: from || (firstDate ?? ''), to })
    } else {
      onChange({ key, ...resolveRange({ key, from: '', to: '' }, today) })
    }
  }
  const setDates = (from: string, to: string) => {
    if (to && to <= today && (!from || from <= to)) onChange({ key: 'custom', from, to })
  }

  return (
    <div className="flex w-full min-w-0 flex-wrap items-center gap-2 sm:w-auto">
      <Select
        aria-label={t('chart.range.label')}
        value={value.key}
        onChange={select}
        options={keys.map((key) => ({ value: key, label: labels[key] ?? key }))}
        className="min-w-0 flex-1 text-[12px] sm:hidden"
      />
      <Segmented
        options={keys}
        value={value.key}
        onChange={select}
        label={(key) => labels[key] ?? key}
        aria-label={t('chart.range.label')}
        className="flex-wrap max-sm:hidden"
      />
      {value.key === 'custom' && (
        // Below sm this wrapper is `contents`, so the clear button joins the
        // Select's row; from sm up it keeps the button with the dates.
        <span className="flex items-center gap-2 max-sm:contents">
          {onClear && (
            <Button variant="ghost" iconOnly aria-label={t('chart.range.clear')} onClick={onClear}>
              <Icon name="x" />
            </Button>
          )}
          <span className="flex w-full items-center gap-2 sm:w-auto">
            <input
              type="date"
              aria-label={t('chart.range.from')}
              value={value.from}
              max={value.to || today}
              onChange={(e) => setDates(e.target.value, value.to)}
              className={`${inputClass()} min-w-0 flex-1 text-[12px] sm:w-36 sm:flex-none`}
            />
            <input
              type="date"
              aria-label={t('chart.range.to')}
              value={value.to}
              min={value.from || undefined}
              max={today}
              onChange={(e) => setDates(value.from, e.target.value)}
              className={`${inputClass()} min-w-0 flex-1 text-[12px] sm:w-36 sm:flex-none`}
            />
          </span>
        </span>
      )}
    </div>
  )
}
