import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { setLocale } from '../../i18n/strings'
import { PreviewListItem, PreviewTableRow, UnknownNames } from './PreviewTable'
import type { Row } from './shared'

afterEach(() => setLocale('tr'))

const row: Row = {
  key: 'trade-1',
  status: 'unknown_instrument',
  type: 'buy',
  date: '2025-12-20',
  symbol: 'XYZ',
  quantity: '1',
  price: '10',
  amount: '10',
  fee: '0',
  currency: 'USD',
  checked: false,
}

const client = new QueryClient()
const item = (r: Row) => (
  <QueryClientProvider client={client}>
    <ul>
      <PreviewListItem row={r} onToggle={() => {}} onEdit={() => {}} onPick={() => {}} />
    </ul>
  </QueryClientProvider>
)
const renderItem = (r: Row) => render(item(r))
const resolved: Row = {
  ...row,
  status: 'ok',
  resolved_by: 'name',
  instrument_symbol: 'XYZW',
  instrument_name: 'Xyz Holding',
}

// An unknown symbol's picker is in the block above the rows, once per name
// (GLG-1193): the row shows the symbol and no picker of its own.
it('shows an unknown row its symbol without a picker', () => {
  const { unmount } = renderItem(row)
  expect(screen.getByText('XYZ')).toBeInTheDocument()
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  unmount()

  render(tableRow(row, false))
  expect(screen.getAllByRole('row')).toHaveLength(1)
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
})

// In the middle column, beside the amount and the edit button, the picker got
// ~142 px at 375 px and cut its placeholder (GLG-1053).
it('opens the change picker of a resolved row on that full-width line', () => {
  renderItem(resolved)
  expect(screen.queryByTestId('picker-line')).not.toBeInTheDocument()

  fireEvent.click(screen.getByRole('button', { name: 'XYZ eşleşmesini değiştir' }))

  const line = screen.getByTestId('picker-line')
  expect(line).toHaveClass('basis-full')
  expect(line).toContainElement(screen.getByRole('combobox', { name: 'XYZ için enstrüman seç' }))
})

// A pick in another row with the same symbol clears this row's resolved_by
// too; its open picker then had no Değiştir or İptal left to close it.
it('closes the change picker once the row is no longer resolved', () => {
  const { rerender } = renderItem(resolved)
  fireEvent.click(screen.getByRole('button', { name: 'XYZ eşleşmesini değiştir' }))
  expect(screen.getByTestId('picker-line')).toBeInTheDocument()

  rerender(item({ ...resolved, symbol: 'XYZW', alias: 'XYZ', resolved_by: undefined }))

  expect(screen.queryByTestId('picker-line')).not.toBeInTheDocument()
  expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
})

// "Alış" and "elle" stacked in the narrow Tür column after an alias pick and
// made the row taller than its neighbours; kept side by side, an opening row's
// three pills widened the table past its card. So Tür holds the type alone
// and the markers sit in the fixed-width Durum column (GLG-1056).
it('puts the opening and manual markers of a table row in Durum, not Tür', () => {
  render(
    <QueryClientProvider client={client}>
      <table>
        <tbody>
          <PreviewTableRow
            row={{ ...resolved, manual: true, opening: true }}
            onToggle={() => {}}
            onEdit={() => {}}
            onPick={() => {}}
          />
        </tbody>
      </table>
    </QueryClientProvider>,
  )

  const cells = screen.getAllByRole('cell')
  expect(cells[2]).toHaveTextContent(/^Alış$/)
  expect(cells[7]).toContainElement(screen.getByText('elle'))
  expect(cells[7]).toContainElement(screen.getByText('Açılış'))
})

// The phone list's type column is ~100-150 px: an opening row's three pills
// must wrap there rather than run into the amount (GLG-1056).
it('lets the type pills wrap in the phone list', () => {
  renderItem({ ...resolved, manual: true, opening: true })

  const pills = screen.getByText('elle').parentElement
  expect(pills).toContainElement(screen.getByText('Açılış'))
  expect(pills).toHaveClass('flex-wrap')
})

// Between 768 and 1280 px the card is 720 to 1000 px and the fixed-width
// Durum column pushed the table past it; there the status and markers sit
// under the instrument and the row has eight cells (GLG-1066).
it('folds Durum under the instrument when the table is not wide', () => {
  render(
    <QueryClientProvider client={client}>
      <table>
        <tbody>
          <PreviewTableRow
            row={{ ...row, opening: true }}
            wide={false}
            onToggle={() => {}}
            onEdit={() => {}}
            onPick={() => {}}
          />
        </tbody>
      </table>
    </QueryClientProvider>,
  )

  const cells = within(screen.getAllByRole('row')[0]).getAllByRole('cell')
  expect(cells).toHaveLength(8)
  expect(cells[3]).toHaveTextContent('Enstrüman bulunamadı: XYZ')
  expect(cells[3]).toContainElement(screen.getByText('Açılış'))
})

const tableRow = (r: Row, wide: boolean) => (
  <QueryClientProvider client={client}>
    <table>
      <tbody>
        <PreviewTableRow
          row={r}
          wide={wide}
          onToggle={() => {}}
          onEdit={() => {}}
          onPick={() => {}}
        />
      </tbody>
    </table>
  </QueryClientProvider>
)

// In the instrument cell the picker's 12rem min width spilled over the
// "Adet × fiyat" cell at 768-860 px. Below xl it gets its own line under the
// row, from the instrument column to the end, as in the phone list (GLG-1080).
it('gives the picker of a table row that is not wide its own line', () => {
  render(tableRow(resolved, false))
  expect(screen.getAllByRole('row')).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: 'XYZ eşleşmesini değiştir' }))
  const [main, line] = screen.getAllByRole('row')
  const picker = screen.getByRole('combobox', { name: 'XYZ için enstrüman seç' })
  expect(main).not.toContainElement(picker)
  expect(line).toContainElement(picker)
  expect(picker.closest('td')).toHaveAttribute('colspan', '5')
})

it('keeps the picker in the instrument cell of a wide table row', () => {
  render(tableRow(resolved, true))
  fireEvent.click(screen.getByRole('button', { name: 'XYZ eşleşmesini değiştir' }))

  expect(screen.getAllByRole('row')).toHaveLength(1)
  expect(screen.getAllByRole('cell')[3]).toContainElement(
    screen.getByRole('combobox', { name: 'XYZ için enstrüman seç' }),
  )
})

// The Garanti Yatırım parser's skip reasons showed as raw English text in
// Türkçe (GLG-1095). A Midas reason still reads as before and a reason
// without a key falls back to the server's text.
it('translates the skip reasons of every parser and shows an unknown one raw', () => {
  const skipped = (reason: string): Row => ({
    key: 'skip-0',
    status: 'skipped',
    reason,
    checked: false,
  })

  const { rerender } = renderItem(skipped('opening balance'))
  expect(screen.getByText('Açılış bakiyesi, atlandı')).toBeInTheDocument()

  rerender(item(skipped('fx conversion')))
  expect(screen.getByText('Döviz işlemi, atlandı')).toBeInTheDocument()

  rerender(item(skipped('something new')))
  expect(screen.getByText('something new')).toBeInTheDocument()

  setLocale('en')
  rerender(item(skipped('opening balance')))
  expect(screen.getByText('Opening balance, skipped')).toBeInTheDocument()
})

// The unknown rows group by name, each name once with its row count (GLG-1193).
it('groups the unknown rows by name and counts each name', () => {
  render(
    <QueryClientProvider client={client}>
      <UnknownNames
        rows={[row, { ...row, key: 'trade-2' }, { ...row, key: 'trade-3', symbol: 'ABC' }]}
        onPick={() => {}}
      />
    </QueryClientProvider>,
  )
  expect(screen.getByRole('heading', { name: 'Eşleşmeyen isimler (2)' })).toBeInTheDocument()
  const [first, second] = screen.getAllByRole('listitem')
  expect(within(first).getByText('2 satır')).toBeInTheDocument()
  expect(within(second).getByText('1 satır')).toBeInTheDocument()
})

// A Garanti fund row names a fund TEFAS may have renamed since by its old name;
// on its own, nothing links that name to the code (GLG-1197). Its unknown name
// says what helps; a stock's does not.
it('hints at a renamed fund under the picker of a TEFAS name only', () => {
  const fund: Row = {
    ...row,
    symbol: 'İSTANBUL PORTFÖY ONDÖRDÜNCÜ SERBEST',
    market: 'TEFAS',
    currency: 'TRY',
  }
  const names = (rows: Row[]) => (
    <QueryClientProvider client={client}>
      <UnknownNames rows={rows} onPick={() => {}} />
    </QueryClientProvider>
  )
  const { rerender } = render(names([fund]))
  expect(
    screen.getByText(
      'Fonun adı değişmiş olabilir. Aynı hesabın daha sonraki bir ekstresini aynı seferde yükle ya da fon koduyla ara.',
    ),
  ).toBeInTheDocument()

  setLocale('en')
  rerender(names([fund]))
  expect(
    screen.getByText(
      'The fund may have been renamed. Upload a later statement of the same account in the same batch, or search by fund code.',
    ),
  ).toBeInTheDocument()

  rerender(names([row]))
  expect(screen.queryByText(/renamed/)).not.toBeInTheDocument()
})
