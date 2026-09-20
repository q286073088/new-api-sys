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
import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'

export type QualityTarget = {
  token_id: number
  model: string
  group: string
  endpoint: 'chat' | 'responses'
}
export type QualityJudge = QualityTarget & { instructions: string }
export type QualityTest = QualityTarget & {
  id: number
  name: string
  prompt: string
  expected_answer: string
  interval_minutes: number
  enabled: boolean
  public: boolean
  latest_status: QualityStatus | ''
  latest_at: number
  next_run_at: number
  requested_at: number
}
export type QualityStatus = 'passed' | 'pending' | 'failed' | 'running'
export type QualityResult = {
  id: number
  test_id: number
  name: string
  model: string
  group: string
  prompt: string
  expected_answer: string
  answer: string
  status: QualityStatus
  verdict: string
  reason: string
  failure_stage: string
  error?: string
  started_at: number
  finished_at: number
  duration_ms: number
  request_id: string
  judge_request_id: string
}
export type QualityTokenOption = {
  id: number
  name: string
  key: string
  groups: { name: string; models: string[] }[]
}
export type QualitySlot = {
  start: number
  status: QualityStatus | ''
  count: number
}
export type QualitySummary = {
  counts: Record<QualityStatus, number>
  next_run_at: number
  timeline: Record<string, QualitySlot[]>
  timeline_start: number
  server_time: number
}
export type QualityFilters = {
  test_id?: string
  model?: string
  status?: string
  from?: number
  to?: number
}
export type QualityHistory = {
  items: QualityResult[]
  total: number
  page: number
  page_size: number
}

export function qualityBase(admin: boolean) {
  return admin ? '/api/quality-tests' : '/api/quality-test-results'
}
export async function qualityGet<T>(path: string, params?: object): Promise<T> {
  const response = await api.get<{
    success: boolean
    message: string
    data: T
  }>(path, { params })
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}
export async function qualityWrite(
  path: string,
  method: 'post' | 'put' | 'delete' | 'patch',
  data?: unknown
) {
  const response = await api[method]<{ success: boolean; message: string }>(
    path,
    data
  )
  if (!response.data.success) throw new Error(response.data.message)
}
export function useQualityTests(admin: boolean, enabled = true) {
  return useQuery({
    queryKey: ['quality-tests', admin, 'tasks'],
    queryFn: () => qualityGet<QualityTest[]>(qualityBase(admin)),
    enabled,
    refetchInterval: 30_000,
  })
}
