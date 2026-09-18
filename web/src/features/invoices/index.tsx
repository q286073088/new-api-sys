import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { getSelf } from '@/lib/api'
import { formatTimestampToDate } from '@/lib/format'

import { createInvoice, getInvoiceSummary, getInvoices, invoiceFileUrl, type InvoiceApplication, type InvoiceSummary } from './api'

const money = (cents: number) => (cents / 100).toFixed(2)
const invoiceStatuses = {
  pending: { label: 'Under review', variant: 'warning' },
  approved: { label: 'Approved', variant: 'success' },
  rejected: { label: 'Rejected', variant: 'danger' },
} as const satisfies Record<InvoiceApplication['status'], { label: string; variant: StatusVariant }>

export function Invoices() {
  const { t } = useTranslation()
  const [summary, setSummary] = useState<InvoiceSummary | null>(null)
  const [allowed, setAllowed] = useState(false)
  const [items, setItems] = useState<InvoiceApplication[]>([])
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

  const refresh = async () => {
    try {
      const summaryResponse = await getInvoiceSummary()
      setAllowed(summaryResponse.settings.allowed)
      const invoices = summaryResponse.settings.allowed ? await getInvoices() : { items: [] as InvoiceApplication[] }
      setSummary(summaryResponse.summary); setItems(invoices.items)
      if (!email) { const self = await getSelf(); setEmail(self.data?.email ?? '') }
    } catch { toast.error(t('Failed to load invoice information')) }
  }
  useEffect(() => { void refresh() }, [])

  const submit = async () => {
    const amountCents = Math.round(Number(amount) * 100)
    if (!Number.isFinite(amountCents) || amountCents <= 0 || !title.trim()) { toast.error(t('Enter a valid amount and invoice title')); return }
    setSubmitting(true)
    try {
      await createInvoice({ type, amount_cents: amountCents, title, tax_number: taxNumber, address, phone, bank_name: bankName, bank_account: bankAccount, email })
      toast.success(t('Invoice application submitted')); setAmount(''); await refresh()
    } catch { toast.error(t('Unable to submit invoice application')) } finally { setSubmitting(false) }
  }
  if (!summary) return <SectionPageLayout><SectionPageLayout.Title>{t('Invoice Center')}</SectionPageLayout.Title><SectionPageLayout.Content><p>{t('Loading')}</p></SectionPageLayout.Content></SectionPageLayout>
  if (!allowed) return <SectionPageLayout><SectionPageLayout.Title>{t('Invoice Center')}</SectionPageLayout.Title><SectionPageLayout.Content><p className='text-muted-foreground'>{t('Invoice service is not available for your account.')}</p></SectionPageLayout.Content></SectionPageLayout>
  return <SectionPageLayout fixedContent>
    <SectionPageLayout.Title>{t('Invoice Center')}</SectionPageLayout.Title>
    <SectionPageLayout.Content><div className='space-y-4 overflow-auto'>
      <div className='grid gap-3 sm:grid-cols-4'>{[['Paid top-ups', summary.topup_amount_cents], ['Invoiced', summary.invoiced_amount_cents], ['Under review', summary.pending_amount_cents], ['Available', summary.available_amount_cents]].map(([label, value]) => <Card key={String(label)} size='sm'><CardHeader><CardTitle>{t(String(label))}</CardTitle></CardHeader><CardContent className='text-lg font-semibold'>¥{money(Number(value))}</CardContent></Card>)}</div>
      {summary.unverified_orders > 0 && <p className='text-muted-foreground text-sm'>{t('Some historical orders have no verified RMB payment amount and cannot be invoiced.')}</p>}
      <Card><CardHeader><CardTitle>{t('Submit invoice application')}</CardTitle></CardHeader><CardContent className='grid gap-3 md:grid-cols-2'>
        <div className='flex gap-2 md:col-span-2'><Button type='button' variant={type === 'general' ? 'default' : 'outline'} onClick={() => setType('general')}>{t('General invoice (1% tax)')}</Button><Button type='button' variant={type === 'special' ? 'default' : 'outline'} onClick={() => setType('special')}>{t('Special invoice (6% tax)')}</Button></div>
        {type === 'special' && <p className='text-destructive text-sm md:col-span-2'>{t('Special invoices require you to pay an additional 5% tax offline before approval.')}</p>}
        <label><Label>{t('Amount (RMB)')}</Label><Input type='number' min='0.01' step='0.01' value={amount} onChange={e => setAmount(e.target.value)} /></label>
        <label><Label>{t('Invoice title')}</Label><Input value={title} onChange={e => setTitle(e.target.value)} /></label>
        <label><Label>{t('Tax number')}</Label><Input value={taxNumber} onChange={e => setTaxNumber(e.target.value)} /></label>
        <label><Label>{t('Email')}</Label><Input type='email' value={email} onChange={e => setEmail(e.target.value)} /></label>
        {type === 'special' && <><label><Label>{t('Address')}</Label><Input value={address} onChange={e => setAddress(e.target.value)} /></label><label><Label>{t('Phone')}</Label><Input value={phone} onChange={e => setPhone(e.target.value)} /></label><label><Label>{t('Bank name')}</Label><Input value={bankName} onChange={e => setBankName(e.target.value)} /></label><label><Label>{t('Bank account')}</Label><Input value={bankAccount} onChange={e => setBankAccount(e.target.value)} /></label></>}
        <Button className='md:col-span-2' onClick={() => void submit()} disabled={submitting}>{submitting ? t('Submitting') : t('Submit invoice application')}</Button>
      </CardContent></Card>
      <Card><CardHeader><CardTitle>{t('Invoice history')}</CardTitle></CardHeader><CardContent><div className='space-y-2'>{items.map(item => <div key={item.id} className='flex flex-wrap items-center justify-between gap-2 border-b py-2 text-sm'><div className='space-y-1'><div>¥{money(item.amount_cents)} · {item.type === 'general' ? t('General') : t('Special')} · {formatTimestampToDate(item.created_at)}</div>{item.admin_note && <p className='text-muted-foreground'>{item.admin_note}</p>}</div><div className='flex items-center gap-3'><StatusBadge label={t(invoiceStatuses[item.status].label)} variant={invoiceStatuses[item.status].variant} copyable={false} />{item.status === 'approved' && <a className='text-primary underline' href={invoiceFileUrl(item.id)}>{t('Download invoice')}</a>}</div></div>)}</div></CardContent></Card>
    </div></SectionPageLayout.Content>
  </SectionPageLayout>
}
