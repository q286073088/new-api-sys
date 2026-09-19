import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'

import { downloadInvoiceFile, type InvoiceApplication } from './api'

export function InvoiceDownloadButton({
  invoice,
}: {
  invoice: InvoiceApplication
}) {
  const [downloading, setDownloading] = useState(false)
  const download = async () => {
    setDownloading(true)
    try {
      await downloadInvoiceFile(invoice.id, invoice.file_name)
    } catch {
      toast.error('下载发票失败，请重试')
    } finally {
      setDownloading(false)
    }
  }
  return (
    <Button
      variant='link'
      size='sm'
      disabled={downloading}
      onClick={() => void download()}
    >
      {downloading ? '下载中...' : '下载发票'}
    </Button>
  )
}
