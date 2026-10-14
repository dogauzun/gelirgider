import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeAll, expect, test } from 'vitest'
import { useConfirm } from './useConfirm'
import { t } from '../../i18n/strings'

// jsdom implements neither method on <dialog>; both only flip the open state.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})

function Harness({ onResult }: { onResult: (confirmed: boolean) => void }) {
  const { confirm, dialog } = useConfirm()
  return (
    <>
      <button
        onClick={async () => {
          onResult(await confirm({ title: 'Portfolio', confirmLabel: 'Delete', danger: true }))
        }}
      >
        ask
      </button>
      {dialog}
    </>
  )
}

test('useConfirm resolves true on confirm and false on cancel', async () => {
  const results: boolean[] = []
  render(<Harness onResult={(confirmed) => results.push(confirmed)} />)

  fireEvent.click(screen.getByText('ask'))
  fireEvent.click(screen.getByText('Delete'))
  await waitFor(() => expect(results).toEqual([true]))

  fireEvent.click(screen.getByText('ask'))
  fireEvent.click(screen.getByText(t('confirm.cancel')))
  await waitFor(() => expect(results).toEqual([true, false]))
})

// A second confirm while one is open (the leave guard on Back) must
// end the first one as cancelled, not leave its await hanging.
test('useConfirm resolves an open confirm to false when a second one opens', async () => {
  const results: string[] = []
  function Twice() {
    const { confirm, dialog } = useConfirm()
    const ask = async (label: string) => {
      results.push(`${label}:${await confirm({ title: label, confirmLabel: `${label} ok` })}`)
    }
    return (
      <>
        <button onClick={() => ask('first')}>first</button>
        <button onClick={() => ask('second')}>second</button>
        {dialog}
      </>
    )
  }
  render(<Twice />)

  fireEvent.click(screen.getByText('first'))
  fireEvent.click(screen.getByText('second'))
  await waitFor(() => expect(results).toEqual(['first:false']))

  fireEvent.click(screen.getByText('second ok'))
  await waitFor(() => expect(results).toEqual(['first:false', 'second:true']))
})

// The confirm closes on a double click's first click, so its second click
// (detail 2) lands on whatever sat under the button: on İşlemler that is a
// row that opens its edit dialog. That click must do nothing; a
// fresh single click on the row still does.
test.each(['Delete', t('confirm.cancel')])(
  'the second click of a double click on %s does not reach the page under it',
  (label) => {
    const rowClicks: number[] = []
    function OverRow() {
      const { confirm, dialog } = useConfirm()
      return (
        <>
          <button onClick={() => confirm({ title: 'Transaction', confirmLabel: 'Delete' })}>
            ask
          </button>
          <div data-testid="row" onClick={(event) => rowClicks.push(event.detail)} />
          {dialog}
        </>
      )
    }
    render(<OverRow />)

    fireEvent.click(screen.getByText('ask'))
    fireEvent.click(screen.getByText(label), { detail: 1 })
    fireEvent.click(screen.getByTestId('row'), { detail: 2 })
    expect(rowClicks).toEqual([])

    fireEvent.click(screen.getByTestId('row'), { detail: 1 })
    expect(rowClicks).toEqual([1])
  },
)
