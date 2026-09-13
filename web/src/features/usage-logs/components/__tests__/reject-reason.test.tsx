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
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test } from 'vitest'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData } from '../../types'
import { DetailsDialog } from '../dialogs/details-dialog'

const queryClients: QueryClient[] = []

function makeLog(other: LogOtherData): UsageLog {
  return {
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 5,
    content: 'request rejected',
    username: 'user',
    token_name: 'token',
    model_name: 'gpt-test',
    quota: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 0,
    is_stream: false,
    channel: 1,
    channel_name: 'channel',
    token_id: 1,
    group: 'default',
    ip: '',
    other: JSON.stringify(other),
    request_id: 'req-1',
    upstream_request_id: '',
  }
}

function renderDetails(
  isAdmin: boolean,
  other: LogOtherData = {
    admin_info: { reject_reason: 'blocked by channel policy' },
  }
): void {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const freshAt = Date.now() + 60_000
  queryClient.setQueryData(['status'], {}, { updatedAt: freshAt })
  queryClients.push(queryClient)

  render(
    <QueryClientProvider client={queryClient}>
      <DetailsDialog
        log={makeLog(other)}
        isAdmin={isAdmin}
        isRoot={false}
        open
        onOpenChange={() => undefined}
      />
    </QueryClientProvider>
  )
}

afterEach(() => {
  for (const queryClient of queryClients) {
    queryClient.clear()
  }
  queryClients.length = 0
})

describe('usage log reject reason', () => {
  test('shows the nested admin reject reason to admins', () => {
    renderDetails(true)

    expect(screen.getByText('Reject Reason')).toBeInTheDocument()
    expect(screen.getByText('blocked by channel policy')).toBeInTheDocument()
  })

  test('hides the nested admin reject reason from non-admin users', () => {
    renderDetails(false)

    expect(screen.queryByText('Reject Reason')).toBeNull()
    expect(screen.queryByText('blocked by channel policy')).toBeNull()
  })
})

const canceledStream: LogOtherData = {
  stream_status: { status: 'error', end_reason: 'client_gone' },
  admin_info: {
    request_diagnostics: {
      client_context_error: 'context canceled',
      client_user_agent: 'ExampleClient/1.0',
      upstream_request_id: 'upstream-trace-123',
      upstream_status: 200,
      headers_elapsed_ms: 1250,
      last_upstream_activity_ms: 30000,
      received_events: 0,
      downstream_written_bytes: 0,
      relay_timeout_seconds: 0,
      ping_enabled: false,
    },
  },
}

describe('administrator request diagnostics', () => {
  test('shows cancellation evidence, zero traffic and an unlimited timeout to admins', () => {
    renderDetails(true, canceledStream)
    expect(screen.getByText('Request diagnostics')).toBeInTheDocument()
    expect(screen.getByText('ExampleClient/1.0')).toBeInTheDocument()
    expect(screen.getByText('upstream-trace-123')).toBeInTheDocument()
    expect(screen.getByText('1250 ms')).toBeInTheDocument()
    expect(screen.getByText('30000 ms')).toBeInTheDocument()
    expect(
      screen.getByText('Upstream events received').nextElementSibling
    ).toHaveTextContent('0')
    expect(
      screen.getByText('Bytes sent to client').nextElementSibling
    ).toHaveTextContent('0')
    expect(
      screen.getByText('Total upstream timeout').nextElementSibling
    ).toHaveTextContent('Unlimited')
  })

  test('hides administrator connection details from users even if present in the supplied data', () => {
    renderDetails(false, canceledStream)
    expect(screen.queryByText('Request diagnostics')).not.toBeInTheDocument()
    expect(screen.queryByText('upstream-trace-123')).not.toBeInTheDocument()
    expect(screen.queryByText('ExampleClient/1.0')).not.toBeInTheDocument()
  })

  test('keeps older logs readable when no diagnostics were recorded', () => {
    renderDetails(true)
    expect(screen.queryByText('Request diagnostics')).not.toBeInTheDocument()
    expect(screen.getByText('blocked by channel policy')).toBeInTheDocument()
  })
})
