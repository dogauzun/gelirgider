/// <reference types="node" />
import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { HEAT_MIX } from './lib/periods'

// Read from disk: Vitest stubs CSS imports, `?raw` included, to an empty string.
const css = readFileSync(`${import.meta.dirname}/index.css`, 'utf8')

// One value per theme block, in file order: light, [data-theme=dark], and
// the prefers-color-scheme dark copy.
function values(token: string): string[] {
  return [...css.matchAll(new RegExp(`--color-${token}: (#[0-9a-f]{6});`, 'g'))].map((m) => m[1])
}

// Trimmed, non-empty declaration lines of one dark block: the
// `[data-theme='dark'] { ... }` rule, or the block inside the
// prefers-color-scheme media query.
function darkBlock(kind: 'attribute' | 'media'): string[] {
  const re =
    kind === 'attribute'
      ? /^\[data-theme='dark'\] \{\n([\s\S]*?)\n\}/m
      : /@media \(prefers-color-scheme: dark\) \{\s*:root:not\(\[data-theme='light'\]\) \{\n([\s\S]*?)\n\s*\}/
  const body = css.match(re)?.[1] ?? ''
  return body
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

describe('color tokens', () => {
  // raised-hover is the hovered table row (GLG-933, GLG-935, GLG-1356).
  it('muted text meets WCAG AA on paper, surface, chip, well and raised-hover in every theme', () => {
    const muted = values('muted')
    expect(muted).toHaveLength(3)
    for (const bg of ['paper', 'surface', 'chip', 'well', 'raised-hover']) {
      const bgs = values(bg)
      expect(bgs).toHaveLength(3)
      muted.forEach((m, i) =>
        expect(contrast(m, bgs[i]), `muted on ${bg} #${i}`).toBeGreaterThanOrEqual(4.5),
      )
    }
  })

  it('faint reaches 3:1 on paper and surface yet stays quieter than muted', () => {
    const faint = values('faint')
    const muted = values('muted')
    expect(faint).toHaveLength(3)
    for (const bg of ['paper', 'surface']) {
      const bgs = values(bg)
      faint.forEach((f, i) => {
        expect(contrast(f, bgs[i]), `faint on ${bg} #${i}`).toBeGreaterThanOrEqual(3)
        expect(contrast(f, bgs[i]), `faint vs muted on ${bg} #${i}`).toBeLessThan(
          contrast(muted[i], bgs[i]),
        )
      })
    }
  })

  // The cash and Diğer (cat-6) slices are graphics, so 3:1 (WCAG 1.4.11) on
  // the card. They share a slate hue and sit side by side in the donut, so
  // they keep 3:1 apart by lightness too, cash the lighter one in every
  // theme (GLG-1033, GLG-1045).
  it('cash and cat-6 reach 3:1 on surface and 3:1 apart, cash the lighter', () => {
    const cash = values('cash')
    const surface = values('surface')
    const cat6 = values('cat-6')
    expect(cash).toHaveLength(3)
    expect(cat6).toHaveLength(3)
    cash.forEach((c, i) => {
      expect(contrast(c, surface[i]), `cash on surface #${i}`).toBeGreaterThanOrEqual(3)
      expect(contrast(cat6[i], surface[i]), `cat-6 on surface #${i}`).toBeGreaterThanOrEqual(3)
      expect(contrast(c, cat6[i]), `cash vs cat-6 #${i}`).toBeGreaterThanOrEqual(3)
      expect(luminance(c), `cash lighter than cat-6 #${i}`).toBeGreaterThan(luminance(cat6[i]))
    })
  })

  const AA_PAIRS = [
    ['gain', 'surface'],
    ['gain', 'gain-soft'],
    ['loss', 'surface'],
    ['loss', 'loss-soft'],
    ['loss', 'raised-hover'],
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
    expect(bgs).toHaveLength(3)
    fgs.forEach((f, i) => expect(contrast(f, bgs[i]), `#${i}`).toBeGreaterThanOrEqual(4.5))
  })

  // The periodic returns heatmap (GLG-955) mixes gain or loss into surface,
  // `color-mix(in srgb, ...)`: a straight blend of the encoded channels.
  it('keeps ink at 4.5:1 on every heatmap step in every theme', () => {
    const mix = (a: string, b: string, percent: number) =>
      '#' +
      [1, 3, 5]
        .map((i) => {
          const p = percent / 100
          const c = parseInt(a.slice(i, i + 2), 16) * p + parseInt(b.slice(i, i + 2), 16) * (1 - p)
          return Math.round(c).toString(16).padStart(2, '0')
        })
        .join('')
    const ink = values('ink')
    const surface = values('surface')
    for (const tone of ['gain', 'loss']) {
      values(tone).forEach((color, i) =>
        HEAT_MIX.forEach((percent) =>
          expect(
            contrast(ink[i], mix(color, surface[i], percent)),
            `ink on ${tone} ${percent}% #${i}`,
          ).toBeGreaterThanOrEqual(4.5),
        ),
      )
    }
  })
})

describe('theme blocks', () => {
  // The utilities read --elevation-*: Tailwind inlines a --shadow-* value,
  // so a dark block that redefined --shadow-* would change nothing.
  it('defines each shadow token in all three theme blocks', () => {
    for (const name of ['card', 'ctl', 'pop']) {
      expect(css).toContain(`--shadow-${name}: var(--elevation-${name});`)
      expect(css.match(new RegExp(`--elevation-${name}:`, 'g'))).toHaveLength(3)
    }
  })

  it('keeps the two dark blocks identical', () => {
    const attribute = darkBlock('attribute')
    expect(attribute.length).toBeGreaterThan(30)
    expect(darkBlock('media')).toEqual(attribute)
  })

  it('has no IBM Plex and no mono font', () => {
    expect(css).not.toMatch(/Plex|--font-mono/)
  })
})
