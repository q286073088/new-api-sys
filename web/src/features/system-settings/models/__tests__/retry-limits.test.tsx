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
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SettingsPageProvider } from '../../components/settings-page-context'
import { RoutingReliabilitySection } from '../routing-reliability-section'

let client: QueryClient
beforeEach(() => {
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
})
afterEach(() => client.clear())

function RoutingSettings() {
  const [actions, setActions] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setActions} />
      <SettingsPageProvider actionsContainer={actions}>
        <RoutingReliabilitySection
          defaultValues={{
            RetryTimes: 3,
            ModelRetryTimes: '{"model-a":2,"model-b":3}',
            ChannelDisableThreshold: '',
            AutomaticDisableChannelEnabled: true,
            AutomaticEnableChannelEnabled: false,
            AutomaticDisableKeywords: '',
            AutomaticDisableStatusCodes: '401,403',
            AutomaticRetryStatusCodes: '429,500-599',
            'monitor_setting.auto_test_channel_enabled': true,
            'monitor_setting.auto_test_channel_minutes': 10,
            'monitor_setting.channel_test_concurrency': 1,
            'monitor_setting.channel_test_mode': 'passive_recovery',
            'monitor_setting.channel_test_prompt': 'Original test question',
            'monitor_setting.channel_test_max_tokens': 4096,
          }}
        />
      </SettingsPageProvider>
    </>
  )
}

function renderSettings() {
  const root = createRootRoute({ component: RoutingSettings })
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

it('saves a model override of zero without changing the global retry count', async () => {
  renderSettings()
  const group = await screen.findByRole('group', {
    name: 'Per-model retry limits',
  })
  const counts = within(group).getAllByRole('textbox', { name: 'Retry Times' })
  await userEvent.clear(counts[0])
  await userEvent.type(counts[0], '0')
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(api.put).toHaveBeenCalledOnce())
  const request = vi.mocked(api.put).mock.calls[0][1] as {
    key: string
    value: string
  }
  expect(request.key).toBe('ModelRetryTimes')
  expect(JSON.parse(request.value)).toEqual({ 'model-a': 0, 'model-b': 3 })
})

it.each(['2.5', '11', 'null'])(
  'rejects the invalid model retry count %s before saving',
  async (count) => {
    renderSettings()
    const group = await screen.findByRole('group', {
      name: 'Per-model retry limits',
    })
    const input = within(group).getAllByRole('textbox', {
      name: 'Retry Times',
    })[0]
    await userEvent.clear(input)
    await userEvent.type(input, count)
    await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
    expect(
      await screen.findByText(
        'Use exact model names and whole retry counts from 0 to 10.'
      )
    ).toBeVisible()
    expect(api.put).not.toHaveBeenCalled()
  }
)

it('saves a multiline test question and its output limit', async () => {
  const user = userEvent.setup()
  renderSettings()
  const prompt = await screen.findByRole('textbox', {
    name: 'Channel test prompt',
  })
  await user.clear(prompt)
  await user.click(prompt)
  await user.paste('Compute 17 × 23.\nExplain "why". 🧪')
  const limit = screen.getByRole('spinbutton', {
    name: 'Test maximum output tokens',
  })
  await user.clear(limit)
  await user.type(limit, '2048')
  expect(prompt).toHaveValue('Compute 17 × 23.\nExplain "why". 🧪')
  expect(limit).toHaveValue(2048)
  await user.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(api.put).toHaveBeenCalledTimes(2))
  expect(vi.mocked(api.put).mock.calls.map((call) => call[1])).toEqual([
    {
      key: 'monitor_setting.channel_test_prompt',
      value: 'Compute 17 × 23.\nExplain "why". 🧪',
    },
    { key: 'monitor_setting.channel_test_max_tokens', value: 2048 },
  ])
})

it('saves an empty question to restore the default test prompt', async () => {
  renderSettings()
  await userEvent.clear(
    await screen.findByRole('textbox', { name: 'Channel test prompt' })
  )
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  await waitFor(() => expect(api.put).toHaveBeenCalledOnce())
  expect(vi.mocked(api.put).mock.calls[0][1]).toEqual({
    key: 'monitor_setting.channel_test_prompt',
    value: '',
  })
})

it('rejects an oversized test question before saving', async () => {
  renderSettings()
  const prompt = await screen.findByRole('textbox', {
    name: 'Channel test prompt',
  })
  fireEvent.change(prompt, { target: { value: '题'.repeat(20001) } })
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  expect(
    await screen.findByText('Test prompt must not exceed 20,000 characters')
  ).toBeVisible()
  expect(prompt).toHaveAttribute('aria-invalid', 'true')
  expect(api.put).not.toHaveBeenCalled()
})

it('rejects a zero test output limit before saving', async () => {
  renderSettings()
  const limit = await screen.findByRole('spinbutton', {
    name: 'Test maximum output tokens',
  })
  await userEvent.clear(limit)
  await userEvent.type(limit, '0')
  expect(limit).toHaveValue(0)
  await userEvent.click(screen.getByRole('button', { name: 'Save Changes' }))
  expect(
    await screen.findByText(
      'Test output limit must be between 1 and 32,768 tokens'
    )
  ).toBeVisible()
  expect(api.put).not.toHaveBeenCalled()
})
