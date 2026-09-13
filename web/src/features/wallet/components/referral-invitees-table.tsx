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
import { useQuery } from '@tanstack/react-query'
import {
  getCoreRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePagination, DataTableView } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { LongText } from '@/components/long-text'
import { Button } from '@/components/ui/button'
import { formatQuota } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import { getReferralInvitees } from '../api'
import { formatTimestamp } from '../lib/billing'
import type { ReferralInvitee } from '../types'

const EMPTY_INVITEES: ReferralInvitee[] = []

export function ReferralInviteesTable() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [parent, setParent] = useState<ReferralInvitee | null>(null)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 10 })
  const query = useQuery({
    queryKey: ['referrals', userId, 'invitees', parent?.id, pagination],
    queryFn: () =>
      getReferralInvitees({
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        parent_id: parent?.id,
      }),
    enabled: Boolean(userId),
    refetchInterval: 60_000,
  })
  const columns: ColumnDef<ReferralInvitee>[] = [
    { accessorKey: 'id', header: t('ID') },
    {
      accessorKey: 'username',
      header: t('Username'),
      cell: ({ row }) => (
        <LongText className='max-w-48'>{row.original.username}</LongText>
      ),
    },
    {
      accessorKey: 'topup_quota',
      header: t('Total Top-ups'),
      cell: ({ row }) => formatQuota(row.original.topup_quota),
    },
    {
      accessorKey: 'direct_reward_quota',
      header: t('Direct Earnings'),
      cell: ({ row }) => formatQuota(row.original.direct_reward_quota),
    },
    {
      accessorKey: 'team_reward_quota',
      header: t('Team Earnings'),
      cell: ({ row }) => {
        if (parent) return formatQuota(row.original.team_reward_quota)
        return (
          <Button
            variant='link'
            size='sm'
            className='h-auto px-0'
            aria-label={t('View team of {{username}}', {
              username: row.original.username,
            })}
            onClick={() => {
              setParent(row.original)
              setPagination((value) => ({ ...value, pageIndex: 0 }))
            }}
          >
            {formatQuota(row.original.team_reward_quota)}
          </Button>
        )
      },
    },
    {
      accessorKey: 'pending_quota',
      header: t('Pending Rewards'),
      cell: ({ row }) => formatQuota(row.original.pending_quota),
    },
    {
      accessorKey: 'created_at',
      header: t('Registration Time'),
      cell: ({ row }) => formatTimestamp(row.original.created_at),
    },
  ]
  const table = useReactTable({
    data: query.data?.items ?? EMPTY_INVITEES,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => String(row.id),
    manualPagination: true,
    rowCount: query.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: setPagination,
    enableSorting: false,
  })

  return (
    <div className='space-y-3'>
      {parent && (
        <div className='flex flex-wrap items-center gap-3'>
          <Button
            variant='outline'
            size='sm'
            onClick={() => {
              setParent(null)
              setPagination((value) => ({ ...value, pageIndex: 0 }))
            }}
          >
            {t('Back to My Invitees')}
          </Button>
          <LongText className='max-w-80 text-sm'>
            {t('Team of {{username}}', { username: parent.username })}
          </LongText>
        </div>
      )}
      <p className='text-muted-foreground text-xs'>
        {t(
          'Top-ups show credited account amounts. Earnings include pending and settled rebates; team earnings include only your second-level rewards.'
        )}
      </p>
      {query.isError ? (
        <ErrorState onRetry={() => void query.refetch()} />
      ) : (
        <>
          <DataTableView
            table={table}
            isLoading={query.isLoading}
            emptyTitle={t('No invited users yet')}
            emptyDescription={t(
              'Users who register through your referral link will appear here.'
            )}
            tableContainerClassName='overflow-x-auto'
            tableClassName='min-w-[760px]'
          />
          <DataTablePagination table={table} compact />
        </>
      )}
    </div>
  )
}
