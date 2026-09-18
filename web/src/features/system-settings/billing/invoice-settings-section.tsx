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

type InvoiceSettings = {
  enabled: boolean
  all_users: boolean
  user_ids: number[]
}

type InvoiceSettingsFormValues = Omit<InvoiceSettings, 'user_ids'> & {
  user_ids: string
}

const DEFAULT_SETTINGS: InvoiceSettings = {
  enabled: false,
  all_users: false,
  user_ids: [],
}

function parseUserIds(value: string): number[] {
  return value
    .split(/[\s,，;；]+/)
    .map((item) => Number.parseInt(item, 10))
    .filter((item) => Number.isInteger(item) && item > 0)
}

export function InvoiceSettingsSection(props: { defaultValue: string }) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const parsed = tryJsonParse<InvoiceSettings>(props.defaultValue)
  const settings = parsed.success ? parsed.data : DEFAULT_SETTINGS
  const schema = z.object({
    enabled: z.boolean(),
    all_users: z.boolean(),
    user_ids: z
      .string()
      .refine(
        (value) => {
          const ids = parseUserIds(value)
          const parts = value
            .split(/[\s,，;；]+/)
            .filter((item) => item !== '')
          return (
            parts.length === ids.length &&
            new Set(ids).size === ids.length &&
            ids.length <= 10000
          )
        },
        t('Enter unique positive user IDs separated by commas.')
      ),
  })
  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<InvoiceSettingsFormValues>({
      resolver: zodResolver(schema) as Resolver<InvoiceSettingsFormValues>,
      defaultValues: {
        enabled: settings.enabled,
        all_users: settings.all_users,
        user_ids: settings.user_ids.join(', '),
      },
      onSubmit: async (data) => {
        await updateOption.mutateAsync({
          key: 'InvoiceSetting',
          value: JSON.stringify({
            enabled: data.enabled,
            all_users: data.all_users,
            user_ids: parseUserIds(data.user_ids),
          }),
        })
      },
    })

  return (
    <SettingsSection title={t('Invoice settings')}>
      <FormNavigationGuard when={isDirty} />
      <Form {...form}>
        <SettingsForm onSubmit={handleSubmit}>
          <SettingsPageFormActions
            onSave={handleSubmit}
            isSaving={isSubmitting}
          />
          <FormDirtyIndicator isDirty={isDirty} />
          <SettingsFormGrid>
            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable invoice applications')}</FormLabel>
                    <FormDescription>
                      {t('Controls whether users can submit invoice requests.')}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='all_users'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Available to all users')}</FormLabel>
                    <FormDescription>
                      {t(
                        'When disabled, only the listed user IDs can access invoice applications.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <FormField
              control={form.control}
              name='user_ids'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Allowed user IDs')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      placeholder={t('1, 23, 456')}
                      disabled={form.watch('all_users')}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Separate user IDs with commas.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </SettingsFormGrid>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
