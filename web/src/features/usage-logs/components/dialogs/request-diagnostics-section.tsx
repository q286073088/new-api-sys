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

import { CopyButton } from '@/components/copy-button'

import type { UsageLog } from '../../data/schema'
import type { LogOtherData, RequestDiagnostics } from '../../types'
import { DetailRow, DetailSection } from './log-detail-layout'

// Legacy records store UTC ISO timestamps; new diagnostics already use Beijing time.
function beijingDiagnosticTime(value: string) {
  if (!/^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(value)) return value
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  return new Date(date.getTime() + 8 * 3600_000)
    .toISOString()
    .slice(0, 23)
    .replace('T', ' ')
}

const phaseLabels: Record<string, string> = {
  request_received: '网关收到请求',
  relay_middleware_started: '进入转发中间件',
  relay_middleware_finished: '基础中间件完成',
  performance_check_started: '性能检查开始',
  performance_check_finished: '性能检查完成',
  authentication_started: '认证开始',
  authentication_finished: '认证完成',
  rate_limit_started: '限流检查开始',
  rate_limit_finished: '限流检查完成',
  distribution_started: '模型解析与渠道分配开始',
  request_body_read_started: '请求体读取开始',
  request_body_read_finished: '请求体读取结束',
  model_request_parsed: '模型信息解析完成',
  channel_selected: '渠道选择完成',
  request_validation_started: '请求校验开始',
  request_validation_finished: '请求校验完成',
  upstream_preparation_started: '上游连接准备开始',
  upstream_request_started: '发起上游请求',
  upstream_headers_finished: '等待上游响应头结束',
}

export function RequestDiagnosticsSection(props: {
  diagnostics: RequestDiagnostics
  endReason?: string
  log?: UsageLog
  streamStatus?: LogOtherData['stream_status']
}) {
  const { t } = useTranslation()
  const data: RequestDiagnostics = {
    ...Object.fromEntries(
      Object.entries(props.diagnostics).map(([key, value]) => [
        key,
        typeof value === 'string' &&
        (key.endsWith('_at') || key.endsWith('_deadline'))
          ? beijingDiagnosticTime(value)
          : value,
      ])
    ),
    timezone: 'Asia/Shanghai (UTC+08:00)',
    recent_upstream_events: props.diagnostics.recent_upstream_events?.map(
      (event) => ({ ...event, at: beijingDiagnosticTime(event.at) })
    ),
    request_phases: props.diagnostics.request_phases?.map((phase) => ({
      ...phase,
      at: beijingDiagnosticTime(phase.at),
    })),
  }
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
    [
      '网关请求 ID（用于本地查询）',
      data.gateway_request_id || props.log?.request_id,
    ],
    ['诊断版本', data.diagnostics_version],
    ['保活成功写出次数', data.downstream_keepalive?.written_count],
    ['首次保活写出时间', data.downstream_keepalive?.first_written_at],
    ['最后保活写出时间', data.downstream_keepalive?.last_written_at],
    ['程序版本', data.gateway_version],
    ['请求路径', data.request_path],
    ['网关收到请求（北京时间）', data.gateway_received_at],
    ['渠道选择完成后的计时起点', data.request_started_at],
    ['诊断记录时间（北京时间）', data.recorded_at],
    ['流开始时间（北京时间）', data.stream_started_at],
    ['流结束时间（北京时间）', data.stream_ended_at],
    ['最后上游数据时间（北京时间）', data.last_upstream_data_at],
    ['网关关闭上游时间（北京时间）', data.upstream_body_closed_at],
    ['流读取报错时间（北京时间）', data.scanner_error_at],
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
    ['上游扫描行数', data.upstream_scanned_lines],
    ['上游注释／保活行数', data.upstream_comment_lines],
    ['上游空行数', data.upstream_blank_lines],
    ['上游其他非 data 行数', data.upstream_other_lines],
  ]
  const timings: [string, number | undefined][] = [
    ['网关全程耗时（截至记录）', data.gateway_elapsed_ms],
    ['渠道选择完成前耗时', data.before_relay_elapsed_ms],
    ['渠道选择完成后耗时', data.request_elapsed_ms],
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

  const evidence = JSON.stringify(
    {
      log_id: props.log?.id,
      gateway_request_id: props.log?.request_id || data.gateway_request_id,
      upstream_request_id:
        props.log?.upstream_request_id || data.upstream_request_id,
      channel_id: props.log?.channel,
      model: props.log?.model_name,
      group: props.log?.group,
      log_created_at: props.log?.created_at
        ? beijingDiagnosticTime(
            new Date(props.log.created_at * 1000).toISOString()
          )
        : undefined,
      timezone: 'Asia/Shanghai (UTC+08:00)',
      error: props.log?.content,
      charged_quota: props.log?.quota,
      prompt_tokens: props.log?.prompt_tokens,
      completion_tokens: props.log?.completion_tokens,
      stream_status: props.streamStatus,
      diagnostics: {
        ...data,
        request_phases: data.request_phases?.map((phase) => ({
          ...phase,
          label: phaseLabels[phase.name] ?? phase.name,
        })),
      },
    },
    null,
    2
  )
  const flags: [string, boolean | undefined][] = [
    ['收到 usage 字段（不代表可结算）', data.usage_event_seen],
    ['收到协议结束事件', data.terminal_event_seen],
    ['未取得可用计费信息', data.missing_billable_usage],
    ['流读取错误发生于网关清理之后', data.scanner_error_after_cleanup],
  ]
  return (
    <DetailSection label={t('Request diagnostics')}>
      <div className='mb-3 flex items-center justify-between gap-2'>
        <p className='text-muted-foreground text-xs'>
          复制后可直接提供给排查人员；本地日志请使用网关请求 ID 查询。
        </p>
        <CopyButton
          value={evidence}
          size='sm'
          variant='outline'
          aria-label='复制完整诊断'
        >
          复制完整诊断
        </CopyButton>
      </div>
      <p className='text-muted-foreground mb-3 text-xs'>
        以下时间均为北京时间（UTC+08:00），耗时使用单调时钟计算。
      </p>
      {!!data.request_phases?.length && (
        <div className='mb-3 space-y-2'>
          <p className='text-sm font-medium'>请求阶段时间线</p>
          {data.request_phases.map((phase, index) => (
            <div key={index} className='text-xs'>
              <p>{phaseLabels[phase.name] ?? phase.name}</p>
              <p className='text-muted-foreground font-mono'>
                {phase.at} · 自进入 {phase.elapsed_ms} 毫秒 · 距上一阶段{' '}
                {phase.since_previous_ms} 毫秒
              </p>
            </div>
          ))}
          {data.request_phases_truncated && (
            <p className='text-muted-foreground text-xs'>
              阶段记录已达到 64 条上限。
            </p>
          )}
        </div>
      )}
      {flags.map(([label, value]) =>
        value !== undefined ? (
          <DetailRow key={label} label={label} value={value ? '是' : '否'} />
        ) : null
      )}
      {explanation && (
        <p className='text-muted-foreground mb-3 text-xs'>{explanation}</p>
      )}
      {fields.map(([label, value]) =>
        value !== undefined && value !== '' ? (
          <DetailRow key={label} label={label} value={value} mono />
        ) : null
      )}
      {!!data.recent_upstream_events?.length && (
        <div className='my-3 space-y-1'>
          <p className='text-muted-foreground text-xs'>
            最近上游事件（最多 8 条，不含正文；北京时间）
          </p>
          {data.recent_upstream_events.map((event, index) => (
            <p key={index} className='font-mono text-xs break-all'>
              {event.at} · {event.type} · {event.bytes} 字节
            </p>
          ))}
        </div>
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
