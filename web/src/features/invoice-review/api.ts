import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'
import type { InvoiceApplication } from '@/features/invoices/api'

export async function getAdminInvoices() { const response = await api.get('/api/user/invoice/admin', { params: { p: 1, page_size: 100 } }); return requireServerSuccess(response.data).data as { items: InvoiceApplication[]; total: number } }
export async function reviewInvoice(id: number, status: string, note: string, taxPaid: boolean, file?: File) {
  const form = new FormData(); form.append('status', status); form.append('note', note); form.append('tax_paid', String(taxPaid)); if (file) form.append('file', file)
  const response = await api.post(`/api/user/invoice/${id}/review`, form); return requireServerSuccess(response.data)
}
