/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { ConsoleAnnouncementDialog } from '@/components/console-announcement-dialog'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { useNotificationStore } from '@/stores/notification-store'

let client: QueryClient
let viewed: Map<number, Set<string>>
const originalGetAnimations = Object.getOwnPropertyDescriptor(
  HTMLElement.prototype,
  'getAnimations'
)
beforeEach(() => {
  Object.defineProperty(HTMLElement.prototype, 'getAnimations', {
    configurable: true,
    value: () => [],
  })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retryDelay: 0 } },
  })
  viewed = new Map()
  useAuthStore.getState().auth.setUser({ id: 1, username: 'first', role: 1 })
  vi.spyOn(api, 'get').mockImplementation(async () => ({
    data: {
      success: true,
      data: [...(viewed.get(useAuthStore.getState().auth.user?.id ?? 0) ?? [])],
    },
  }))
  vi.spyOn(api, 'post').mockImplementation(async (_, data) => {
    const body = data as { key: string }
    const userId = useAuthStore.getState().auth.user?.id ?? 0
    const keys = viewed.get(userId) ?? new Set<string>()
    keys.add(body.key)
    viewed.set(userId, keys)
    return { data: { success: true } }
  })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
  useNotificationStore.setState({
    lastReadNotice: '',
    readAnnouncementKeys: [],
  })
  localStorage.clear()
  if (originalGetAnimations) {
    Object.defineProperty(
      HTMLElement.prototype,
      'getAnimations',
      originalGetAnimations
    )
  } else Reflect.deleteProperty(HTMLElement.prototype, 'getAnimations')
})

const announcements = [
  { id: 11, content: 'First announcement' },
  { id: 12, content: 'Second announcement' },
]

function showAnnouncements(notice = '') {
  return render(
    <QueryClientProvider client={client}>
      <ConsoleAnnouncementDialog
        notice={notice}
        announcements={announcements}
        loading={false}
      />
    </QueryClientProvider>
  )
}

it('shows the notice and announcements once, then remembers them after a reload', async () => {
  const view = showAnnouncements('Console notice')
  expect(await screen.findByText('Console notice')).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Close' }))
  expect(await screen.findByText('First announcement')).toBeVisible()
  await userEvent.keyboard('{Escape}')
  expect(await screen.findByText('Second announcement')).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Close' }))
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  await waitFor(() => expect(viewed.get(1)?.size).toBe(3))
  view.unmount()
  client.clear()
  showAnnouncements('Console notice')
  await waitFor(() => expect(client.isFetching()).toBe(0))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(api.post).toHaveBeenCalledTimes(3)
})

it('does not share announcement views between accounts on the same browser', async () => {
  viewed.set(1, new Set(['id:11', 'id:12']))
  showAnnouncements()
  await waitFor(() => expect(client.isFetching()).toBe(0))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  act(() =>
    useAuthStore.getState().auth.setUser({ id: 2, username: 'second', role: 1 })
  )
  expect(await screen.findByText('First announcement')).toBeVisible()
  await waitFor(() => expect(viewed.get(2)?.has('id:11')).toBe(true))
})

it('does not show the same announcement again while its read acknowledgement is pending', async () => {
  let acknowledge!: (value: { data: { success: boolean } }) => void
  vi.mocked(api.post).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        acknowledge = resolve
      })
  )
  const view = showAnnouncements()
  expect(await screen.findByText('First announcement')).toBeVisible()
  view.unmount()
  showAnnouncements()
  await waitFor(() =>
    expect(screen.getByText('Second announcement')).toBeVisible()
  )
  expect(screen.queryByText('First announcement')).not.toBeInTheDocument()
  await act(async () => acknowledge({ data: { success: true } }))
})

it('does not retry a read acknowledgement using a different account after switching users', async () => {
  let rejectFirst!: (error: Error) => void
  vi.mocked(api.post).mockImplementationOnce(
    () =>
      new Promise((_, reject) => {
        rejectFirst = reject
      })
  )
  viewed.set(2, new Set(['id:11', 'id:12']))
  showAnnouncements()
  expect(await screen.findByText('First announcement')).toBeVisible()
  act(() =>
    useAuthStore.getState().auth.setUser({ id: 2, username: 'second', role: 1 })
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  await act(async () => rejectFirst(new Error('Connection lost')))
  await waitFor(() => expect(client.isMutating()).toBe(0))
  expect(api.post).toHaveBeenCalledOnce()
})
