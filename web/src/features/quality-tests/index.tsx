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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, Clock, Plus, RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { StaticDataTable } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout/components/section-page-layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { handleServerError } from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'

import {
  qualityBase,
  qualityGet,
  qualityWrite,
  useQualityTests,
  type QualityFilters,
  type QualityHistory,
  type QualityResult,
  type QualitySlot,
  type QualityStatus,
  type QualitySummary,
  type QualityTest,
  type QualityTokenOption,
} from './api'
import { QualityJudgePanel, QualityTaskForm } from './forms'

const statusLabels: Record<QualityStatus | '', string> = {
  passed: 'Passed',
  pending: 'Pending verification',
  failed: 'Request failed',
  running: 'Testing',
  '': 'No data',
}
const statusColors: Record<QualityStatus | '', string> = {
  passed: 'bg-emerald-500',
  pending: 'bg-rose-500',
  failed: 'bg-amber-500',
  running: 'bg-blue-500',
  '': 'bg-muted',
}

function QualityStatusBadge({ status }: { status: QualityStatus | '' }) {
  const { t } = useTranslation()
  return (
    <Badge variant='outline' className='gap-1.5'>
      <span className={cn('size-2 rounded-full', statusColors[status])} />
      {t(statusLabels[status])}
    </Badge>
  )
}

function QualityTimeline({
  slots,
  start,
  onSelect,
}: {
  slots?: QualitySlot[]
  start: number
  onSelect: (slot: QualitySlot) => void
}) {
  const { t } = useTranslation()
  const series =
    slots ??
    Array.from({ length: 48 }, (_, index) => ({
      start: start + index * 1800,
      status: '' as const,
      count: 0,
    }))
  return (
    <div className='flex gap-0.5' aria-label={t('24-hour test timeline')}>
      {series.map((slot) => (
        <Tooltip key={slot.start}>
          <TooltipTrigger
            render={
              <button
                type='button'
                onClick={() => onSelect(slot)}
                aria-label={`${new Date(slot.start * 1000).toLocaleString()} · ${t(statusLabels[slot.status])} · ${slot.count}`}
                className={cn(
                  'h-8 min-w-0 flex-1 rounded-sm outline-offset-2 hover:opacity-75 focus-visible:outline-2',
                  statusColors[slot.status]
                )}
              />
            }
          />
          <TooltipContent>
            {new Date(slot.start * 1000).toLocaleString()}
            <br />
            {t(statusLabels[slot.status])} ·{' '}
            {t('{{count}} test records', { count: slot.count })}
          </TooltipContent>
        </Tooltip>
      ))}
    </div>
  )
}

function ResultDetails({ result }: { result: QualityResult }) {
  const { t } = useTranslation()
  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center gap-3'>
        <QualityStatusBadge status={result.status} />
        <span>
          {result.model} · {result.group}
        </span>
        <span className='text-muted-foreground'>
          {new Date(result.started_at * 1000).toLocaleString()} ·{' '}
          {(result.duration_ms / 1000).toFixed(1)}s
        </span>
      </div>
      {[
        ['Test question', result.prompt],
        ['Reference answer', result.expected_answer],
        ['Actual answer', result.answer],
        ['Judge reason', result.reason],
      ].map(([label, value]) => (
        <div key={label}>
          <h4 className='mb-2 text-sm font-medium'>{t(label)}</h4>
          <pre className='bg-muted rounded-lg p-3 font-sans text-sm break-words whitespace-pre-wrap'>
            {value || '—'}
          </pre>
        </div>
      ))}
      {result.failure_stage && (
        <p className='text-destructive text-sm'>
          {t('Failure stage')}:{' '}
          {t(
            result.failure_stage === 'judge' ? 'Judge model' : 'Test execution'
          )}
          {result.error && ` · ${result.error}`}
        </p>
      )}
      {result.request_id && (
        <p className='text-muted-foreground text-xs break-all'>
          {t('Request ID')}: {result.request_id}
          {result.judge_request_id && ` / ${result.judge_request_id}`}
        </p>
      )}
    </div>
  )
}

function QualityContent({ admin }: { admin: boolean }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const base = qualityBase(admin)
  const tasks = useQualityTests(admin)
  const [filters, setFilters] = useState<QualityFilters>({})
  const [page, setPage] = useState(1)
  const [editor, setEditor] = useState<QualityTest | 'new' | null>(null)
  const [deleting, setDeleting] = useState<QualityTest | null>(null)
  const [detail, setDetail] = useState<QualityResult | null>(null)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const options = useQuery({
    queryKey: ['quality-tests', 'options'],
    queryFn: () =>
      qualityGet<QualityTokenOption[]>('/api/quality-tests/options'),
    enabled: admin,
  })
  const summary = useQuery({
    queryKey: ['quality-tests', admin, 'summary', filters],
    queryFn: () => qualityGet<QualitySummary>(`${base}/summary`, filters),
    refetchInterval: 15_000,
  })
  const history = useQuery({
    queryKey: ['quality-tests', admin, 'results', filters, page],
    queryFn: () =>
      qualityGet<QualityHistory>(`${base}/results`, {
        ...filters,
        page,
        page_size: 20,
      }),
    refetchInterval: 15_000,
  })
  const action = useMutation({
    mutationFn: ({ id, remove }: { id: number; remove: boolean }) =>
      qualityWrite(
        `${base}/${id}${remove ? '' : '/run'}`,
        remove ? 'delete' : 'post'
      ),
    onSuccess: () => {
      setDeleting(null)
      void queryClient.invalidateQueries({ queryKey: ['quality-tests'] })
    },
    onError: (error) => handleServerError(error),
  })
  const flags = useMutation({
    mutationFn: ({
      id,
      patch,
    }: {
      id: number
      patch: { enabled?: boolean; public?: boolean }
    }) => qualityWrite(`${base}/${id}`, 'patch', patch),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['quality-tests'] })
    },
    onError: (error) => handleServerError(error),
  })
  const counts = summary.data?.counts
  const answered = (counts?.passed ?? 0) + (counts?.pending ?? 0)
  const next = summary.data?.next_run_at ?? 0
  const seconds = Math.max(0, next - Math.floor(now / 1000))
  const countdown = `${Math.floor(seconds / 3600)
    .toString()
    .padStart(2, '0')}:${Math.floor((seconds % 3600) / 60)
    .toString()
    .padStart(2, '0')}:${(seconds % 60).toString().padStart(2, '0')}`
  const visible = (tasks.data ?? []).filter(
    (task) =>
      (!filters.test_id || String(task.id) === filters.test_id) &&
      (!filters.model || task.model === filters.model)
  )
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['quality-tests'] })
  }
  const filter = (patch: Partial<QualityFilters>) => {
    setFilters((old) => ({ ...old, ...patch }))
    setPage(1)
  }
  const models = [...new Set((tasks.data ?? []).map((task) => task.model))]
  if (
    tasks.isError ||
    summary.isError ||
    history.isError ||
    (admin && options.isError)
  ) {
    return <ErrorState onRetry={refresh} />
  }
  return (
    <div className='space-y-6'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-muted-foreground text-sm'>
          {t('Real model responses, reference answers and traceable verdicts.')}
        </p>
        <div className='flex gap-2'>
          <Button variant='outline' onClick={refresh}>
            <RefreshCw className='size-4' />
            {t('Refresh records')}
          </Button>
          {admin && (
            <Button
              onClick={() => setEditor('new')}
              disabled={options.isPending}
            >
              <Plus className='size-4' />
              {t('Add quality test')}
            </Button>
          )}
        </div>
      </div>
      {admin && options.data && <QualityJudgePanel options={options.data} />}
      <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        <Card>
          <CardHeader>
            <CardTitle className='text-muted-foreground text-sm'>
              {t('Answer pass rate')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className='text-3xl font-semibold text-emerald-600'>
              {answered
                ? `${(((counts?.passed ?? 0) / answered) * 100).toFixed(1)}%`
                : '—'}
            </p>
            <p className='text-muted-foreground mt-2 text-xs'>
              {counts?.passed ?? 0} / {answered} · {t('Valid answers')}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className='text-muted-foreground text-sm'>
              {t('Completed tests')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className='text-3xl font-semibold'>
              {answered + (counts?.failed ?? 0)}
            </p>
            <p className='text-muted-foreground mt-2 text-xs'>
              {filters.from || filters.to
                ? t('Selected time range')
                : t('Last 24 hours')}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle className='text-muted-foreground text-sm'>
              {t('Anomalous records')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className='text-3xl font-semibold'>
              {(counts?.pending ?? 0) + (counts?.failed ?? 0)}
            </p>
            <p className='text-muted-foreground mt-2 text-xs'>
              {t('Pending verification')} {counts?.pending ?? 0} ·{' '}
              {t('Request failed')} {counts?.failed ?? 0}
            </p>
          </CardContent>
        </Card>
        <Card className='bg-emerald-950 text-white'>
          <CardHeader>
            <CardTitle className='flex items-center gap-2 text-sm text-emerald-100'>
              <Clock className='size-4' />
              {t('Next test')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className='font-mono text-3xl'>
              {next ? countdown : t('Paused')}
            </p>
            <p className='mt-2 text-xs text-emerald-100'>
              {next
                ? new Date(next * 1000).toLocaleString()
                : t('No scheduled tests')}
            </p>
          </CardContent>
        </Card>
      </div>
      {tasks.isPending && <p role='status'>{t('Loading...')}</p>}
      {!tasks.isPending && !tasks.data?.length && (
        <EmptyState icon={Activity} title={t('No quality tests available')} />
      )}
      <div className='flex flex-wrap items-end gap-3'>
        <div className='space-y-1'>
          <Label htmlFor='quality-task-filter'>{t('Test')}</Label>
          <NativeSelect
            id='quality-task-filter'
            value={filters.test_id ?? ''}
            onChange={(event) =>
              filter({ test_id: event.target.value || undefined })
            }
          >
            <option value=''>{t('All')}</option>
            {tasks.data?.map((task) => (
              <option key={task.id} value={task.id}>
                {task.name}
              </option>
            ))}
          </NativeSelect>
        </div>
        <div className='space-y-1'>
          <Label htmlFor='quality-model-filter'>{t('Model')}</Label>
          <NativeSelect
            id='quality-model-filter'
            value={filters.model ?? ''}
            onChange={(event) =>
              filter({ model: event.target.value || undefined })
            }
          >
            <option value=''>{t('All')}</option>
            {models.map((model) => (
              <option key={model} value={model}>
                {model}
              </option>
            ))}
          </NativeSelect>
        </div>
        <div className='space-y-1'>
          <Label htmlFor='quality-status-filter'>{t('Status')}</Label>
          <NativeSelect
            id='quality-status-filter'
            value={filters.status ?? ''}
            onChange={(event) =>
              filter({ status: event.target.value || undefined })
            }
          >
            <option value=''>{t('All')}</option>
            {Object.entries(statusLabels)
              .filter(([status]) => status)
              .map(([status, label]) => (
                <option key={status} value={status}>
                  {t(label)}
                </option>
              ))}
          </NativeSelect>
        </div>
        <div className='space-y-1'>
          <Label htmlFor='quality-from'>{t('Start time')}</Label>
          <Input
            key={`from-${filters.from ?? ''}`}
            id='quality-from'
            type='datetime-local'
            defaultValue={filters.from ? localDateTime(filters.from) : ''}
            onBlur={(event) =>
              filter({
                from: event.target.value
                  ? Math.floor(new Date(event.target.value).getTime() / 1000)
                  : undefined,
              })
            }
          />
        </div>
        <div className='space-y-1'>
          <Label htmlFor='quality-to'>{t('End time')}</Label>
          <Input
            key={`to-${filters.to ?? ''}`}
            id='quality-to'
            type='datetime-local'
            defaultValue={filters.to ? localDateTime(filters.to) : ''}
            onBlur={(event) =>
              filter({
                to: event.target.value
                  ? Math.floor(new Date(event.target.value).getTime() / 1000)
                  : undefined,
              })
            }
          />
        </div>
        <Button
          variant='ghost'
          onClick={() => {
            setFilters({})
            setPage(1)
          }}
        >
          {t('Reset filters')}
        </Button>
      </div>
      {visible.map((task) => {
        const slots = summary.data?.timeline[String(task.id)]
        const latest = task.latest_status ?? ''
        return (
          <Card key={task.id}>
            <CardHeader>
              <div className='flex flex-wrap items-center justify-between gap-3'>
                <div>
                  <CardTitle>
                    {task.name} · {task.model}
                  </CardTitle>
                  <p className='text-muted-foreground mt-2 text-xs'>
                    {task.group} ·{' '}
                    {t('Every {{minutes}} minutes', {
                      minutes: task.interval_minutes,
                    })}{' '}
                    · {task.enabled ? t('Enabled') : t('Paused')}
                  </p>
                </div>
                <div className='flex flex-wrap items-center gap-2'>
                  <QualityStatusBadge status={latest} />
                  {admin && (
                    <>
                      <Button
                        size='sm'
                        variant='outline'
                        disabled={
                          action.isPending ||
                          task.requested_at > 0 ||
                          latest === 'running'
                        }
                        onClick={() =>
                          action.mutate({ id: task.id, remove: false })
                        }
                      >
                        {t('Test now')}
                      </Button>
                      <Button
                        size='sm'
                        variant='ghost'
                        onClick={() => setEditor(task)}
                      >
                        {t('Edit')}
                      </Button>
                      <Button
                        size='sm'
                        variant='ghost'
                        onClick={() => setDeleting(task)}
                      >
                        {t('Delete')}
                      </Button>
                    </>
                  )}
                </div>
              </div>
            </CardHeader>
            <CardContent className='space-y-3'>
              {admin && (
                <div className='flex flex-wrap gap-4'>
                  <Label className='flex items-center gap-2'>
                    <Switch
                      checked={task.enabled}
                      disabled={flags.isPending}
                      onCheckedChange={(enabled) =>
                        flags.mutate({ id: task.id, patch: { enabled } })
                      }
                    />
                    {t('Enable scheduled tests')}
                  </Label>
                  <Label className='flex items-center gap-2'>
                    <Switch
                      checked={task.public}
                      disabled={flags.isPending}
                      onCheckedChange={(isPublic) =>
                        flags.mutate({
                          id: task.id,
                          patch: { public: isPublic },
                        })
                      }
                    />
                    {t('Show results to signed-in users')}
                  </Label>
                </div>
              )}

              <p className='text-muted-foreground text-xs'>
                {t('24-hour test timeline')} · {t('48 half-hour slots')}
              </p>
              <QualityTimeline
                slots={slots}
                start={summary.data?.timeline_start ?? 0}
                onSelect={(slot) =>
                  filter({
                    test_id: String(task.id),
                    from: slot.start,
                    to: slot.start + 1800,
                    status: undefined,
                  })
                }
              />
              <div className='text-muted-foreground flex justify-between text-xs'>
                <span>{t('24 hours ago')}</span>
                <span>{t('Now')}</span>
              </div>
            </CardContent>
          </Card>
        )
      })}
      <div className='flex flex-wrap gap-3'>
        {Object.keys(statusLabels).map((status) => (
          <QualityStatusBadge
            key={status}
            status={status as QualityStatus | ''}
          />
        ))}
      </div>
      <div className='space-y-3'>
        <h3 className='font-semibold'>{t('Test history')}</h3>
        <StaticDataTable
          data={history.data?.items ?? []}
          getRowKey={(row) => row.id}
          emptyContent={t('No results')}
          columns={[
            {
              id: 'time',
              header: t('Time'),
              cell: (row) => new Date(row.started_at * 1000).toLocaleString(),
            },
            {
              id: 'test',
              header: t('Test'),
              cell: (row) => (
                <div>
                  {row.name}
                  <p className='text-muted-foreground text-xs'>
                    {row.model} · {row.group}
                  </p>
                </div>
              ),
            },
            {
              id: 'status',
              header: t('Status'),
              cell: (row) => <QualityStatusBadge status={row.status} />,
            },
            {
              id: 'details',
              header: t('Details'),
              cell: (row) => (
                <Button
                  size='sm'
                  variant='ghost'
                  onClick={() => setDetail(row)}
                >
                  {t('View result')}
                </Button>
              ),
            },
          ]}
        />
        <div className='flex items-center justify-end gap-3'>
          <span className='text-muted-foreground text-sm'>
            {t('{{count}} test records', { count: history.data?.total ?? 0 })} ·{' '}
            {page}
          </span>
          <Button
            variant='outline'
            disabled={page <= 1 || history.isFetching}
            onClick={() => setPage((value) => value - 1)}
          >
            {t('Previous')}
          </Button>
          <Button
            variant='outline'
            disabled={
              page * 20 >= (history.data?.total ?? 0) || history.isFetching
            }
            onClick={() => setPage((value) => value + 1)}
          >
            {t('Next')}
          </Button>
        </div>
      </div>
      <Dialog
        open={editor !== null}
        onOpenChange={(open) => {
          if (!open) setEditor(null)
        }}
      >
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>
              {t(editor === 'new' ? 'Add quality test' : 'Edit quality test')}
            </DialogTitle>
            <DialogDescription>
              {t('Test one question against one model and group.')}
            </DialogDescription>
          </DialogHeader>
          {editor !== null && options.data && (
            <QualityTaskForm
              key={editor === 'new' ? 'new' : editor.id}
              task={editor === 'new' ? undefined : editor}
              options={options.data}
              onClose={() => setEditor(null)}
            />
          )}
        </DialogContent>
      </Dialog>
      <Dialog
        open={detail !== null}
        onOpenChange={(open) => {
          if (!open) setDetail(null)
        }}
      >
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>{t('Test result')}</DialogTitle>
            <DialogDescription>{detail?.name}</DialogDescription>
          </DialogHeader>
          {detail && <ResultDetails result={detail} />}
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
        title={t('Delete quality test')}
        desc={t(
          'Stop this test and hide its results. Historical records will be retained.'
        )}
        destructive
        isLoading={action.isPending}
        handleConfirm={() => {
          if (deleting) action.mutate({ id: deleting.id, remove: true })
        }}
      />
    </div>
  )
}

function localDateTime(timestamp: number) {
  const date = new Date(timestamp * 1000)
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16)
}

export function QualityTestSettings() {
  return <QualityContent admin />
}
export function QualityTestsPage() {
  const { t } = useTranslation()
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Model quality tests')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='p-3 sm:p-4'>
          <QualityContent admin={false} />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
