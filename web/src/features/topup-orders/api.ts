/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either not, version 3 of
the License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranties of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { keepPreviousData, useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { createServerError } from '@/lib/server-error-message'

export type TopupOrderStatus = 'success' | 'pending' | 'failed' | 'expired'

export type TopupOrder = {
  id: number
  user_id: number
  username: string
  display_name: string
  amount: number
  credited_quota: number
  money: number
  trade_no: string
  payment_method: string
  payment_provider: string
  create_time: number
  complete_time: number
  status: TopupOrderStatus
  is_gift: boolean
  invoice_amount_cents?: number | null
}

export type TopupOrderSummary = {
  order_count: number
  success_count: number
  gift_count: number
  total_paid_money: number
  total_credited_quota: number
  gift_quota: number
  first_success_at: number
  last_success_at: number
}

export type TopupOrderPage = {
  page: number
  page_size: number
  total: number
  items: TopupOrder[]
}

export type TopupOrderFilters = {
  keyword?: string
  status?: string
  payment_method?: string
  from?: number
  to?: number
}

async function request<T>(url: string, params?: object) {
  const response = await api.get<{
    success: boolean
    message?: string
    data: T
  }>(url, { params })
  const payload = response.data
  if (!payload.success) throw createServerError(payload)
  return payload.data
}

export function useTopupOrders(
  page: number,
  pageSize: number,
  filters: TopupOrderFilters
) {
  return useQuery({
    queryKey: ['topup-orders', page, pageSize, filters],
    queryFn: () =>
      request<TopupOrderPage>('/api/user/topup/details', {
        p: page,
        page_size: pageSize,
        ...filters,
      }),
    placeholderData: keepPreviousData,
  })
}

export function useTopupOrder(id: number | null) {
  return useQuery({
    queryKey: ['topup-orders', 'detail', id],
    queryFn: () => request<TopupOrder>(`/api/user/topup/details/${id}`),
    enabled: id !== null && id > 0,
  })
}

export function useTopupOrderSummary(userId: number | null) {
  return useQuery({
    queryKey: ['topup-orders', 'summary', userId],
    queryFn: () =>
      request<TopupOrderSummary>(`/api/user/topup/details/summary/${userId}`),
    enabled: userId !== null && userId > 0,
  })
}
