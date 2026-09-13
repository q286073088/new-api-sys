/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useId, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import { sendUserEmails } from '../api'
import { userEmailSchema, type UserEmailValues } from '../lib/user-form'

export function UsersEmailDialog(props: {
  ids?: number[]
  onClose: () => void
}) {
  const { t } = useTranslation()
  const formId = useId()
  const submitted = useRef<{ fingerprint: string; requestId: string } | null>(
    null
  )
  const form = useForm<UserEmailValues>({
    resolver: zodResolver(userEmailSchema),
    defaultValues: { subject: '', content: '' },
  })
  const send = useMutation({
    mutationFn: (values: UserEmailValues) => {
      const payload = {
        ...values,
        ids: props.ids,
        all_users: props.ids === undefined,
      }
      const fingerprint = JSON.stringify(payload)
      if (submitted.current?.fingerprint !== fingerprint) {
        submitted.current = { fingerprint, requestId: crypto.randomUUID() }
      }
      return sendUserEmails({
        ...payload,
        request_id: submitted.current.requestId,
      })
    },
    onSuccess: (result) => {
      toast.success(
        t('Queued {{queued}} emails; skipped {{skipped}} recipients.', result)
      )
      props.onClose()
    },
    onError: (error) => handleServerError(error),
  })
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !send.isPending) props.onClose()
      }}
      title={t('Send email')}
      description={
        props.ids
          ? t('Send to {{count}} selected users with a bound email.', {
              count: props.ids.length,
            })
          : t(
              'Send to all registered users you can manage who have a bound email, including disabled users.'
            )
      }
      footer={
        <>
          <Button
            variant='outline'
            disabled={send.isPending}
            onClick={props.onClose}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='submit'
            form={formId}
            disabled={send.isPending || props.ids?.length === 0}
          >
            {send.isPending ? t('Processing...') : t('Send email')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={formId}
          onSubmit={form.handleSubmit((values) => send.mutate(values))}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='subject'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Email subject')}</FormLabel>
                <FormControl>
                  <Input {...field} maxLength={160} disabled={send.isPending} />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='content'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Email content')}</FormLabel>
                <FormControl>
                  <Textarea
                    {...field}
                    rows={10}
                    maxLength={20000}
                    disabled={send.isPending}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <p className='text-muted-foreground text-sm'>
            {t(
              'Emails are sent individually as plain text using the configured SMTP service. Delivery runs in the background.'
            )}
          </p>
        </form>
      </Form>
    </Dialog>
  )
}
