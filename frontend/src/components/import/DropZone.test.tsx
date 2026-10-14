import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { DropZone } from './DropZone'

const csv = (name: string, body = 'date,type\n') => new File([body], name, { type: 'text/csv' })
const pdf = (name: string) => new File(['%PDF'], name, { type: 'application/pdf' })

function setup(props: Partial<Parameters<typeof DropZone>[0]> = {}) {
  const onFiles = vi.fn()
  render(<DropZone accept=".csv,text/csv" multiple disabled={false} onFiles={onFiles} {...props} />)
  const zone = screen.getByRole('button', { name: /Dosyaları buraya bırak/ })
  // The input sits outside the accessibility tree; the zone opens it.
  const input = document.querySelector<HTMLInputElement>('input[type="file"]')!
  const click = vi.spyOn(input, 'click')
  return { onFiles, zone, input, click }
}

// jsdom has no DataTransfer; the handlers read `files` and, for a folder,
// `items[i].webkitGetAsEntry()`.
function drop(target: HTMLElement, files: File[], folders: string[] = []) {
  const items = files.map((file) => ({
    kind: 'file',
    getAsFile: () => file,
    webkitGetAsEntry: () => ({ isDirectory: folders.includes(file.name) }),
  }))
  fireEvent.drop(target, { dataTransfer: { files, items, types: ['Files'] } })
}

it('opens the file dialog on click, Enter and Space', () => {
  const { zone, click } = setup()
  fireEvent.click(zone)
  fireEvent.keyDown(zone, { key: 'Enter' })
  fireEvent.keyDown(zone, { key: ' ' })
  expect(click).toHaveBeenCalledTimes(3)
  expect(zone).toHaveAttribute('tabindex', '0')
})

it('passes chosen files on and lets the same file be chosen again', () => {
  const { onFiles, input } = setup()
  const file = csv('a.csv')
  fireEvent.change(input, { target: { files: [file] } })
  expect(onFiles).toHaveBeenCalledWith([file], [])
  expect(input.value).toBe('')
})

it('passes dropped files on', () => {
  const { onFiles, zone } = setup()
  const files = [csv('a.csv'), csv('b.CSV'), csv('c.csv')]
  drop(zone, files)
  expect(onFiles).toHaveBeenCalledWith(files, [])
})

it('marks the zone while files are dragged over it', () => {
  const { zone } = setup()
  fireEvent.dragEnter(zone, { dataTransfer: { types: ['Files'] } })
  expect(zone).toHaveAttribute('data-over')
  fireEvent.dragLeave(zone, { relatedTarget: document.body })
  expect(zone).not.toHaveAttribute('data-over')
  fireEvent.dragOver(zone, { dataTransfer: { types: ['Files'] } })
  drop(zone, [csv('a.csv')])
  expect(zone).not.toHaveAttribute('data-over')
})

it('reports a file of a type the broker refuses and passes the rest on', () => {
  const { onFiles, zone } = setup()
  const good = csv('a.csv')
  const refused = pdf('ekstre.pdf')
  drop(zone, [good, refused])
  // The page names it once this step is gone.
  expect(onFiles).toHaveBeenCalledWith(
    [good],
    [{ file: refused, reason: 'Seçilen aracı kurum bu dosya türünü almıyor: ekstre.pdf' }],
  )
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Seçilen aracı kurum bu dosya türünü almıyor: ekstre.pdf',
  )
})

// A CSV under "Otomatik (PDF)" names the CSV choice, not a broker the user
// never chose.
it('takes a PDF by its type for a PDF broker and points a CSV to the CSV choice', () => {
  const { onFiles, zone } = setup({ accept: 'application/pdf' })
  const file = pdf('ekstre.pdf')
  const sheet = csv('a.csv')
  drop(zone, [file, sheet])
  const reason = 'CSV dosyası için aracı kurum seçiminde CSV seçilmeli: a.csv'
  expect(onFiles).toHaveBeenCalledWith([file], [{ file: sheet, reason }])
  expect(screen.getByRole('alert')).toHaveTextContent(reason)
})

it('reports a folder and an empty file without passing them on', () => {
  const { onFiles, zone } = setup()
  drop(zone, [csv('ekstreler'), csv('bos.csv', '')], ['ekstreler'])
  expect(onFiles).not.toHaveBeenCalled()
  const alert = screen.getByRole('alert')
  expect(alert).toHaveTextContent('Klasör bırakılamaz, içindeki dosyaları seç: ekstreler')
  expect(alert).toHaveTextContent('Dosya boş: bos.csv')
})

it('clears the message on the next good choice', () => {
  const { zone } = setup()
  drop(zone, [pdf('x.pdf')])
  expect(screen.getByRole('alert')).toBeInTheDocument()
  drop(zone, [csv('a.csv')])
  expect(screen.queryByRole('alert')).not.toBeInTheDocument()
})

it('takes only the first file when it takes one', () => {
  const { onFiles, zone } = setup({ multiple: false })
  const first = csv('a.csv')
  drop(zone, [first, csv('b.csv')])
  expect(onFiles).toHaveBeenCalledWith([first], [])
})

it('ignores click, keys and drops while disabled and says why on a drop', () => {
  const { onFiles, zone, click } = setup({ disabled: true })
  expect(zone).toHaveAttribute('aria-disabled', 'true')
  fireEvent.click(zone)
  fireEvent.keyDown(zone, { key: 'Enter' })
  drop(zone, [csv('a.csv')])
  expect(click).not.toHaveBeenCalled()
  expect(onFiles).not.toHaveBeenCalled()
  expect(screen.getByRole('alert')).toHaveTextContent('Bir ekstre okunuyor')
})
