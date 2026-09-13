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
import { useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import type { ReferralInvitee, ReferralOverview } from '../../types'
import { AffiliateRewardsCard } from '../affiliate-rewards-card'
import { ReferralDetailsDialog } from '../dialogs/referral-details-dialog'

const overview: ReferralOverview = {
  summary: {
    direct_count: 11,
    team_count: 1,
    available_quota: 5000,
    total_earned: 5000,
    pending_quota: 1800,
  },
  settings: {
    enabled: true,
    level1_percent: 5,
    level2_percent: 2,
    delay_days: 3,
  },
}
const invitee: ReferralInvitee = {
  id: 2,
  username: 'direct-friend',
  display_name: '',
  created_at: 1700000000,
  topup_quota: 100000,
  direct_reward_quota: 5000,
  team_reward_quota: 1800,
  pending_quota: 1800,
}
let client: QueryClient
let failInvitees: boolean

beforeEach(() => {
  failInvitees = false
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  useAuthStore.getState().auth.setUser({
    id: 1,
    username: 'owner',
    role: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
  })
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG, quotaPerUnit: 1000 } })
  vi.spyOn(api, 'get').mockImplementation(async (url, config) => {
    if (url === '/api/user/referrals/summary') {
      return { data: { success: true, data: overview } }
    }
    if (url === '/api/user/referrals') {
      if (failInvitees) {
        return { data: { success: false, message: 'Request failed' } }
      }
      const params = config?.params as { parent_id?: number; p: number }
      let row = invitee
      if (params.parent_id) {
        row = {
          ...invitee,
          id: 3,
          username: 'team-buyer',
          direct_reward_quota: 0,
        }
      }
      if (params.p === 2) {
        row = { ...invitee, id: 12, username: 'next-page-friend' }
      }
      return {
        data: {
          success: true,
          data: {
            items: [row],
            total: params.parent_id ? 1 : 11,
            page: params.p,
            page_size: 10,
          },
        },
      }
    }
    if (url === '/api/user/referrals/rewards') {
      return {
        data: {
          success: true,
          data: {
            total: 1,
            page: 1,
            page_size: 10,
            items: [
              {
                id: 1,
                invitee_id: 3,
                invitee_username: 'team-buyer',
                level: 2,
                base_quota: 90000,
                rate: 2,
                quota: 1800,
                status: 'pending',
                created_at: 1700000000,
                available_at: 1700259200,
                settled_at: 0,
              },
            ],
          },
        },
      }
    }
    throw new Error(`Unexpected request: ${url}`)
  })
})
afterEach(() => {
  client.clear()
  useAuthStore.getState().auth.reset()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

function ReferralEntry() {
  const [open, setOpen] = useState(false)
  return (
    <>
      <AffiliateRewardsCard
        user={null}
        affiliateLink='https://example.com/register?aff=owner'
        onTransfer={() => undefined}
        onViewDetails={() => setOpen(true)}
        summary={overview.summary}
      />
      {open && <ReferralDetailsDialog open={open} onOpenChange={setOpen} />}
    </>
  )
}

function renderDetails() {
  return render(
    <QueryClientProvider client={client}>
      <ReferralDetailsDialog open onOpenChange={() => undefined} />
    </QueryClientProvider>
  )
}

it('opens referral details from the wallet and separates pending from transferable rewards', async () => {
  render(
    <QueryClientProvider client={client}>
      <ReferralEntry />
    </QueryClientProvider>
  )
  expect(screen.getByText('Available Rewards')).toBeInTheDocument()
  expect(screen.getByText('Pending Rewards')).toBeInTheDocument()
  await userEvent.click(
    screen.getByRole('button', { name: 'Referral Details' })
  )
  const dialog = await screen.findByRole('dialog', { name: 'Referral Details' })
  expect(await within(dialog).findByText('direct-friend')).toBeInTheDocument()
  expect(within(dialog).getByText('Team Members')).toBeInTheDocument()
  await userEvent.keyboard('{Escape}')
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
})

it('paginates invitees, drills into the direct invitee team and shows rebate due dates', async () => {
  renderDetails()
  await screen.findByText('direct-friend')
  await userEvent.click(screen.getByRole('button', { name: 'Go to next page' }))
  expect(await screen.findByText('next-page-friend')).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Go to next page' })).toBeDisabled()
  await userEvent.click(
    screen.getByRole('button', { name: 'Go to previous page' })
  )
  await userEvent.click(
    await screen.findByRole('button', { name: 'View team of direct-friend' })
  )
  expect(await screen.findByText('team-buyer')).toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'View team of team-buyer' })
  ).not.toBeInTheDocument()
  await userEvent.click(
    screen.getByRole('button', { name: 'Back to My Invitees' })
  )
  expect(await screen.findByText('direct-friend')).toBeInTheDocument()
  await userEvent.click(screen.getByRole('tab', { name: 'Rebate History' }))
  expect(
    await screen.findByRole('cell', { name: 'Pending' })
  ).toBeInTheDocument()
  expect(screen.getByRole('cell', { name: '2%' })).toBeInTheDocument()
  expect(
    screen.getByRole('columnheader', { name: 'Available At' })
  ).toBeInTheDocument()
  expect(screen.getByRole('cell', { name: '—' })).toBeInTheDocument()
})

it('shows a retryable failure instead of claiming the invite list is empty', async () => {
  failInvitees = true
  renderDetails()
  expect(
    await screen.findByRole('button', { name: 'Retry' })
  ).toBeInTheDocument()
  expect(screen.queryByText('No invited users yet')).not.toBeInTheDocument()
  failInvitees = false
  await userEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(await screen.findByText('direct-friend')).toBeInTheDocument()
})

it('keeps an in-flight list out of the empty state and then displays the actual empty result', async () => {
  let resolveList: (value: unknown) => void = () => undefined
  const pending = new Promise((resolve) => {
    resolveList = resolve
  })
  const get = vi.mocked(api.get)
  const fallback = get.getMockImplementation()
  expect(fallback).toBeDefined()
  if (!fallback) throw new Error('Expected the payment API fixture')
  get.mockImplementation((url, config) =>
    url === '/api/user/referrals'
      ? (pending as ReturnType<typeof api.get>)
      : fallback(url, config)
  )
  renderDetails()
  expect(screen.queryByText('No invited users yet')).not.toBeInTheDocument()
  await act(async () =>
    resolveList({
      data: {
        success: true,
        data: { items: [], total: 0, page: 1, page_size: 10 },
      },
    })
  )
  expect(await screen.findByText('No invited users yet')).toBeInTheDocument()
})
