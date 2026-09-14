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
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { expect, test, vi } from 'vitest'

import { EpayDomainsEditor } from '../epay-domains-editor'

const savedAccount = {
  id: 'saved-account',
  domain: 'cn.example.com',
  merchant_id: '10001',
  pay_address: '',
  key_configured: true,
}

function PaymentSettingsFixture(props: {
  initialValue?: string
  onChange: (value: string) => void
  onSubmit?: () => void
}) {
  const [value, setValue] = useState(props.initialValue ?? '[]')
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        props.onSubmit?.()
      }}
    >
      <EpayDomainsEditor
        value={value}
        onChange={(next) => {
          setValue(next)
          props.onChange(next)
        }}
      />
    </form>
  )
}

test('adds a normalized account without submitting the surrounding payment settings', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  const onSubmit = vi.fn()
  render(<PaymentSettingsFixture onChange={onChange} onSubmit={onSubmit} />)
  expect(screen.getByText('No domain accounts configured.')).toBeInTheDocument()

  await user.click(screen.getByRole('button', { name: 'Add domain account' }))
  const dialog = screen.getByRole('dialog', { name: 'Add domain account' })
  await user.type(
    within(dialog).getByRole('textbox', { name: 'Site domain' }),
    'https://CN.Example.com.:8443/'
  )
  await user.type(
    within(dialog).getByRole('textbox', { name: 'Epay merchant ID' }),
    '10001'
  )
  await user.type(
    within(dialog).getByLabelText('Epay secret key'),
    'new-merchant-secret'
  )
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))

  await waitFor(() => expect(onChange).toHaveBeenCalledOnce())
  expect(JSON.parse(onChange.mock.calls[0][0])).toEqual([
    expect.objectContaining({
      domain: 'cn.example.com',
      merchant_id: '10001',
      key: 'new-merchant-secret',
      pay_address: '',
    }),
  ])
  expect(screen.getByRole('table')).toHaveTextContent('cn.example.com')
  expect(screen.getByRole('table')).toHaveTextContent('Configured')
  expect(screen.getByRole('table')).not.toHaveTextContent('new-merchant-secret')
  expect(onSubmit).not.toHaveBeenCalled()
})

test.each([
  ['https://CN.EXAMPLE.com:443/', 'This domain already has an Epay account.'],
  ['cn.example.com/path', 'Enter a domain without paths or wildcards.'],
  ['*.example.com', 'Enter a domain without paths or wildcards.'],
  ['cn..example.com', 'Enter a domain without paths or wildcards.'],
])(
  'rejects invalid or duplicate site domain %s before changing settings',
  async (domain, message) => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(
      <PaymentSettingsFixture
        initialValue={JSON.stringify([savedAccount])}
        onChange={onChange}
      />
    )
    await user.click(screen.getByRole('button', { name: 'Add domain account' }))
    const dialog = screen.getByRole('dialog')
    const input = within(dialog).getByRole('textbox', { name: 'Site domain' })
    await user.type(input, domain)
    await user.type(
      within(dialog).getByRole('textbox', { name: 'Epay merchant ID' }),
      '10002'
    )
    await user.type(
      within(dialog).getByLabelText('Epay secret key'),
      'another-secret'
    )
    await user.click(within(dialog).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(input).toHaveAttribute('aria-invalid', 'true'))
    expect(input).toHaveAccessibleDescription(
      new RegExp(message.replaceAll(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    )
    expect(onChange).not.toHaveBeenCalled()
  }
)

test('preserves the hidden saved key when only the domain changes', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(
    <PaymentSettingsFixture
      initialValue={JSON.stringify([savedAccount])}
      onChange={onChange}
    />
  )
  await user.click(screen.getByRole('button', { name: 'Edit domain account' }))
  const dialog = screen.getByRole('dialog', { name: 'Edit domain account' })
  expect(within(dialog).getByLabelText('Epay secret key')).toHaveValue('')
  await user.clear(within(dialog).getByRole('textbox', { name: 'Site domain' }))
  await user.type(
    within(dialog).getByRole('textbox', { name: 'Site domain' }),
    'renamed.example.com'
  )
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(onChange).toHaveBeenCalledOnce())
  expect(JSON.parse(onChange.mock.calls[0][0])).toEqual([
    {
      ...savedAccount,
      domain: 'renamed.example.com',
      key: '',
    },
  ])
})

test.each([
  ['Epay merchant ID', '10002'],
  ['Epay endpoint', 'https://pay.other.example'],
])('requires a new secret when %s changes', async (label, value) => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(
    <PaymentSettingsFixture
      initialValue={JSON.stringify([savedAccount])}
      onChange={onChange}
    />
  )
  await user.click(screen.getByRole('button', { name: 'Edit domain account' }))
  const dialog = screen.getByRole('dialog')
  const input = within(dialog).getByRole('textbox', { name: label })
  await user.clear(input)
  await user.type(input, value)
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  const keyInput = within(dialog).getByLabelText('Epay secret key')
  await waitFor(() => expect(keyInput).toHaveAttribute('aria-invalid', 'true'))
  expect(onChange).not.toHaveBeenCalled()
  await user.type(keyInput, 'replacement-secret')
  await user.click(within(dialog).getByRole('button', { name: 'Save' }))
  await waitFor(() => expect(onChange).toHaveBeenCalledOnce())
  expect(JSON.parse(onChange.mock.calls[0][0])[0].key).toBe(
    'replacement-secret'
  )
})

test('cancel discards edits and clears unsaved secrets when the dialog is reopened', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  render(<PaymentSettingsFixture onChange={onChange} />)
  await user.click(screen.getByRole('button', { name: 'Add domain account' }))
  await user.type(screen.getByLabelText('Epay secret key'), 'discarded-secret')
  await user.click(screen.getByRole('button', { name: 'Cancel' }))
  await waitFor(() =>
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  )
  await user.click(screen.getByRole('button', { name: 'Add domain account' }))
  expect(screen.getByLabelText('Epay secret key')).toHaveValue('')
  expect(onChange).not.toHaveBeenCalled()
})

test('deleting an account only removes the selected domain from the pending settings', async () => {
  const user = userEvent.setup()
  const onChange = vi.fn()
  const onSubmit = vi.fn()
  const second = {
    ...savedAccount,
    id: 'second',
    domain: 'overseas.example.com',
    merchant_id: '10002',
  }
  render(
    <PaymentSettingsFixture
      initialValue={JSON.stringify([savedAccount, second])}
      onChange={onChange}
      onSubmit={onSubmit}
    />
  )
  const row = screen.getByRole('row', { name: /cn.example.com/ })
  await user.click(within(row).getByRole('button', { name: 'Actions' }))
  await user.click(screen.getByRole('menuitem', { name: 'Delete' }))
  expect(JSON.parse(onChange.mock.calls[0][0])).toEqual([second])
  expect(
    screen.queryByRole('row', { name: /cn.example.com/ })
  ).not.toBeInTheDocument()
  expect(
    screen.getByRole('row', { name: /overseas.example.com/ })
  ).toBeInTheDocument()
  expect(onSubmit).not.toHaveBeenCalled()
})
