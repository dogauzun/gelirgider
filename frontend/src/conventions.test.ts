/// <reference types="node" />
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { expect, it } from 'vitest'

// Class patterns ADR 0023 bans from components. A task that removes the last
// use of a pattern adds it here, so it cannot come back.
const BANNED: [pattern: RegExp, why: string][] = [
  [/\bfont-mono\b/, 'IBM Plex Mono is gone; symbols are Inter semibold'],
  [/rounded-card border border-line bg-surface/, 'use the `card` utility'],
  [/\bshadow-(sm|md|lg|xl|2xl)\b/, 'use shadow-card, shadow-ctl or shadow-pop'],
  [/<select\b/, 'use ui/Select'],
  [/type="checkbox"/, 'use ui/Checkbox'],
  // GLG-1022: `faint` misses WCAG AA text contrast; it is for placeholders,
  // disabled text and chart axis labels (charts use var(--color-faint)).
  [/(?<!placeholder:|disabled:)\btext-faint\b/, 'readable text is text-muted'],
]

// Read from disk: the test wants the raw source text, not a module. Class
// strings shared from .ts files (ui/table.ts) are scanned too; test/ holds
// test helpers, not components.
const dir = import.meta.dirname
const files = (readdirSync(dir, { recursive: true }) as string[]).filter(
  (f) => /\.tsx?$/.test(f) && !/\.test\.tsx?$/.test(f) && !f.startsWith('test/'),
)

it('finds the components to scan', () => {
  expect(files.length).toBeGreaterThan(20)
})

it.each(BANNED)('no component uses %s (%s)', (pattern) => {
  const hits = files.filter((f) => pattern.test(readFileSync(`${dir}/${f}`, 'utf8')))
  expect(hits).toEqual([])
})

// GLG-915: Recharts and the charts are one lazy chunk. Only files under
// components/charts/ import recharts, and only LazyChart.tsx imports them, so
// no page pulls Recharts into the main chunk. Tests are left out: they are
// not in the bundle.
const CHARTS = 'components/charts'
const sources = (readdirSync(dir, { recursive: true }) as string[]).filter(
  (f) => /\.tsx?$/.test(f) && !/\.test\.tsx?$/.test(f),
)
// Every module specifier: import/export ... from '...', import '...', import('...').
const specifiers = (f: string) =>
  [
    ...readFileSync(`${dir}/${f}`, 'utf8').matchAll(/(?:from|import)\s*\(?\s*['"]([^'"]+)['"]/g),
  ].map((m) => m[1])
const inCharts = (f: string) => f.startsWith(`${CHARTS}/`)

it('imports recharts only under components/charts/', () => {
  const hits = sources.filter(
    (f) => !inCharts(f) && specifiers(f).some((s) => s === 'recharts' || s.startsWith('recharts/')),
  )
  expect(sources.filter(inCharts).length).toBeGreaterThan(0)
  expect(hits).toEqual([])
})

it('imports components/charts/ only from LazyChart.tsx', () => {
  const charts = resolve(dir, CHARTS)
  const hits = sources.filter(
    (f) =>
      !inCharts(f) &&
      f !== 'components/LazyChart.tsx' &&
      specifiers(f).some((s) => {
        const target = resolve(dirname(`${dir}/${f}`), s)
        return s.startsWith('.') && (target === charts || target.startsWith(`${charts}/`))
      }),
  )
  expect(hits).toEqual([])
})
