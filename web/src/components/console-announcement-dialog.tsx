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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'

import { AnnouncementDetailModal } from '@/features/dashboard/components/overview/announcement-detail-dialog'
import { getAnnouncementKey } from '@/hooks/use-notifications'
import { api } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'
import { useAuthStore } from '@/stores/auth-store'
import { useNotificationStore } from '@/stores/notification-store'

type Announcement = {
  key: string
  content: string
  publishDate?: string
  extra?: string
  notice?: boolean
}
type Props = {
  notice: string
  announcements: Record<string, unknown>[]
  loading: boolean
}

export function ConsoleAnnouncementDialog(props: Props) {
  const userId = useAuthStore((state) => state.auth.user?.id)
  if (!userId) return null
  return <AccountAnnouncements key={userId} {...props} userId={userId} />
}

function AccountAnnouncements(props: Props & { userId: number }) {
  const queryClient = useQueryClient()
  const queryKey = ['announcement-views', props.userId]
  const [current, setCurrent] = useState<Announcement | null>(null)
  const shown = useRef(new Set<string>())
  const markNoticeRead = useNotificationStore((state) => state.markNoticeRead)
  const markAnnouncementsRead = useNotificationStore(
    (state) => state.markAnnouncementsRead
  )
  const views = useQuery({
    queryKey,
    queryFn: async ({ signal }) => {
      const response = await api.get<{ success: boolean; data: string[] }>(
        '/api/user/self/announcement-views',
        { signal }
      )
      return requireServerSuccess(response.data).data
    },
    staleTime: 60_000,
    meta: { errorToast: false },
  })
  const markViewed = useMutation({
    mutationFn: async (key: string) => {
      if (useAuthStore.getState().auth.user?.id !== props.userId) {
        throw new Error('Account changed')
      }
      return requireServerSuccess(
        (await api.post('/api/user/self/announcement-views', { key })).data
      )
    },
    retry: (failureCount) =>
      failureCount < 3 &&
      useAuthStore.getState().auth.user?.id === props.userId,
    meta: { errorToast: false },
    onMutate: async (key) => {
      await queryClient.cancelQueries({ queryKey })
      queryClient.setQueryData<string[]>(queryKey, (keys = []) => [
        ...new Set([...keys, key]),
      ])
    },
  })
  const latest = useMemo<Announcement | null>(() => {
    let selected: Record<string, unknown> | undefined
    let latestTime = Number.NEGATIVE_INFINITY
    let latestId = Number.NEGATIVE_INFINITY
    for (const item of props.announcements) {
      if (typeof item.content !== 'string' || !item.content.trim()) continue
      const timestamp = Date.parse(String(item.publishDate ?? ''))
      const publishedAt = Number.isFinite(timestamp)
        ? timestamp
        : Number.NEGATIVE_INFINITY
      const numericId = Number(item.id)
      const id = Number.isFinite(numericId)
        ? numericId
        : Number.NEGATIVE_INFINITY
      if (
        selected &&
        (publishedAt < latestTime ||
          (publishedAt === latestTime && id <= latestId))
      ) {
        continue
      }
      selected = item
      latestTime = publishedAt
      latestId = id
    }
    if (selected) {
      return {
        key: getAnnouncementKey(selected),
        content: String(selected.content),
        publishDate:
          typeof selected.publishDate === 'string'
            ? selected.publishDate
            : undefined,
        extra: typeof selected.extra === 'string' ? selected.extra : undefined,
      }
    }
    const notice = props.notice.trim()
    if (notice) {
      return {
        key: `notice:${getAnnouncementKey({ content: notice })}`,
        content: notice,
        notice: true,
      }
    }
    return null
  }, [props.announcements, props.notice])
  // Read status must not turn an older announcement into the next popup.
  const next =
    latest &&
    !shown.current.has(latest.key) &&
    !views.data?.includes(latest.key)
      ? latest
      : null
  const { mutate } = markViewed
  useEffect(() => {
    if (props.loading || !views.isSuccess || current || !next) return
    shown.current.add(next.key)
    setCurrent(next)
    mutate(next.key)
    if (next.notice) markNoticeRead(next.content)
    else markAnnouncementsRead([next.key])
  }, [
    props.loading,
    views.isSuccess,
    current,
    next,
    mutate,
    markNoticeRead,
    markAnnouncementsRead,
  ])

  return (
    <AnnouncementDetailModal
      open={current !== null}
      onOpenChange={(open) => {
        if (!open) setCurrent(null)
      }}
      announcement={current}
    />
  )
}
