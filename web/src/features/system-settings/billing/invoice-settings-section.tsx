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
  const updateOption = useUpdateOption()
  const parsed = tryJsonParse<InvoiceSettings>(props.defaultValue)
  const rawSettings = parsed.success ? parsed.data : DEFAULT_SETTINGS
  const settings: InvoiceSettings = {
    enabled: rawSettings?.enabled === true,
    all_users: rawSettings?.all_users === true,
    user_ids: Array.isArray(rawSettings?.user_ids)
      ? rawSettings.user_ids.filter(
          (userId) => Number.isInteger(userId) && userId > 0
        )
      : [],
  }
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
        '请输入用逗号分隔的唯一正整数用户 ID。'
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
    <SettingsSection title='发票设置'>
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
                    <FormLabel>开启开票申请</FormLabel>
                    <FormDescription>
                      控制用户是否可以提交开票申请。
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
                    <FormLabel>对所有用户开放</FormLabel>
                    <FormDescription>
                      关闭后，仅列表中的用户 ID 可以使用开票申请。
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
                  <FormLabel>允许用户 ID</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      placeholder='1, 23, 456'
                      disabled={form.watch('all_users')}
                    />
                  </FormControl>
                  <FormDescription>
                    用户 ID 请用逗号分隔。
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
