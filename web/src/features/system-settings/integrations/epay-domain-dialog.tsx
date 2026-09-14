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
import { nanoid } from 'nanoid'
import { useId } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { PasswordInput } from '@/components/password-input'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import {
  createEpayDomainSchema,
  normalizeEpayDomain,
  type EpayDomainAccount,
  type EpayDomainFields,
} from './lib/epay-domain-schema'

type EpayDomainDialogProps = {
  accounts: EpayDomainAccount[]
  editing: EpayDomainAccount | null
  onSave: (account: EpayDomainAccount) => void
  onClose: () => void
}

export function EpayDomainDialog(props: EpayDomainDialogProps) {
  const { t } = useTranslation()
  const formId = useId()
  const form = useForm<EpayDomainFields>({
    resolver: zodResolver(
      createEpayDomainSchema(t, props.accounts, props.editing)
    ),
    defaultValues: {
      domain: props.editing?.domain ?? '',
      merchant_id: props.editing?.merchant_id ?? '',
      key: props.editing?.key ?? '',
      pay_address: props.editing?.pay_address ?? '',
    },
  })

  function saveAccount(values: EpayDomainFields) {
    props.onSave({
      id: props.editing?.id ?? nanoid(),
      domain: normalizeEpayDomain(values.domain),
      merchant_id: values.merchant_id,
      key: values.key,
      pay_address: values.pay_address.replace(/\/+$/, ''),
      key_configured: props.editing?.key_configured ?? false,
    })
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) props.onClose()
      }}
      title={props.editing ? t('Edit domain account') : t('Add domain account')}
      description={t('Changes take effect after saving all payment settings.')}
      footer={
        <>
          <Button type='button' variant='outline' onClick={props.onClose}>
            {t('Cancel')}
          </Button>
          <Button type='submit' form={formId}>
            {t('Save')}
          </Button>
        </>
      }
    >
      <Form {...form}>
        <form
          id={formId}
          onSubmit={(event) => {
            event.stopPropagation()
            void form.handleSubmit(saveAccount)(event)
          }}
          className='space-y-4'
        >
          <FormField
            control={form.control}
            name='domain'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Site domain')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    placeholder='api.example.com'
                    autoComplete='off'
                    maxLength={1024}
                  />
                </FormControl>
                <FormDescription>
                  {t('Enter a domain without paths or wildcards.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='merchant_id'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Epay merchant ID')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    placeholder='10001'
                    autoComplete='off'
                    maxLength={64}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='key'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Epay secret key')}</FormLabel>
                <FormControl>
                  <PasswordInput
                    {...field}
                    placeholder={t('Enter new key to update')}
                    autoComplete='new-password'
                    maxLength={512}
                  />
                </FormControl>
                <FormDescription>
                  {t('Leave blank unless rotating the secret')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name='pay_address'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Epay endpoint')}</FormLabel>
                <FormControl>
                  <Input
                    {...field}
                    placeholder='https://pay.example.com'
                    autoComplete='off'
                    maxLength={2048}
                  />
                </FormControl>
                <FormDescription>
                  {t('Leave blank to use the default Epay endpoint.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </form>
      </Form>
    </Dialog>
  )
}
