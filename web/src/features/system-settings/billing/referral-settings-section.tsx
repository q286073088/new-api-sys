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
import { zodResolver } from '@hookform/resolvers/zod'
import type { Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import type { ReferralSettings } from '@/features/wallet/types'

import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import {
  SettingsForm,
  SettingsFormGrid,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { tryJsonParse } from '../utils/json-parser'

const DEFAULT_SETTINGS: ReferralSettings = {
  enabled: false,
  level1_percent: 5,
  level2_percent: 0,
  delay_days: 3,
}

export function ReferralSettingsSection(props: {
  defaultValue: string
  complianceConfirmed: boolean
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const percent = z.coerce
    .number()
    .min(
      0,
      t('Enter a percentage from 0 to 100 with at most two decimal places.')
    )
    .max(
      100,
      t('Enter a percentage from 0 to 100 with at most two decimal places.')
    )
    .multipleOf(
      0.01,
      t('Enter a percentage from 0 to 100 with at most two decimal places.')
    )
  const schema = z
    .object({
      enabled: z.boolean(),
      level1_percent: percent,
      level2_percent: percent,
      delay_days: z.coerce
        .number()
        .int(t('Enter a whole number of days from 0 to 365.'))
        .min(0, t('Enter a whole number of days from 0 to 365.'))
        .max(365, t('Enter a whole number of days from 0 to 365.')),
    })
    .refine(
      (value) =>
        Math.round(value.level1_percent * 100) +
          Math.round(value.level2_percent * 100) <=
        10_000,
      {
        message: t('The two rebate rates must add up to no more than 100%.'),
        path: ['level2_percent'],
      }
    )
  const parsed = tryJsonParse<ReferralSettings>(props.defaultValue)
  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<ReferralSettings>({
      resolver: zodResolver(schema) as Resolver<ReferralSettings>,
      defaultValues: parsed.success ? parsed.data : DEFAULT_SETTINGS,
      onSubmit: async (data) => {
        await updateOption.mutateAsync({
          key: 'ReferralSetting',
          value: JSON.stringify(data),
        })
      },
    })

  return (
    <SettingsSection title={t('Referral Rebates')}>
      <FormNavigationGuard when={isDirty} />
      {!props.complianceConfirmed && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t(
              'Non-zero invitation rewards require compliance confirmation in Payment Gateway settings.'
            )}
          </AlertDescription>
        </Alert>
      )}
      <Form {...form}>
        <SettingsForm onSubmit={handleSubmit}>
          <SettingsPageFormActions
            onSave={handleSubmit}
            isSaving={isSubmitting}
          />
          <FormDirtyIndicator isDirty={isDirty} />
          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Recharge Rebates')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Disabling rebates stops new rewards. Pending rewards keep their original rates and due dates.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={!props.complianceConfirmed || isSubmitting}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />
          <SettingsFormGrid>
            <FormField
              control={form.control}
              name='level1_percent'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Direct Rebate Rate (%)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={100}
                      step={0.01}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Percentage awarded to the user who directly invited the paying user.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='level2_percent'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Team Rebate Rate (%)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={100}
                      step={0.01}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Percentage awarded to the inviter of the direct inviter. Set to 0 to disable level 2.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='delay_days'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Reward Delay (days)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={0}
                      max={365}
                      step={1}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      '0 means immediate arrival. Delayed rewards are settled automatically after the specified number of 24-hour periods.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsFormGrid>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Rebates use the paid amount converted to account credits at the checkout price. Discounts reduce rebates. Redemption codes, balance transfers and subscriptions do not earn rebates.'
            )}
          </p>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
