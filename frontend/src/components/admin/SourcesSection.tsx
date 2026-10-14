import { useState } from 'react'
import { useBackfill, useSaveSchedule, useSchedule, type ScheduleSettings } from '../../api/sources'
import { t, type StringKey } from '../../i18n/strings'
import { todayIstanbul } from '../../lib/date'
import { useOneAtATime } from '../../lib/useOneAtATime'
import { Button } from '../ui/Button'
import { Checkbox } from '../ui/Checkbox'
import { Field } from '../ui/Field'
import { inputClass } from '../ui/inputClass'
import { SourcesTable } from './SourcesTable'
import { toastQueued } from './toastQueued'

// Takes no props, so the setup wizard's step 2 renders it as is.
export function SourcesSection() {
  const backfillAll = useBackfill('', toastQueued)
  const once = useOneAtATime()

  return (
    <section id="admin-sources" className="card scroll-mt-20">
      <div className="border-line flex flex-wrap items-center justify-between gap-3 border-b px-4 py-3">
        <h2 className="text-ink text-[14px] font-semibold">{t('admin.section.sources')}</h2>
        <Button
          variant="secondary"
          pending={backfillAll.isPending}
          onClick={() => once(() => backfillAll.mutateAsync())}
        >
          {t('admin.sources.backfillAll')}
        </Button>
      </div>
      {backfillAll.isError && (
        <p role="alert" className="text-loss px-4 pt-3 text-[13px]">
          {backfillAll.error.message}
        </p>
      )}

      <ScheduleBlock />

      <div className="border-line border-t">
        <SourcesTable />
      </div>
    </section>
  )
}

function ScheduleBlock() {
  const { data: schedule } = useSchedule()
  // The typed start date; undefined until edited, showing the stored one.
  const [from, setFrom] = useState<string>()
  const save = useSaveSchedule()
  const once = useOneAtATime()

  // A change while a save runs is ignored: the toggles stay enabled then, see
  // SourcesTable.
  function update(change: Partial<ScheduleSettings>) {
    if (schedule) once(() => save.mutateAsync({ ...schedule, ...change }))
  }

  const toggles: ['catchup_enabled' | 'backfill_enabled', StringKey, StringKey][] = [
    ['catchup_enabled', 'admin.sources.catchup', 'admin.sources.catchupHint'],
    ['backfill_enabled', 'admin.sources.backfill', 'admin.sources.backfillHint'],
  ]

  return (
    // One row of controls from md, above the table.
    <div className="grid gap-x-6 gap-y-3 px-4 py-3 md:grid-cols-3">
      {toggles.map(([field, label, hint]) => (
        <Checkbox
          key={field}
          label={t(label)}
          description={t(hint)}
          checked={Boolean(schedule?.[field])}
          disabled={!schedule}
          pending={save.isPending}
          onChange={(checked) => update({ [field]: checked })}
        />
      ))}
      <div className="max-w-56">
        <Field
          label={t('admin.sources.backfillFrom')}
          hint={t('admin.sources.backfillFromHint')}
          error={save.isError ? save.error.message : undefined}
        >
          <input
            type="date"
            min="1990-01-01"
            max={todayIstanbul()}
            value={from ?? schedule?.backfill_from ?? ''}
            disabled={!schedule}
            onChange={(event) => {
              setFrom(event.target.value)
              save.reset()
            }}
            onBlur={() => {
              if (from !== undefined && from !== schedule?.backfill_from) {
                update({ backfill_from: from })
              }
            }}
            className={inputClass()}
          />
        </Field>
      </div>
    </div>
  )
}
