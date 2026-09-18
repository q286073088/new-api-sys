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
import { useTranslation } from 'react-i18next'

import type { RequestDiagnostics } from '../../types'
import { DetailRow, DetailSection } from './log-detail-layout'

export function RequestDiagnosticsSection(props: {
  diagnostics: RequestDiagnostics
  endReason?: string
}) {
  const { t } = useTranslation()
  const data = props.diagnostics
  let explanation: string | undefined
  if (data.downstream_write_error_kind === 'timeout') {
    explanation = t(
      'The gateway socket write timed out. This does not establish that the user canceled the request.'
    )
  } else if (data.client_context_error === 'context deadline exceeded') {
    explanation = t('The downstream request deadline expired.')
  } else if (
    props.endReason === 'client_gone' ||
    data.client_context_error === 'context canceled'
  ) {
    explanation = t(
      'The downstream request context was canceled. Socket errors and client or proxy logs are needed to identify the cause; this alone does not indicate a manual cancellation.'
    )
  } else if (props.endReason === 'timeout') {
    explanation = t('No upstream data arrived before the stream idle timeout.')
  }

  const fields: [string, string | number | undefined][] = [
    [t('Node'), data.node_name],
    [t('Request host'), data.request_host],
    [t('Cloudflare Ray ID'), data.cloudflare_ray],
    [t('Request attempt'), data.attempt_number],
    [t('User Agent'), data.client_user_agent],
    [t('Client HTTP protocol'), data.client_protocol],
    [t('Client context error'), data.client_context_error],
    [t('Downstream read error'), data.downstream_read_error],
    [t('Downstream read failure time'), data.downstream_read_error_at],
    [t('Downstream write error'), data.downstream_write_error],
    [t('Downstream write failure time'), data.downstream_write_error_at],
    [t('Downstream write deadline'), data.downstream_write_deadline],
    [t('Gateway connection close time'), data.gateway_connection_closed_at],
    [t('Client deadline'), data.client_deadline],
    [t('Request body size (bytes)'), data.request_body_bytes],
    [t('Estimated input tokens'), data.estimated_input_tokens],
    [t('Downstream HTTP status'), data.downstream_status],
    [
      t('Response headers written'),
      data.downstream_headers_written === undefined
        ? undefined
        : String(data.downstream_headers_written),
    ],
    [t('Bytes sent to client'), data.downstream_written_bytes],
    [t('Upstream host'), data.upstream_host],
    [t('Upstream HTTP status'), data.upstream_status],
    [t('Upstream HTTP protocol'), data.upstream_protocol],
    [t('Upstream Request ID'), data.upstream_request_id],
    [t('Upstream error kind'), data.upstream_error_kind],
    [t('Upstream request error'), data.upstream_error],
    [t('Upstream read error'), data.upstream_read_error],
    [t('Upstream events received'), data.received_events],
  ]
  const timings: [string, number | undefined][] = [
    [t('Total request time'), data.request_elapsed_ms],
    [t('Response header wait'), data.headers_elapsed_ms],
    [t('Stream duration'), data.stream_elapsed_ms],
    [t('First stream event wait'), data.first_event_elapsed_ms],
    [t('Time since last upstream data'), data.last_upstream_activity_ms],
  ]
  const timeouts: [string, number | undefined][] = [
    [t('Total upstream timeout'), data.relay_timeout_seconds],
    [t('Stream idle timeout'), data.stream_idle_timeout_seconds],
    [t('Client write timeout'), data.client_write_timeout_seconds],
  ]

  return (
    <DetailSection label={t('Request diagnostics')}>
      {explanation && (
        <p className='text-muted-foreground mb-3 text-xs'>{explanation}</p>
      )}
      {fields.map(([label, value]) =>
        value !== undefined && value !== '' ? (
          <DetailRow key={label} label={label} value={value} mono />
        ) : null
      )}
      {timings.map(([label, value]) =>
        value !== undefined ? (
          <DetailRow
            key={label}
            label={label}
            value={`${value} ${t('ms')}`}
            mono
          />
        ) : null
      )}
      {timeouts.map(([label, value]) =>
        value !== undefined ? (
          <DetailRow
            key={label}
            label={label}
            value={value === 0 ? t('Unlimited') : `${value} ${t('seconds')}`}
            mono
          />
        ) : null
      )}
      {data.ping_enabled !== undefined && (
        <DetailRow
          label={t('Stream keepalive')}
          value={data.ping_enabled ? t('Enabled') : t('Disabled')}
        />
      )}
      {data.ping_enabled && data.ping_interval_seconds !== undefined && (
        <DetailRow
          label={t('Keepalive interval')}
          value={`${data.ping_interval_seconds} ${t('seconds')}`}
        />
      )}
    </DetailSection>
  )
}
