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
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import type { QualityTest, QualityTokenOption } from '../api'
import { QualityTaskForm } from '../forms'
import { QualityTestsPage } from '../index'

const options: QualityTokenOption[] = [
  {
    id: 9,
    name: 'Administrator',
    key: 'masked',
    groups: [
      { name: 'codex', models: ['gpt-5.6-sol'] },
      { name: 'discount', models: ['deepseek-v4-flash'] },
    ],
  },
]
const task: QualityTest = {
  id: 1,
  token_id: 9,
  model: 'gpt-5.6-sol',
  group: 'codex',
  endpoint: 'responses',
  name: 'Arithmetic',
  prompt: '6*7?',
  expected_answer: '42',
  interval_minutes: 30,
  enabled: false,
  public: true,
  next_run_at: 0,
  requested_at: 0,
  latest_status: 'passed',
  latest_at: 100,
}
const clients: QueryClient[] = []
function client() {
  const value = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  clients.push(value)
  return value
}
afterEach(() => {
  for (const item of clients) item.clear()
  clients.length = 0
})

test('editing preserves interval and switches and submits the selected local key reference', async () => {
  const put = vi
    .spyOn(api, 'put')
    .mockResolvedValue({ data: { success: true } })
  const close = vi.fn()
  render(
    <QueryClientProvider client={client()}>
      <QualityTaskForm task={task} options={options} onClose={close} />
    </QueryClientProvider>
  )
  expect(screen.getByLabelText('Test interval (minutes)')).toHaveValue(30)
  expect(
    screen.getByRole('switch', { name: 'Enable scheduled tests' })
  ).not.toBeChecked()
  expect(
    screen.getByRole('switch', { name: 'Show results to signed-in users' })
  ).toBeChecked()
  fireEvent.change(screen.getByLabelText('Group'), {
    target: { value: 'discount' },
  })
  expect(screen.getByLabelText('Model')).toHaveValue('')
  fireEvent.change(screen.getByLabelText('Model'), {
    target: { value: 'deepseek-v4-flash' },
  })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(put).toHaveBeenCalledOnce())
  expect(put.mock.calls[0][1]).toMatchObject({
    token_id: 9,
    group: 'discount',
    model: 'deepseek-v4-flash',
    interval_minutes: 30,
    enabled: false,
    public: true,
  })
  expect(JSON.stringify(put.mock.calls[0][1])).not.toContain('masked')
  expect(close).toHaveBeenCalledOnce()
})

test('public board displays real counts, separate latest status, slot history and safe text answers', async () => {
  const start = Math.floor(Date.now() / 1000) - 86400
  const slots = Array.from({ length: 48 }, (_, i) => ({
    start: start + i * 1800,
    status: i === 47 ? 'failed' : 'passed',
    count: 1,
  }))
  const result = {
    id: 7,
    test_id: 1,
    name: 'Arithmetic',
    model: 'gpt-5.6-sol',
    group: 'codex',
    prompt: '6*7?',
    expected_answer: '42',
    answer: '<script>alert(1)</script>',
    status: 'passed',
    reason: 'Equivalent',
    failure_stage: '',
    request_id: '',
    judge_request_id: '',
    duration_ms: 500,
    started_at: start,
    finished_at: start + 1,
  }
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/quality-test-results')
      return { data: { success: true, data: [task] } }
    if (url === '/api/quality-test-results/summary')
      return {
        data: {
          success: true,
          data: {
            counts: { passed: 3, pending: 1, failed: 2, running: 0 },
            next_run_at: 0,
            timeline: { 1: slots },
            timeline_start: start,
            server_time: start + 86400,
          },
        },
      }
    if (url === '/api/quality-test-results/results')
      return {
        data: {
          success: true,
          data: { items: [result], total: 21, page: 1, page_size: 20 },
        },
      }
    throw new Error(`Unexpected ${url}`)
  })
  render(
    <QueryClientProvider client={client()}>
      <QualityTestsPage />
    </QueryClientProvider>
  )
  expect(await screen.findByText('75.0%')).toBeVisible()
  expect(screen.queryByText('Shared judge configuration')).toBeNull()
  const timeline = screen.getByLabelText('24-hour test timeline')
  expect(timeline.querySelectorAll('button')).toHaveLength(48)
  fireEvent.click(screen.getByRole('button', { name: 'View result' }))
  expect(await screen.findByText('<script>alert(1)</script>')).toBeVisible()
  expect(document.querySelector('script')).toBeNull()
  fireEvent.keyDown(document, { key: 'Escape' })
  fireEvent.click(timeline.querySelectorAll('button')[47])
  await waitFor(() =>
    expect(
      get.mock.calls.some(
        ([url, config]) =>
          url.endsWith('/results') &&
          config?.params?.test_id === '1' &&
          config.params.from === slots[47].start
      )
    ).toBe(true)
  )
})
