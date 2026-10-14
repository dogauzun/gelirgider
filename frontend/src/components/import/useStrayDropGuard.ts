import { useEffect } from 'react'

// A file dropped outside the zone would open in the tab and leave the app,
// losing a preview under review. The import page calls this for as long as
// it is mounted, so such a drop does nothing in every step, also while the
// zone is gone (GLG-1052).
export function useStrayDropGuard() {
  useEffect(() => {
    const block = (event: globalThis.DragEvent) => {
      if (event.dataTransfer?.types.includes('Files')) event.preventDefault()
    }
    window.addEventListener('dragover', block)
    window.addEventListener('drop', block)
    return () => {
      window.removeEventListener('dragover', block)
      window.removeEventListener('drop', block)
    }
  }, [])
}
