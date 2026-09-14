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
import { Plus } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { StaticDataTable, StaticRowActions } from '@/components/data-table'
import { Button } from '@/components/ui/button'

import { safeJsonParseWithValidation } from '../utils/json-parser'
import { EpayDomainDialog } from './epay-domain-dialog'
import {
  epayDomainAccountsSchema,
  type EpayDomainAccount,
} from './lib/epay-domain-schema'

type EpayDomainsEditorProps = {
  value: string
  onChange: (value: string) => void
}

export function EpayDomainsEditor(props: EpayDomainsEditorProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<EpayDomainAccount | null>(null)
  const accounts = useMemo(
    () =>
      safeJsonParseWithValidation<EpayDomainAccount[]>(props.value, {
        fallback: [],
        validator: (value): value is EpayDomainAccount[] =>
          epayDomainAccountsSchema.safeParse(value).success,
        silent: true,
      }),
    [props.value]
  )

  function editAccount(account: EpayDomainAccount | null) {
    setEditing(account)
    setOpen(true)
  }

  function saveAccount(account: EpayDomainAccount) {
    const next = editing
      ? accounts.map((item) => (item.id === editing.id ? account : item))
      : [...accounts, account]
    props.onChange(JSON.stringify(next))
    setOpen(false)
  }

  return (
    <div className='min-w-0 space-y-3'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <h4 className='font-medium'>{t('Epay domain accounts')}</h4>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => editAccount(null)}
          disabled={accounts.length >= 100}
        >
          <Plus aria-hidden='true' />
          {t('Add domain account')}
        </Button>
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Use a different Epay merchant for each site domain. Unmatched domains use the default account above.'
        )}
      </p>
      <StaticDataTable
        data={accounts}
        getRowKey={(account) => account.id}
        emptyContent={t('No domain accounts configured.')}
        columns={[
          {
            id: 'domain',
            header: t('Site domain'),
            cell: (account) => account.domain,
          },
          {
            id: 'merchant_id',
            header: t('Epay merchant ID'),
            cell: (account) => account.merchant_id,
          },
          {
            id: 'pay_address',
            header: t('Epay endpoint'),
            cell: (account) => account.pay_address || t('Default'),
          },
          {
            id: 'key',
            header: t('Epay secret key'),
            cell: (account) =>
              account.key || account.key_configured
                ? t('Configured')
                : t('Not configured'),
          },
          {
            id: 'actions',
            header: t('Actions'),
            cell: (account) => (
              <StaticRowActions
                editLabel={t('Edit domain account')}
                deleteLabel={t('Delete')}
                menuLabel={t('Actions')}
                onEdit={() => editAccount(account)}
                onDelete={() =>
                  props.onChange(
                    JSON.stringify(
                      accounts.filter((item) => item.id !== account.id)
                    )
                  )
                }
              />
            ),
          },
        ]}
      />
      {open && (
        <EpayDomainDialog
          accounts={accounts}
          editing={editing}
          onSave={saveAccount}
          onClose={() => setOpen(false)}
        />
      )}
    </div>
  )
}
