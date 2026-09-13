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
import { getCoreRowModel, useReactTable } from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import { expect, it } from 'vitest'

import { DataTableView } from '@/components/data-table'

import type { User } from '../../types'
import { useUsersColumns } from '../users-columns'

function EmailTable() {
  const columns = useUsersColumns().filter(
    (column) => 'accessorKey' in column && column.accessorKey === 'email'
  )
  const users: User[] = [
    {
      id: 1,
      username: 'bound',
      display_name: '',
      email: 'bound@example.com',
      quota: 0,
      used_quota: 0,
      request_count: 0,
      group: 'default',
      status: 1,
      role: 1,
    },
    {
      id: 2,
      username: 'unbound',
      display_name: '',
      email: '',
      quota: 0,
      used_quota: 0,
      request_count: 0,
      group: 'default',
      status: 1,
      role: 1,
    },
  ]
  const table = useReactTable({
    columns,
    data: users,
    getCoreRowModel: getCoreRowModel(),
  })
  return <DataTableView table={table} />
}

it('shows the bound email and a placeholder when no email is bound', () => {
  render(<EmailTable />)
  expect(
    screen.getByRole('columnheader', { name: 'Email' })
  ).toBeInTheDocument()
  expect(
    screen.getByRole('cell', { name: 'bound@example.com' })
  ).toBeInTheDocument()
  expect(screen.getByRole('cell', { name: '—' })).toBeInTheDocument()
})
