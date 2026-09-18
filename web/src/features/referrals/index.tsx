/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ErrorState } from '@/components/error-state'
import { formatQuota } from '@/lib/format'

import { ReferralInviteesTable } from '../wallet/components/referral-invitees-table'
import { ReferralRewardsTable } from '../wallet/components/referral-rewards-table'
import { useReferralSummary } from '../wallet/hooks/use-referral-summary'

export function Referrals() {
  const { t } = useTranslation()
  const query = useReferralSummary()
  const overview = query.data

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Referral Rewards')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
          <Card data-card-hover='false'>
            <CardHeader className='pb-3'>
              <CardTitle className='text-base'>{t('Referral Rules')}</CardTitle>
            </CardHeader>
            <CardContent>
              {query.isError && (
                <ErrorState onRetry={() => void query.refetch()} />
              )}
              {query.isLoading && <Skeleton className='h-16 w-full' />}
              {overview && (
                <>
                  <dl className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
                    {[
                      [t('Direct Invitees'), overview.summary.direct_count],
                      [t('Team Members'), overview.summary.team_count],
                      [
                        t('Available Rewards'),
                        formatQuota(overview.summary.available_quota),
                      ],
                      [
                        t('Pending Rewards'),
                        formatQuota(overview.summary.pending_quota),
                      ],
                    ].map(([label, value]) => (
                      <div key={String(label)} className='bg-muted/40 rounded-lg p-3'>
                        <dt className='text-muted-foreground text-xs'>{label}</dt>
                        <dd className='mt-1 text-lg font-semibold tabular-nums'>
                          {value}
                        </dd>
                      </div>
                    ))}
                  </dl>
                  <p className='text-muted-foreground mt-4 text-sm'>
                    {overview.settings.enabled
                      ? t(
                          'Current rates: direct {{direct}}%, team {{team}}%. Rewards arrive after {{days}} days.',
                          {
                            direct: overview.settings.level1_percent,
                            team: overview.settings.level2_percent,
                            days: overview.settings.delay_days,
                          }
                        )
                      : t(
                          'New recharge rebates are currently disabled. Existing rewards retain their due dates.'
                        )}
                  </p>
                </>
              )}
            </CardContent>
          </Card>

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
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
