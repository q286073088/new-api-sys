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
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { ReferralSettingsSection } from '../referral-settings-section'

let client: QueryClient
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})
afterEach(() => client.clear())

function SettingsHarness(props: { confirmed: boolean }) {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setActions} />
      <SettingsPageProvider actionsContainer={actions}>
        <ReferralSettingsSection
          defaultValue='{"enabled":false,"level1_percent":5,"level2_percent":0,"delay_days":3}'
          complianceConfirmed={props.confirmed}
        />
      </SettingsPageProvider>
    </>
  )
}

function renderSettings(confirmed = true) {
  const root = createRootRoute({
    component: () => <SettingsHarness confirmed={confirmed} />,
  })
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

it('saves both percentages and the delay atomically as one setting', async () => {
  renderSettings()
  await userEvent.click(
    await screen.findByRole('switch', { name: 'Enable Recharge Rebates' })
  )
  const rate = screen.getByRole('spinbutton', { name: 'Team Rebate Rate (%)' })
  await userEvent.clear(rate)
  await userEvent.type(rate, '2')
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() =>
    expect(api.put).toHaveBeenCalledWith('/api/option/', {
      key: 'ReferralSetting',
      value:
        '{"enabled":true,"level1_percent":5,"level2_percent":2,"delay_days":3}',
    })
  )
})

it('rejects a combined rebate above 100 percent before saving', async () => {
  renderSettings()
  const direct = await screen.findByRole('spinbutton', {
    name: 'Direct Rebate Rate (%)',
  })
  const team = screen.getByRole('spinbutton', { name: 'Team Rebate Rate (%)' })
  await userEvent.clear(direct)
  await userEvent.type(direct, '99')
  await userEvent.clear(team)
  await userEvent.type(team, '2')
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  expect(
    await screen.findByText(
      'The two rebate rates must add up to no more than 100%.'
    )
  ).toBeInTheDocument()
  expect(api.put).not.toHaveBeenCalled()
})

it('keeps rebate enablement disabled until existing payment terms are confirmed', async () => {
  renderSettings(false)
  const toggle = await screen.findByRole('switch', {
    name: 'Enable Recharge Rebates',
  })
  expect(toggle).toHaveAttribute('aria-disabled', 'true')
  await userEvent.click(toggle)
  expect(toggle).toHaveAttribute('aria-checked', 'false')
})
