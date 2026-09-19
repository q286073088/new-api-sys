import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'

import { DataTablePagination } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getSelf } from '@/lib/api'
import { formatTimestampToDate } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import {
  createInvoice,
  getInvoiceSummary,
  getInvoices,
  type InvoiceApplication,
} from './api'
import { InvoiceDownloadButton } from './invoice-download-button'

const money = (cents: number) => (cents / 100).toFixed(2)

const invoiceStatuses = {
  pending: { label: '审核中', variant: 'warning' },
  approved: { label: '已通过', variant: 'success' },
  rejected: { label: '已拒绝', variant: 'danger' },
} as const satisfies Record<
  InvoiceApplication['status'],
  { label: string; variant: StatusVariant }
>

export function Invoices() {
  const userId = useAuthStore((state) => state.auth.user?.id)
  const queryClient = useQueryClient()
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 20 })
  const summaryQuery = useQuery({
    queryKey: ['invoice-summary', userId],
    queryFn: getInvoiceSummary,
  })
  const summary = summaryQuery.data?.summary
  const allowed = summaryQuery.data?.settings.allowed ?? false
  const invoicesQuery = useQuery({
    queryKey: ['invoices', userId, pagination],
    queryFn: () => getInvoices(pagination.pageIndex + 1, pagination.pageSize),
    enabled: allowed,
  })
  const items = invoicesQuery.data?.items ?? []
  const table = useReactTable<InvoiceApplication>({
    columns: [],
    data: items,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    rowCount: invoicesQuery.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: setPagination,
  })
  const [type, setType] = useState<'general' | 'special'>('general')
  const [amount, setAmount] = useState('')
  const [title, setTitle] = useState('')
  const [taxNumber, setTaxNumber] = useState('')
  const [address, setAddress] = useState('')
  const [phone, setPhone] = useState('')
  const [bankName, setBankName] = useState('')
  const [bankAccount, setBankAccount] = useState('')
  const [email, setEmail] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    let active = true
    void getSelf()
      .then((self) => {
        if (active) setEmail((current) => current || self.data?.email || '')
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [userId])

  const submit = async () => {
    const amountCents = Math.round(Number(amount) * 100)
    if (!Number.isFinite(amountCents) || amountCents <= 0 || !title.trim()) {
      toast.error('请输入有效的金额和发票抬头')
      return
    }
    setSubmitting(true)
    try {
      await createInvoice({
        type,
        amount_cents: amountCents,
        title,
        tax_number: taxNumber,
        address,
        phone,
        bank_name: bankName,
        bank_account: bankAccount,
        email,
      })
      toast.success('开票申请已提交')
      setAmount('')
      setPagination((current) => ({ ...current, pageIndex: 0 }))
      await Promise.all([
        summaryQuery.refetch(),
        queryClient.invalidateQueries({ queryKey: ['invoices', userId] }),
      ])
    } catch {
      toast.error('提交开票申请失败')
    } finally {
      setSubmitting(false)
    }
  }

  if (!summary) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>开票中心</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          {summaryQuery.isError ? (
            <ErrorState onRetry={() => void summaryQuery.refetch()} />
          ) : (
            <p className='text-muted-foreground text-sm'>加载中...</p>
          )}
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  if (!allowed) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>开票中心</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <p className='text-muted-foreground text-sm'>
            当前账号未开通开票服务。
          </p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  const summaryCards = [
    ['累计充值金额', summary.topup_amount_cents],
    ['已开票金额', summary.invoiced_amount_cents],
    ['审核中金额', summary.pending_amount_cents],
    ['可开票金额', summary.available_amount_cents],
  ] as const

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>开票中心</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4'>
            {summaryCards.map(([label, value]) => (
              <Card key={label} data-card-hover='false'>
                <CardHeader className='pb-2'>
                  <CardTitle className='text-muted-foreground text-sm font-medium'>
                    {label}
                  </CardTitle>
                </CardHeader>
                <CardContent className='text-xl font-semibold tabular-nums'>
                  ¥{money(Number(value))}
                </CardContent>
              </Card>
            ))}
          </div>

          {summary.unverified_orders > 0 && (
            <p className='text-muted-foreground bg-muted/40 rounded-lg px-3 py-2 text-sm'>
              部分历史订单没有核验的人民币实付金额，无法开票。
            </p>
          )}

          <Card data-card-hover='false'>
            <CardHeader className='pb-3'>
              <CardTitle className='text-base'>提交开票申请</CardTitle>
            </CardHeader>
            <CardContent className='space-y-5'>
              <div className='flex flex-wrap gap-2'>
                <Button
                  type='button'
                  variant={type === 'general' ? 'default' : 'outline'}
                  onClick={() => setType('general')}
                >
                  普票（1% 税点，技术服务费）
                </Button>
                <Button
                  type='button'
                  variant={type === 'special' ? 'default' : 'outline'}
                  onClick={() => setType('special')}
                >
                  专票（6% 税点，增值税专用发票）
                </Button>
              </div>

              {type === 'special' && (
                <div className='border-destructive/30 bg-destructive/5 text-destructive rounded-lg border px-3 py-2 text-sm'>
                  专票需在审核通过前由您自行承担额外 5%
                  税点，费用线下收取，请先与客服确认收款后再提交。
                </div>
              )}

              <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
                <div className='space-y-1.5'>
                  <Label>金额（人民币）</Label>
                  <Input
                    type='number'
                    min='0.01'
                    step='0.01'
                    value={amount}
                    onChange={(e) => setAmount(e.target.value)}
                  />
                </div>
                <div className='space-y-1.5'>
                  <Label>发票抬头</Label>
                  <Input
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                  />
                </div>
                <div className='space-y-1.5'>
                  <Label>税号</Label>
                  <Input
                    value={taxNumber}
                    onChange={(e) => setTaxNumber(e.target.value)}
                  />
                </div>
                <div className='space-y-1.5'>
                  <Label>接收邮箱</Label>
                  <Input
                    type='email'
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    placeholder='不填默认为注册邮箱'
                  />
                </div>
                {type === 'special' && (
                  <>
                    <div className='space-y-1.5 md:col-span-2'>
                      <Label>注册地址</Label>
                      <Input
                        value={address}
                        onChange={(e) => setAddress(e.target.value)}
                      />
                    </div>
                    <div className='space-y-1.5'>
                      <Label>电话</Label>
                      <Input
                        value={phone}
                        onChange={(e) => setPhone(e.target.value)}
                      />
                    </div>
                    <div className='space-y-1.5'>
                      <Label>开户行</Label>
                      <Input
                        value={bankName}
                        onChange={(e) => setBankName(e.target.value)}
                      />
                    </div>
                    <div className='space-y-1.5 md:col-span-2'>
                      <Label>银行账号</Label>
                      <Input
                        value={bankAccount}
                        onChange={(e) => setBankAccount(e.target.value)}
                      />
                    </div>
                  </>
                )}
              </div>

              <div className='flex justify-end'>
                <Button onClick={() => void submit()} disabled={submitting}>
                  {submitting ? '提交中...' : '提交开票申请'}
                </Button>
              </div>
            </CardContent>
          </Card>

          <Card data-card-hover='false'>
            <CardHeader className='pb-3'>
              <CardTitle className='text-base'>开票记录</CardTitle>
            </CardHeader>
            <CardContent className='space-y-3'>
              {invoicesQuery.isError && (
                <ErrorState onRetry={() => void invoicesQuery.refetch()} />
              )}
              {invoicesQuery.isLoading && (
                <p className='text-muted-foreground text-sm'>加载中...</p>
              )}
              {!invoicesQuery.isError &&
                !invoicesQuery.isLoading &&
                items.length === 0 && (
                  <p className='text-muted-foreground py-8 text-center text-sm'>
                    暂无开票记录
                  </p>
                )}
              {items.map((item) => (
                <div
                  key={item.id}
                  className='flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3'
                >
                  <div className='min-w-0 space-y-1'>
                    <div className='text-sm font-medium'>
                      ¥{money(item.amount_cents)} ·{' '}
                      {item.type === 'general' ? '普票' : '专票'} ·{' '}
                      {formatTimestampToDate(item.created_at)}
                    </div>
                    {item.admin_note && (
                      <p className='text-muted-foreground text-xs'>
                        {item.admin_note}
                      </p>
                    )}
                  </div>
                  <div className='flex items-center gap-3'>
                    <StatusBadge
                      label={invoiceStatuses[item.status].label}
                      variant={invoiceStatuses[item.status].variant}
                      copyable={false}
                    />
                    {item.status === 'approved' && (
                      <InvoiceDownloadButton invoice={item} />
                    )}
                  </div>
                </div>
              ))}
              <DataTablePagination table={table} compact />
            </CardContent>
          </Card>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
