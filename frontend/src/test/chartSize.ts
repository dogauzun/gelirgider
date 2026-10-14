import { act, fireEvent } from '@testing-library/react'

// Recharts sizes a responsive chart from getBoundingClientRect and a
// ResizeObserver, and jsdom has neither a layout nor an observer, so a chart
// renders nothing in a test. mockChartSize reports every element as
// width x height and returns the function that undoes it.
export function mockChartSize(width = 600, height = 240): () => void {
  const rect = Element.prototype.getBoundingClientRect
  const observer = globalThis.ResizeObserver
  Element.prototype.getBoundingClientRect = () =>
    ({ x: 0, y: 0, top: 0, left: 0, right: width, bottom: height, width, height }) as DOMRect
  globalThis.ResizeObserver = class {
    callback: ResizeObserverCallback
    constructor(callback: ResizeObserverCallback) {
      this.callback = callback
    }
    observe() {
      this.callback([{ contentRect: { width, height } } as ResizeObserverEntry], this)
    }
    unobserve() {}
    disconnect() {}
  }
  return () => {
    Element.prototype.getBoundingClientRect = rect
    globalThis.ResizeObserver = observer
  }
}

// A drag on a chart mounted under mockChartSize, by clientX at a height inside
// the plot; `chart` is the Recharts wrapper (`.recharts-wrapper`). Recharts
// reads the active point on a mouse move in the next animation frame, so each
// move waits one. The pointerdown before the press carries the pointer type,
// as a browser sends it before the mousedown.
const PLOT_Y = 100
const frame = () =>
  act(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())))

export async function chartMove(chart: Element, x: number) {
  fireEvent.mouseMove(chart, { clientX: x, clientY: PLOT_Y })
  await frame()
}

export async function chartPress(chart: Element, x: number, pointerType = 'mouse') {
  await chartMove(chart, x)
  fireEvent.pointerDown(chart, { pointerType, clientX: x, clientY: PLOT_Y })
  fireEvent.mouseDown(chart, { button: 0, clientX: x, clientY: PLOT_Y })
}

export function chartRelease(chart: Element, x: number) {
  fireEvent.mouseUp(chart, { button: 0, clientX: x, clientY: PLOT_Y })
}

export async function chartDrag(chart: Element, fromX: number, toX: number, pointerType = 'mouse') {
  await chartPress(chart, fromX, pointerType)
  await chartMove(chart, toX)
  chartRelease(chart, toX)
}
