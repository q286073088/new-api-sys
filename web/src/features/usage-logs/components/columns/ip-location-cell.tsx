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
import { useTranslation } from 'react-i18next'

import { TruncatedCell } from '@/components/data-table'
import { Button } from '@/components/ui/button'

import { useUsageLogsContext } from '../usage-logs-provider'

type IpGeoResponse = {
  city?: string
  region?: string
  country?: string
  country_code?: string
}

const IP_GEO_ENDPOINT = 'https://get.geojs.io/v1/ip/geo'

function formatIpLocation(data: IpGeoResponse | undefined): string {
  if (!data) return ''

  const parts = [data.city, data.region, data.country]
    .map((value) => value?.trim())
    .filter((value): value is string => Boolean(value))

  return [...new Set(parts)].join(', ') || data.country_code?.trim() || ''
}

async function fetchIpLocation(
  ip: string,
  signal: AbortSignal
): Promise<IpGeoResponse> {
  const response = await fetch(
    `${IP_GEO_ENDPOINT}/${encodeURIComponent(ip)}.json`,
    {
      headers: { Accept: 'application/json' },
      signal,
    }
  )
  if (!response.ok) {
    throw new Error(`IP geolocation request failed: ${response.status}`)
  }
  return (await response.json()) as IpGeoResponse
}

export function IpLocationCell({
  ip,
  showIp = true,
}: {
  ip: string
  showIp?: boolean
}) {
  const { t } = useTranslation()
  const { sensitiveVisible } = useUsageLogsContext()
  const { data, isError, isFetching, refetch } = useQuery({
    queryKey: ['usage-log-ip-geo', ip],
    queryFn: ({ signal }) => fetchIpLocation(ip, signal),
    enabled: false,
    retry: false,
    staleTime: 24 * 60 * 60 * 1000,
  })

  if (!ip) {
    return <span className='text-muted-foreground'>—</span>
  }

  const location = sensitiveVisible ? formatIpLocation(data) : ''
  const actionLabel = isError ? t('Retry') : t('Get location')

  return (
    <div className='flex max-w-48 flex-col gap-0.5'>
      {showIp && (
        <TruncatedCell
          className='font-mono text-xs'
          tabIndex={sensitiveVisible ? 0 : undefined}
        >
          {sensitiveVisible ? ip : '••••'}
        </TruncatedCell>
      )}
      {sensitiveVisible &&
        (location ? (
          <span className='text-muted-foreground max-w-full truncate text-xs'>
            {location}
          </span>
        ) : (
          <Button
            type='button'
            variant='link'
            size='sm'
            className='h-auto justify-start px-0 py-0 text-xs font-normal'
            aria-label={`${actionLabel}: ${ip}`}
            disabled={isFetching}
            onClick={(event) => {
              event.stopPropagation()
              void refetch()
            }}
          >
            {isFetching ? t('Loading...') : actionLabel}
          </Button>
        ))}
    </div>
  )
}
