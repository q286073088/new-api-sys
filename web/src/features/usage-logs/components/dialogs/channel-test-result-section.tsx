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
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'

import type { LogOtherData } from '../../types'
import { DetailSection } from './log-detail-layout'

type ChannelTestResult = NonNullable<
  NonNullable<LogOtherData['admin_info']>['channel_test']
>

export function ChannelTestResultSection(props: { result: ChannelTestResult }) {
  const { t } = useTranslation()

  return (
    <section
      aria-label={t('Channel test result')}
      className='min-w-0 space-y-3'
    >
      <DetailSection label={t('Channel test prompt')}>
        <div className='flex justify-end'>
          <CopyButton value={props.result.prompt} size='sm' />
        </div>
        <pre className='max-h-48 overflow-auto text-xs leading-relaxed wrap-break-word whitespace-pre-wrap'>
          {props.result.prompt}
        </pre>
      </DetailSection>
      <DetailSection label={t('Model response')}>
        {props.result.output ? (
          <>
            <div className='flex justify-end'>
              <CopyButton value={props.result.output} size='sm' />
            </div>
            <pre className='max-h-80 overflow-auto text-xs leading-relaxed wrap-break-word whitespace-pre-wrap'>
              {props.result.output}
            </pre>
          </>
        ) : (
          <p className='text-muted-foreground text-xs'>
            {t('No text response was returned.')}
          </p>
        )}
        {props.result.output_truncated && (
          <p className='text-muted-foreground mt-2 text-xs'>
            {t('Only the first 32 KiB of the response is stored.')}
          </p>
        )}
      </DetailSection>
    </section>
  )
}
