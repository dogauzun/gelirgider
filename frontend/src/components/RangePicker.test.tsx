import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { chooseOption } from '../test/select'
import { RangePicker } from './RangePicker'

afterEach(() => vi.useRealTimers())

it('offers the calendar years back to the first transaction and a custom range', () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-28T09:00:00Z'))
  const onChange = vi.fn()
  const { rerender } = render(
    <RangePicker
      value={{ key: '3m', from: '', to: '' }}
      onChange={onChange}
      firstDate="2023-05-01"
    />,
  )

  const chips = screen.getAllByRole('radio').map((r) => r.textContent)
  expect(chips).toEqual(expect.arrayContaining(['2025', '2024', '2023']))
  expect(chips).not.toContain('2022')
  expect(chips).not.toContain('2026')

  fireEvent.click(screen.getByRole('radio', { name: '2025' }))
  expect(onChange).toHaveBeenLastCalledWith({ key: '2025', from: '2025-01-01', to: '2025-12-31' })

  fireEvent.click(screen.getByRole('radio', { name: 'Özel' }))
  expect(onChange).toHaveBeenLastCalledWith({ key: 'custom', from: '2026-06-28', to: '2026-09-28' })

  rerender(
    <RangePicker
      value={{ key: 'custom', from: '2026-06-28', to: '2026-09-28' }}
      onChange={onChange}
      firstDate="2023-05-01"
    />,
  )
  fireEvent.change(screen.getByLabelText('Başlangıç'), { target: { value: '2024-03-15' } })
  expect(onChange).toHaveBeenLastCalledWith({ key: 'custom', from: '2024-03-15', to: '2026-09-28' })
  fireEvent.change(screen.getByLabelText('Bitiş'), { target: { value: '2026-08-31' } })
  expect(onChange).toHaveBeenLastCalledWith({ key: 'custom', from: '2026-06-28', to: '2026-08-31' })
})

// Tailwind does not run in jsdom, so both are in the DOM; the classes pick one.
it('is a select below sm and the chips from sm up, both named and with the same options', async () => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-28T09:00:00Z'))
  const onChange = vi.fn()
  render(
    <RangePicker
      value={{ key: '1y', from: '', to: '' }}
      onChange={onChange}
      firstDate="2024-05-01"
    />,
  )

  const chips = screen.getByRole('radiogroup', { name: 'Grafik aralığı' })
  expect(chips.closest('.max-sm\\:hidden')).not.toBeNull()
  const select = screen.getByRole('combobox', { name: 'Grafik aralığı' })
  expect(select.closest('.sm\\:hidden')).not.toBeNull()
  expect(select).toHaveTextContent('1Y')

  fireEvent.click(select)
  const options = (await screen.findAllByRole('option')).map((o) => o.textContent)
  expect(options).toEqual(screen.getAllByRole('radio').map((r) => r.textContent))
  fireEvent.click(select)
  await chooseOption(select, '2024')
  expect(onChange).toHaveBeenLastCalledWith({ key: '2024', from: '2024-01-01', to: '2024-12-31' })
})

it('shows a clear button on Özel when given onClear, and only there', () => {
  const onClear = vi.fn()
  const custom = { key: 'custom', from: '2026-08-01', to: '2026-09-01' }
  const { rerender } = render(<RangePicker value={custom} onChange={vi.fn()} onClear={onClear} />)
  fireEvent.click(screen.getByRole('button', { name: 'Özel aralığı temizle' }))
  expect(onClear).toHaveBeenCalledTimes(1)

  rerender(
    <RangePicker value={{ key: '1y', from: '', to: '' }} onChange={vi.fn()} onClear={onClear} />,
  )
  expect(screen.queryByRole('button', { name: 'Özel aralığı temizle' })).toBeNull()

  rerender(<RangePicker value={custom} onChange={vi.fn()} />)
  expect(screen.queryByRole('button', { name: 'Özel aralığı temizle' })).toBeNull()
})
