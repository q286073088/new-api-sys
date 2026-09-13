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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { UsersEmailDialog } from '../users-email-dialog'

let client: QueryClient
beforeEach(() => {
  client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
})
afterEach(() => client.clear())

function renderEmail(ids?: number[]) {
  const onClose = vi.fn()
  render(
    <QueryClientProvider client={client}>
      <UsersEmailDialog ids={ids} onClose={onClose} />
    </QueryClientProvider>
  )
  return onClose
}

it('rejects empty fields and overlong subjects before queuing emails', async () => {
  const post = vi.spyOn(api, 'post')
  renderEmail([11])
  await userEvent.click(screen.getByRole('button', { name: 'Send email' }))
  expect(await screen.findByText('Enter an email subject.')).toBeVisible()
  expect(screen.getByText('Enter the email content.')).toBeVisible()
  expect(
    screen.getByRole('textbox', { name: 'Email subject' })
  ).toHaveAttribute('aria-invalid', 'true')
  fireEvent.change(screen.getByRole('textbox', { name: 'Email subject' }), {
    target: { value: 'a'.repeat(161) },
  })
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email content' }),
    'Welcome back'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Send email' }))
  expect(
    await screen.findByText('The subject must be at most 160 characters.')
  ).toBeVisible()
  expect(post).not.toHaveBeenCalled()
})

it('queues only the selected users and prevents duplicate submits while pending', async () => {
  let finish!: (value: {
    data: { success: boolean; data: { queued: number; skipped: number } }
  }) => void
  const post = vi.spyOn(api, 'post').mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve
      })
  )
  const onClose = renderEmail([11, 23])
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email subject' }),
    ' Welcome back '
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email content' }),
    'New models are available.'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Send email' }))
  await waitFor(() =>
    expect(post).toHaveBeenCalledWith(
      '/api/user/email',
      expect.objectContaining({
        ids: [11, 23],
        all_users: false,
        subject: 'Welcome back',
        content: 'New models are available.',
        request_id: expect.any(String),
      })
    )
  )
  expect(screen.getByRole('button', { name: 'Processing...' })).toBeDisabled()
  expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
  expect(screen.getByRole('textbox', { name: 'Email content' })).toBeDisabled()
  await userEvent.keyboard('{Escape}')
  expect(onClose).not.toHaveBeenCalled()
  await act(async () =>
    finish({ data: { success: true, data: { queued: 1, skipped: 1 } } })
  )
  await waitFor(() => expect(onClose).toHaveBeenCalledOnce())
  expect(post).toHaveBeenCalledOnce()
})

it('reuses the request ID for a failed send but creates a new ID when the draft changes', async () => {
  const post = vi
    .spyOn(api, 'post')
    .mockRejectedValue(new Error('SMTP queue unavailable'))
  const onClose = renderEmail()
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email subject' }),
    'Welcome back'
  )
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email content' }),
    'First draft'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Send email' }))
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Send email' })).toBeEnabled()
  )
  expect(onClose).not.toHaveBeenCalled()
  expect(screen.getByRole('textbox', { name: 'Email content' })).toHaveValue(
    'First draft'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Send email' }))
  await waitFor(() => expect(post).toHaveBeenCalledTimes(2))
  expect(post.mock.calls[1][1]).toEqual(post.mock.calls[0][1])
  await userEvent.clear(screen.getByRole('textbox', { name: 'Email content' }))
  await userEvent.type(
    screen.getByRole('textbox', { name: 'Email content' }),
    'Updated draft'
  )
  await userEvent.click(screen.getByRole('button', { name: 'Send email' }))
  await waitFor(() => expect(post).toHaveBeenCalledTimes(3))
  const first = post.mock.calls[0][1] as { request_id: string }
  expect(post.mock.calls[2][1]).toEqual(
    expect.objectContaining({
      all_users: true,
      content: 'Updated draft',
      request_id: expect.not.stringMatching(first.request_id),
    })
  )
})
