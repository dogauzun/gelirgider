import { describe, expect, it } from 'vitest'
import type { Position, Summary } from '../api/summary'
import { buildAiPrompt } from './aiPrompt'

function position(fields: Partial<Position>): Position {
  return {
    instrument_id: 1,
    symbol: 'THYAO',
    name: 'Türk Hava Yolları',
    market: 'BIST',
    asset_class: null,
    sector: 'unknown',
    currency: 'TRY',
    quantity: '10',
    avg_cost: '100.00',
    cost_basis: '1000.00',
    last_price: '125.00',
    has_price: true,
    price_source: null,
    price_date: null,
    prev_close: null,
    prev_price_date: null,
    day_change: null,
    day_change_reporting: null,
    market_value: '1250.00',
    unrealized_pl: '250.00',
    realized_pl: '0.00',
    market_value_reporting: '1250.00',
    unrealized_pl_reporting: '250.00',
    realized_pl_reporting: '0.00',
    total_pl_reporting: '250.00',
    closed: false,
    ...fields,
  }
}

describe('buildAiPrompt', () => {
  it('lists each open position, cash and totals, then the Turkish prompt', () => {
    const summary: Summary = {
      currency: 'TRY',
      cash: '750.00',
      cash_by_currency: { TRY: '750.00', EUR: '0.00' },
      cash_by_currency_reporting: { TRY: '750.00', EUR: '0.00' },
      fx_pl: null,
      missing_rates: [],
      stats: { first_transaction_date: null, by_currency: {}, reporting: null },
      total_value: '5000.00',
      positions_value: '4250.00',
      cost_basis: '3500.00',
      unrealized_pl: '750.00',
      realized_pl: '120.50',
      positions: [
        position({}),
        position({
          instrument_id: 2,
          symbol: 'AAPL',
          market: 'US',
          currency: 'USD',
          quantity: '2.5',
          avg_cost: '80.00',
          cost_basis: '200.00',
          last_price: '100.00',
          market_value: '250.00',
          unrealized_pl: '50.00',
          market_value_reporting: '3000.00',
        }),
        // Closed: sold down to zero, only realized P/L left.
        position({
          instrument_id: 3,
          symbol: 'GARAN',
          quantity: '0',
          realized_pl: '120.50',
          closed: true,
        }),
      ],
    }

    const text = buildAiPrompt(summary, 'Emeklilik', '2026-09-29')
    const lines = text.split('\n')

    expect(lines[0]).toBe('# Emeklilik · 2026-09-29 · TRY')
    expect(text).toContain(
      '| Sembol | Piyasa | Adet | Ort. maliyet | Son fiyat | Değer (TRY) | Ağırlık % | K/Z % |',
    )
    expect(text).toContain(
      '| THYAO | BIST | 10 | 100.00 TRY | 125.00 TRY | 1250.00 | 25.00 | 25.00 |',
    )
    expect(text).toContain('| AAPL | US | 2.5 | 80.00 USD | 100.00 USD | 3000.00 | 60.00 | 25.00 |')
    expect(text).not.toContain('GARAN')
    expect(lines.filter((l) => l.startsWith('| ') && !l.startsWith('| ---'))).toHaveLength(3)
    expect(text).toContain('Nakit: 750.00 TRY\n')
    expect(text).toContain(
      'Toplam değer: 5000.00 TRY · Gerçekleşmemiş K/Z: 750.00 TRY · Gerçekleşmiş K/Z: 120.50 TRY',
    )
    expect(text).toContain('Alım ya da satım önerisi verme.')
  })

  it('weighs positions like the allocation card when cash is negative', () => {
    // A USD buy with no USD deposit: net cash is negative, so total_value
    // (positions plus cash) is smaller than the positions alone.
    const summary: Summary = {
      currency: 'TRY',
      cash: '-3000.00',
      cash_by_currency: { TRY: '1000.00', USD: '-100.00' },
      cash_by_currency_reporting: { TRY: '1000.00', USD: '-4000.00' },
      fx_pl: null,
      missing_rates: [],
      stats: { first_transaction_date: null, by_currency: {}, reporting: null },
      total_value: '1000.00',
      positions_value: '4000.00',
      cost_basis: '4000.00',
      unrealized_pl: '0.00',
      realized_pl: '0.00',
      positions: [
        position({ market_value_reporting: '1000.00' }),
        position({
          instrument_id: 2,
          symbol: 'AAPL',
          market: 'US',
          market_value_reporting: '3000.00',
        }),
      ],
    }

    const text = buildAiPrompt(summary, 'Emeklilik', '2026-09-30')

    expect(text).toContain('| THYAO | BIST | 10 | 100.00 TRY | 125.00 TRY | 1000.00 | 25.00 |')
    expect(text).toContain('| AAPL | US | 10 | 100.00 TRY | 125.00 TRY | 3000.00 | 75.00 |')
  })

  it('gives a position with a negative value no weight, like the positions table', () => {
    // A short is worth less than nothing: it has no share of the
    // total and leaves the others' weights alone.
    const summary: Summary = {
      currency: 'TRY',
      cash: '0.00',
      cash_by_currency: { TRY: '0.00' },
      cash_by_currency_reporting: { TRY: '0.00' },
      fx_pl: null,
      missing_rates: [],
      stats: { first_transaction_date: null, by_currency: {}, reporting: null },
      total_value: '1000.00',
      positions_value: '1000.00',
      cost_basis: '0.00',
      unrealized_pl: '0.00',
      realized_pl: '0.00',
      positions: [
        position({ market_value_reporting: '1250.00' }),
        position({
          instrument_id: 2,
          symbol: 'GARAN',
          quantity: '-2',
          cost_basis: '-200.00',
          last_price: '125.00',
          market_value: '-250.00',
          unrealized_pl: '-50.00',
          market_value_reporting: '-250.00',
        }),
      ],
    }

    const text = buildAiPrompt(summary, 'Emeklilik', '2026-10-12')

    expect(text).toContain('| THYAO | BIST | 10 | 100.00 TRY | 125.00 TRY | 1250.00 | 100.00 |')
    expect(text).toContain('| GARAN | BIST | -2 | 100.00 TRY | 125.00 TRY | -250.00 | - | -25.00 |')
  })
})
