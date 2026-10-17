import { cleanup, render, screen } from '@testing-library/react'
import { createRef } from 'react'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import type { Transaction } from '../../api/transactions'
import { setLocale } from '../../i18n/strings'
import { TransactionList } from './TransactionList'

function deposit(id: number, amount: string, currency: string): Transaction {
  return {
    id,
    portfolio_id: 1,
    instrument_id: null,
    instrument_symbol: null,
    instrument_name: null,
    type: 'deposit',
    date: '2026-09-01',
    quantity: null,
    price: null,
    amount,
    fee: '0',
    currency,
    note: null,
    created_at: '2026-09-01T00:00:00Z',
    import_id: null,
  }
}

// jsdom has no matchMedia: the table from md up, the phone list below.
let desktop = true
beforeAll(() => {
  window.matchMedia = (query) =>
    ({
      matches: desktop,
      media: query,
      addEventListener() {},
      removeEventListener() {},
    }) as unknown as MediaQueryList
})

afterEach(() => {
  cleanup()
  desktop = true
  setLocale('tr')
})

describe('TransactionList', () => {
  // A TRY and a USD deposit on one day read as the same "sil"
  // button; the amount tells them apart.
  it.each([
    ['tr', true],
    ['tr', false],
    ['en', true],
    ['en', false],
  ] as const)('names each row delete button apart (%s, desktop %s)', (locale, isDesktop) => {
    setLocale(locale)
    desktop = isDesktop
    render(
      <TransactionList
        rows={[deposit(1, '100000', 'TRY'), deposit(2, '5000', 'USD')]}
        imports={new Map()}
        editingId={undefined}
        cardRef={createRef()}
        onEdit={() => {}}
        onClone={() => {}}
        onDelete={() => {}}
      />,
    )
    const names = screen
      .getAllByRole('button', { name: locale === 'tr' ? /işlemini sil$/ : /^Delete / })
      .map((button) => button.getAttribute('aria-label'))
    expect(names).toHaveLength(2)
    expect(names[0]).not.toBe(names[1])
  })
})
