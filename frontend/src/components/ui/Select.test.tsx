import { fireEvent, render, screen, within } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { chooseOption } from '../../test/select'
import { Field } from './Field'
import { Select } from './Select'

const options = [
  { value: 'TRY', label: 'Türk lirası' },
  { value: 'USD', label: 'ABD doları' },
] as const

it('shows the selected label and reports the chosen value', async () => {
  const onChange = vi.fn()
  render(<Select aria-label="Para birimi" value="TRY" onChange={onChange} options={options} />)
  const trigger = screen.getByRole('combobox', { name: 'Para birimi' })
  expect(trigger).toHaveTextContent('Türk lirası')
  await chooseOption(trigger, 'ABD doları')
  expect(onChange).toHaveBeenCalledWith('USD')
})

it('takes its name from a Field label', () => {
  render(
    <Field label="Para birimi">
      <Select value="TRY" onChange={() => {}} options={options} />
    </Field>,
  )
  expect(screen.getByRole('combobox', { name: 'Para birimi' })).toBeInTheDocument()
})

it('opens its list inside the dialog that contains it', async () => {
  render(
    <dialog open>
      <Select aria-label="Para birimi" value="TRY" onChange={() => {}} options={options} />
    </dialog>,
  )
  fireEvent.click(screen.getByRole('combobox'))
  const list = await screen.findByRole('listbox')
  expect(list.closest('dialog')).not.toBeNull()
})

it('lists grouped options under their label', async () => {
  render(
    <Select
      aria-label="Karşılaştır"
      value=""
      onChange={() => {}}
      options={[
        { value: '', label: 'Yok' },
        { value: 'instrument:7', label: 'GJH', group: 'Pozisyonlar' },
      ]}
    />,
  )
  fireEvent.click(screen.getByRole('combobox'))
  const group = within(await screen.findByRole('listbox')).getByRole('group', {
    name: 'Pozisyonlar',
  })
  expect(
    within(group)
      .getAllByRole('option')
      .map((o) => o.textContent),
  ).toEqual(['GJH'])
})

it('reports nothing when the current option is picked again, as a native select', async () => {
  const onChange = vi.fn()
  render(<Select aria-label="Para birimi" value="TRY" onChange={onChange} options={options} />)
  await chooseOption(screen.getByRole('combobox'), 'Türk lirası')
  expect(onChange).not.toHaveBeenCalled()
})
