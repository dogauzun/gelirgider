import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { PeriodBucket } from '../api/history'
import { t } from '../i18n/strings'
import { formatMarketDate } from '../lib/date'
import { MISSING, signedPercent } from '../lib/format'
import { heatStep } from '../lib/periods'
import { PeriodReturnsTable } from './PeriodReturnsTable'

afterEach(cleanup)

const bucket = (
  period: string,
  r: { simple: string | null; twr: string | null; mwr: string | null },
  extra: Partial<PeriodBucket> = {},
): PeriodBucket => ({
  period,
  from: period.length === 4 ? `${period}-01-01` : `${period}-01`,
  to: period.length === 4 ? `${period}-12-31` : `${period}-28`,
  partial: false,
  returns: r,
  benchmark: null,
  ...extra,
})
const same = (v: string | null) => ({ simple: v, twr: v, mwr: v })

// 2025 starts in December; 2026 has January to March, February's twr null.
const months = [
  bucket('2025-12', same('0.004000'), { from: '2025-12-15', partial: true }),
  bucket('2026-01', { simple: '0.031000', twr: '0.030000', mwr: '0.029000' }),
  bucket('2026-02', { simple: '-0.024000', twr: null, mwr: '-0.023000' }),
  bucket('2026-03', same('0.090000'), { to: '2026-03-10', partial: true }),
]
const years = [
  bucket('2025', same('0.004000'), { from: '2025-12-15', partial: true }),
  bucket('2026', same('0.123456'), { to: '2026-03-10', partial: true }),
]

function renderTable(method: 'simple' | 'twr' | 'mwr' = 'simple', excess = false, ms = months) {
  render(
    <>
      <h2 id="heading">Dönemsel getiri</h2>
      <PeriodReturnsTable
        months={ms}
        years={years}
        method={method}
        excess={excess}
        labelledBy="heading"
      />
    </>,
  )
  const table = screen.getByRole('table', { name: 'Dönemsel getiri' })
  const rows = within(table).getAllByRole('row')
  return { table, rows }
}

it('has a header row of months and a row header per year, newest first', () => {
  const { rows } = renderTable()
  const headers = within(rows[0])
    .getAllByRole('columnheader')
    .map((h) => h.textContent)
  expect(headers).toHaveLength(13)
  expect(headers[0]).toBe(formatMarketDate('2026-01-01', { month: 'short' }))
  expect(headers[12]).toBe(t('performance.periods.year'))
  expect(rows.slice(1).map((r) => within(r).getByRole('rowheader').textContent)).toEqual([
    '2026',
    '2025',
  ])
  // Twelve months and the year in every row.
  expect(within(rows[1]).getAllByRole('cell')).toHaveLength(13)
})

it("prints the method's percentage with one decimal and colours it by heatStep", () => {
  const { rows } = renderTable('simple')
  const jan = within(rows[1]).getAllByRole('cell')[0]
  expect(jan).toHaveTextContent(signedPercent(0.031, { minimumFractionDigits: 1 }))
  expect(jan).toHaveAttribute('data-step', String(heatStep(0.031)))
  expect(jan).toHaveAttribute('data-step', '3')
  expect(jan.style.backgroundColor).toContain('--color-gain')
  const feb = within(rows[1]).getAllByRole('cell')[1]
  expect(feb).toHaveAttribute('data-step', '-3')
  expect(feb.style.backgroundColor).toContain('--color-loss')
})

it('follows the method', () => {
  const { rows } = renderTable('mwr')
  expect(within(rows[1]).getAllByRole('cell')[0]).toHaveTextContent(
    signedPercent(0.029, { minimumFractionDigits: 1 }),
  )
})

it('shows a dot for a month without a bucket and a dash for a null return', () => {
  const { rows } = renderTable('twr')
  const cells2026 = within(rows[1]).getAllByRole('cell')
  expect(cells2026[1]).toHaveTextContent(MISSING)
  expect(cells2026[1]).toHaveAttribute('data-step', '0')
  // April onwards has no bucket.
  expect(cells2026[3]).toHaveTextContent(`·${t('performance.periods.noData')}`)
  expect(cells2026[3]).not.toHaveAttribute('data-step')
  // 2025 has December only.
  const cells2025 = within(rows[2]).getAllByRole('cell')
  expect(cells2025.slice(0, 11).every((c) => c.textContent?.startsWith('·'))).toBe(true)
})

it('marks a partial bucket and titles it with its dates and the exact percentage', () => {
  const { rows } = renderTable()
  const year = within(rows[1]).getAllByRole('cell')[12]
  expect(year).toHaveTextContent(`${signedPercent(0.123456, { minimumFractionDigits: 1 })}*`)
  expect(year).toHaveTextContent(t('performance.periods.partial'))
  expect(year).toHaveAttribute(
    'title',
    `${t('performance.periods.partial')} · ${formatMarketDate('2026-01-01')} – ${formatMarketDate(
      '2026-03-10',
    )} · ${signedPercent(0.123456, { maximumFractionDigits: 4 })}`,
  )
  const jan = within(rows[1]).getAllByRole('cell')[0]
  expect(jan.textContent).not.toContain('*')
  expect(jan.getAttribute('title')).not.toContain(t('performance.periods.partial'))
})

it('shows the excess over the benchmark, a dash where the benchmark has none', () => {
  const withBenchmark = [
    bucket('2026-01', same('0.100000'), { benchmark: same('0.050000') }),
    bucket('2026-02', same('0.100000')),
  ]
  const { rows } = renderTable('simple', true, withBenchmark)
  const cells = within(rows[1]).getAllByRole('cell')
  expect(cells[0]).toHaveTextContent(signedPercent(1.1 / 1.05 - 1, { minimumFractionDigits: 1 }))
  expect(cells[1]).toHaveTextContent(MISSING)
})

// Below md the heatmap scrolls sideways and holds nothing focusable,
// so its scroller must be a named tab stop that arrow keys scroll.
it('makes the sideways scroller a named, focusable region', () => {
  const { table } = renderTable()
  const region = screen.getByRole('region', { name: 'Dönemsel getiri' })
  expect(region).toHaveAttribute('tabindex', '0')
  expect(region).toContainElement(table)
  expect(region).toHaveClass('relative', 'overflow-x-auto')
})
