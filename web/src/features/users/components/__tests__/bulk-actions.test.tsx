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
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { PageFooterProvider } from '@/components/layout/components/page-footer'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import type { User } from '../../types'
import { UsersProvider } from '../users-provider'
import { UsersTable } from '../users-table'

const users: User[] = [
  {
    id: 11,
    username: 'alpha',
    display_name: '',
    role: 1,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    group: 'default',
  },
  {
    id: 23,
    username: 'beta',
    display_name: '',
    role: 1,
    status: 2,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    group: 'default',
  },
  {
    id: 1,
    username: 'root-account',
    display_name: '',
    role: 100,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    group: 'default',
  },
  {
    id: 31,
    username: 'removed',
    display_name: '',
    role: 1,
    status: -1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    group: 'default',
    DeletedAt: '2026-01-01',
  },
]
let client: QueryClient
let pages: User[][]
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  pages = [users]
  vi.spyOn(window, 'scrollTo').mockImplementation(() => undefined)
  useAuthStore
    .getState()
    .auth.setUser({ id: 50, username: 'operator', role: 10 })
  vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (url === '/api/group/') {
      return { data: { success: true, data: ['default'] } }
    }
    const page = Number(config?.params?.p ?? 1)
    return {
      data: {
        success: true,
        data: { items: pages[page - 1] ?? [], total: pages.flat().length },
      },
    }
  })
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: true, data: { queued: 2, skipped: 0 } },
  })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
  localStorage.clear()
})

function UserManagement() {
  const [footer, setFooter] = useState<HTMLDivElement | null>(null)
  return (
    <UsersProvider>
      <PageFooterProvider container={footer}>
        <UsersTable />
        <div ref={setFooter} />
      </PageFooterProvider>
    </UsersProvider>
  )
}

function renderUsers(pageSize = 20) {
  const root = createRootRoute()
  const authenticated = createRoute({
    getParentRoute: () => root,
    id: '_authenticated',
  })
  const route = createRoute({
    getParentRoute: () => authenticated,
    path: 'users/',
    component: UserManagement,
    validateSearch: (search: Record<string, unknown>) => ({
      page: Number(search.page) || 1,
      pageSize: Number(search.pageSize) || pageSize,
    }),
  })
  const router = createRouter({
    routeTree: root.addChildren([authenticated.addChildren([route])]),
    history: createMemoryHistory({
      initialEntries: [`/users/?pageSize=${pageSize}`],
    }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

it('confirms bulk disable and retains failed selections while clearing successful users', async () => {
  vi.mocked(api.post).mockImplementation(async (_, body) => ({
    data: {
      success: (body as { id: number }).id === 11,
      message: 'Account could not be updated',
    },
  }))
  renderUsers()
  await screen.findByText('alpha')
  expect(
    within(screen.getByRole('row', { name: /root-account/ })).getByRole(
      'checkbox'
    )
  ).toHaveAttribute('aria-disabled', 'true')
  expect(
    within(screen.getByRole('row', { name: /removed/ })).getByRole('checkbox')
  ).toHaveAttribute('aria-disabled', 'true')
  await userEvent.click(screen.getByRole('checkbox', { name: 'Select all' }))
  await userEvent.click(
    screen.getByRole('button', { name: 'Disable selected' })
  )
  expect(await screen.findByRole('alertdialog')).toHaveTextContent(
    '2 selected users'
  )
  expect(api.post).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
  await waitFor(() => expect(api.post).toHaveBeenCalledTimes(2))
  expect(api.post).toHaveBeenCalledWith('/api/user/manage', {
    id: 11,
    action: 'disable',
  })
  expect(api.post).toHaveBeenCalledWith('/api/user/manage', {
    id: 23,
    action: 'disable',
  })
  await waitFor(() =>
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  )
  expect(
    within(screen.getByRole('row', { name: /alpha/ })).getByRole('checkbox')
  ).not.toBeChecked()
  expect(
    within(screen.getByRole('row', { name: /beta/ })).getByRole('checkbox')
  ).toBeChecked()
  await userEvent.click(screen.getByRole('button', { name: 'Enable selected' }))
  await userEvent.click(screen.getByRole('button', { name: 'Continue' }))
  await waitFor(() =>
    expect(api.post).toHaveBeenLastCalledWith('/api/user/manage', {
      id: 23,
      action: 'enable',
    })
  )
})

it('does not transfer selection to different users when the table page changes', async () => {
  pages = [[users[0]], [users[1]]]
  renderUsers(1)
  await userEvent.click(
    within(await screen.findByRole('row', { name: /alpha/ })).getByRole(
      'checkbox'
    )
  )
  await userEvent.click(screen.getByRole('button', { name: 'Go to next page' }))
  const beta = await screen.findByRole('row', { name: /beta/ })
  expect(within(beta).getByRole('checkbox')).not.toBeChecked()
  expect(
    screen.queryByRole('button', { name: 'Disable selected' })
  ).not.toBeInTheDocument()
  expect(api.post).not.toHaveBeenCalled()
})

it('sends re-engagement emails to checked users and to all users after clearing selection', async () => {
  renderUsers()
  await userEvent.click(
    within(await screen.findByRole('row', { name: /alpha/ })).getByRole(
      'checkbox'
    )
  )
  await userEvent.click(screen.getByRole('button', { name: 'Re-engage users' }))
  expect(await screen.findByRole('dialog')).toHaveTextContent(
    '1 selected users'
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email subject' }),
    'Hello'
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email content' }),
    'Welcome back'
  )
  await userEvent.click(
    within(screen.getByRole('dialog')).getByRole('button', {
      name: 'Send email',
    })
  )
  await waitFor(() =>
    expect(api.post).toHaveBeenCalledWith(
      '/api/user/email',
      expect.objectContaining({ ids: [11], all_users: false })
    )
  )
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  await userEvent.click(
    within(screen.getByRole('row', { name: /alpha/ })).getByRole('checkbox')
  )
  await userEvent.click(screen.getByRole('button', { name: 'Re-engage users' }))
  expect(await screen.findByRole('dialog')).toHaveTextContent(
    'all registered users'
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email subject' }),
    'News'
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email content' }),
    'New models'
  )
  await userEvent.click(
    within(screen.getByRole('dialog')).getByRole('button', {
      name: 'Send email',
    })
  )
  await waitFor(() =>
    expect(api.post).toHaveBeenLastCalledWith(
      '/api/user/email',
      expect.objectContaining({ all_users: true, subject: 'News' })
    )
  )
})
