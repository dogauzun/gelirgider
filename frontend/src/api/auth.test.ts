import { QueryClient, QueryObserver } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { authConfigQueryKey, dropDataOnUserChange, meQueryKey, setupStatusQueryKey } from './auth'

describe('dropDataOnUserChange', () => {
  // After a logout the page still on screen can refetch its queries,
  // without a session, before it unmounts. The next user's page must not join
  // those requests and get their 401.
  it('keeps a request sent while signed out from answering the next user', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    dropDataOnUserChange(queryClient)
    const user = (id: string) => ({ id, email: '', reporting_currency: 'TRY', is_admin: false })
    queryClient.setQueryData(meQueryKey, user('1'))
    queryClient.setQueryData(meQueryKey, null)

    let deny = () => {}
    void queryClient.prefetchQuery({
      queryKey: ['portfolios'],
      queryFn: () => new Promise((_, reject) => (deny = () => reject(new Error('unauthorized')))),
    })
    queryClient.setQueryData(meQueryKey, user('2'))
    const list = queryClient.fetchQuery({ queryKey: ['portfolios'], queryFn: async () => ['Bora'] })
    deny()

    expect(await list).toEqual(['Bora'])
  })

  // After a logout the page still on screen can render once more
  // before it unmounts, and useQuery hands its observers their options again.
  // Its data goes when it unmounts, not before: a query removed under a
  // mounted observer is rebuilt and fetched, without a session.
  it('leaves the page still on screen nothing to refetch after a logout', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    dropDataOnUserChange(queryClient)
    queryClient.setQueryData(meQueryKey, {
      id: '1',
      email: '',
      reporting_currency: 'TRY',
      is_admin: false,
    })
    const list = vi.fn(async () => ['Ana'])
    const options = { queryKey: ['portfolios'], queryFn: list }
    const observer = new QueryObserver(queryClient, options)
    const unsubscribe = observer.subscribe(() => {})
    await vi.waitFor(() => expect(observer.getCurrentResult().isSuccess).toBe(true))

    queryClient.setQueryData(meQueryKey, null)
    observer.setOptions(options)
    unsubscribe()

    expect(list).toHaveBeenCalledTimes(1)
    expect(queryClient.getQueryData(['portfolios'])).toBeUndefined()
  })

  // The setup status and the auth config are the same for everybody. A
  // sign-in leaves them alone: SetupGate renders nothing while the status
  // loads, and setup awaits its own status refetch across the sign-in.
  it('keeps the public queries across a sign-in', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    dropDataOnUserChange(queryClient)
    queryClient.setQueryData(meQueryKey, null)
    const config = vi.fn(async () => ({ registration_open: true }))
    const observer = new QueryObserver(queryClient, {
      queryKey: authConfigQueryKey,
      queryFn: config,
    })
    const unsubscribe = observer.subscribe(() => {})
    await vi.waitFor(() => expect(observer.getCurrentResult().isSuccess).toBe(true))
    // In flight, as SetupPage's refetch is when the first admin signs in.
    let answer = () => {}
    const status = queryClient.fetchQuery({
      queryKey: setupStatusQueryKey,
      queryFn: () =>
        new Promise<{ needs_setup: boolean }>(
          (resolve) => (answer = () => resolve({ needs_setup: false })),
        ),
    })

    queryClient.setQueryData(meQueryKey, {
      id: '1',
      email: '',
      reporting_currency: 'TRY',
      is_admin: true,
    })
    answer()

    expect(await status).toEqual({ needs_setup: false })
    expect(queryClient.getQueryData(setupStatusQueryKey)).toEqual({ needs_setup: false })
    expect(queryClient.getQueryData(authConfigQueryKey)).toEqual({ registration_open: true })
    expect(config).toHaveBeenCalledTimes(1)
    unsubscribe()
  })
})
