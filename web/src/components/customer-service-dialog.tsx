/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { MessageCircle, QrCode } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { useStatus } from '@/hooks/use-status'

export function CustomerServiceDialog() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const [open, setOpen] = useState(false)
  const account = String(status?.customer_service_qq ?? '').trim()
  const qrCode = String(status?.customer_service_qr_code ?? '').trim()

  if (!account && !qrCode) return null

  return (
    <>
      <Button
        type='button'
        variant='ghost'
        size='sm'
        className='gap-1.5'
        onClick={() => setOpen(true)}
        aria-label={t('Contact support')}
      >
        <MessageCircle className='size-4' />
        <span className='hidden sm:inline'>{t('Contact support')}</span>
      </Button>
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('Contact support')}
        description={t('Scan the QR code or copy the customer service account to contact support')}
        contentClassName='sm:max-w-md'
        contentHeight='auto'
        footer={
          <Button variant='outline' onClick={() => setOpen(false)}>
            {t('Close')}
          </Button>
        }
      >
        <div className='flex flex-col items-center gap-4 py-2'>
          {qrCode ? (
            <div className='bg-muted/30 flex size-56 items-center justify-center rounded-lg border p-3'>
              <img
                src={qrCode}
                alt={t('Customer service QR code')}
                className='size-full object-contain'
              />
            </div>
          ) : (
            <div className='bg-muted/30 text-muted-foreground flex size-56 items-center justify-center rounded-lg border'>
              <QrCode className='size-12' aria-hidden='true' />
            </div>
          )}
          {account ? (
            <div className='flex w-full items-center justify-between gap-3 rounded-lg border px-3 py-2'>
              <div className='min-w-0'>
                <div className='text-muted-foreground text-xs'>{t('Customer service account')}</div>
                <div className='truncate text-sm font-medium'>{account}</div>
              </div>
              <CopyButton
                value={account}
                variant='outline'
                size='sm'
                tooltip={t('Copy customer service account')}
              >
                {t('Copy')}
              </CopyButton>
            </div>
          ) : null}
        </div>
      </Dialog>
    </>
  )
}
