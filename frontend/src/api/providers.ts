import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'

// Admin data provider keys (GLG-302, GLG-308). The server never returns a
// key, only whether one is stored and its last 4 characters ("" for keys of
// 4 characters or fewer). error is "undecryptable" when the stored key
// cannot be read after a SECRET_KEY change (GLG-320).
export interface ProviderStatus {
  name: string
  configured: boolean
  last4: string
  error?: 'undecryptable'
}

const providersQueryKey = ['admin-providers'] as const

export function useProviders() {
  return useQuery({
    queryKey: providersQueryKey,
    queryFn: () => apiFetch<ProviderStatus[]>('/api/admin/providers'),
  })
}

// An empty key clears the stored one.
function saveProviderKey(name: string, apiKey: string): Promise<void> {
  return apiFetch<void>(`/api/admin/providers/${name}`, {
    method: 'PUT',
    body: JSON.stringify({ api_key: apiKey }),
  })
}

// error is "invalid key", "rate limited" or "unreachable" when ok is false.
export interface KeyTestResult {
  ok: boolean
  error?: string
}

// Tests apiKey, or the stored key when apiKey is empty.
function testProviderKey(name: string, apiKey: string): Promise<KeyTestResult> {
  return apiFetch<KeyTestResult>(`/api/admin/providers/${name}/test`, {
    method: 'POST',
    body: JSON.stringify({ api_key: apiKey }),
  })
}

// Saves apiKey, or clears the stored key when apiKey is empty. mutate() and
// the test below take no variables, so a typed key never lands in the
// mutation cache, and the list is refetched rather than patched with it.
// `onSaved` runs before the refetch.
export function useSaveProviderKey(name: string, apiKey: string, onSaved: () => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => saveProviderKey(name, apiKey),
    onSuccess: () => {
      onSaved()
      queryClient.invalidateQueries({ queryKey: providersQueryKey })
    },
  })
}

export function useTestProviderKey(name: string, apiKey: string) {
  return useMutation({ mutationFn: () => testProviderKey(name, apiKey) })
}
