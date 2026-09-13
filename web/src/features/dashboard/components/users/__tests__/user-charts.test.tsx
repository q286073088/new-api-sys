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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { ThemeProvider } from '@/context/theme-provider'
import { processUserChartData } from '@/features/dashboard/lib/charts'
import type { UserChartsFilters } from '@/features/dashboard/types'
import { api } from '@/lib/api'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'

import { UserCharts } from '../user-charts'

// Canvas rendering is a browser boundary; inspect the data actually sent to
// VChart while retaining the real filters, data fetching and aggregation.
vi.mock('@visactor/react-vchart', () => ({
  VChart: (props: { spec: { data: { id: string; values: unknown[] }[] } }) => (
    <output data-testid={props.spec.data[0].id}>
      {JSON.stringify(props.spec.data[0].values)}
    </output>
  ),
}))
vi.mock('@visactor/vchart', () => ({
  ThemeManager: { setCurrentTheme: () => undefined },
}))

const data = [
  {
    username: 'costly',
    created_at: 1700000000,
    quota: 5000000,
    token_used: 10,
  },
  {
    username: 'token-heavy',
    created_at: 1700000000,
    quota: 500000,
    token_used: 3000,
  },
  {
    username: 'token-heavy',
    created_at: 1700003600,
    quota: 500000,
    token_used: 2000,
  },
]
let client: QueryClient

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
  vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data } })
})
afterEach(() => {
  client.clear()
  useSystemConfigStore
    .getState()
    .setConfig({ currency: { ...DEFAULT_CURRENCY_CONFIG } })
})

function Charts() {
  const [filters, setFilters] = useState<UserChartsFilters>({
    timeGranularity: 'day',
    selectedRange: 7,
    topUserLimit: 5,
    metric: 'quota',
  })
  return <UserCharts filters={filters} onFiltersChange={setFilters} />
}

it('switches the ranking and trend from charged amounts to actual token counts', async () => {
  render(
    <QueryClientProvider client={client}>
      <ThemeProvider defaultTheme='light'>
        <Charts />
      </ThemeProvider>
    </QueryClientProvider>
  )
  const ranking = await screen.findByTestId('userRankData')
  await waitFor(() => expect(ranking.textContent).toContain('costly'))
  expect(JSON.parse(ranking.textContent ?? '')[0]).toMatchObject({
    User: 'costly',
    rawQuota: 5000000,
  })
  await userEvent.click(screen.getByRole('tab', { name: 'Tokens' }))
  expect(screen.getByRole('tab', { name: 'Tokens' })).toHaveAttribute(
    'aria-selected',
    'true'
  )
  expect(screen.getByText('User Token Usage Ranking')).toBeInTheDocument()
  expect(
    JSON.parse(screen.getByTestId('userRankData').textContent ?? '')[0]
  ).toMatchObject({ User: 'token-heavy', rawQuota: 5000, Usage: 5000 })
  expect(
    JSON.parse(screen.getByTestId('userTrendData').textContent ?? '')
  ).toContainEqual(
    expect.objectContaining({ User: 'token-heavy', rawQuota: 5000 })
  )
  await userEvent.click(screen.getByRole('tab', { name: 'Amount' }))
  expect(
    JSON.parse(screen.getByTestId('userRankData').textContent ?? '')[0].User
  ).toBe('costly')
})

it('ranks top users by tokens and never formats token counts as currency', () => {
  const result = processUserChartData(data, 'day', undefined, 1, 'tokens')
  const rank = result.spec_user_rank as {
    data: { values: { User: string; rawQuota: number }[] }[]
    title: { subtext: string }
    label: { formatMethod: (value: number) => string }
  }
  expect(rank.data[0].values).toHaveLength(1)
  expect(rank.data[0].values[0]).toMatchObject({
    User: 'token-heavy',
    rawQuota: 5000,
  })
  expect(rank.label.formatMethod(5000)).toBe('5,000 Tokens')
  expect(rank.title.subtext).not.toContain('$')
})

it('keeps empty datasets empty and treats a missing token count as zero', () => {
  const empty = processUserChartData([], 'day', undefined, 10, 'tokens')
  expect(empty.spec_user_rank.data).toEqual([
    { id: 'userRankData', values: [] },
  ])
  const result = processUserChartData(
    [{ username: 'legacy', created_at: 1700000000, quota: 999999 }],
    'day',
    undefined,
    10,
    'tokens'
  )
  expect(result.spec_user_rank.data).toEqual([
    { id: 'userRankData', values: [{ User: 'legacy', rawQuota: 0, Usage: 0 }] },
  ])
})
