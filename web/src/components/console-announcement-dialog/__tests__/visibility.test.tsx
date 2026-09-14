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
import { act, render, screen, waitFor, within } from '@testing-library/react'
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
  {
    id: 11,
    content: 'First announcement',
    publishDate: '2026-09-12T08:00:00Z',
  },
  {
    id: 12,
    content: 'Second announcement',
    publishDate: '2026-09-14T08:00:00Z',
    extra: '[Release notes](https://example.com/release-notes.pdf)',
  },
  {
    id: 13,
    content: 'Third announcement',
    publishDate: '2026-09-13T08:00:00Z',
  },
]

function showAnnouncements(
  notice = '',
  items: Record<string, unknown>[] = announcements
) {
  return render(
    <QueryClientProvider client={client}>
      <ConsoleAnnouncementDialog
        notice={notice}
        announcements={items}
        loading={false}
      />
    </QueryClientProvider>
  )
}

it.each(['confirm', 'close', 'escape'])(
  'shows only the newest announcement and never queues older items after %s or a reload',
  async (action) => {
    const user = userEvent.setup()
    const view = showAnnouncements('Console notice')
    const dialog = await screen.findByRole('dialog', {
      name: 'System Announcements',
    })
    expect(within(dialog).getByText('Second announcement')).toBeVisible()
    expect(screen.queryByText('First announcement')).not.toBeInTheDocument()
    expect(screen.queryByText('Third announcement')).not.toBeInTheDocument()
    expect(screen.queryByText('Console notice')).not.toBeInTheDocument()
    if (action === 'escape') await user.keyboard('{Escape}')
    else {
      await user.click(
        within(dialog).getByRole('button', {
          name: action === 'confirm' ? 'Got it' : 'Close',
        })
      )
    }
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    )
    await waitFor(() => expect(viewed.get(1)).toEqual(new Set(['id:12'])))
    view.unmount()
    client.clear()
    showAnnouncements('Console notice')
    await waitFor(() => expect(client.isFetching()).toBe(0))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(api.post).toHaveBeenCalledOnce()
  }
)

it('does not fall back to an older unread announcement when the newest was already seen', async () => {
  viewed.set(1, new Set(['id:12']))
  showAnnouncements()
  await waitFor(() => expect(client.isFetching()).toBe(0))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(api.post).not.toHaveBeenCalled()
})

it('shows a newly published announcement after the previous latest one was read', async () => {
  viewed.set(1, new Set(['id:12']))
  const view = showAnnouncements()
  await waitFor(() => expect(client.isFetching()).toBe(0))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  view.rerender(
    <QueryClientProvider client={client}>
      <ConsoleAnnouncementDialog
        notice=''
        announcements={[
          ...announcements,
          {
            id: 14,
            content: 'Newly published announcement',
            publishDate: '2026-09-14T09:00:00Z',
          },
        ]}
        loading={false}
      />
    </QueryClientProvider>
  )
  await waitFor(() => expect(viewed.get(1)?.has('id:14')).toBe(true))
  await waitFor(() =>
    expect(screen.getByText('Newly published announcement')).toBeVisible()
  )
  expect(api.post).toHaveBeenCalledOnce()
})

it('shows a visible title, attachment and publication date in a square dialog with a confirm button', async () => {
  showAnnouncements()
  const dialog = await screen.findByRole('dialog', {
    name: 'System Announcements',
  })
  expect(
    within(dialog).getByRole('heading', { name: 'System Announcements' })
  ).toBeVisible()
  expect(dialog).toHaveClass('rounded-none')
  expect(
    within(dialog).getByRole('link', { name: 'Release notes' })
  ).toHaveAttribute('href', 'https://example.com/release-notes.pdf')
  expect(dialog.querySelector('time')).toHaveAttribute(
    'datetime',
    '2026-09-14T08:00:00Z'
  )
  const confirm = within(dialog).getByRole('button', { name: 'Got it' })
  expect(confirm).toBeEnabled()
  expect(confirm.closest('[data-slot=dialog-footer]')).toHaveClass(
    'flex-shrink-0'
  )
  expect(
    within(dialog).getByText('Second announcement').closest('.overflow-y-auto')
  ).toBeInTheDocument()
})

it('shows a legacy notice once when there are no announcement entries', async () => {
  const view = showAnnouncements('Console notice', [])
  expect(await screen.findByText('Console notice')).toBeVisible()
  await userEvent.click(screen.getByRole('button', { name: 'Got it' }))
  await waitFor(() => expect(viewed.get(1)?.size).toBe(1))
  view.unmount()
  client.clear()
  showAnnouncements('Console notice', [])
  await waitFor(() => expect(client.isFetching()).toBe(0))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(api.post).toHaveBeenCalledOnce()
})

it('ignores empty content and invalid dates when selecting a dated announcement', async () => {
  showAnnouncements('', [
    { id: 20, content: '   ', publishDate: '2026-09-20T08:00:00Z' },
    { id: 21, content: 'Undated announcement', publishDate: 'invalid-date' },
    ...announcements,
  ])
  expect(await screen.findByText('Second announcement')).toBeVisible()
  expect(screen.queryByText('Undated announcement')).not.toBeInTheDocument()
})

it('uses the latest numeric ID when legacy announcements have no publication date', async () => {
  showAnnouncements('', [
    { id: 11, content: 'Older legacy announcement' },
    { id: 12, content: 'Latest legacy announcement' },
  ])
  expect(await screen.findByText('Latest legacy announcement')).toBeVisible()
  expect(
    screen.queryByText('Older legacy announcement')
  ).not.toBeInTheDocument()
})

it('does not share announcement views between accounts on the same browser', async () => {
  viewed.set(1, new Set(['id:11', 'id:12']))
  showAnnouncements()
  await waitFor(() => expect(client.isFetching()).toBe(0))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  act(() =>
    useAuthStore.getState().auth.setUser({ id: 2, username: 'second', role: 1 })
  )
  expect(await screen.findByText('Second announcement')).toBeVisible()
  await waitFor(() => expect(viewed.get(2)?.has('id:12')).toBe(true))
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
  expect(await screen.findByText('Second announcement')).toBeVisible()
  view.unmount()
  showAnnouncements()
  await waitFor(() => expect(client.isFetching()).toBe(0))
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  expect(screen.queryByText('First announcement')).not.toBeInTheDocument()
  expect(api.post).toHaveBeenCalledOnce()
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
  expect(await screen.findByText('Second announcement')).toBeVisible()
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
