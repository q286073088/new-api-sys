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
import { StatusBadge } from '@/components/status-badge'
import { formatQuota } from '@/lib/format'
import { useAuthStore } from '@/stores/auth-store'

import { getReferralRewards } from '../api'
import { formatTimestamp } from '../lib/billing'
import type { ReferralReward } from '../types'

const EMPTY_REWARDS: ReferralReward[] = []

export function ReferralRewardsTable() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 10 })
  const query = useQuery({
    queryKey: ['referrals', userId, 'rewards', pagination],
    queryFn: () =>
      getReferralRewards({
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
      }),
    enabled: Boolean(userId),
    refetchInterval: 60_000,
  })
  const columns: ColumnDef<ReferralReward>[] = [
    { accessorKey: 'id', header: t('ID') },
    {
      accessorKey: 'invitee_username',
      header: t('Paying User'),
      cell: ({ row }) => (
        <LongText className='max-w-48'>
          {row.original.invitee_username || `#${row.original.invitee_id}`}
        </LongText>
      ),
    },
    {
      accessorKey: 'level',
      header: t('Reward Type'),
      cell: ({ row }) =>
        row.original.level === 1 ? t('Direct Earnings') : t('Team Earnings'),
    },
    {
      accessorKey: 'base_quota',
      header: t('Rebate Base'),
      cell: ({ row }) => formatQuota(row.original.base_quota),
    },
    {
      accessorKey: 'rate',
      header: t('Rebate Rate'),
      cell: ({ row }) => `${row.original.rate}%`,
    },
    {
      accessorKey: 'quota',
      header: t('Reward Amount'),
      cell: ({ row }) => formatQuota(row.original.quota),
    },
    {
      accessorKey: 'status',
      header: t('Status'),
      cell: ({ row }) => {
        if (row.original.status === 'settled') {
          return (
            <StatusBadge
              label={t('Settled')}
              variant='success'
              copyable={false}
            />
          )
        }
        if (row.original.status === 'cancelled') {
          return (
            <StatusBadge
              label={t('Cancelled')}
              variant='neutral'
              copyable={false}
            />
          )
        }
        return (
          <StatusBadge
            label={t('Pending')}
            variant='warning'
            copyable={false}
          />
        )
      },
    },
    {
      accessorKey: 'available_at',
      header: t('Available At'),
      cell: ({ row }) => formatTimestamp(row.original.available_at),
    },
    {
      accessorKey: 'settled_at',
      header: t('Settled At'),
      cell: ({ row }) =>
        row.original.settled_at
          ? formatTimestamp(row.original.settled_at)
          : '—',
    },
  ]
  const table = useReactTable({
    columns,
    data: query.data?.items ?? EMPTY_REWARDS,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => String(row.id),
    manualPagination: true,
    rowCount: query.data?.total ?? 0,
    state: { pagination },
    onPaginationChange: setPagination,
    enableSorting: false,
  })
  if (query.isError) return <ErrorState onRetry={() => void query.refetch()} />
  return (
    <div className='space-y-3'>
      <DataTableView
        table={table}
        isLoading={query.isLoading}
        emptyTitle={t('No recharge rebates yet')}
        emptyDescription={t(
          'Eligible paid top-ups generate rebate records here.'
        )}
        tableContainerClassName='overflow-x-auto'
        tableClassName='min-w-[960px]'
      />
      <DataTablePagination table={table} compact />
    </div>
  )
}
