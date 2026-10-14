import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { Menu, MenuItem } from './Menu'

// The account menu in the drawer. A popup portaled to <body> would
// sit under the modal dialog, inert.
it('opens its popup inside the dialog that contains it', async () => {
  render(
    <dialog open>
      <Menu trigger={<button type="button">Hesap menüsü</button>}>
        <MenuItem onSelect={() => {}}>Çıkış yap</MenuItem>
      </Menu>
    </dialog>,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Hesap menüsü' }))
  const menu = await screen.findByRole('menu')
  expect(menu.closest('dialog')).not.toBeNull()
})
