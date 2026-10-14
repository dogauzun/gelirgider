import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'

// Admin market-data sources and schedule (GLG-307, GLG-309, GLG-314). Times
// are HH:MM in Europe/Istanbul; started_at and finished_at are UTC instants.
interface SourceRun {
  started_at: string
  finished_at: string | null // null while the run is going
  kind: string
  rows: number
  error: string | null
}

export interface SourceStatus {
  name: string
  enabled: boolean
  at: string
  can_backfill: boolean
  last_run: SourceRun | null
  last_success_at: string | null
}

export interface ScheduleSettings {
  catchup_enabled: boolean
  backfill_enabled: boolean
  backfill_from: string // "" means the earliest transaction
}

const sourcesQueryKey = ['admin-sources'] as const
const scheduleQueryKey = ['admin-schedule'] as const

export function isRunning(source: SourceStatus): boolean {
  return source.last_run !== null && source.last_run.finished_at === null
}

const STALE_MS = 48 * 60 * 60 * 1000

// A source that is on and has never succeeded, or last succeeded more than 48
// hours before now: data has stopped flowing. A source that is off is never
// stale, however old its last success.
export function isStale(
  source: Pick<SourceStatus, 'enabled' | 'last_success_at'>,
  now: Date,
): boolean {
  if (!source.enabled) return false
  if (!source.last_success_at) return true
  return now.getTime() - Date.parse(source.last_success_at) > STALE_MS
}

// Polls every 5 s while any source is running, and not at all otherwise.
export function useSources() {
  return useQuery({
    queryKey: sourcesQueryKey,
    queryFn: () => apiFetch<SourceStatus[]>('/api/admin/sources'),
    refetchInterval: (query) => (query.state.data?.some(isRunning) ? 5000 : false),
  })
}

function saveSource(name: string, enabled: boolean, at: string): Promise<void> {
  return apiFetch<void>(`/api/admin/sources/${name}`, {
    method: 'PUT',
    body: JSON.stringify({ enabled, at }),
  })
}

// Answers at once; the run goes on in the background.
function runSource(name: string): Promise<void> {
  return apiFetch<void>(`/api/admin/sources/${name}/run`, { method: 'POST' })
}

// Queues the named source's history walk, or every enabled source's when
// name is empty, and returns the sources queued.
function backfill(name = ''): Promise<{ queued: string[] }> {
  return apiFetch('/api/admin/backfill', {
    method: 'POST',
    body: name ? JSON.stringify({ source: name }) : undefined,
  })
}

export function useSaveSource(name: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ enabled, at }: { enabled: boolean; at: string }) =>
      saveSource(name, enabled, at),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: sourcesQueryKey }),
  })
}

export function useRunSource(name: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => runSource(name),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: sourcesQueryKey }),
  })
}

// The named source's backfill, or every enabled source's when name is empty.
// `onQueued` gets the sources queued, before the list refreshes.
export function useBackfill(name: string, onQueued: (queued: string[]) => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => backfill(name),
    onSuccess: (result) => {
      onQueued(result.queued)
      queryClient.invalidateQueries({ queryKey: sourcesQueryKey })
    },
  })
}

export function useSchedule() {
  return useQuery({
    queryKey: scheduleQueryKey,
    queryFn: () => apiFetch<ScheduleSettings>('/api/admin/schedule'),
  })
}

function saveSchedule(schedule: ScheduleSettings): Promise<void> {
  return apiFetch<void>('/api/admin/schedule', {
    method: 'PUT',
    body: JSON.stringify(schedule),
  })
}

export function useSaveSchedule() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: saveSchedule,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: scheduleQueryKey }),
  })
}
