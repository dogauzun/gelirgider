import { useCallback, useRef, useState } from 'react'
import { ConfirmDialog, type ConfirmOptions } from './ConfirmDialog'

// Turns the dialog into `await confirm(...)` so a call site stays a straight
// line: `if (!(await confirm({ ... }))) return`.
export function useConfirm(): {
  confirm: (options: ConfirmOptions) => Promise<boolean>
  dialog: React.ReactElement
} {
  const [options, setOptions] = useState<ConfirmOptions | null>(null)
  const resolveRef = useRef<((confirmed: boolean) => void) | null>(null)

  const confirm = useCallback((next: ConfirmOptions) => {
    // A second confirm replaces an open one (the leave guard on Back): end
    // the first as cancelled so its caller's await returns.
    resolveRef.current?.(false)
    setOptions(next)
    return new Promise<boolean>((resolve) => {
      resolveRef.current = resolve
    })
  }, [])

  const settle = useCallback((confirmed: boolean) => {
    setOptions(null)
    resolveRef.current?.(confirmed)
    resolveRef.current = null
  }, [])

  const dialog = (
    <ConfirmDialog
      open={options !== null}
      title={options?.title ?? ''}
      body={options?.body}
      confirmLabel={options?.confirmLabel ?? ''}
      danger={options?.danger}
      onConfirm={() => settle(true)}
      onCancel={() => settle(false)}
    />
  )

  return { confirm, dialog }
}
