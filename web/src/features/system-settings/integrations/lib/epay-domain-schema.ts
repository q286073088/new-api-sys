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
import { z } from 'zod'

const epayDomainAccountSchema = z.object({
  id: z.string(),
  domain: z.string(),
  merchant_id: z.string(),
  key: z.string().optional(),
  pay_address: z.string(),
  key_configured: z.boolean().optional(),
})

export const epayDomainAccountsSchema = z.array(epayDomainAccountSchema)
export type EpayDomainAccount = z.infer<typeof epayDomainAccountSchema>

export function normalizeEpayDomain(value: string): string {
  let address = value.trim()
  if (!address.includes('://')) {
    if (address.split(':').length > 2 && !address.startsWith('[')) {
      address = `[${address}]`
    }
    address = `https://${address}`
  }
  const url = new URL(address)
  if (
    !['http:', 'https:'].includes(url.protocol) ||
    address.includes('@') ||
    address.includes('\\') ||
    url.href.includes('?') ||
    url.hash ||
    url.port === '0' ||
    (url.pathname !== '' && url.pathname !== '/')
  ) {
    throw new Error('Invalid site domain')
  }
  const hostname = url.hostname
    .toLowerCase()
    .replace(/\.$/, '')
    .replaceAll(/^\[|\]$/g, '')
  if (
    !hostname.includes(':') &&
    (hostname.length > 253 ||
      !hostname
        .split('.')
        .every((label) => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label)))
  ) {
    throw new Error('Invalid site domain')
  }
  return hostname
}

export function createEpayDomainSchema(
  t: (key: string) => string,
  accounts: EpayDomainAccount[],
  editing: EpayDomainAccount | null
) {
  return z
    .object({
      domain: z.string().trim().min(1, t('Site domain is required')),
      merchant_id: z
        .string()
        .trim()
        .min(1, t('Merchant ID is required'))
        .max(64, t('Enter a merchant ID without spaces (up to 64 characters).'))
        .regex(
          /^\S+$/,
          t('Enter a merchant ID without spaces (up to 64 characters).')
        ),
      key: z
        .string()
        .trim()
        .max(
          512,
          t('Enter a secret key without line breaks (up to 512 characters).')
        )
        .regex(
          /^[^\r\n]*$/,
          t('Enter a secret key without line breaks (up to 512 characters).')
        ),
      pay_address: z.string().trim(),
    })
    .superRefine((values, ctx) => {
      let domain = ''
      try {
        domain = normalizeEpayDomain(values.domain)
      } catch {
        ctx.addIssue({
          code: 'custom',
          path: ['domain'],
          message: t('Enter a domain without paths or wildcards.'),
        })
      }
      if (
        accounts.some(
          (account) => account.id !== editing?.id && account.domain === domain
        )
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['domain'],
          message: t('This domain already has an Epay account.'),
        })
      }
      const address = values.pay_address.replace(/\/+$/, '')
      if (address) {
        try {
          const url = new URL(address)
          if (
            !['http:', 'https:'].includes(url.protocol) ||
            address.includes('@') ||
            url.href.includes('?') ||
            url.hash ||
            address.length > 2048
          ) {
            throw new Error('Invalid endpoint')
          }
        } catch {
          ctx.addIssue({
            code: 'custom',
            path: ['pay_address'],
            message: t('Enter a valid HTTP or HTTPS payment endpoint.'),
          })
        }
      }
      const canKeepKey =
        editing?.key_configured &&
        editing.merchant_id === values.merchant_id &&
        editing.pay_address === address
      if (!values.key && !canKeepKey) {
        ctx.addIssue({
          code: 'custom',
          path: ['key'],
          message: t('A secret key is required for a new merchant or gateway.'),
        })
      }
    })
}

export type EpayDomainFields = z.infer<
  ReturnType<typeof createEpayDomainSchema>
>
