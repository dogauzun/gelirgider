import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { apiFetch } from '../api'
import type { Locale } from '../i18n/strings'
import { meQueryKey, type Me } from './auth'

// Admin user management. Admin only; the server answers
// 403 to anyone else.
export interface AdminUser {
  id: number
  email: string
  is_admin: boolean
  created_at: string
}

const adminUsersQueryKey = ['admin-users'] as const

export function useAdminUsers() {
  return useQuery({
    queryKey: adminUsersQueryKey,
    queryFn: () => apiFetch<AdminUser[]>('/api/admin/users'),
  })
}

function createUser(email: string, password: string, locale: Locale): Promise<AdminUser> {
  return apiFetch<AdminUser>('/api/admin/users', {
    method: 'POST',
    body: JSON.stringify({ email, password, locale }),
  })
}

// Ends every session of that user, the caller's own included.
function resetPassword(id: number, password: string): Promise<void> {
  return apiFetch<void>(`/api/admin/users/${id}/password`, {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}

function setAdmin(id: number, isAdmin: boolean): Promise<void> {
  return apiFetch<void>(`/api/admin/users/${id}/admin`, {
    method: 'PUT',
    body: JSON.stringify({ is_admin: isAdmin }),
  })
}

function deleteUser(id: number): Promise<void> {
  return apiFetch<void>(`/api/admin/users/${id}`, { method: 'DELETE' })
}

// Demoting yourself changes /api/me too, so every user action refreshes both.
function refreshUsers(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: adminUsersQueryKey })
  void queryClient.invalidateQueries({ queryKey: meQueryKey })
}

// `onAdded` and the hooks' other callbacks run before the refresh. mutate()
// takes no variables, so a password never lands in the mutation cache.
export function useCreateUser(
  user: { email: string; password: string; locale: Locale },
  onAdded: () => void,
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => createUser(user.email, user.password, user.locale),
    onSuccess: () => {
      onAdded()
      refreshUsers(queryClient)
    },
  })
}

export function useResetPassword(id: number, password: string, onDone: () => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => resetPassword(id, password),
    onSuccess: () => {
      onDone()
      refreshUsers(queryClient)
    },
  })
}

// Flips the user's admin flag. When the caller demotes themselves, the cached
// me loses its admin flag before `onDemotedSelf` runs: the refresh gets a 403
// "admin only" for the users list, and App's handler would toast the demotion
// a second time.
export function useToggleAdmin(onDemotedSelf: () => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (user: AdminUser) => setAdmin(user.id, !user.is_admin),
    onSuccess: (_, user) => {
      const me = queryClient.getQueryData<Me | null>(meQueryKey)
      if (user.id === me?.id && user.is_admin) {
        queryClient.setQueryData<Me>(meQueryKey, (old) => old && { ...old, is_admin: false })
        onDemotedSelf()
      }
      refreshUsers(queryClient)
    },
  })
}

export function useDeleteUser(onDeleted: (user: AdminUser) => void) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (user: AdminUser) => deleteUser(user.id),
    onSuccess: (_, user) => {
      onDeleted(user)
      refreshUsers(queryClient)
    },
  })
}
