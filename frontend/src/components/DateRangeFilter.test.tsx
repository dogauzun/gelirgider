import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { DateRangeFilter } from './DateRangeFilter'

describe('DateRangeFilter', () => {
  it('names the range on its button and sets and clears the dates in its popover', async () => {
    const onChange = vi.fn()
    const onClear = vi.fn()
    const { rerender } = render(<DateRangeFilter onChange={onChange} onClear={onClear} />)

    // Closed, the inputs are not on the page; no range, nothing to clear.
    expect(screen.queryByLabelText('Başlangıç')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Tarih: Tümü' }))
    // The popup is a dialog with a name.
    expect(await screen.findByRole('dialog', { name: 'Tarih' })).toBeTruthy()
    fireEvent.change(await screen.findByLabelText('Başlangıç'), {
      target: { value: '2025-01-01' },
    })
    expect(onChange).toHaveBeenLastCalledWith('from', '2025-01-01')
    fireEvent.change(screen.getByLabelText('Bitiş'), { target: { value: '2025-03-31' } })
    expect(onChange).toHaveBeenLastCalledWith('to', '2025-03-31')
    expect(screen.queryByRole('button', { name: 'Tarih filtresini kaldır' })).toBeNull()

    // A past year's dates carry the year, this year's would not: the test
    // keeps to a year that is never the current one again.
    rerender(
      <DateRangeFilter from="2025-01-01" to="2025-03-31" onChange={onChange} onClear={onClear} />,
    )
    expect(screen.getByRole('button', { name: 'Tarih: 01 Oca 2025 – 31 Mar 2025' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Tarih filtresini kaldır' }))
    expect(onClear).toHaveBeenCalledOnce()
    // A new range starts at the start date.
    expect(document.activeElement).toBe(screen.getByLabelText('Başlangıç'))

    // An open end reads as one date and a dash.
    rerender(<DateRangeFilter to="2025-03-31" onChange={onChange} onClear={onClear} />)
    expect(screen.getByRole('button', { name: 'Tarih: – 31 Mar 2025' })).toBeTruthy()
  })
})
