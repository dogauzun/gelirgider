import { useState } from 'react'
import { useLocation, useSearchParams } from 'react-router'
import { todayIstanbul } from './date'
import { RANGES, resolveRange, type RangeValue } from './ranges'
import { patchSearchParams } from './searchParams'

// The chart range of Özet, Tüm portföyler and the position page lives in the
// query string, so it survives a visit to a position page and a reload:
// ?range=<RANGES key or past year>, or ?from=YYYY-MM-DD&to=YYYY-MM-DD for
// Özel (`from` may be absent: Özel from Tümü with no transaction yet). No
// parameter, or one that does not parse, is the default, 3A.
const DEFAULT = RANGES[1].key
const PARAMS = ['range', 'from', 'to']

// A real calendar date: the pattern alone takes 2025-02-30.
function isDate(s: string): boolean {
  return (
    /^\d{4}-\d{2}-\d{2}$/.test(s) &&
    !Number.isNaN(Date.parse(s)) &&
    new Date(s).toISOString().startsWith(s)
  )
}

function parse(params: URLSearchParams, today: string): RangeValue {
  const from = params.get('from')
  const to = params.get('to')
  if (to !== null) {
    if (isDate(to) && to <= today && (from === null || (isDate(from) && from <= to))) {
      return { key: 'custom', from: from ?? '', to }
    }
  } else {
    const key = params.get('range') ?? ''
    if (RANGES.some((r) => r.key === key)) return { key, from: '', to: '' }
    // The picker offers past calendar years only.
    if (/^\d{4}$/.test(key) && key < today.slice(0, 4)) {
      return { key, ...resolveRange({ key, from: '', to: '' }, today) }
    }
  }
  return { key: DEFAULT, from: '', to: '' }
}

export function useRange(): [RangeValue, (next: RangeValue) => void] {
  const [params, setParams] = useSearchParams()
  const set = (next: RangeValue) =>
    setParams(
      (prev) =>
        patchSearchParams(prev, {
          range: next.key !== 'custom' && next.key !== DEFAULT ? next.key : undefined,
          from: next.key === 'custom' ? next.from : undefined,
          to: next.key === 'custom' ? next.to : undefined,
        }),
      { replace: true },
    )
  return [parse(params, todayIstanbul()), set]
}

// Drag to zoom for a page's chart and range picker: onZoom sets the
// dragged span as an Özel range and remembers the range before the first
// zoom, and onClear returns to it. The memory is component state: without one
// (after a reload, or Özel filled in by hand) onClear returns to the default
// range. A range picked in the picker forgets it.
export function useZoom(range: RangeValue, setRange: (next: RangeValue) => void) {
  const [before, setBefore] = useState<RangeValue | null>(null)
  return {
    onChange: (next: RangeValue) => {
      setBefore(null)
      setRange(next)
    },
    onZoom: (from: string, to: string) => {
      setBefore(before ?? range)
      setRange({ key: 'custom', from, to })
    },
    onClear: () => {
      setBefore(null)
      setRange(before ?? { key: DEFAULT, from: '', to: '' })
    },
  }
}

// The range parameters of a query string, as "?…" or "", for a link to
// another page that shows the range.
export function rangeSearch(search: string): string {
  const params = new URLSearchParams(search)
  const out = new URLSearchParams()
  for (const p of PARAMS) {
    const v = params.get(p)
    if (v !== null) out.set(p, v)
  }
  const s = out.toString()
  return s ? `?${s}` : ''
}

// The link to a position page, carrying the page's range.
export function usePositionPath(): (scope: number | 'all', instrumentId: number) => string {
  const { search } = useLocation()
  return (scope, instrumentId) =>
    `/portfolios/${scope}/positions/${instrumentId}${rangeSearch(search)}`
}
