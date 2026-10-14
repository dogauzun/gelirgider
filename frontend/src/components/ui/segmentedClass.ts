// The segmented control's classes, shared with the aria-pressed toggle groups
// (LanguageSwitch, AccountPage's LanguageRow), which must look like it.

// The sunken track of a role="radiogroup".
export const segmentedTrack = 'flex rounded-ctl bg-well p-0.5'

// One role="radio" button; the checked one is raised.
export function segmentedItem(active: boolean): string {
  return `rounded-[6px] px-2.5 py-1 text-[12px] font-medium ${
    active ? 'bg-raised text-ink shadow-ctl' : 'text-muted hover:text-ink'
  }`
}
