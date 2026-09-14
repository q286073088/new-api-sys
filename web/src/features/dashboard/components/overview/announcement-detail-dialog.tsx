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
import { Megaphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { RichContent } from '@/components/rich-content'
import { Button } from '@/components/ui/button'
import { IconBadge } from '@/components/ui/icon-badge'
import { Separator } from '@/components/ui/separator'
import { formatDateTimeObject } from '@/lib/time'

interface AnnouncementDetailModalProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  announcement: {
    title?: string
    content?: string
    tag?: string
    publishDate?: string
    extra?: string
  } | null
}

export function AnnouncementDetailModal(props: AnnouncementDetailModalProps) {
  const { t } = useTranslation()
  const publishDate = props.announcement?.publishDate
  const publishedAt = publishDate ? new Date(publishDate) : null
  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={
        <>
          <IconBadge tone='primary' size='sm' className='rounded-sm'>
            <Megaphone />
          </IconBadge>
          {t('System Announcements')}
        </>
      }
      titleClassName='flex items-center gap-2.5 font-semibold'
      headerClassName='border-b pb-4 pr-8'
      contentClassName='rounded-none border shadow-xl ring-0 sm:max-w-xl'
      contentHeight='auto'
      bodyClassName='space-y-5 py-1'
      footerClassName='flex-row items-center justify-between gap-4 rounded-none sm:justify-between'
      footer={
        <>
          <div className='text-muted-foreground min-w-0 flex-1 text-xs leading-5'>
            {publishedAt && Number.isFinite(publishedAt.getTime()) && (
              <p>
                {t('Published:')}{' '}
                <time dateTime={publishDate}>
                  {formatDateTimeObject(publishedAt)}
                </time>
              </p>
            )}
          </div>
          <Button
            type='button'
            className='min-w-24 shrink-0 rounded-sm'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Got it')}
          </Button>
        </>
      }
    >
      {props.announcement?.content && (
        <RichContent
          breaks
          content={props.announcement.content}
          className='text-sm leading-7 break-words'
        />
      )}
      {props.announcement?.extra && (
        <div className='space-y-4'>
          <Separator />
          <RichContent
            breaks
            content={props.announcement.extra}
            className='text-muted-foreground text-sm break-words'
          />
        </div>
      )}
    </Dialog>
  )
}
