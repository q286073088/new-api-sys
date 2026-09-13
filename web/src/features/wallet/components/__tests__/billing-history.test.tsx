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
import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { BillingHistoryDialog } from '../dialogs/billing-history-dialog'

const initialSystemConfig = useSystemConfigStore.getState()

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
  useSystemConfigStore.setState(initialSystemConfig)
})

it('shows manual credits in account units alongside ordinary payment orders', async () => {
  useAuthStore.getState().auth.setUser({
    id: 3,
    username: 'wallet-owner',
    display_name: 'Wallet owner',
    role: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
  })
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG, quotaPerUnit: 1000 },
  })
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        total: 2,
        items: [
          {
            id: 1,
            user_id: 3,
            amount: 12340,
            money: 0,
            trade_no: 'ADMIN_ORDER',
            payment_method: 'admin',
            status: 'success',
            create_time: 1700000000,
          },
          {
            id: 2,
            user_id: 3,
            amount: 20,
            money: 20,
            trade_no: 'PAY_ORDER',
            payment_method: 'alipay',
            status: 'success',
            create_time: 1700000000,
          },
        ],
      },
    },
  })
  render(<BillingHistoryDialog open onOpenChange={() => {}} />)

  expect(await screen.findByText('Manual credit')).toBeInTheDocument()
  expect(screen.getByText('$12.34')).toBeInTheDocument()
  expect(screen.getByText(/^\$20(?:\.00)?$/)).toBeInTheDocument()
})

it('shows the empty state only after billing history has finished loading', async () => {
  let resolveResponse!: () => void
  const responseReady = new Promise<void>((resolve) => {
    resolveResponse = resolve
  })
  vi.spyOn(api, 'get').mockImplementation(async () => {
    await responseReady
    return { data: { success: true, data: { total: 0, items: [] } } }
  })
  render(<BillingHistoryDialog open onOpenChange={() => {}} />)
  expect(screen.queryByText('No billing records found')).not.toBeInTheDocument()
  await act(async () => resolveResponse())
  expect(
    await screen.findByText('No billing records found')
  ).toBeInTheDocument()
})
