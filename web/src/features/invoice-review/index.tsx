import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { useState } from 'react'
import { toast } from 'sonner'

import { DataTablePagination } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { InvoiceApplication } from '@/features/invoices/api'
import { InvoiceDownloadButton } from '@/features/invoices/invoice-download-button'
import { formatTimestampToDate } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import { getAdminInvoices, reviewInvoice } from './api'

const money = (cents: number) => (cents / 100).toFixed(2)
const invoiceStatuses = {
  pending: { label: '审核中', variant: 'warning' },
  approved: { label: '已通过', variant: 'success' },
  rejected: { label: '已拒绝', variant: 'danger' },
} as const satisfies Record<
  InvoiceApplication['status'],
  { label: string; variant: StatusVariant }
>

export function InvoiceReview() {
  const userId = useAuthStore((state) => state.auth.user?.id)
  const queryClient = useQueryClient()
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const [status, setStatus] = useState('pending')
  const query = useQuery({
    queryKey: ['admin-invoices', userId, pagination, status],
    queryFn: () =>
      getAdminInvoices(
        pagination.pageIndex + 1,
        pagination.pageSize,
        status === 'all' ? undefined : status
      ),
  })
  const items = query.data?.items ?? []
  const table = useReactTable<InvoiceApplication>({
    columns: [],
    data: items,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: query.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: setPagination,
  })
  const [files, setFiles] = useState<Record<number, File | undefined>>({})
  const [notes, setNotes] = useState<Record<number, string>>({})
  const [taxPaid, setTaxPaid] = useState<Record<number, boolean>>({})
  const [saving, setSaving] = useState<number | null>(null)
  const action = async (item: InvoiceApplication, decision: string) => {
    setSaving(item.id)
    try {
      await reviewInvoice(
        item.id,
        decision,
        notes[item.id] ?? '',
        taxPaid[item.id] ?? false,
        files[item.id]
      )
      toast.success('发票审核完成')
      if (
        items.length === 1 &&
        pagination.pageIndex > 0 &&
        status === 'pending'
      ) {
        setPagination((current) => ({
          ...current,
          pageIndex: current.pageIndex - 1,
        }))
      }
      setFiles((current) => {
        const next = { ...current }
        delete next[item.id]
        return next
      })
      await queryClient.invalidateQueries({
        queryKey: ['admin-invoices', userId],
      })
    } catch {
      toast.error('发票审核失败')
    } finally {
      setSaving(null)
    }
  }
  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>发票审核</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='space-y-3 overflow-auto'>
          <Tabs
            value={status}
            onValueChange={(value) => {
              setStatus(value)
              setPagination((current) => ({ ...current, pageIndex: 0 }))
            }}
          >
            <TabsList aria-label='发票状态'>
              <TabsTrigger value='pending'>待审核</TabsTrigger>
              <TabsTrigger value='approved'>已通过</TabsTrigger>
              <TabsTrigger value='rejected'>已拒绝</TabsTrigger>
              <TabsTrigger value='all'>全部</TabsTrigger>
            </TabsList>
          </Tabs>
          {query.isError ? (
            <ErrorState onRetry={() => void query.refetch()} />
          ) : query.isLoading ? (
            <div className='text-muted-foreground flex min-h-40 items-center justify-center text-sm'>
              加载中...
            </div>
          ) : items.length === 0 ? (
            <EmptyState
              title='暂无发票申请'
              description='当前筛选条件下没有发票申请。'
              bordered
            />
          ) : (
            items.map((item) => (
              <Card key={item.id} size='sm'>
                <CardContent className='space-y-3'>
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <div className='text-sm font-medium'>
                      #{item.id} · 用户 ID {item.user_id} · ¥
                      {money(item.amount_cents)}
                    </div>
                    <StatusBadge
                      label={invoiceStatuses[item.status].label}
                      variant={invoiceStatuses[item.status].variant}
                      copyable={false}
                    />
                  </div>
                  <div className='text-muted-foreground grid gap-1 text-sm sm:grid-cols-2'>
                    <span>
                      类型：{item.type === 'general' ? '普票' : '专票'}
                    </span>
                    <span>
                      申请时间：{formatTimestampToDate(item.created_at)}
                    </span>
                    <span>接收邮箱：{item.email || '-'}</span>
                    {item.type === 'special' && (
                      <span>
                        额外税费（5%）：¥{money(item.extra_tax_cents)}
                      </span>
                    )}
                  </div>
                  <div className='text-muted-foreground grid gap-1 text-sm sm:grid-cols-2'>
                    <span>发票抬头：{item.title || '-'}</span>
                    <span>税号：{item.tax_number || '-'}</span>
                    {item.type === 'special' && (
                      <>
                        <span>地址：{item.address || '-'}</span>
                        <span>电话：{item.phone || '-'}</span>
                        <span>开户行：{item.bank_name || '-'}</span>
                        <span>银行账号：{item.bank_account || '-'}</span>
                      </>
                    )}
                  </div>
                  {item.admin_note && (
                    <p className='text-muted-foreground text-sm'>
                      审核备注：{item.admin_note}
                    </p>
                  )}
                  {item.status === 'pending' && (
                    <div className='space-y-3 border-t pt-3'>
                      <Input
                        type='file'
                        accept='.pdf,.png,.jpg,.jpeg'
                        onChange={(e) =>
                          setFiles({ ...files, [item.id]: e.target.files?.[0] })
                        }
                      />
                      <Input
                        placeholder='审核备注'
                        value={notes[item.id] ?? ''}
                        onChange={(e) =>
                          setNotes({ ...notes, [item.id]: e.target.value })
                        }
                      />
                      {item.type === 'special' && (
                        <div className='flex items-start gap-2'>
                          <Checkbox
                            id={`invoice-tax-paid-${item.id}`}
                            checked={taxPaid[item.id] ?? false}
                            onCheckedChange={(value) =>
                              setTaxPaid({
                                ...taxPaid,
                                [item.id]: value === true,
                              })
                            }
                            className='mt-0.5'
                          />
                          <Label
                            htmlFor={`invoice-tax-paid-${item.id}`}
                            className='text-muted-foreground font-normal'
                          >
                            我已确认线下收取额外 5% 税费。
                          </Label>
                        </div>
                      )}
                      <div className='flex gap-2'>
                        <Button
                          size='sm'
                          disabled={
                            saving !== null ||
                            !files[item.id] ||
                            (item.type === 'special' && !taxPaid[item.id])
                          }
                          onClick={() => void action(item, 'approved')}
                        >
                          通过
                        </Button>
                        <Button
                          size='sm'
                          variant='outline'
                          disabled={saving !== null}
                          onClick={() => void action(item, 'rejected')}
                        >
                          拒绝
                        </Button>
                      </div>
                    </div>
                  )}
                  {item.status === 'approved' && (
                    <InvoiceDownloadButton invoice={item} />
                  )}
                </CardContent>
              </Card>
            ))
          )}
          <DataTablePagination table={table} compact />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
