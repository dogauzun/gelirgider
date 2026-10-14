import { useRef, useState, type DragEvent, type ReactNode } from 'react'
import { t, type StringKey } from '../../i18n/strings'
import { Notice } from '../ui/Notice'

// Whether `file` matches an <input accept> list: ".csv" by name, "text/csv"
// or "image/*" by type. The file dialog filters by the same list; a drop
// does not, so this does.
function accepts(accept: string, file: File): boolean {
  const name = file.name.toLowerCase()
  return accept.split(',').some((raw) => {
    const token = raw.trim().toLowerCase()
    if (token.startsWith('.')) return name.endsWith(token)
    if (token.endsWith('/*')) return file.type.startsWith(token.slice(0, -1))
    return file.type === token
  })
}

// A file the zone did not pass on, with the message that names it.
export type Refused = { file: File; reason: string }

// The upload of step 1 (GLG-965): a button-like region that opens the file
// dialog on click, Enter and Space and takes dropped files. A folder, an
// empty file and a file `accept` refuses are named in a message and never
// passed on as files; next to a good file they go to `onFiles` as `refused`.
// While `disabled` (a preview is running) a drop is refused with
// a message rather than lost.
export function DropZone({
  accept,
  multiple,
  disabled,
  onFiles,
  hint,
}: {
  accept: string
  multiple: boolean
  disabled: boolean
  onFiles: (files: File[], refused: Refused[]) => void
  hint?: ReactNode
}) {
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)
  const [messages, setMessages] = useState<string[]>([])

  function take(files: File[], folders: Set<File> = new Set()) {
    const named = (list: File[]) => list.map((file) => file.name).join(', ')
    const folder = files.filter((file) => folders.has(file))
    const empty = files.filter((file) => !folders.has(file) && file.size === 0)
    const refused = files.filter(
      (file) => !folders.has(file) && file.size > 0 && !accepts(accept, file),
    )
    // A CSV under a PDF choice ("Otomatik (PDF)" included) needs the CSV
    // choice, which no broker the user picked explains (GLG-1064).
    const csv = refused.filter((file) => accepts('.csv,text/csv', file))
    const other = refused.filter((file) => !csv.includes(file))
    const good = files.filter(
      (file) => !folders.has(file) && file.size > 0 && accepts(accept, file),
    )
    const groups: [File[], StringKey][] = [
      [folder, 'import.drop.folder'],
      [other, 'import.drop.refused'],
      [csv, 'import.drop.csvRefused'],
      [empty, 'import.drop.empty'],
    ]
    setMessages(
      groups
        .filter(([list]) => list.length > 0)
        .map(([list, key]) => t(key, { names: named(list) })),
    )
    // The page names the refused files once a good one has replaced this
    // step: in the batch's queue or in the review (GLG-1064).
    const reasons = groups.flatMap(([list, key]) =>
      list.map((file) => ({ file, reason: t(key, { names: file.name }) })),
    )
    if (good.length > 0) onFiles(multiple ? good : good.slice(0, 1), reasons)
  }

  function open() {
    if (!disabled) input.current?.click()
  }

  function handleDrop(event: DragEvent) {
    event.preventDefault()
    setOver(false)
    if (disabled) {
      setMessages([t('import.drop.busy')])
      return
    }
    // A dropped folder arrives as a File; only its entry tells it apart, so
    // the files are read from the items when there are any.
    const items = Array.from(event.dataTransfer.items ?? []).filter((item) => item.kind === 'file')
    if (items.length === 0) return take(Array.from(event.dataTransfer.files))
    const files: File[] = []
    const folders = new Set<File>()
    for (const item of items) {
      const file = item.getAsFile()
      if (!file) continue
      files.push(file)
      if (item.webkitGetAsEntry?.()?.isDirectory) folders.add(file)
    }
    take(files, folders)
  }

  return (
    <div>
      <input
        ref={input}
        type="file"
        accept={accept}
        multiple={multiple}
        className="hidden"
        onChange={(event) => {
          take(Array.from(event.target.files ?? []))
          // Lets the same file be chosen again after a Vazgeç.
          event.target.value = ''
        }}
      />
      <div
        role="button"
        tabIndex={0}
        // eslint-disable-next-line no-restricted-syntax -- a div, not a Button
        aria-disabled={disabled}
        data-over={over || undefined}
        onClick={open}
        onKeyDown={(event) => {
          if (event.key !== 'Enter' && event.key !== ' ') return
          event.preventDefault()
          open()
        }}
        onDragEnter={(event) => {
          event.preventDefault()
          if (!disabled) setOver(true)
        }}
        onDragOver={(event) => {
          event.preventDefault()
          if (!disabled) setOver(true)
        }}
        onDragLeave={(event) => {
          if (event.currentTarget.contains(event.relatedTarget as Node | null)) return
          setOver(false)
        }}
        onDrop={handleDrop}
        // The dashed edge is `faint`, the 3:1 a control's boundary needs
        // (WCAG 1.4.11, GLG-1083); `line-strong` drew it at 1.5:1.
        className="cursor-pointer rounded-card border-[1.5px] border-dashed border-faint bg-well/40 px-4 py-6 text-center text-[13px] text-muted outline-accent hover:bg-raised-hover focus-visible:outline-2 focus-visible:outline-offset-2 aria-disabled:cursor-default aria-disabled:opacity-60 aria-disabled:hover:bg-well/40 data-over:border-accent data-over:bg-accent-soft"
      >
        <span className="block font-semibold text-ink">{t('import.drop.title')}</span>
        {t('import.drop.or')}{' '}
        <span className="font-medium text-accent">{t('import.drop.choose')}</span>
        {hint && <> · {hint}</>}
      </div>
      {messages.length > 0 && (
        <div className="mt-3">
          <Notice role="alert">
            {messages.map((message) => (
              <p key={message}>{message}</p>
            ))}
          </Notice>
        </div>
      )}
    </div>
  )
}
