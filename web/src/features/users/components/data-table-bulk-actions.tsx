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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { Table } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { handleServerError } from '@/lib/handle-server-error'

import { manageUsers } from '../api'
import type { User } from '../types'
import { UsersEmailDialog } from './users-email-dialog'

interface DataTableBulkActionsProps {
  table: Table<User>
}

export function DataTableBulkActions({ table }: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [action, setAction] = useState<'enable' | 'disable' | null>(null)
  const [emailOpen, setEmailOpen] = useState(false)
  const ids = table
    .getFilteredSelectedRowModel()
    .rows.map((row) => row.original.id)
  const manage = useMutation({
    mutationFn: (selectedAction: 'enable' | 'disable') =>
      manageUsers(ids, selectedAction),
    onSuccess: (result) => {
      table.setRowSelection((selection) => {
        const remaining = { ...selection }
        result.succeeded.forEach((id) => {
          delete remaining[String(id)]
        })
        return remaining
      })
      void queryClient.invalidateQueries({ queryKey: ['users'] })
      if (result.succeeded.length) {
        toast.success(
          t('Updated {{count}} users.', { count: result.succeeded.length })
        )
      }
      if (result.failed.length) {
        handleServerError(
          result.failed[0].error,
          t('Failed to update {{count}} users.', {
            count: result.failed.length,
          })
        )
      }
      setAction(null)
    },
  })
  return (
    <>
      <BulkActionsToolbar table={table} entityName='user'>
        <Button
          size='sm'
          variant='outline'
          disabled={manage.isPending}
          onClick={() => setAction('enable')}
        >
          {t('Enable selected')}
        </Button>
        <Button
          size='sm'
          variant='destructive'
          disabled={manage.isPending}
          onClick={() => setAction('disable')}
        >
          {t('Disable selected')}
        </Button>
        <Button
          size='sm'
          variant='outline'
          disabled={manage.isPending}
          onClick={() => setEmailOpen(true)}
        >
          {t('Send email')}
        </Button>
      </BulkActionsToolbar>
      <ConfirmDialog
        open={action !== null}
        onOpenChange={(open) => {
          if (!open && !manage.isPending) setAction(null)
        }}
        title={
          action === 'disable' ? t('Disable selected') : t('Enable selected')
        }
        desc={t(
          'Apply this action to {{count}} selected users? Disabled users will be signed out.',
          { count: ids.length }
        )}
        destructive={action === 'disable'}
        isLoading={manage.isPending}
        disabled={ids.length === 0}
        handleConfirm={() => {
          if (action) manage.mutate(action)
        }}
      />
      {emailOpen && (
        <UsersEmailDialog ids={ids} onClose={() => setEmailOpen(false)} />
      )}
    </>
  )
}
