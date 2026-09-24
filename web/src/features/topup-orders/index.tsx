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
import { useQueryClient } from '@tanstack/react-query'
import { Search } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
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
import { formatLocalCurrencyAmount } from '@/lib/currency'
import { formatQuota, formatTimestampToDate } from '@/lib/format'

import {
  useTopupOrder,
  useTopupOrders,
  useTopupOrderSummary,
  type TopupOrder,
} from './api'

const orderStatuses: Record<
  TopupOrder['status'],
  { label: string; variant: StatusVariant }
> = {
  success: { label: 'Success', variant: 'success' },
  pending: { label: 'Pending', variant: 'warning' },
  failed: { label: 'Failed', variant: 'danger' },
  expired: { label: 'Expired', variant: 'neutral' },
}

function paymentChannel(order: TopupOrder) {
  return order.payment_provider || order.payment_method || '-'
}

function DetailField({
  label,
  children,
}: {
  label: string
  children: ReactNode
}) {
  return (
    <div className='space-y-1'>
      <p className='text-muted-foreground text-xs'>{label}</p>
      <div className='text-sm font-medium break-all'>{children || '-'}</div>
    </div>
  )
}

function OrderDetailDialog({
  orderId,
  onClose,
}: {
  orderId: number | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const detail = useTopupOrder(orderId)
  const summary = useTopupOrderSummary(detail.data?.user_id ?? null)
  const order = detail.data
  const totals = summary.data
  return (
    <Dialog open={orderId !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{t('Order details')}</DialogTitle>
          <DialogDescription>
            {t('Order information and the user lifetime recharge summary.')}
          </DialogDescription>
        </DialogHeader>
        {detail.isError ? (
          <ErrorState onRetry={() => void detail.refetch()} />
        ) : detail.isLoading || !order ? (
          <div className='text-muted-foreground py-10 text-center text-sm'>
            {t('Loading...')}
          </div>
        ) : (
          <div className='space-y-5'>
            <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-3'>
              <DetailField label={t('User')}>
                {order.username || `#${order.user_id}`}
              </DetailField>
              <DetailField label={t('User ID')}>{order.user_id}</DetailField>
              <DetailField label={t('Trade No.')}>{order.trade_no}</DetailField>
              <DetailField label={t('Created at')}>
                {formatTimestampToDate(order.create_time)}
              </DetailField>
              <DetailField label={t('Completed at')}>
                {formatTimestampToDate(order.complete_time)}
              </DetailField>
              <DetailField label={t('Status')}>
                <StatusBadge
                  label={t(orderStatuses[order.status].label)}
                  variant={orderStatuses[order.status].variant}
                  copyable={false}
                />
              </DetailField>
              <DetailField label={t('Payment channel')}>
                {paymentChannel(order)}
              </DetailField>
              <DetailField label={t('Payment method')}>
                {order.payment_method || '-'}
              </DetailField>
              <DetailField label={t('Payment amount')}>
                {formatLocalCurrencyAmount(order.money || 0, {
                  digitsLarge: 2,
                  digitsSmall: 2,
                  abbreviate: false,
                })}
              </DetailField>
              <DetailField label={t('Credited amount')}>
                {formatQuota(order.credited_quota)}
              </DetailField>
              <DetailField label={t('Gift')}>
                {order.is_gift ? t('Yes') : t('No')}
              </DetailField>
            </div>
            <div className='border-t pt-4'>
              <h4 className='mb-3 text-sm font-semibold'>
                {t('Lifetime recharge summary')}
              </h4>
              {summary.isError ? (
                <ErrorState onRetry={() => void summary.refetch()} />
              ) : summary.isLoading || !totals ? (
                <div className='text-muted-foreground py-6 text-center text-sm'>
                  {t('Loading...')}
                </div>
              ) : (
                <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-4'>
                  <DetailField label={t('Paid recharge amount')}>
                    {formatLocalCurrencyAmount(totals.total_paid_money, {
                      digitsLarge: 2,
                      digitsSmall: 2,
                      abbreviate: false,
                    })}
                  </DetailField>
                  <DetailField label={t('Total credited amount')}>
                    {formatQuota(totals.total_credited_quota)}
                  </DetailField>
                  <DetailField label={t('Gift amount')}>
                    {formatQuota(totals.gift_quota)}
                  </DetailField>
                  <DetailField label={t('Successful orders')}>
                    {totals.success_count}
                  </DetailField>
                </div>
              )}
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

export function TopupOrders() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [orderId, setOrderId] = useState<number | null>(null)
  const filters = {
    keyword: keyword || undefined,
    status: status || undefined,
    from: from ? Math.floor(new Date(from).getTime() / 1000) : undefined,
    to: to ? Math.ceil(new Date(to).getTime() / 1000) + 1 : undefined,
  }
  const query = useTopupOrders(
    pagination.pageIndex + 1,
    pagination.pageSize,
    filters
  )
  const orders = query.data?.items ?? []

  const columns: StaticDataTableColumn<TopupOrder>[] = [
    { id: 'id', header: t('ID'), cell: (order) => order.id },
    {
      id: 'username',
      header: t('User'),
      cell: (order) => (
        <div className='max-w-44 truncate'>
          <div>{order.username || `#${order.user_id}`}</div>
          {order.display_name && (
            <div className='text-muted-foreground truncate text-xs'>
              {order.display_name}
            </div>
          )}
        </div>
      ),
    },
    {
      id: 'trade_no',
      header: t('Trade No.'),
      cell: (order) => (
        <code className='block max-w-56 truncate font-mono text-xs'>
          {order.trade_no}
        </code>
      ),
    },
    {
      id: 'create_time',
      header: t('Created at'),
      cell: (order) => formatTimestampToDate(order.create_time),
    },
    {
      id: 'credited_quota',
      header: t('Credited amount'),
      cell: (order) => formatQuota(order.credited_quota),
    },
    {
      id: 'money',
      header: t('Payment amount'),
      cell: (order) =>
        formatLocalCurrencyAmount(order.money || 0, {
          digitsLarge: 2,
          digitsSmall: 2,
          abbreviate: false,
        }),
    },
    {
      id: 'payment_method',
      header: t('Payment channel'),
      cell: (order) => paymentChannel(order),
    },
    {
      id: 'status',
      header: t('Status'),
      cell: (order) => (
        <StatusBadge
          label={t(orderStatuses[order.status].label)}
          variant={orderStatuses[order.status].variant}
          copyable={false}
        />
      ),
    },
    {
      id: 'actions',
      header: t('Actions'),
      cell: (order) => (
        <Button size='sm' variant='ghost' onClick={() => setOrderId(order.id)}>
          {t('Details')}
        </Button>
      ),
    },
  ]

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Order Details')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <div className='space-y-3'>
            <div className='flex flex-wrap items-end gap-3'>
              <div className='relative min-w-56 flex-1'>
                <Search className='text-muted-foreground absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2' />
                <Input
                  className='pl-9'
                  placeholder={t('Search user, display name or trade no.')}
                  value={keyword}
                  onChange={(event) => {
                    setKeyword(event.target.value)
                    setPagination((current) => ({ ...current, pageIndex: 0 }))
                  }}
                />
              </div>
              <div className='w-36 space-y-1'>
                <Label htmlFor='topup-order-status'>{t('Status')}</Label>
                <NativeSelect
                  id='topup-order-status'
                  value={status}
                  onChange={(event) => {
                    setStatus(event.target.value)
                    setPagination((current) => ({ ...current, pageIndex: 0 }))
                  }}
                >
                  <option value=''>{t('All')}</option>
                  {Object.entries(orderStatuses).map(([value, item]) => (
                    <option key={value} value={value}>
                      {t(item.label)}
                    </option>
                  ))}
                </NativeSelect>
              </div>
              <div className='w-44 space-y-1'>
                <Label htmlFor='topup-order-from'>{t('Start time')}</Label>
                <Input
                  id='topup-order-from'
                  type='datetime-local'
                  value={from}
                  onChange={(event) => {
                    setFrom(event.target.value)
                    setPagination((current) => ({ ...current, pageIndex: 0 }))
                  }}
                />
              </div>
              <div className='w-44 space-y-1'>
                <Label htmlFor='topup-order-to'>{t('End time')}</Label>
                <Input
                  id='topup-order-to'
                  type='datetime-local'
                  value={to}
                  onChange={(event) => {
                    setTo(event.target.value)
                    setPagination((current) => ({ ...current, pageIndex: 0 }))
                  }}
                />
              </div>
              <Button
                variant='outline'
                onClick={() => {
                  setKeyword('')
                  setStatus('')
                  setFrom('')
                  setTo('')
                  setPagination({ pageIndex: 0, pageSize: pagination.pageSize })
                  void queryClient.invalidateQueries({
                    queryKey: ['topup-orders'],
                  })
                }}
              >
                {t('Reset')}
              </Button>
            </div>
            {query.isError ? (
              <ErrorState onRetry={() => void query.refetch()} />
            ) : (
              <>
                {query.isLoading ? (
                  <div className='text-muted-foreground flex h-24 items-center justify-center text-sm'>
                    {t('Loading...')}
                  </div>
                ) : orders.length === 0 ? (
                  <div className='text-muted-foreground flex h-24 items-center justify-center text-sm'>
                    {t('No recharge orders match the current filters.')}
                  </div>
                ) : (
                  <StaticDataTable
                    data={orders}
                    getRowKey={(order) => order.id}
                    columns={columns}
                  />
                )}
                <div className='flex items-center justify-end gap-3'>
                  <span className='text-muted-foreground text-sm'>
                    {t('Total:')} {query.data?.total ?? 0} ·
                    {pagination.pageIndex + 1}
                  </span>
                  <Button
                    variant='outline'
                    disabled={pagination.pageIndex === 0 || query.isFetching}
                    onClick={() =>
                      setPagination((current) => ({
                        ...current,
                        pageIndex: current.pageIndex - 1,
                      }))
                    }
                  >
                    {t('Previous')}
                  </Button>
                  <Button
                    variant='outline'
                    disabled={
                      (pagination.pageIndex + 1) * pagination.pageSize >=
                        (query.data?.total ?? 0) || query.isFetching
                    }
                    onClick={() =>
                      setPagination((current) => ({
                        ...current,
                        pageIndex: current.pageIndex + 1,
                      }))
                    }
                  >
                    {t('Next')}
                  </Button>
                </div>
              </>
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <OrderDetailDialog orderId={orderId} onClose={() => setOrderId(null)} />
    </>
  )
}
