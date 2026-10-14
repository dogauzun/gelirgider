import { afterEach, describe, expect, it } from 'vitest'
import { MASK } from './format'
import { setPrivacy } from './privacy'
import {
  axisTicks,
  dateTicks,
  formatPercentTicks,
  formatTicks,
  niceTicks,
  placeTooltip,
  rangeDelta,
} from './chart'

describe('dateTicks', () => {
  const days = (n: number) =>
    Array.from({ length: n }, (_, i) =>
      new Date(Date.UTC(2026, 0, 1 + i)).toISOString().slice(0, 10),
    )

  it('labels one date per about 120 px, 3 to 7, evenly spaced, with both ends', () => {
    const dates = days(93)
    const wide = dateTicks(dates, 900)
    expect(wide).toHaveLength(7)
    expect(wide[0]).toBe(dates[0])
    expect(wide.at(-1)).toBe(dates[92])
    expect(new Set(wide).size).toBe(7)
    expect(wide).toEqual([0, 15, 31, 46, 61, 77, 92].map((i) => dates[i]))
    expect(dateTicks(dates, 300)).toEqual([dates[0], dates[46], dates[92]])
    expect(dateTicks(dates, 200)).toEqual([dates[0], dates[46], dates[92]])
  })

  it('labels every date when there are fewer than the width allows', () => {
    expect(dateTicks(days(1), 900)).toEqual(days(1))
    expect(dateTicks(days(2), 900)).toEqual(days(2))
    expect(dateTicks(days(5), 1200)).toEqual(days(5))
  })

  it('keeps labels with a year apart, down to the two ends on a narrow plot (GLG-1035)', () => {
    const dates = days(366)
    // A 320 px phone's 172 px plot fits "30 Eyl 2024" and "06 Eki 2026" only.
    expect(dateTicks(dates, 172, true)).toEqual([dates[0], dates[365]])
    expect(dateTicks(dates, 900, true)).toHaveLength(7)
    // Without a year the same plot keeps three.
    expect(dateTicks(dates, 172)).toHaveLength(3)
  })
})

describe('niceTicks', () => {
  it('rounds to a nice step, extends the ends outward and includes zero', () => {
    const ticks = niceTicks(-6025, 21200)
    expect(ticks[0]).toBeLessThanOrEqual(-6025)
    expect(ticks[ticks.length - 1]).toBeGreaterThanOrEqual(21200)
    expect(ticks).toContain(0)
    for (const tick of ticks) expect(tick % 5000 === 0).toBe(true)
  })
})

describe('axisTicks', () => {
  it('fits the axis to the data, keeps 0 only when the data reaches it, and keeps labels distinct', () => {
    // A 905k-1M portfolio: no zero baseline, the plot spans the data.
    expect(axisTicks([905000, 998000, 940000])).toEqual([
      900000, 920000, 940000, 960000, 980000, 1000000,
    ])
    // Data touching or crossing 0 keeps it on the axis.
    expect(axisTicks([0, 1200])).toContain(0)
    expect(axisTicks([-300, 1200])).toContain(0)
    // A flat series still gets a range around its value.
    const flat = axisTicks([950000, 950000])
    expect(flat[0]).toBeLessThan(950000)
    expect(flat.at(-1)).toBeGreaterThan(950000)
    // Narrow ticks across a unit boundary do not collapse into repeated `1 Mn`.
    // Also for ranges that need more than three fraction digits: a fund price
    // and a total that barely moves.
    for (const range of [
      [996000, 1012000],
      [0.0312, 0.0321],
      [1000000, 1000400],
    ]) {
      const labels = formatTicks(axisTicks(range))
      expect(new Set(labels).size).toBe(labels.length)
    }
  })
})

describe('formatPercentTicks', () => {
  afterEach(() => setPrivacy(false))

  it('prints fractions as percentages, distinct, and never masks them', () => {
    expect(formatPercentTicks(axisTicks([-0.04, 0, 0.081]))).toEqual(['-%5', '%0', '%5', '%10'])
    // A range under one percent needs fraction digits to keep labels apart.
    const narrow = formatPercentTicks(axisTicks([0, 0.004]))
    expect(new Set(narrow).size).toBe(narrow.length)
    expect(narrow).toContain('%0,1')

    setPrivacy(true)
    expect(formatPercentTicks([0, 0.05])).not.toContain(MASK)
  })
})

describe('rangeDelta', () => {
  it('returns the first-to-last change net of cash flows, or null without two points', () => {
    expect(
      rangeDelta(
        [
          { date: '2026-01-01', total_value: '1000.00', cash: '0', cost_basis: '900.00' },
          { date: '2026-02-01', total_value: '1250.00', cash: '0', cost_basis: '900.00' },
        ],
        '0.00',
      ),
    ).toBe('250.00')

    // A 200 deposit inside the range is money put in, not a gain: the value
    // rose by 250, only 50 of it earned (GLG-219).
    expect(
      rangeDelta(
        [
          { date: '2026-01-01', total_value: '1000.00', cash: '0', cost_basis: '900.00' },
          { date: '2026-02-01', total_value: '1250.00', cash: '200.00', cost_basis: '900.00' },
        ],
        '200.00',
      ),
    ).toBe('50.00')

    expect(
      rangeDelta(
        [
          { date: '2026-01-01', total_value: '10000.00', cash: '0', cost_basis: '0' },
          { date: '2026-02-01', total_value: '1011000.00', cash: '0', cost_basis: '0' },
        ],
        '1000000.00',
      ),
    ).toBe('1000.00')

    expect(
      rangeDelta(
        [
          { date: '2026-01-01', total_value: '0', cash: '0', cost_basis: '0' },
          { date: '2026-02-01', total_value: '1200.00', cash: '1200.00', cost_basis: '0' },
        ],
        '1200.00',
      ),
    ).toBe('0.00')

    expect(
      rangeDelta(
        [
          { date: '2026-01-01', total_value: '0', cash: '0', cost_basis: '0' },
          { date: '2026-02-01', total_value: '500.00', cash: '0', cost_basis: '0' },
        ],
        '0.00',
      ),
    ).toBe('500.00')

    expect(
      rangeDelta(
        [{ date: '2026-01-01', total_value: '1000.00', cash: '0', cost_basis: '0' }],
        '0.00',
      ),
    ).toBeNull()
  })

  it('subtracts exactly, not as floats (GLG-942)', () => {
    const points = (first: string, last: string) => [
      { date: '2026-01-01', total_value: first, cash: '0', cost_basis: '0' },
      { date: '2026-02-01', total_value: last, cash: '0', cost_basis: '0' },
    ]
    // 0.30 - 0.10 - 0.20 is a hair below zero in floats, printed `-0.00`.
    expect(rangeDelta(points('0.10', '0.30'), '0.20')).toBe('0.00')
    // Above 2^53 cents a float has no cent left: .01 would print as .02.
    expect(rangeDelta(points('0.00', '100000000000000.01'), '0.00')).toBe('100000000000000.01')
  })
})

describe('placeTooltip', () => {
  const plot = { x: 56, y: 12, width: 227, height: 164 }
  const card = { width: 195, height: 68 }
  const contains = (p: { x: number; y: number }, at: { x: number; y: number }, size = card) =>
    p.x >= at.x && p.x <= at.x + size.width && p.y >= at.y && p.y <= at.y + size.height

  it('puts the card 12px below a point in the upper half, inside the plot', () => {
    const point = { x: 170, y: 38 }
    const at = placeTooltip(point, card, plot, 12)
    expect(at.y).toBe(50)
    expect(at.x).toBe(56)
    expect(contains(point, at)).toBe(false)
  })

  it('puts the card 12px above a point in the lower half, inside the plot', () => {
    const point = { x: 80, y: 150 }
    const at = placeTooltip(point, card, plot, 12)
    expect(at.y).toBe(150 - 12 - 68)
    expect(at.x).toBe(56 + 227 - 195)
    expect(contains(point, at)).toBe(false)
  })

  it('lets a card pushed back inside the plot come within half the gap', () => {
    // 79.5px above the point: 68 + 12 does not fit, 68 + 11.5 does.
    const point = { x: 130, y: 251 }
    const at = placeTooltip(point, card, { x: 96, y: 171.5, width: 227, height: 158 }, 12)
    expect(at).toEqual({ x: 128, y: 171.5 })
    expect(contains(point, at)).toBe(false)
  })

  it('moves a card too tall for either side beside the point', () => {
    const tall = { width: 60, height: 150 }
    const point = { x: 250, y: 90 }
    const at = placeTooltip(point, tall, plot, 12)
    expect(at.y).toBeGreaterThanOrEqual(plot.y)
    expect(at.x).toBe(250 - 12 - 60)
    expect(contains(point, at, tall)).toBe(false)
  })
})
