import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeAll, expect, it, vi } from 'vitest'
import type { CashGap } from '../api/history'
import type { PortfolioScope } from '../api/summary'
import { t } from '../i18n/strings'
import { CashGapNotice } from './CashGapNotice'

// jsdom implements neither method on <dialog>.
beforeAll(() => {
  HTMLDialogElement.prototype.showModal = function () {
    this.open = true
  }
  HTMLDialogElement.prototype.close = function () {
    this.open = false
  }
})

afterEach(() => {
  vi.restoreAllMocks()
})

const gap = (portfolio_id: number, date: string, currency: string, amount: string): CashGap => ({
  portfolio_id,
  date,
  currency,
  amount,
})

// Portfolio 1 dips twice in TRY and once in USD; portfolio 2 once in TRY.
const gaps = [
  gap(1, '2024-03-12', 'TRY', '48000'),
  gap(1, '2024-03-12', 'USD', '1150'),
  gap(1, '2024-04-02', 'TRY', '200.5'),
  gap(2, '2024-05-20', 'TRY', '75'),
]

// `post` answers the POST; the gaps GET answers `answer` until a post went through.
function renderNotice(
  scope: PortfolioScope,
  answer: CashGap[],
  post: () => Response | Promise<Response> = () => Response.json([10, 11]),
) {
  const posts: unknown[] = []
  let filled = false
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const url = String(input)
    if (url === '/api/portfolios') {
      return Response.json([
        { id: 1, name: 'Emeklilik' },
        { id: 2, name: 'Kripto' },
      ])
    }
    if (url === `/api/portfolios/${scope}/cash-gaps`) {
      if (init?.method !== 'POST') return Response.json(filled ? [] : answer)
      posts.push(JSON.parse(String(init.body)))
      const response = await post()
      filled = response.ok
      return response
    }
    throw new Error(`unexpected request: ${url}`)
  })
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <CashGapNotice scope={scope} />
    </QueryClientProvider>,
  )
  return posts
}

const line = (date: string, amounts: string) => t('performance.cashGaps.line', { date, amounts })

it('names the first gap date and the shortfall per currency', async () => {
  renderNotice(1, gaps.slice(0, 3))

  expect(await screen.findByText(line('12.03.2024', '₺48.200,50, $1.150,00'))).toBeInTheDocument()
  expect(screen.getByText(t('performance.cashGaps.cause'))).toBeInTheDocument()
  expect(screen.getByRole('button', { name: t('performance.cashGaps.add') })).toBeInTheDocument()
})

it('groups the gaps by portfolio in the all scope', async () => {
  renderNotice('all', gaps)

  const items = await screen.findAllByRole('listitem')
  await waitFor(() => expect(items[0]).toHaveTextContent('Emeklilik:'))
  expect(items.map((item) => item.textContent)).toEqual([
    `Emeklilik: ${line('12.03.2024', '₺48.200,50, $1.150,00')}`,
    `Kripto: ${line('20.05.2024', '₺75,00')}`,
  ])

  fireEvent.click(screen.getByRole('button', { name: t('performance.cashGaps.add') }))
  const rows = within(screen.getByRole('dialog')).getAllByRole('row')
  expect(rows).toHaveLength(5)
  expect(rows[4]).toHaveTextContent('20.05.2024KriptoTRY₺75,00')
})

// The table scrolls sideways inside its box, never the page.
it('scrolls the gaps table sideways inside the dialog', async () => {
  renderNotice(1, gaps.slice(0, 3))

  fireEvent.click(await screen.findByRole('button', { name: t('performance.cashGaps.add') }))
  const table = within(screen.getByRole('dialog')).getByRole('table')
  expect(table.parentElement).toHaveClass('relative', 'overflow-x-auto')
})

it('lets the portfolio name wrap in the all dialog', async () => {
  renderNotice('all', gaps)

  fireEvent.click(await screen.findByRole('button', { name: t('performance.cashGaps.add') }))
  const cell = await within(screen.getByRole('dialog')).findByRole('cell', { name: 'Kripto' })
  expect(cell).not.toHaveClass('whitespace-nowrap')
  expect(cell).toHaveClass('wrap-anywhere')
})

it('posts the note on confirm and closes', async () => {
  const posts = renderNotice(1, gaps.slice(0, 3))

  fireEvent.click(await screen.findByRole('button', { name: t('performance.cashGaps.add') }))
  const dialog = screen.getByRole('dialog')
  expect(within(dialog).getAllByRole('row')).toHaveLength(4)
  fireEvent.click(within(dialog).getByRole('button', { name: t('performance.cashGaps.confirm') }))

  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  expect(posts).toEqual([{ note: t('performance.cashGaps.note') }])
})

// isPending reaches the button a tick after the click, so a double
// click's second click came before pending and posted the deposits twice.
it('posts once on a double click of confirm', async () => {
  const posts = renderNotice(1, gaps.slice(0, 1), () => new Promise<Response>(() => {}))

  fireEvent.click(await screen.findByRole('button', { name: t('performance.cashGaps.add') }))
  const confirm = screen.getByRole('button', { name: t('performance.cashGaps.confirm') })
  fireEvent.click(confirm)
  fireEvent.click(confirm)

  await waitFor(() => expect(confirm).toHaveAttribute('aria-disabled', 'true'))
  expect(posts).toHaveLength(1)
})

it('keeps the dialog open with the error when the post fails', async () => {
  renderNotice(1, gaps.slice(0, 1), () =>
    Response.json({ error: 'internal error' }, { status: 500 }),
  )

  fireEvent.click(await screen.findByRole('button', { name: t('performance.cashGaps.add') }))
  fireEvent.click(screen.getByRole('button', { name: t('performance.cashGaps.confirm') }))

  expect(await screen.findByRole('alert')).toHaveTextContent(t('error.internal'))
  expect(screen.getByRole('dialog')).toBeInTheDocument()
})

it('gives the no-value reason and no button without gaps', async () => {
  renderNotice(1, [])

  expect(await screen.findByText(t('performance.cashGaps.noValue'))).toBeInTheDocument()
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})
