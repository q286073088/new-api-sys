import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { SectionPageLayout } from '@/components/layout'
import { EmptyState } from '@/components/empty-state'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { formatTimestampToDate } from '@/lib/format'
import { getAdminInvoices, reviewInvoice } from './api'
import type { InvoiceApplication } from '@/features/invoices/api'
import { invoiceFileUrl } from '@/features/invoices/api'

const money = (cents: number) => (cents / 100).toFixed(2)
const invoiceStatuses = {
  pending: { label: '审核中', variant: 'warning' },
  approved: { label: '已通过', variant: 'success' },
  rejected: { label: '已拒绝', variant: 'danger' },
} as const satisfies Record<InvoiceApplication['status'], { label: string; variant: StatusVariant }>

export function InvoiceReview() {
  const [items, setItems] = useState<InvoiceApplication[]>([])
  const [files, setFiles] = useState<Record<number, File | undefined>>({})
  const [notes, setNotes] = useState<Record<number, string>>({})
  const [taxPaid, setTaxPaid] = useState<Record<number, boolean>>({})
  const [saving, setSaving] = useState<number | null>(null)
  const [loading, setLoading] = useState(true)
  const refresh = async () => {
    setLoading(true)
    try {
      const result = await getAdminInvoices()
      setItems(result.items ?? [])
    } catch {
      toast.error('加载发票信息失败')
    } finally {
      setLoading(false)
    }
  }
  useEffect(() => { void refresh() }, [])
  const action = async (item: InvoiceApplication, status: string) => {
    setSaving(item.id)
    try {
      await reviewInvoice(item.id, status, notes[item.id] ?? '', taxPaid[item.id] ?? false, files[item.id])
      toast.success('发票审核完成')
      await refresh()
    } catch {
      toast.error('发票审核失败')
    } finally {
      setSaving(null)
    }
  }
  return <SectionPageLayout fixedContent>
    <SectionPageLayout.Title>发票审核</SectionPageLayout.Title>
    <SectionPageLayout.Content><div className='space-y-3 overflow-auto'>
      {loading ? <div className='text-muted-foreground flex min-h-40 items-center justify-center text-sm'>加载中...</div> : items.length === 0 ? <EmptyState title='暂无发票申请' description='当前没有待审核或已处理的发票申请。' bordered /> : items.map(item => <Card key={item.id} size='sm'><CardContent className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='text-sm font-medium'>#{item.id} · 用户 ID {item.user_id} · ¥{money(item.amount_cents)}</div>
          <StatusBadge label={invoiceStatuses[item.status].label} variant={invoiceStatuses[item.status].variant} copyable={false} />
        </div>
        <div className='text-muted-foreground grid gap-1 text-sm sm:grid-cols-2'>
          <span>类型：{item.type === 'general' ? '普票' : '专票'}</span>
          <span>申请时间：{formatTimestampToDate(item.created_at)}</span>
          <span>接收邮箱：{item.email || '-'}</span>
          {item.type === 'special' && <span>额外税费（5%）：¥{money(item.extra_tax_cents)}</span>}
        </div>
        <div className='text-muted-foreground grid gap-1 text-sm sm:grid-cols-2'>
          <span>发票抬头：{item.title || '-'}</span>
          <span>税号：{item.tax_number || '-'}</span>
          {item.type === 'special' && <>
            <span>地址：{item.address || '-'}</span>
            <span>电话：{item.phone || '-'}</span>
            <span>开户行：{item.bank_name || '-'}</span>
            <span>银行账号：{item.bank_account || '-'}</span>
          </>}
        </div>
        {item.admin_note && <p className='text-muted-foreground text-sm'>审核备注：{item.admin_note}</p>}
        {item.status === 'pending' && <div className='space-y-3 border-t pt-3'>
          <Input type='file' accept='.pdf,.png,.jpg,.jpeg' onChange={e => setFiles({ ...files, [item.id]: e.target.files?.[0] })} />
          <Input placeholder='审核备注' value={notes[item.id] ?? ''} onChange={e => setNotes({ ...notes, [item.id]: e.target.value })} />
          {item.type === 'special' && <div className='flex items-start gap-2'>
            <Checkbox id={`invoice-tax-paid-${item.id}`} checked={taxPaid[item.id] ?? false} onCheckedChange={value => setTaxPaid({ ...taxPaid, [item.id]: value === true })} className='mt-0.5' />
            <Label htmlFor={`invoice-tax-paid-${item.id}`} className='text-muted-foreground font-normal'>我已确认线下收取额外 5% 税费。</Label>
          </div>}
          <div className='flex gap-2'>
            <Button size='sm' disabled={saving === item.id || !files[item.id] || (item.type === 'special' && !taxPaid[item.id])} onClick={() => void action(item, 'approved')}>通过</Button>
            <Button size='sm' variant='outline' disabled={saving === item.id} onClick={() => void action(item, 'rejected')}>拒绝</Button>
          </div>
        </div>}
        {item.status === 'approved' && <a className='text-primary text-sm underline' href={invoiceFileUrl(item.id)}>下载发票</a>}
      </CardContent></Card>)}
    </div></SectionPageLayout.Content>
  </SectionPageLayout>
}
