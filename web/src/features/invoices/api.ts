import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

export type InvoiceSummary = {
  topup_amount_cents: number
  invoiced_amount_cents: number
  pending_amount_cents: number
  available_amount_cents: number
  unverified_orders: number
}

export type InvoiceApplication = {
  id: number
  user_id: number
  type: 'general' | 'special'
  amount_cents: number
  tax_rate: number
  extra_tax_cents: number
  title: string
  tax_number: string
  address: string
  phone: string
  bank_name: string
  bank_account: string
  email: string
  status: 'pending' | 'approved' | 'rejected'
  file_name: string
  admin_note: string
  created_at: number
  reviewed_at: number
}

export async function getInvoiceSummary() {
  const response = await api.get('/api/user/invoice/summary')
  return requireServerSuccess(response.data).data as { summary: InvoiceSummary; settings: { enabled: boolean; allowed: boolean } }
}

export async function getInvoices() {
  const response = await api.get('/api/user/invoice', { params: { p: 1, page_size: 100 } })
  return requireServerSuccess(response.data).data as { items: InvoiceApplication[]; total: number }
}

export async function createInvoice(payload: Omit<InvoiceApplication, 'id' | 'user_id' | 'status' | 'file_name' | 'admin_note' | 'created_at' | 'reviewed_at' | 'tax_rate' | 'extra_tax_cents'>) {
  const response = await api.post('/api/user/invoice', payload)
  return requireServerSuccess(response.data).data as InvoiceApplication
}

export function invoiceFileUrl(id: number) { return `/api/user/invoice/${id}/file` }
