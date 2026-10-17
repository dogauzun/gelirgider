import { useEffect, useRef, useState } from 'react'
import { flushSync } from 'react-dom'
import { Link, useParams, useSearchParams } from 'react-router'
import { PortfolioHeader } from '../components/PortfolioHeader'
import { signedAmount } from '../lib/transactionCells'
import { TransactionDialog } from '../components/TransactionDialog'
import { fromTransaction, typeLabelKeyFor, type FormState } from '../components/transactionForm'
import { FilterControls, FilterSheet } from '../components/transactions/TransactionFilters'
import {
  dateFromParam,
  filterFromParam,
  importFromParam,
  instrumentFromParam,
} from '../components/transactions/filters'
import { TransactionList } from '../components/transactions/TransactionList'
import { Button } from '../components/ui/Button'
import { buttonClass } from '../components/ui/buttonClass'
import { useConfirm } from '../components/ui/useConfirm'
import { Icon } from '../components/ui/Icon'
import { LoadError } from '../components/ui/LoadError'
import { SkeletonRows } from '../components/ui/Skeleton'
import { importLabel, useImports, type Import } from '../api/imports'
import type { Instrument } from '../api/instruments'
import {
  transactionsCsvUrl,
  useDeleteTransaction,
  useTransactions,
  type Transaction,
  type TransactionFilter,
} from '../api/transactions'
import { t } from '../i18n/strings'
import { createStore } from '../lib/createStore'
import { formatMarketDate, todayIstanbul } from '../lib/date'
import { useNavState } from '../lib/navState'
import { patchSearchParams } from '../lib/searchParams'
import { useIsDesktop } from '../lib/useIsDesktop'
import { usePortfolioTitle } from '../lib/useTitle'
import { toast } from '../lib/toast'
import { focusIfLost, neighbourOf } from '../lib/useFocusLater'

// Rows deleted while their undo toast is up. Kept here rather than
// in the page's state, like the toast queue that holds the delete back, so a
// row stays hidden when the page is left and opened again before the toast
// goes.
const pendingDeletes = createStore<ReadonlySet<number>>(() => new Set())

function setPendingDelete(id: number, pending: boolean): void {
  const next = new Set(pendingDeletes.get())
  if (pending) next.add(id)
  else next.delete(id)
  pendingDeletes.set(next)
}

// The one line the delete dialog shows, built from the same formatters as the
// row. A cash transaction has no instrument, so the parts are
// joined rather than slotted, the way the phone row's details are.
function deleteSummary(tx: Transaction): string {
  const parts = [t(typeLabelKeyFor(tx.type, tx.instrument_id)), formatMarketDate(tx.date)]
  if (tx.instrument_id !== null) parts.push(tx.instrument_symbol ?? `#${tx.instrument_id}`)
  parts.push(signedAmount(tx))
  return parts.join(' · ')
}

// The card of a portfolio with no transactions at all.
function NoTransactions({ portfolioId }: { portfolioId: number }) {
  return (
    <section className="flex flex-col items-center gap-3 card px-6 py-12 text-center">
      <p className="text-[16px] font-semibold text-ink">{t('transactions.empty')}</p>
      {/* The add button is in the header; a statement is the other
          way to start, and the faster one for a broker user. */}
      <Link
        to={`/portfolios/${portfolioId}/import`}
        className={buttonClass({ variant: 'secondary' })}
      >
        {t('transactions.emptyImport')}
      </Link>
    </section>
  )
}

// The list a filter emptied: it names the instrument filter when one is on,
// and whether a type filter narrows it further.
function FilteredEmpty({
  instrumentSymbol,
  typed,
}: {
  instrumentSymbol: string | null
  typed: boolean
}) {
  return (
    <p className="card px-6 py-8 text-center text-[13px] text-muted">
      {instrumentSymbol
        ? t(
            typed
              ? 'transactions.filter.emptyInstrument'
              : 'transactions.filter.emptyInstrumentAll',
            { symbol: instrumentSymbol },
          )
        : t(typed ? 'transactions.filter.empty' : 'transactions.filter.emptyAny')}
    </p>
  )
}

// The banner of the import the page opened on. `onClose(true)`
// keeps that import selected in Kaynak, `onClose(false)` drops the filter.
// The page shows it only on arrival from an import commit, whose İçe aktar
// button left with ImportPage and dropped focus to <body>; it renders once
// the imports load, and takes focus then.
function ImportBanner({ item, onClose }: { item: Import; onClose: (keep: boolean) => void }) {
  const showOnly = useRef<HTMLButtonElement>(null)
  useEffect(() => focusIfLost(showOnly.current), [])
  return (
    <div
      role="status"
      className="mb-3 flex items-center justify-between gap-3 rounded-card bg-accent-soft py-1.5 pr-1.5 pl-4 text-[13px] text-accent-ink"
    >
      <span className="min-w-0">
        <span className="font-semibold">{importLabel(item)}</span>
        {` ${t('transactions.imported')} · ${t('transactions.count', {
          count: item.transaction_count,
        })}`}
      </span>
      <span className="flex shrink-0 items-center gap-1">
        <Button ref={showOnly} variant="secondary" onClick={() => onClose(true)}>
          {t('transactions.banner.showOnly')}
        </Button>
        <Button
          variant="ghost"
          iconOnly
          aria-label={t('transactions.banner.close')}
          onClick={() => onClose(false)}
        >
          <Icon name="x" />
        </Button>
      </span>
    </div>
  )
}

export function TransactionsPage() {
  const params = useParams()
  const portfolioId = Number(params.id)
  usePortfolioTitle(t('transactions.title'), portfolioId)
  // What arrives with the navigation: the summary page's add button asks for
  // the add dialog, the import page names the import it just
  // committed, for the banner.
  const { state, clear } = useNavState()
  // The filters live in the query string so they survive a reload and the
  // back button, and so a filtered list can be linked to.
  const [searchParams, setSearchParams] = useSearchParams()
  const filter = filterFromParam(searchParams.get('type') ?? '')
  const instrumentId = instrumentFromParam(searchParams.get('instrument'))
  const importFilter = importFromParam(searchParams.get('import'))
  // Set by picking or clearing an instrument: the chip and the search swap
  // places, which dropped focus to <body>, so the one that mounts takes focus.
  // A page loaded with a filter leaves focus alone.
  const [focusInstrumentFilter, setFocusInstrumentFilter] = useState(false)
  const from = dateFromParam(searchParams.get('from'))
  const to = dateFromParam(searchParams.get('to'))
  // A `to` before `from` is shown as an error and never sent: the list and
  // the export keep the last valid range until the user fixes it.
  const rangeValid = !from || !to || from <= to
  const [range, setRange] = useState(() => (rangeValid ? { from, to } : {}))
  if (rangeValid && (range.from !== from || range.to !== to)) setRange({ from, to })
  // One filter for the list and the CSV export, so the export holds the rows
  // the list shows.
  const listFilter: TransactionFilter = {
    types: filter.types,
    instrumentId,
    from: range.from,
    to: range.to,
    importId: importFilter,
  }
  const {
    data,
    isLoading,
    isError,
    error,
    refetch,
    isFetching,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useTransactions(portfolioId, listFilter)
  // The user's imports, across portfolios; this portfolio's name the Kaynak
  // pills and fill the Kaynak filter. A failed load leaves the pills generic.
  const { data: allImports } = useImports()
  const portfolioImports = (allImports ?? []).filter((item) => item.portfolio_id === portfolioId)
  const importById = new Map(portfolioImports.map((item) => [item.id, item]))
  // `editing` also drives the edited row's highlight, so it stays set while
  // the dialog is open and is cleared when it closes. `formOpen` is the only
  // thing that opens the dialog: add mode is `formOpen` with no `editing`.
  const [editing, setEditing] = useState<Transaction | null>(null)
  // Add mode's starting form: a cloned row, else an empty one.
  const [initial, setInitial] = useState<FormState | undefined>(undefined)
  // Navigation state is read once on mount, then cleared so a remount cannot
  // reopen the dialog or bring the banner back.
  const [formOpen, setFormOpen] = useState(() => Boolean(state?.add))
  const [bannerId, setBannerId] = useState(() => state?.imported ?? null)
  const [sheetOpen, setSheetOpen] = useState(false)
  const hidden = pendingDeletes.use()
  const { confirm, dialog: confirmDialog } = useConfirm()
  const deleteMutation = useDeleteTransaction(portfolioId)
  const tableCardRef = useRef<HTMLDivElement>(null)
  const desktop = useIsDesktop()

  useEffect(() => {
    if (state?.add || state?.imported) clear()
  }, [state, clear])

  // Where the table is wider than its card, focusing a row's edit or delete
  // button scrolls the card to its right edge and it stayed there after the
  // dialog closed, hiding the date column.
  function resetTableScroll() {
    if (tableCardRef.current) tableCardRef.current.scrollLeft = 0
  }

  function closeForm() {
    setFormOpen(false)
    setEditing(null)
    setInitial(undefined)
    resetTableScroll()
  }

  function setParam(name: string, value: string | undefined) {
    setSearchParams((prev) => patchSearchParams(prev, { [name]: value }))
  }

  function clearDates() {
    setSearchParams((prev) => patchSearchParams(prev, { from: undefined, to: undefined }))
  }

  // The symbol rides along in ?symbol= so the chip can name the instrument
  // even when the filtered list is empty.
  function selectInstrument(instrument: Instrument | undefined) {
    setFocusInstrumentFilter(true)
    setSearchParams((prev) =>
      patchSearchParams(prev, {
        instrument: instrument && String(instrument.id),
        symbol: instrument?.symbol,
      }),
    )
  }

  // The banner names the filter the page opened on; once Kaynak names another
  // source it goes, so its ✕ cannot undo that choice.
  if (bannerId !== null && importFilter !== bannerId) setBannerId(null)

  // Either banner button takes the banner, and the focus it held, away; the
  // Kaynak filter is what both act on. `keep` keeps the import selected,
  // else the filter goes back to every row. Below md Kaynak is inside the
  // closed sheet, so its Filtrele button takes focus.
  function closeBanner(keep: boolean) {
    if (bannerId === null) return
    setParam('import', keep ? String(bannerId) : undefined)
    flushSync(() => setBannerId(null))
    document
      .querySelector<HTMLElement>(desktop ? '#transactions-source' : '[data-open-filters]')
      ?.focus()
  }

  function startEdit(tx: Transaction) {
    setEditing(tx)
    setFormOpen(true)
  }

  // A recurring buy is re-entered as a copy dated today. It opens
  // in add mode, so the save is a new row: the form carries no id and no
  // import_id of the source.
  function startClone(tx: Transaction) {
    setEditing(null)
    setInitial({ ...fromTransaction(tx), date: todayIstanbul() })
    setFormOpen(true)
  }

  // `keyboard`: the delete button was pressed with a key (a click with no
  // pointer, detail 0). The undo toast then takes focus, else it sits past
  // the whole table in the tab order.
  async function handleDelete(tx: Transaction, keyboard: boolean) {
    // Read before the dialog takes focus: the delete button just pressed.
    const trigger = document.activeElement
    const confirmed = await confirm({
      title: t('transactions.deleteConfirm'),
      body: t('transactions.deleteConfirmBody', { summary: deleteSummary(tx) }),
      confirmLabel: t('transactions.deleteButton'),
      danger: true,
    })
    resetTableScroll()
    if (!confirmed) return
    // The delete waits behind the toast's undo: the row is hidden
    // now and the request goes when the toast does, so an undo has nothing
    // to restore and an imported row keeps its id and import link.
    const neighbour = neighbourOf(
      rows.map((row) => row.id),
      tx.id,
    )
    const show = () => setPendingDelete(tx.id, false)
    flushSync(() => setPendingDelete(tx.id, true))
    toast(t('transactions.deleted'), {
      // Focus goes back to the restored row's delete button; the undo button
      // leaves with its toast, which dropped focus to <body>.
      undo: () => {
        flushSync(show)
        document.querySelector<HTMLElement>(`[data-delete-tx="${tx.id}"]`)?.focus()
      },
      focus: keyboard,
      // Returns the request, so logout can wait for it (flushToasts). The
      // page may be gone by then; useDeleteTransaction refreshes the cache.
      commit: () =>
        deleteMutation.mutateAsync(tx.id).then(
          show,
          // A toast, since the page that would show a notice may be gone
          // and the user elsewhere in the app.
          (mutationError: Error) => {
            toast(t('transactions.deleteFailed', { error: mutationError.message }), {
              error: true,
            })
            show()
          },
        ),
    })
    // The dialog hands focus back to this row's delete button, which hiding
    // the row removes, dropping focus to <body>. Move it to the
    // next row, else the previous one, else the add button -- unless the
    // user has already moved it elsewhere.
    focusIfLost(
      document.querySelector<HTMLElement>(
        neighbour !== undefined ? `[data-delete-tx="${neighbour}"]` : '[data-add-transaction]',
      ),
      trigger,
    )
  }

  // The server returns each page newest first (date then id, both
  // descending), so the loaded pages' rows are already the display order.
  const rows = (data ?? []).filter((tx) => !hidden.has(tx.id))
  // The active filter is named by ?symbol=, else by a filtered row,
  // which carries the symbol; the id stands in when neither has it.
  const instrumentSymbol = instrumentId
    ? searchParams.get('symbol') ||
      (rows.find((tx) => tx.instrument_id === instrumentId)?.instrument_symbol ??
        `#${instrumentId}`)
    : null
  const activeFilters = [filter.types.length > 0, instrumentId, from || to, importFilter].filter(
    Boolean,
  ).length
  const bannerImport = bannerId !== null ? importById.get(bannerId) : undefined

  const filterControls = (
    <FilterControls
      filter={filter}
      from={from}
      to={to}
      rangeValid={rangeValid}
      importFilter={importFilter}
      imports={portfolioImports}
      instrumentSymbol={instrumentSymbol}
      focusInstrumentFilter={focusInstrumentFilter}
      onSelectInstrument={selectInstrument}
      setParam={setParam}
      clearDates={clearDates}
    />
  )

  return (
    <PortfolioHeader
      portfolioId={portfolioId}
      onAdd={() => {
        setEditing(null)
        setFormOpen(true)
      }}
    >
      {/* As wide as the portfolio header above it and the summary page, so
          the table has room on a laptop. */}
      <div className="mx-auto max-w-6xl px-6 pt-6 pb-12">
        {formOpen && (
          <TransactionDialog
            key={editing?.id ?? 'new'}
            portfolioId={portfolioId}
            editing={editing}
            initial={initial}
            onClose={closeForm}
            // Opened by another page's "İşlem ekle", the button
            // that had focus is gone on close; this page's takes it.
            focusFallback="[data-add-transaction]"
          />
        )}
        {confirmDialog}

        {bannerImport && <ImportBanner item={bannerImport} onClose={closeBanner} />}

        {isLoading && <SkeletonRows label={t('transactions.loading')} />}
        {isError && (
          <LoadError
            title={t('transactions.error')}
            error={error}
            onRetry={() => refetch()}
            retrying={isFetching}
            className="mb-3"
          />
        )}
        {data &&
          // A portfolio with no transactions at all gets the empty card;
          // once a filter is on, the toolbar stays so the filter that
          // emptied the list can be cleared again.
          (rows.length === 0 && activeFilters === 0 ? (
            <NoTransactions portfolioId={portfolioId} />
          ) : (
            <>
              {/* One row at 1280 px: the filters, then the count and the
                  export on the right. A narrower laptop wraps it rather than
                  overflow; below md the filters move into a sheet. */}
              <div className="mb-3 flex flex-wrap items-center gap-2">
                {desktop ? (
                  filterControls
                ) : (
                  <Button
                    variant="secondary"
                    data-open-filters
                    aria-haspopup="dialog"
                    onClick={() => setSheetOpen(true)}
                  >
                    {activeFilters > 0
                      ? t('transactions.filter.openCount', { count: activeFilters })
                      : t('transactions.filter.open')}
                  </Button>
                )}
                <p className="ml-auto text-[12px] whitespace-nowrap text-muted">
                  {t(hasNextPage ? 'transactions.countMore' : 'transactions.count', {
                    count: rows.length,
                  })}
                </p>
                {/* A plain download link: the browser sends the session
                    cookie and saves the file under the server's name. It
                    carries the list's filters. */}
                <a
                  href={transactionsCsvUrl(portfolioId, listFilter)}
                  download
                  // "CSV" on the button keeps the row short; the name says
                  // what it does.
                  aria-label={t('transactions.exportCsv')}
                  className={buttonClass({ variant: 'secondary', className: 'shrink-0' })}
                >
                  CSV
                </a>
              </div>
              {!desktop && sheetOpen && (
                <FilterSheet onClose={() => setSheetOpen(false)}>{filterControls}</FilterSheet>
              )}
              {!rangeValid && (
                <p id="transactions-date-error" role="alert" className="mb-3 text-[12px] text-loss">
                  {t('transactions.filter.dateOrder')}
                </p>
              )}

              {rows.length === 0 ? (
                <FilteredEmpty
                  instrumentSymbol={instrumentSymbol}
                  typed={filter.types.length > 0}
                />
              ) : (
                <>
                  <TransactionList
                    rows={rows}
                    imports={importById}
                    editingId={editing?.id}
                    cardRef={tableCardRef}
                    onEdit={startEdit}
                    onClone={startClone}
                    onDelete={handleDelete}
                  />

                  {hasNextPage && (
                    <div className="mt-4 flex justify-center">
                      <Button
                        variant="secondary"
                        onClick={() => fetchNextPage()}
                        pending={isFetchingNextPage}
                      >
                        {isFetchingNextPage
                          ? t('transactions.loadingMore')
                          : t('transactions.loadMore')}
                      </Button>
                    </div>
                  )}
                </>
              )}
            </>
          ))}
      </div>
    </PortfolioHeader>
  )
}
