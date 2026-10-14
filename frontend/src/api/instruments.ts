import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'

export interface Instrument {
  id: number
  symbol: string
  market: string
  name: string
  currency: string
}

// GET /api/instruments returns at most this many matches (searchLimit in
// backend/internal/instrument/routes.go); a full page may have been cut.
export const SEARCH_LIMIT = 20

function searchInstruments(q: string): Promise<Instrument[]> {
  return apiFetch<Instrument[]>(`/api/instruments?q=${encodeURIComponent(q)}`)
}

export function useInstrumentSearch(q: string) {
  return useQuery({
    queryKey: ['instruments', q] as const,
    queryFn: () => searchInstruments(q),
    enabled: q.length > 0,
  })
}

interface NewInstrument {
  market: string
  symbol: string
  name: string
  currency: string
}

// Admin only. Idempotent: a known market+symbol answers with the
// existing instrument.
function createInstrument(instrument: NewInstrument): Promise<Instrument> {
  return apiFetch<Instrument>('/api/admin/instruments', {
    method: 'POST',
    body: JSON.stringify(instrument),
  })
}

// Invalidates every instrument search so the new one shows up at once.
export function useCreateInstrument() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: createInstrument,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['instruments'] }),
  })
}
