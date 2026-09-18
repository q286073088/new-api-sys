import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { SectionPageLayout } from '@/components/layout'
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
  pending: { label: 'Under review', variant: 'warning' },
  approved: { label: 'Approved', variant: 'success' },
  rejected: { label: 'Rejected', variant: 'danger' },
} as const satisfies Record<InvoiceApplication['status'], { label: string; variant: StatusVariant }>

export function InvoiceReview() {
  const { t } = useTranslation()
  const [items, setItems] = useState<InvoiceApplication[]>([])
  const [files, setFiles] = useState<Record<number, File | undefined>>({})
  const [notes, setNotes] = useState<Record<number, string>>({})
  const [taxPaid, setTaxPaid] = useState<Record<number, boolean>>({})
  const [saving, setSaving] = useState<number | null>(null)
  const refresh = async () => { try { setItems((await getAdminInvoices()).items) } catch { toast.error(t('Failed to load invoice information')) } }
  useEffect(() => { void refresh() }, [])
  const action = async (item: InvoiceApplication, status: string) => {
    setSaving(item.id)
    try {
      await reviewInvoice(item.id, status, notes[item.id] ?? '', taxPaid[item.id] ?? false, files[item.id])
      toast.success(t('Invoice reviewed'))
      await refresh()
    } catch {
      toast.error(t('Unable to review invoice'))
    } finally {
      setSaving(null)
    }
  }
  return <SectionPageLayout fixedContent>
    <SectionPageLayout.Title>{t('Invoice review')}</SectionPageLayout.Title>
    <SectionPageLayout.Content><div className='space-y-3 overflow-auto'>
      {items.map(item => <Card key={item.id} size='sm'><CardContent className='space-y-3'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='text-sm font-medium'>#{item.id} · {t('User ID')} {item.user_id} · ¥{money(item.amount_cents)}</div>
          <StatusBadge label={t(invoiceStatuses[item.status].label)} variant={invoiceStatuses[item.status].variant} copyable={false} />
        </div>
        <div className='text-muted-foreground grid gap-1 text-sm sm:grid-cols-2'>
          <span>{t('Type')}: {item.type === 'general' ? t('General') : t('Special')}</span>
          <span>{t('Applied at')}: {formatTimestampToDate(item.created_at)}</span>
          <span>{t('Receiving email')}: {item.email || '-'}</span>
          {item.type === 'special' && <span>{t('Extra tax (5%)')}: ¥{money(item.extra_tax_cents)}</span>}
        </div>
        <div className='text-muted-foreground grid gap-1 text-sm sm:grid-cols-2'>
          <span>{t('Invoice title')}: {item.title || '-'}</span>
          <span>{t('Tax number')}: {item.tax_number || '-'}</span>
          {item.type === 'special' && <>
            <span>{t('Address')}: {item.address || '-'}</span>
            <span>{t('Phone')}: {item.phone || '-'}</span>
            <span>{t('Bank name')}: {item.bank_name || '-'}</span>
            <span>{t('Bank account')}: {item.bank_account || '-'}</span>
          </>}
        </div>
        {item.admin_note && <p className='text-muted-foreground text-sm'>{t('Review note')}: {item.admin_note}</p>}
        {item.status === 'pending' && <div className='space-y-3 border-t pt-3'>
          <Input type='file' accept='.pdf,.png,.jpg,.jpeg' onChange={e => setFiles({ ...files, [item.id]: e.target.files?.[0] })} />
          <Input placeholder={t('Review note')} value={notes[item.id] ?? ''} onChange={e => setNotes({ ...notes, [item.id]: e.target.value })} />
          {item.type === 'special' && <div className='flex items-start gap-2'>
            <Checkbox id={`invoice-tax-paid-${item.id}`} checked={taxPaid[item.id] ?? false} onCheckedChange={value => setTaxPaid({ ...taxPaid, [item.id]: value === true })} className='mt-0.5' />
            <Label htmlFor={`invoice-tax-paid-${item.id}`} className='text-muted-foreground font-normal'>{t('I confirm the 5% extra tax has been collected offline.')}</Label>
          </div>}
          <div className='flex gap-2'>
            <Button size='sm' disabled={saving === item.id || !files[item.id] || (item.type === 'special' && !taxPaid[item.id])} onClick={() => void action(item, 'approved')}>{t('Approve')}</Button>
            <Button size='sm' variant='outline' disabled={saving === item.id} onClick={() => void action(item, 'rejected')}>{t('Reject')}</Button>
          </div>
        </div>}
        {item.status === 'approved' && <a className='text-primary text-sm underline' href={invoiceFileUrl(item.id)}>{t('Download invoice')}</a>}
      </CardContent></Card>)}
    </div></SectionPageLayout.Content>
  </SectionPageLayout>
}
