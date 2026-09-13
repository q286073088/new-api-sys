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

import { Dialog } from '@/components/dialog'
import { ErrorState } from '@/components/error-state'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { formatQuota } from '@/lib/format'

import { useReferralSummary } from '../../hooks/use-referral-summary'
import { ReferralInviteesTable } from '../referral-invitees-table'
import { ReferralRewardsTable } from '../referral-rewards-table'

export function ReferralDetailsDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const query = useReferralSummary()
  const overview = query.data
  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Referral Details')}
      description={t(
        'View your invitees, team earnings and recharge rebate history.'
      )}
      contentClassName='sm:max-w-6xl'
      bodyClassName='space-y-4'
    >
      {query.isError && <ErrorState onRetry={() => void query.refetch()} />}
      {query.isLoading && <Skeleton className='h-24 w-full' />}
      {overview && (
        <>
          <dl className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
            {[
              [
                t('Direct Invitees'),
                overview.summary.direct_count.toLocaleString(),
              ],
              [t('Team Members'), overview.summary.team_count.toLocaleString()],
              [
                t('Available Rewards'),
                formatQuota(overview.summary.available_quota),
              ],
              [
                t('Pending Rewards'),
                formatQuota(overview.summary.pending_quota),
              ],
            ].map(([label, value]) => (
              <div key={label} className='bg-muted/40 rounded-lg p-3'>
                <dt className='text-muted-foreground text-xs'>{label}</dt>
                <dd className='mt-1 text-lg font-semibold tabular-nums'>
                  {value}
                </dd>
              </div>
            ))}
          </dl>
          {overview.settings.enabled ? (
            <p className='text-muted-foreground text-sm'>
              {t(
                'Current rates: direct {{direct}}%, team {{team}}%. Rewards arrive after {{days}} days.',
                {
                  direct: overview.settings.level1_percent,
                  team: overview.settings.level2_percent,
                  days: overview.settings.delay_days,
                }
              )}
            </p>
          ) : (
            <p className='text-muted-foreground text-sm'>
              {t(
                'New recharge rebates are currently disabled. Existing rewards retain their due dates.'
              )}
            </p>
          )}
        </>
      )}
      <Tabs defaultValue='invitees'>
        <TabsList aria-label={t('Referral Details')}>
          <TabsTrigger value='invitees'>{t('My Invitees')}</TabsTrigger>
          <TabsTrigger value='rewards'>{t('Rebate History')}</TabsTrigger>
        </TabsList>
        <TabsContent value='invitees'>
          <ReferralInviteesTable />
        </TabsContent>
        <TabsContent value='rewards'>
          <ReferralRewardsTable />
        </TabsContent>
      </Tabs>
    </Dialog>
  )
}
