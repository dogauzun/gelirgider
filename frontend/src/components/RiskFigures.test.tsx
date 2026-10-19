import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { t } from '../i18n/strings'
import type { RiskStats } from '../lib/risk'
import { RiskFigures } from './RiskFigures'

const stats = (days: number): RiskStats => ({
  volatility: 0.1,
  downside: 0.05,
  best: { date: '2026-01-02', value: 0.01 },
  worst: { date: '2026-01-03', value: -0.01 },
  days,
  benchmark: null,
})

it('says there is not enough data without stats', () => {
  render(<RiskFigures stats={null} benchmark={null} />)

  expect(screen.getByText(t('risk.tooShort'))).toBeInTheDocument()
})

it('names the benchmark that has gaps when the stats carry no benchmark figures', () => {
  render(<RiskFigures stats={stats(40)} benchmark="USD" benchmarkLabel="USD" />)

  expect(screen.getByText(t('risk.benchmarkNoData', { benchmark: 'USD' }))).toBeInTheDocument()
})

it('says there is not enough data under 30 daily returns', () => {
  render(<RiskFigures stats={stats(29)} benchmark="USD" benchmarkLabel="USD" />)

  expect(screen.getByText(t('risk.tooShort'))).toBeInTheDocument()
})
