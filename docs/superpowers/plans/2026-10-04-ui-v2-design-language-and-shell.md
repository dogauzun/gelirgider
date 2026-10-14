# UI v2: Design Language and App Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give gelirgider the approved "soft depth" look (Inter, tinted ground, floating white cards, blue accent, light and dark) and a sidebar shell that lists the portfolios, without changing any page layout.

**Architecture:** Token-first, in place. Task 2 changes token values in `frontend/src/index.css`, which flips color and type app-wide because every component already uses semantic tokens. Tasks 3 to 7 change the shared primitives. Tasks 8 and 9 replace `Header.tsx` with a layout route (`AppShell`) that renders a `Sidebar`. `main` works after every task.

**Tech Stack:** React 19, TypeScript, Vite, Tailwind v4 (`@theme`, `@utility`), TanStack Query, react-router 8, Vitest + Testing Library (`fireEvent`; `user-event` is not installed). New dependency: `@base-ui/react` (Task 6).

**Spec:** [docs/superpowers/specs/2026-10-04-ui-v2-design-language-and-shell-design.md](../specs/2026-10-04-ui-v2-design-language-and-shell-design.md). Visual reference: [2026-10-04-ui-v2-mockup.html](../specs/2026-10-04-ui-v2-mockup.html). Read both before any task. The spec holds the token tables; this plan does not repeat them.

## Global Constraints

- Frontend and docs only. No backend, API or schema change.
- Token names do not change. Values come from the spec's tables. Where a value fails the contrast test, the test wins and the value is adjusted.
- The two dark blocks in `index.css` (`[data-theme='dark']` and the `prefers-color-scheme` copy) stay identical.
- Components use semantic tokens only: no raw palette class, no `dark:` variant, and, from the task noted, no `font-mono` (Task 2), no raw `shadow-sm|md|lg|xl|2xl` (Task 3), no native `<select>` (Task 10).
- Base text size stays 13 px. No page layout changes; no chart changes.
- Dialogs and the drawer use the native `<dialog>` through `components/ui/useModal.ts`.
- The only new dependency is `@base-ui/react`. Nothing else is installed.
- Every new UI string gets a `tr` and an `en` key in `frontend/src/i18n/strings.ts`. Insert keys mid-file next to related keys, not at the end.
- Code, comments, commits, tickets and PRs in English. No AI attribution anywhere.
- Checks for every PR, from the repo root, all green: `make lint`, `make test`, `make build-frontend`.
- Non-trivial logic ships with one test. Thin wrappers and class-string changes ship with none.

## Process (applies to every task)

Project rules from `CLAUDE.md` override the generic flow:

1. **Before Task 1**, the orchestrator creates the Jira epic "Phase 24 - UI v2: design language and shell" (label `ui`) and one GLG ticket per task below, with the merge order in the epic description. If Phase 24 is taken, use the next free number. Where this plan says `GLG-<n>`, use that task's ticket key.
2. One ticket, one subagent (`model: "opus"`), one PR. Branch `GLG-<n>-short-slug` from current `main`.
3. Transition the ticket to `In Progress` at start, `In Review` when the PR opens, `Done` after merge.
4. PR body uses the format in `CLAUDE.md` (Description, Ticket link, Testing with the commands run and what was observed).
5. The merger runs the three checks on the PR head, spawns `pr-reviewer`, addresses and resolves every thread, then squash-merges.
6. Tasks that say "browser check" attach screenshots to the PR: light and dark, at 1280 px and 375 px unless the task narrows it. Use a fresh dev DB seeded through the API; never the owner's real data.

**Merge order:** 1 → 2 → 3 (alone, no other frontend PR open) → 4, 5, 6 in parallel → 7 → 8 → 9 and 10 in parallel.

## File Structure

| File                                                 | Responsibility                                                 | Task       |
| ---------------------------------------------------- | -------------------------------------------------------------- | ---------- |
| `docs/adr/0023-ui-design-system-v2.md`               | The decisions of the spec as an ADR                            | 1          |
| `frontend/src/index.css`                             | Tokens: color, shadow, radius, font; the `card` utility        | 2, 3       |
| `frontend/src/index.css.test.ts`                     | Contrast and token-structure tests                             | 2          |
| `frontend/src/conventions.test.ts`                   | Scans `src/**/*.tsx` for banned class patterns                 | 2, 3, 10   |
| `frontend/public/fonts/`                             | Inter subset files and license                                 | 2          |
| `frontend/src/components/ui/Button.tsx`, `Badge.tsx` | Restyled primitives                                            | 4          |
| `frontend/src/components/ui/segmented.ts`            | Shared class strings for segmented controls                    | 5          |
| `frontend/src/components/ui/Menu.tsx`                | Styled wrapper over Base UI `Menu`                             | 6          |
| `frontend/src/components/ui/Select.tsx`              | Styled wrapper over Base UI `Select`                           | 7          |
| `frontend/src/test/select.ts`                        | Test helper to pick an option in the new `Select`              | 7          |
| `frontend/src/components/AppShell.tsx`               | Layout route: skip link, sidebar, top bar, drawer, `<Outlet/>` | 8, 9       |
| `frontend/src/components/Sidebar.tsx`                | Portfolio navigation                                           | 8          |
| `frontend/src/components/AccountMenu.tsx`            | Account menu and the logout mutation                           | 8          |
| `frontend/src/components/CreatePortfolioDialog.tsx`  | Create dialog, extracted from `HomePage.tsx`                   | 8          |
| `frontend/src/components/PortfolioHeader.tsx`        | Shrinks to page header: title, tabs, add button, menu          | 5, 6, 8, 9 |
| `frontend/src/components/Header.tsx`                 | Deleted                                                        | 8          |

## Review Focus

Conditions the spec implies that are most likely to bite a user. Each is pinned to a step in the task that owns the code.

1. **A `Select` inside the transaction dialog.** The dialog is in the top layer; a popup portaled to `<body>` is hidden and inert. Expected: the list opens above the dialog and works with mouse and keyboard. Pinned in Task 7 (test and browser check).
2. **Sidebar totals after a transaction or a manual price.** The sidebar never remounts, so nothing refetches `['portfolios']`. Expected: the row value changes without a reload. Pinned in Task 8 (test).
3. **Negative, zero and privacy-masked totals in compact form.** Overdraft is allowed, so a total can be negative. Expected: `-₺35,2 B`, `₺0`, and `•••` in privacy mode. Pinned in Task 8 (tests).
4. **A 100-character portfolio name and 15 portfolios.** Expected: the name truncates, the list scrolls, and the admin link, privacy toggle and account menu stay reachable at 1280×700. Pinned in Task 8 (browser check).
5. **`Esc` with a `Select` or `Menu` open inside a dialog.** Expected: it closes the popup only; the dialog and its form input stay. Pinned in Task 7 (browser check).

---

### Task 1: Docs — ADR 0023 and every conflicting document

**Files:**

- Create: `docs/adr/0023-ui-design-system-v2.md`
- Modify: `docs/adr/0005-ui-design-system.md`, `docs/adr/README.md`, `docs/adr/0021-product-and-ui-decisions.md` (lines 102 and 216), `CLAUDE.md` (line 21), `frontend/README.md` (line 21), `docs/ROADMAP.md`
- Carry: `docs/superpowers/specs/2026-10-04-ui-v2-*` and this plan (cherry-pick from branch `claude/ui-ux-improvements-200acc`), so they reach `main` with this PR

**Interfaces:**

- Produces: ADR 0023, which every later task and review cites.

- [ ] **Step 1: Write ADR 0023.** Same section layout as ADR 0005: status line (`accepted (GLG-<n>, 2026-10-04)`), Decision (color, depth, shape, type, primitives, shell, with the spec's tables), "Rules that survive from ADR 0005" (the list in the spec's "Docs that must change" section), Alternatives considered (visual languages A Linear-like and C Vercel-like; shells 1 and 3; accents violet, teal, ink; migration by parallel `/v2` tree; Radix instead of Base UI), Dependencies (`@base-ui/react` now, Recharts in the charts sub-project, and why they are the exception to PRINCIPLES #1), Known risks (the spec's Risks), Consequences (the ten tickets). Link the mockup file as the visual reference.
- [ ] **Step 2: Add dated notes to ADR 0005.** Form: `> Superseded 2026-10-04 (GLG-<n>): <what>, see [ADR 0023](0023-ui-design-system-v2.md).` One note at each of: the status paragraph, the Decision paragraph, the color table, "Type", "Shape and control sizes", the "One app shell" paragraph, the Alternatives rows for Type, Components and Chart, and the "No new runtime dependency" consequence. Do not rewrite existing text.
- [ ] **Step 3: Update the index and ADR 0021.** `docs/adr/README.md`: new row for 0023; `0023` in the "Superseded in part by" column of the 0005 row. `0021`: a dated note after line 102 and after line 216 saying the Defter system and the chart-library rejection are superseded by ADR 0023.
- [ ] **Step 4: Update `CLAUDE.md` line 21 and `frontend/README.md` line 21.** `CLAUDE.md` frontend line becomes: "Frontend: React + TypeScript + Vite, TanStack Query, react-router, Tailwind, Base UI for menus and selects. Talks to backend over JSON REST at `/api/*`. Semantic color and shadow tokens in index.css, light and dark; no raw palette or shadow classes in components (ADR 0023, which supersedes parts of ADR 0005)." `frontend/README.md`: same rule, link to ADR 0023.
- [ ] **Step 5: Add the phase to `docs/ROADMAP.md`** under "In progress": the epic key, one paragraph, and the five sub-projects from the spec's Scope, marking 1 and 2 as this epic.
- [ ] **Step 6: Verify.** Run `make lint`. Expected: passes (it runs prettier on `docs/`). Run `grep -n "ADR 0005" CLAUDE.md frontend/README.md`. Expected: only the "supersedes parts of ADR 0005" mention.
- [ ] **Step 7: Commit, open the PR.**
- [ ] **Step 8 (orchestrator, after merge): cloud routine prompts.** Read the prompts of the hourly kanban worker and the daily QA routine. List every line that contradicts ADR 0023 (Defter, IBM Plex, "no library") and show the owner the proposed replacement text before changing a routine.

---

### Task 2: Tokens, font and the convention scanner

**Files:**

- Modify: `frontend/src/index.css`, `frontend/src/index.css.test.ts`, `frontend/index.html`, `frontend/public/manifest.webmanifest`, and the 15 `font-mono` call sites (`grep -rn font-mono frontend/src --include='*.tsx'`)
- Create: `frontend/src/conventions.test.ts`, `frontend/public/fonts/InterVariable-latin.woff2`, `InterVariable-latin-ext.woff2`, `OFL-Inter.txt`
- Delete: the four `IBMPlex*.woff2` files and `frontend/public/fonts/OFL.txt`

**Interfaces:**

- Produces: utilities `shadow-card`, `shadow-ctl`, `shadow-pop`; `rounded-card` = 12 px, `rounded-ctl` = 8 px; `font-sans` = Inter. `conventions.test.ts` exports nothing; later tasks append to its `BANNED` list.

- [ ] **Step 1: Write the failing token tests** in `index.css.test.ts`, next to the existing two:

```ts
const AA_PAIRS = [
  ['gain', 'surface'],
  ['gain', 'gain-soft'],
  ['loss', 'surface'],
  ['loss', 'loss-soft'],
  ['accent', 'surface'],
  ['accent-ink', 'accent-soft'],
  ['on-ink', 'accent'],
  ['warn', 'warn-soft'],
  ['plum', 'plum-soft'],
] as const

it.each(AA_PAIRS)('%s on %s meets WCAG AA in every theme', (fg, bg) => {
  const fgs = values(fg)
  const bgs = values(bg)
  expect(fgs).toHaveLength(3)
  fgs.forEach((f, i) => expect(contrast(f, bgs[i]), `#${i}`).toBeGreaterThanOrEqual(4.5))
})

it('defines each shadow token in all three theme blocks', () => {
  for (const name of ['card', 'ctl', 'pop'])
    expect(css.match(new RegExp(`--shadow-${name}:`, 'g'))).toHaveLength(3)
})

it('keeps the two dark blocks identical', () => {
  // Body of `[data-theme='dark'] { ... }` and of the block inside the
  // prefers-color-scheme media query, compared line by line after trim().
  expect(darkBlock('media')).toEqual(darkBlock('attribute'))
})

it('has no IBM Plex and no mono font', () => {
  expect(css).not.toMatch(/Plex|--font-mono/)
})
```

`darkBlock(kind: 'attribute' | 'media'): string[]` is a helper in the test file that returns the trimmed, non-empty declaration lines of that block.

- [ ] **Step 2: Write the failing convention scanner** `frontend/src/conventions.test.ts`:

```ts
/// <reference types="node" />
import { readdirSync, readFileSync } from 'node:fs'
import { expect, it } from 'vitest'

// Class patterns ADR 0023 bans from components. A task that removes the last
// use of a pattern adds it here, so it cannot come back.
const BANNED: [pattern: RegExp, why: string][] = [
  [/\bfont-mono\b/, 'IBM Plex Mono is gone; symbols are Inter semibold'],
]

const dir = import.meta.dirname
const files = (readdirSync(dir, { recursive: true }) as string[]).filter(
  (f) => f.endsWith('.tsx') && !f.endsWith('.test.tsx'),
)

it.each(BANNED)('no component uses %s (%s)', (pattern) => {
  const hits = files.filter((f) => pattern.test(readFileSync(`${dir}/${f}`, 'utf8')))
  expect(hits).toEqual([])
})
```

- [ ] **Step 3: Run both.** `cd frontend && npx vitest run src/index.css.test.ts src/conventions.test.ts`. Expected: the new tests fail (no shadow tokens, Plex present, 15 `font-mono` files).
- [ ] **Step 4: Fetch the Inter files without adding a dependency.** In the session scratchpad: `npm pack @fontsource-variable/inter`, unpack, copy `files/inter-latin-wght-normal.woff2` and `files/inter-latin-ext-wght-normal.woff2` to `frontend/public/fonts/` under the names above, and its `LICENSE` to `OFL-Inter.txt`. Copy the two `unicode-range` values from the package's `index.css`. `package.json` must not change.
- [ ] **Step 5: Edit `index.css`.** Replace every color value in the three blocks with the spec's table. Add the three `--shadow-*` tokens to `@theme` (light) and to both dark blocks (dark recipe). Set `--radius-card: 12px`, `--radius-ctl: 8px`, `--font-sans: 'Inter', system-ui, sans-serif`; remove `--font-mono`. Replace the four Plex `@font-face` rules with two Inter rules: `font-weight: 100 900`, `font-display: swap`, each with its `unicode-range`.
- [ ] **Step 6: Edit the files outside `index.css`.** `index.html`: preload `/fonts/InterVariable-latin.woff2`; `theme-color` `#ffffff` (light) and `#161a22` (dark). `manifest.webmanifest`: `background_color` `#f6f8fb`. Remove `font-mono` from the 15 call sites; where it marked an instrument symbol, use `font-semibold` instead. `Badge.tsx` just loses `font-mono` here (Task 4 restyles it).
- [ ] **Step 7: Run the tests.** Same command as Step 3. Expected: pass. If a contrast pair fails, darken (light) or lighten (dark) that token in all affected blocks until it passes, and note the final value in the PR.
- [ ] **Step 8: Browser check.** Özet and İşlemler, light and dark, 1280 and 375. In the console: `document.fonts.check('600 13px Inter', '₺ğşıİ')` must be `true`, and the lira sign must not look heavier or lighter than the digits next to it. Record the byte size of the two font files in the PR and in ADR 0023's Type section.
- [ ] **Step 9: Run `make lint`, `make test`, `make build-frontend`; commit; open the PR.**

---

### Task 3: The `card` utility and the shadow tokens everywhere

**Files:**

- Modify: `frontend/src/index.css`, `frontend/src/conventions.test.ts`, every `.tsx` that matches the patterns below (about 25 files)

**Interfaces:**

- Consumes: `shadow-card`, `shadow-ctl`, `shadow-pop` (Task 2).
- Produces: the class `card`.

- [ ] **Step 1: Add the failing bans** to `BANNED` in `conventions.test.ts`:

```ts
[/rounded-card border border-line bg-surface/, 'use the `card` utility'],
[/\bshadow-(sm|md|lg|xl|2xl)\b/, 'use shadow-card, shadow-ctl or shadow-pop'],
```

Run `npx vitest run src/conventions.test.ts`. Expected: both fail and list the files.

- [ ] **Step 2: Add the utility** to `index.css`:

```css
/* @apply, not raw declarations: shadow-card has to go through Tailwind's
   --tw-shadow so a ring-* on the same element composes with it. */
@utility card {
  @apply rounded-card bg-surface shadow-card;
}
```

- [ ] **Step 3: Replace, by these rules.**
  - `rounded-card border border-line bg-surface` → `card` (27 places; keep the other classes on the element).
  - Floating layers (menus, dialogs, toasts, tooltips, the search dropdown): `shadow-lg` and `shadow-2xl` → `shadow-pop`, and drop `border border-line` on that element, because `shadow-pop` carries its own ring.
  - `shadow-sm` → `shadow-ctl`.
  - `HomePage.tsx` card hover: keep `hover:ring-3 hover:ring-accent-soft`, drop `hover:border-accent-line`.
  - A `rounded-card` element that is not a card (a skeleton block, an inset panel with `bg-paper`) keeps its classes.
- [ ] **Step 4: Run all frontend tests.** `cd frontend && npm test`. Expected: pass; fix tests that asserted the old class strings.
- [ ] **Step 5: Browser check.** Portföyler, Özet, İşlemler (open the transaction dialog and a toast), light and dark, 1280 only. Cards float on the ground with no hairline border; menus and the dialog show `shadow-pop`.
- [ ] **Step 6: Run the three checks; commit; open the PR.** This PR merges alone.

---

### Task 4: Restyle Button and Badge

**Files:**

- Modify: `frontend/src/components/ui/Button.tsx`, `frontend/src/components/ui/Badge.tsx`

**Interfaces:**

- Produces: same props as today. Only class strings change.

- [ ] **Step 1: Set the `Button` variants:**

```ts
const variants = {
  primary: 'bg-accent text-on-ink shadow-ctl hover:bg-accent-ink',
  secondary: 'bg-surface text-ink shadow-ctl hover:bg-paper',
  ghost: 'text-ink-2 hover:bg-paper',
  danger: 'bg-loss text-on-ink shadow-ctl',
  'danger-quiet': 'bg-surface text-loss shadow-ctl hover:bg-loss-soft',
}
```

Keep the existing comments on `danger-quiet`.

- [ ] **Step 2: Set the `Badge` class** to `inline-flex h-[18px] items-center rounded-full bg-chip px-2 text-[11px] font-medium text-muted`.
- [ ] **Step 3: Leave `inputClass` in `Field.tsx` unchanged** (spec: inputs keep their border). `Pill`, `Notice`, `Toast`, `ConfirmDialog`, `Skeleton` need no edit beyond Task 3; confirm that in the browser check.
- [ ] **Step 4: Run `npm test`.** Expected: pass; update any test that asserted `bg-ink` on a primary button.
- [ ] **Step 5: Browser check.** A page with all button variants (İşlemler with a row selected for delete, the confirm dialog, the account page), light and dark, 1280. In dark, the primary button label must be readable on the lighter blue.
- [ ] **Step 6: Run the three checks; commit; open the PR.**

---

### Task 5: Segmented control and tabs

**Files:**

- Create: `frontend/src/components/ui/segmented.ts`
- Modify: `frontend/src/components/RangePicker.tsx` (frame and item classes), `frontend/src/components/Allocation.tsx` (view toggle), `frontend/src/components/PortfolioHeader.tsx` (`tabClass`)

**Interfaces:**

- Produces:

```ts
// The sunken track of a role="radiogroup".
export const segmentedTrack = 'flex rounded-ctl bg-chip p-0.5'
// One role="radio" button; the checked one is raised.
export function segmentedItem(active: boolean): string
// active: 'rounded-[6px] px-2.5 py-1 text-[12px] font-medium bg-surface text-ink shadow-ctl'
// else:   'rounded-[6px] px-2.5 py-1 text-[12px] font-medium text-muted hover:text-ink'
```

- [ ] **Step 1: Create `segmented.ts`** with the two exports above.
- [ ] **Step 2: Use them** in `RangePicker.tsx` and `Allocation.tsx`. The track replaces `rounded-lg border border-line bg-paper`; keep `max-w-full`, the scroller, `TAP_Y` and all ARIA and keyboard code as they are.
- [ ] **Step 3: Change `tabClass`** in `PortfolioHeader.tsx`: the active tab is `border-accent text-ink` (was `border-ink`).
- [ ] **Step 4: Run `npm test`.** Expected: `RangePicker.test.tsx`, `Allocation.test.tsx` and `PortfolioHeader.test.tsx` pass unchanged; they test roles, not classes.
- [ ] **Step 5: Browser check.** Özet range picker and allocation toggle, light and dark, 1280 and 375 (the range picker scrolls sideways on a phone and its touch targets still work).
- [ ] **Step 6: Run the three checks; commit; open the PR.**

---

### Task 6: Base UI and the `Menu` primitive

**Files:**

- Modify: `frontend/package.json`, `frontend/package-lock.json`, `frontend/src/components/PortfolioHeader.tsx`, `frontend/src/pages/HomePage.tsx`, their tests, `frontend/src/setupTests.ts` only if a jsdom API is missing
- Create: `frontend/src/components/ui/Menu.tsx`

**Interfaces:**

- Produces:

```tsx
import type { ReactElement, ReactNode } from 'react'

// `trigger` is the caller's own <Button aria-label=...>; it is passed to
// Base UI's Menu.Trigger through the `render` prop.
export function Menu(props: {
  trigger: ReactElement
  align?: 'start' | 'end' // default 'end'
  children: ReactNode
}): ReactElement
export function MenuItem(props: {
  onSelect: () => void
  tone?: 'default' | 'danger'
  disabled?: boolean
  children: ReactNode
}): ReactElement
// A navigation item: Base UI Menu.LinkItem rendering a react-router <Link>.
export function MenuLink(props: { to: string; children: ReactNode }): ReactElement
export function MenuSeparator(): ReactElement
```

- [ ] **Step 1: Install.** `cd frontend && npm install @base-ui/react`. Confirm in `package.json` that it is the only new entry and that its peer range accepts React 19.
- [ ] **Step 2: CSP check before writing the wrapper.** Build a throwaway page change that renders one Base UI menu, run `npm run build`, add `<meta http-equiv="Content-Security-Policy" content="default-src 'self'; script-src 'self'; object-src 'none'; base-uri 'self'">` to `dist/index.html` by hand, serve with `npx vite preview`, open the menu and read the console. Expected: no CSP violation and the popup is positioned under its trigger. **If there is a violation: stop. Do not edit `deploy/nginx.conf`. Report to the orchestrator; the owner decides.** Discard the throwaway change.
- [ ] **Step 3: Write `Menu.tsx`** on `import { Menu as Base } from '@base-ui/react/menu'` (`Root`, `Trigger`, `Portal`, `Positioner` with `sideOffset={4}` and `align`, `Popup`, `Item`, `LinkItem`, `Separator`). Classes: popup `min-w-44 rounded-card bg-surface p-1 shadow-pop outline-none`; item `flex w-full cursor-default items-center gap-2 rounded-ctl px-2 py-1.5 text-[13px] text-ink outline-none data-highlighted:bg-paper data-disabled:opacity-50`; `tone="danger"` swaps `text-ink` for `text-loss`; separator `my-1 h-px bg-line`.
- [ ] **Step 4: Migrate the two hand-rolled menus.** `PortfolioHeader.tsx` (rename, delete) and the card menu in `HomePage.tsx`. Delete their `menuOpen` state, click-away catcher, `Escape` handlers and `menuItemClass`. The trigger keeps its `aria-label`.
- [ ] **Step 5: Run the existing tests.** `npx vitest run src/components/PortfolioHeader.test.tsx src/pages/HomePage.test.tsx`. Update queries to the roles Base UI renders (`menu`, `menuitem`). The focus test "hands focus back to the menu button after a rename or a cancelled delete (GLG-441)" must still pass with its assertions unchanged; if Base UI's own focus return fights the inline rename input, fix the component, not the test. If jsdom lacks an API Base UI calls (`ResizeObserver`, `PointerEvent`, `scrollIntoView`), stub only that one in `setupTests.ts` with a comment naming the caller.
- [ ] **Step 6: Browser check.** Open both menus by mouse and by keyboard (Enter, arrows, Esc), light and dark, 1280 and 375.
- [ ] **Step 7: Run the three checks; commit; open the PR.** State the CSP result in the PR's Testing section.

---

### Task 7: The `Select` primitive and the transaction dialog

**Files:**

- Create: `frontend/src/components/ui/Select.tsx`, `frontend/src/components/ui/Select.test.tsx`, `frontend/src/test/select.ts`
- Modify: `frontend/src/components/TransactionDialog.tsx` (the currency select near line 696), `frontend/src/components/TransactionDialog.test.tsx`, `frontend/src/components/ui/Field.tsx` (one selector)

**Interfaces:**

- Consumes: `inputClass` from `ui/Field.tsx`; `@base-ui/react` (Task 6).
- Produces:

```tsx
export type SelectOption<T extends string> = { value: T; label: string; disabled?: boolean }

// Drop-in for a controlled native <select>. `id`, `aria-invalid` and
// `aria-describedby` go to the trigger, so it works as the child of <Field>.
export function Select<T extends string>(props: {
  value: T
  onChange: (value: T) => void
  options: readonly SelectOption<T>[]
  id?: string
  'aria-label'?: string
  'aria-invalid'?: boolean
  'aria-describedby'?: string
  disabled?: boolean
  className?: string // extra classes on the trigger, e.g. a width
}): ReactElement
```

```ts
// frontend/src/test/select.ts — for every test that used to change a <select>.
// Clicks the trigger, then the option with that visible name.
export async function chooseOption(trigger: HTMLElement, optionName: string): Promise<void>
```

- [ ] **Step 1: Write the failing tests** in `Select.test.tsx`:

```tsx
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
```

- [ ] **Step 2: Run them.** `npx vitest run src/components/ui/Select.test.tsx`. Expected: fail, module not found.
- [ ] **Step 3: Write `Select.tsx` and `chooseOption`.** On `import { Select as Base } from '@base-ui/react/select'`: `Root` (`value`, `onValueChange`, `items={options}`), `Trigger`, `Value`, `Icon` (the existing `chevronDown`), `Portal`, `Positioner` (`sideOffset={4}`, `alignItemWithTrigger={false}`: the list opens below the trigger like a dropdown), `Popup`, `List`, `Item`, `ItemText`, `ItemIndicator`. The portal's `container` is the trigger's closest `<dialog>`, or the default when there is none: a native modal dialog is in the top layer, and a popup outside it is hidden and inert. Classes: trigger `${inputClass} flex items-center justify-between gap-2 text-left`; popup `max-h-[var(--available-height)] min-w-[var(--anchor-width)] overflow-y-auto rounded-card bg-surface p-1 shadow-pop outline-none`; item as `MenuItem` in Task 6.
- [ ] **Step 4: Widen `Field.tsx`'s focus guard.** The selector `'input, select, textarea'` becomes `'input, select, textarea, [role="combobox"]'`, so a server error does not steal focus from a `Select` the user has moved to.
- [ ] **Step 5: Run the tests.** Expected: the three pass.
- [ ] **Step 6: Migrate the currency select in `TransactionDialog.tsx`:** `options={CURRENCIES.map((c) => ({ value: c, label: t(\`currency.${c}\`) }))}`, `onChange={(currency) => setForm({ ...form, currency })}`. Update `TransactionDialog.test.tsx`with`chooseOption`.
- [ ] **Step 7: Browser check (Review Focus 1 and 5).** Open İşlem ekle for a cash transaction, change the currency by mouse, then by keyboard (Tab to it, Enter, arrows, Enter). With the list open press `Esc`: the list closes, the dialog stays and keeps its typed values. Light and dark, 1280 and 375 (bottom sheet).
- [ ] **Step 8: Run the three checks; commit; open the PR.**

---

### Task 8: `AppShell`, `Sidebar`, and the page header

**Files:**

- Create: `frontend/src/components/AppShell.tsx`, `Sidebar.tsx`, `Sidebar.test.tsx`, `AccountMenu.tsx`, `AccountMenu.test.tsx`, `CreatePortfolioDialog.tsx`
- Create also: `frontend/src/api/transactions.test.ts`
- Modify: `frontend/src/App.tsx`, `App.test.tsx`, `components/PortfolioHeader.tsx`, `PortfolioHeader.test.tsx`, `pages/HomePage.tsx`, `lib/format.ts`, `lib/format.test.ts`, `api/portfolios.ts`, `api/transactions.ts`, `api/prices.ts`, `components/ui/Icon.tsx`
- Delete: `frontend/src/components/Header.tsx`, `Header.test.tsx`

**Interfaces:**

- Consumes: `Menu`, `MenuItem`, `MenuLink` (Task 6); `card` (Task 3); `usePortfolios`, `portfoliosQueryKey` from `api/portfolios.ts`; `useMe` from `api/auth.ts` (`me.is_admin`, `me.email`).
- Produces:

```ts
// api/portfolios.ts — HomePage's exact decimal sum, moved and exported.
export function sumPortfolios(
  portfolios: Portfolio[] | undefined,
  pick: (p: Portfolio) => string,
): string
// lib/format.ts — MASK in privacy mode, else
// formatNumber(value, { style: 'currency', currency, currencyDisplay: 'narrowSymbol',
//                       notation: 'compact', maximumFractionDigits: 2 })
export function formatCompactMoney(value: string | number, currency: string): string
```

```tsx
// components/CreatePortfolioDialog.tsx — HomePage's CreateDialog, moved unchanged.
export function CreatePortfolioDialog(props: {
  onClose: () => void
  onCreated: (id: number) => void
}): ReactElement
// components/AccountMenu.tsx — avatar initial + email as the trigger; items
// Hesap (/account) and Çıkış. Owns the logout mutation moved from Header.tsx,
// including flushToasts() before logout().
export function AccountMenu(): ReactElement
// components/Sidebar.tsx — `onNavigate` fires on every link click (Task 9's
// drawer closes on it).
export function Sidebar(props: { onNavigate?: () => void }): ReactElement
// components/AppShell.tsx — the layout route element.
export function AppShell(): ReactElement
```

- [ ] **Step 1: Write the failing format tests** in `lib/format.test.ts` (compare after replacing `\s` with a plain space; `Intl` may emit a no-break space):

```ts
it('formats compact money in tr', () => {
  expect(formatCompactMoney('1284650.40', 'TRY')).toBe('₺1,28 Mn')
  expect(formatCompactMoney('842000', 'TRY')).toBe('₺842 B')
  expect(formatCompactMoney('0', 'TRY')).toBe('₺0')
  expect(formatCompactMoney('-35200', 'TRY')).toBe('-₺35,2 B')
  expect(formatCompactMoney('12500.5', 'USD')).toBe('$12,5 B')
})
it('masks compact money in privacy mode', () => {
  setPrivacy(true)
  expect(formatCompactMoney('1284650.40', 'TRY')).toBe(MASK)
})
```

- [ ] **Step 2: Write the failing invalidation test** in a new `api/transactions.test.ts`: "a transaction change refreshes the portfolio list". Build a `new QueryClient()`, `vi.spyOn(queryClient, 'invalidateQueries')`, call the exported `invalidatePortfolio(queryClient, 1)` from `api/transactions.ts`, and expect a call with `{ queryKey: ['portfolios'] }`.
- [ ] **Step 3: Write the failing `Sidebar.test.tsx`.** Mock `GET /api/portfolios` the way `HomePage.test.tsx` does. Fixture: `{ id: 1, name: 'Ana portföy', total_value: '1284650.40' }`, `{ id: 2, name: 'Emeklilik', total_value: '842000' }`, both `currency: 'TRY'`. Tests:
  - "lists each portfolio with its compact value and the combined total": row `Ana portföy` contains `₺1,28 Mn`, row `Emeklilik` contains `₺842 B`, link `Tüm portföyler` contains `₺2,13 Mn` and has `href="/portfolios/all"`.
  - "marks the open portfolio and keeps the tab": at `/portfolios/1/transactions`, the `Ana portföy` link has `aria-current="page"` and the `Emeklilik` link has `href="/portfolios/2/transactions"`; at `/portfolios/1/positions/5` the `Emeklilik` link has `href="/portfolios/2"`.
  - "shows the admin link only to an admin": `me.is_admin` true → link `Yönetim` present; false → absent.
  - "masks values in privacy mode": `setPrivacy(true)` → both rows show `•••`.
  - "shows a negative total with its sign": `total_value: '-35200'` → `-₺35,2 B`.
  - "still navigates when the list fails to load": a 500 from the API → `Tüm portföyler` link present, no crash, no value text.
  - "offers to create the first portfolio": empty list → a button named by `portfolios.emptyCta`; clicking it opens a dialog named by `portfolios.new`.
  - "is a named navigation landmark": `getByRole('navigation', { name: t('nav.label') })`.
- [ ] **Step 4: Write the failing `AccountMenu.test.tsx`** by moving these three from `Header.test.tsx` and pointing them at `AccountMenu`: "opens the account menu and closes it on Escape", "posts the logout request from the menu", "runs pending toast commits before the logout request".
- [ ] **Step 5: Run them.** `npx vitest run src/lib/format.test.ts src/api/transactions.test.ts src/components/Sidebar.test.tsx src/components/AccountMenu.test.tsx`. Expected: fail.
- [ ] **Step 6: Implement `formatCompactMoney` and the invalidation.** Add `queryClient.invalidateQueries({ queryKey: portfoliosQueryKey })` to the exported `invalidatePortfolio` in `api/transactions.ts` and to the local one in `api/prices.ts` (same one line; the test covers the first).
- [ ] **Step 7: Extract `CreatePortfolioDialog.tsx`** from `HomePage.tsx`'s `CreateDialog`; `HomePage` imports it. Move HomePage's local `sum` to `api/portfolios.ts` as `export function sumPortfolios(portfolios: Portfolio[] | undefined, pick: (p: Portfolio) => string): string`, body unchanged, so `Sidebar` can use it for the combined total.
- [ ] **Step 8: Write `AccountMenu.tsx` and `Sidebar.tsx`.** Sidebar layout, top to bottom, as in the spec's "Sidebar" section. Decisions the spec leaves open:
  - Root: `<nav aria-label={t('nav.label')} className="flex h-full w-[232px] flex-col gap-0.5 bg-paper px-2.5 py-3">`.
  - Rows are `NavLink`s; the active row is `bg-surface text-ink shadow-ctl`, others `text-ink-2 hover:bg-chip`.
  - Tab suffix: move `TAB_SUFFIXES` from `PortfolioHeader.tsx`; `to` is `/portfolios/${id}${suffix}` where `suffix` is the entry the current pathname ends with, else `''`.
  - Dot: an 8 px round `span` with `style={{ backgroundColor: \`var(--color-cat-${(index % 6) + 1})\` }}`.
  - Value: `formatCompactMoney(p.total_value, p.currency)`; combined total: `formatCompactMoney(sumPortfolios(data, (p) => p.total_value), data[0].currency)`, the same rule `HomePage` uses.
  - The portfolio list is the only scrolling part: `min-h-0 flex-1 overflow-y-auto`; names `truncate`.
  - Loading: three `Skeleton` rows. Error: no rows and no values; the static links remain.
  - Bottom block: `Yönetim` when `me.is_admin`, the privacy toggle (moved from `Header.tsx` with its labels), `AccountMenu`.
  - New icons in `Icon.tsx`: `menu` (three lines), `layers` (all portfolios), `shield` (admin). Reuse existing string keys: `nav.label`, `nav.portfolios`, `nav.admin`, `portfolio.all`, `portfolios.new`, `portfolios.emptyCta`, `privacy.show`, `privacy.hide`, `account.menu`, `account.link`, `auth.logout`.
- [ ] **Step 9: Write `AppShell.tsx` and rewire `App.tsx`.**
  - `AppShell`: the skip link from `Header.tsx` (to `#main`), then `<div className="lg:grid lg:grid-cols-[232px_1fr]">`. Left: `<div className="sticky top-0 hidden h-dvh lg:block"><Sidebar /></div>`. Above the content below `lg`: a 48 px bar `sticky top-0 z-20 flex h-12 items-center gap-2 bg-surface px-4 shadow-ctl lg:hidden` with the brand link to `/`, the privacy toggle and `AccountMenu` (Task 9 adds the menu button and the title). Right: `<Outlet />`. `AppShell` adds no `<main>`; pages and `PortfolioHeader` own it.
  - `App.tsx`: one `<Route element={<ProtectedRoute><AppShell /></ProtectedRoute>}>` that nests `/`, `/account`, `/admin` and the five `/portfolios/...` routes, each without its own `ProtectedRoute`. `/setup`, `/setup/configure`, `/login`, `/register` and `*` stay outside. Remove `<Header />`.
- [ ] **Step 10: Shrink `PortfolioHeader.tsx`.** Remove the portfolio `<select>`, its wrapper, the "Portföy" eyebrow label and the `sr-only` h1. The name is a visible `<h1 className="truncate text-[20px] font-semibold text-ink">`; when `ownHeading` is set it renders as a `<p>` with the same classes, so the page still has exactly one h1. The sticky tab row moves from `max-sm:top-14` to `max-sm:top-12`. Rename, delete, tabs, `onAdd`, `quietAdd`, the not-found state and the `all` scope keep their behavior.
- [ ] **Step 11: Update the existing tests.** `PortfolioHeader.test.tsx`: delete "switches to the chosen portfolio on the tab the user is on" (now covered by `Sidebar.test.tsx`); adjust the h1 tests (GLG-668, GLG-690, GLG-677) to the visible h1 and the sticky test (GLG-571) to `top-12`. `App.test.tsx`: the logo-link and skip-link assertions from `Header.test.tsx` move here against the shell. Delete `Header.test.tsx` and `Header.tsx`.
- [ ] **Step 12: Run `npm test`.** Expected: all pass.
- [ ] **Step 13: Browser check.** At 1280, light and dark: sidebar with three portfolios, switch between them on İşlemler (tab kept), add a transaction and watch the row value change without a reload (Review Focus 2). At 1280×700 with 15 portfolios, one named with 100 characters: the name truncates, the list scrolls, the bottom block stays visible (Review Focus 4). At 375: top bar present, `/` reachable through the brand link, nothing scrolls sideways.
- [ ] **Step 14: Run the three checks; commit; open the PR.**

---

### Task 9: Drawer, page title and floating add button on small screens

**Files:**

- Modify: `frontend/src/components/AppShell.tsx`, `frontend/src/components/PortfolioHeader.tsx`, `frontend/src/i18n/strings.ts`
- Create: `frontend/src/components/AppShell.test.tsx`

**Interfaces:**

- Consumes: `Sidebar({ onNavigate })`, `AppShell` (Task 8); `useModal` from `components/ui/useModal.ts`.
- Produces: new string key `nav.openMenu` (`tr`: `Menüyü aç`, `en`: `Open menu`).

- [ ] **Step 1: Write the failing tests** in `AppShell.test.tsx` (render `AppShell` inside a `MemoryRouter` with a child route, portfolios mocked as in `Sidebar.test.tsx`):
  - "opens the drawer from the menu button": click the button named `Menüyü aç`; a `dialog` containing the `navigation` landmark appears and the button has `aria-expanded="true"`.
  - "closes the drawer when a link is followed": open it, click `Emeklilik`; the `dialog` is gone and the location is `/portfolios/2`.
  - "closes the drawer on cancel": open it, `fireEvent(dialog, new Event('cancel'))`; the `dialog` is gone.
  - "names the open portfolio in the top bar": at `/portfolios/1/transactions` the bar shows `Ana portföy`; at `/portfolios/all` it shows `Tüm portföyler`; at `/account` it shows the app name.
- [ ] **Step 2: Run them.** Expected: fail.
- [ ] **Step 3: Implement in `AppShell.tsx`.** The top bar gets, on the left, a ghost icon `Button` (`aria-label={t('nav.openMenu')}`, `aria-expanded`, `menu` icon) and the title: `useMatch('/portfolios/:id/*')` plus `usePortfolios()` gives the portfolio name, `all` gives `t('portfolio.all')`, anything else `t('app.name')`; the brand mark stays as the link to `/`. The drawer is a component rendered only while open: `<dialog ref={useModal()}>` with `backdrop:bg-scrim`, its panel `fixed inset-y-0 left-0 w-[264px] bg-paper shadow-pop`, holding `<Sidebar onNavigate={close} />`. `onCancel` and a click on the backdrop close it. The whole bar stays `lg:hidden`.
- [ ] **Step 4: Floating add button in `PortfolioHeader.tsx`.** When the add button is the primary one (`quietAdd` not set), add below `md`: `max-md:fixed max-md:right-4 max-md:bottom-4 max-md:z-20 max-md:size-14 max-md:rounded-full max-md:p-0 max-md:shadow-pop`, with a `plus` icon and the label in a `max-md:sr-only` span. The header's `<main>` gets `max-md:pb-24` so the button never covers the last row. A `quietAdd` button stays inline.
- [ ] **Step 5: Run `npm test`.** Expected: pass, including the `strings.test.ts` check that `tr` and `en` have the same keys.
- [ ] **Step 6: Browser check.** At 375 and at 800 (drawer, table layout), light and dark: open the drawer, switch portfolio, close with `Esc` and with a backdrop tap; toggle privacy from the drawer; on İşlemler the floating `+` opens the transaction dialog and does not cover the last row when scrolled to the end. At 1280 the bar and the drawer are absent.
- [ ] **Step 7: Run the three checks; commit; open the PR.**

---

### Task 10: The remaining native selects

**Files:**

- Modify: `frontend/src/pages/SummaryPage.tsx` (two, near lines 1572 and 1596), `pages/ImportPage.tsx` (near 1689), `pages/AccountPage.tsx` (near 48), `components/admin/UsersSection.tsx` (near 406), `components/admin/InstrumentsSection.tsx` (near 49 and 65), their tests, `frontend/src/conventions.test.ts`

**Interfaces:**

- Consumes: `Select`, `SelectOption` (Task 7); `chooseOption` from `src/test/select.ts`.

- [ ] **Step 1: Add the failing ban** to `BANNED`: `[/<select\b/, 'use ui/Select']`. Run `npx vitest run src/conventions.test.ts`. Expected: fails and lists the five files.
- [ ] **Step 2: Migrate each site** to `<Select value onChange options />`. Each `<option>` becomes one `{ value, label }`; a disabled option keeps `disabled: true`. Keep every `aria-label` and every `Field` wrapper. Where a value was a number, convert at the call site (`String(id)` in, `Number(value)` out); `Select` stays string-typed. The benchmark select on Özet uses `''` for "none": keep `''` as that option's value and check in the test that choosing it reports `''`; if Base UI treats `''` as no selection, map it to a `'none'` sentinel at that call site only.
- [ ] **Step 3: Update the tests** of those five files: every `fireEvent.change(select, ...)` becomes `await chooseOption(trigger, '<visible label>')`, and `getByRole('combobox')` queries keep working.
- [ ] **Step 4: Run `npm test`.** Expected: pass, the ban included.
- [ ] **Step 5: Browser check.** Each migrated control once by mouse and once by keyboard: the benchmark and return-method selects on Özet, the broker select on İçe aktar, the reporting currency on the account page, the language select in `UsersSection.tsx`, market and currency in `InstrumentsSection.tsx`. Light and dark at 1280; Özet and İçe aktar also at 375.
- [ ] **Step 6: Run the three checks; commit; open the PR.**

---

## After Task 10

- The orchestrator walks the "Done when" list in the spec against `main` and closes the epic.
- Sub-projects 3 to 5 (charts, core pages, secondary pages) each start with their own brainstorm and spec.
