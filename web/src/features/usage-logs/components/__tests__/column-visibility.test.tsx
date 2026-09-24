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
import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { usageLogSchema } from '../../data/schema'
import { UsageLogsProvider } from '../usage-logs-provider'
import { UsageLogsTable } from '../usage-logs-table'

const ipv4 = '203.0.113.42'
const ipv6 = '2001:db8:1234:5678:9abc:def0:1234:5678'
const logs = [ipv4, ipv6, ''].map((ip, index) =>
  usageLogSchema.parse({
    id: index + 1,
    user_id: 2,
    created_at: 1788840000,
    type: 2,
    content: '',
    model_name: `ip-test-${index + 1}`,
    ip,
  })
)
const clients: QueryClient[] = []

function LogsFixture() {
  return (
    <UsageLogsProvider>
      <UsageLogsTable logCategory='common' />
    </UsageLogsProvider>
  )
}

async function renderLogs(role: number = ROLE.USER) {
  useAuthStore.getState().auth.setUser({ id: 2, username: 'viewer', role })
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/group/') {
      return { data: { success: true, data: [] } }
    }
    if (url === '/api/user/self/groups') {
      return { data: { success: true, data: {} } }
    }
    if (url.includes('/stat?')) {
      return { data: { success: true, data: { quota: 0, rpm: 0, tpm: 0 } } }
    }
    if (url.startsWith('/api/log')) {
      return {
        data: { success: true, data: { items: logs, total: logs.length } },
      }
    }
    throw new Error(`Unexpected request: ${url}`)
  })
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const usageLogs = createRoute({
    getParentRoute: () => auth,
    path: '/usage-logs/$section',
    component: LogsFixture,
    validateSearch: (search: Record<string, unknown>) => search,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([usageLogs])]),
    history: createMemoryHistory({ initialEntries: ['/usage-logs/common'] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const rendered = render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByText('ip-test-1')
  return rendered
}

afterEach(() => {
  cleanup()
  for (const client of clients.splice(0)) client.clear()
  useAuthStore.getState().auth.reset()
  localStorage.clear()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

it.each([
  { role: ROLE.USER, scope: 'self', saved: null },
  { role: ROLE.ADMIN, scope: 'admin', saved: { token_name: false } },
])(
  'hides IP by default in the $scope view and remembers explicit column choices',
  async ({ role, scope, saved }) => {
    const user = userEvent.setup()
    if (saved) {
      localStorage.setItem(
        `usage-logs:common:${scope}:column-visibility`,
        JSON.stringify(saved)
      )
    }
    const page = await renderLogs(role)
    expect(
      screen.queryByRole('columnheader', { name: 'IP Address' })
    ).not.toBeInTheDocument()
    expect(screen.queryByText(ipv4)).not.toBeInTheDocument()
    if (saved) {
      expect(
        screen.queryByRole('columnheader', { name: 'Token' })
      ).not.toBeInTheDocument()
    }

    await user.click(screen.getByRole('button', { name: 'View' }))
    const toggle = screen.getByRole('menuitemcheckbox', { name: 'IP Address' })
    expect(toggle).not.toBeChecked()
    await user.click(toggle)
    await user.keyboard('{Escape}')
    expect(
      screen.getByRole('columnheader', { name: 'IP Address' })
    ).toBeVisible()
    expect(screen.getByText(ipv4)).toBeVisible()
    expect(screen.getByText(ipv6)).toBeVisible()

    page.unmount()
    await renderLogs(role)
    expect(
      screen.getByRole('columnheader', { name: 'IP Address' })
    ).toBeVisible()
    expect(screen.getByText(ipv4)).toBeVisible()
    if (saved) {
      expect(
        screen.queryByRole('columnheader', { name: 'Token' })
      ).not.toBeInTheDocument()
    }
    await user.click(screen.getByRole('button', { name: 'View' }))
    await user.click(
      screen.getByRole('menuitemcheckbox', { name: 'IP Address' })
    )
    await user.keyboard('{Escape}')
    expect(screen.queryByText(ipv4)).not.toBeInTheDocument()
  }
)

it('shows a placeholder for unrecorded IPs and masks recorded IPs with the privacy control', async () => {
  localStorage.setItem(
    'usage-logs:common:self:column-visibility',
    JSON.stringify({ ip: true })
  )
  const user = userEvent.setup()
  await renderLogs()
  const ipIndex = screen
    .getAllByRole('columnheader')
    .findIndex((header) => header.textContent === 'IP Address')
  const emptyRow = screen.getByRole('row', { name: /ip-test-3/ })
  expect(within(emptyRow).getAllByRole('cell')[ipIndex]).toHaveTextContent('—')

  await user.click(screen.getByRole('button', { name: 'Hide' }))
  expect(screen.queryByText(ipv4)).not.toBeInTheDocument()
  expect(screen.queryByText(ipv6)).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Show' }))
  expect(screen.getByText(ipv4)).toBeVisible()
  expect(screen.getByText(ipv6)).toBeVisible()
})

it('uses the same column toggle on mobile and reveals the full IPv6 address on tap', async () => {
  const matchMedia = window.matchMedia
  vi.stubGlobal('matchMedia', (query: string) => ({
    ...matchMedia(query),
    matches: query === '(max-width: 640px)',
  }))
  const user = userEvent.setup()
  await renderLogs()
  expect(screen.queryByText(ipv6)).not.toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'View' }))
  await user.click(screen.getByRole('menuitemcheckbox', { name: 'IP Address' }))
  await user.keyboard('{Escape}')
  await user.click(screen.getByRole('button', { name: `IP Address: ${ipv6}` }))
  const dialog = await screen.findByRole('dialog', { name: 'IP Address' })
  expect(within(dialog).getByText(ipv6)).toBeVisible()
})

it('looks up the IP region on demand and displays the returned location', async () => {
  localStorage.setItem(
    'usage-logs:common:self:column-visibility',
    JSON.stringify({ ip: true })
  )
  const user = userEvent.setup()
  const geoFetch = vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({
      ip: ipv4,
      city: 'Tokyo',
      region: 'Tokyo',
      country: 'Japan',
    }),
  })
  vi.stubGlobal('fetch', geoFetch)

  await renderLogs()

  const lookup = screen.getByRole('button', {
    name: `Get location: ${ipv4}`,
  })
  expect(geoFetch).not.toHaveBeenCalled()
  await user.click(lookup)

  expect(await screen.findByText('Tokyo, Japan')).toBeVisible()
  expect(geoFetch).toHaveBeenCalledWith(
    `https://get.geojs.io/v1/ip/geo/${ipv4}.json`,
    expect.objectContaining({ signal: expect.any(AbortSignal) })
  )
})
