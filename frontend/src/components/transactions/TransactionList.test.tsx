import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { createRef } from 'react'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
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
    instrument_market: null,
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

function termDeposit(): Transaction {
  return {
    ...deposit(3, '50000', 'TRY'),
    type: 'buy',
    instrument_id: 9,
    instrument_symbol: 'TD-5e15a427b09cf13f',
    instrument_name: 'Vadeli mevduat TRY 2026-12-01',
    instrument_market: 'DEPOSIT',
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

  // ADR 0024: the user never sees a term deposit as an instrument, so no
  // generated TD- symbol and no stored Turkish name, in either locale.
  it.each([
    ['tr', true, 'Vadeli mevduat TRY 2026-12-01'],
    ['tr', false, 'Vadeli mevduat TRY 2026-12-01'],
    ['en', true, 'Term deposit TRY 2026-12-01'],
    ['en', false, 'Term deposit TRY 2026-12-01'],
  ] as const)('labels a term deposit row (%s, desktop %s)', (locale, isDesktop, label) => {
    setLocale(locale)
    desktop = isDesktop
    render(
      <TransactionList
        rows={[termDeposit()]}
        imports={new Map()}
        editingId={undefined}
        cardRef={createRef()}
        onEdit={() => {}}
        onClone={() => {}}
        onDelete={() => {}}
      />,
    )
    expect(screen.getAllByText(label)).toHaveLength(1)
    expect(screen.queryByText(/TD-/)).not.toBeInTheDocument()
    if (locale === 'en') expect(screen.queryByText(/Vadeli/)).not.toBeInTheDocument()
  })
  // A term deposit's rows change on the Mevduat tab (ADR 0024): the server
  // refuses the generic update, create and delete, so the list offers none of
  // them, and the other rows keep theirs.
  it.each([true, false])('offers no action on a term deposit row (desktop %s)', (isDesktop) => {
    desktop = isDesktop
    const onEdit = vi.fn()
    const onDelete = vi.fn()
    render(
      <TransactionList
        rows={[termDeposit(), deposit(1, '100000', 'TRY')]}
        imports={new Map()}
        editingId={undefined}
        cardRef={createRef()}
        onEdit={onEdit}
        onClone={() => {}}
        onDelete={onDelete}
      />,
    )
    const label = screen.getByText('Vadeli mevduat TRY 2026-12-01')
    fireEvent.click(label)
    expect(onEdit).not.toHaveBeenCalled()
    // The cash deposit row keeps its actions: the pencil exists on the
    // desktop table only, the phone row is itself the edit button.
    expect(screen.queryAllByRole('button', { name: /işlemini düzenle$/ })).toHaveLength(
      isDesktop ? 1 : 0,
    )
    expect(screen.getAllByRole('button', { name: /işlemini kopyala$/ })).toHaveLength(1)
    const remove = screen.getAllByRole('button', { name: /işlemini sil$/ })
    expect(remove).toHaveLength(1)
    fireEvent.click(remove[0])
    expect(onDelete).toHaveBeenCalledTimes(1)
    expect(onDelete.mock.calls[0][0].id).toBe(1)
    // The phone row stays a (disabled) button around the label; the desktop
    // label sits in no button.
    const rowButton = label.closest('button')
    if (isDesktop) expect(rowButton).toBeNull()
    else expect(rowButton).toBeDisabled()
  })
})
