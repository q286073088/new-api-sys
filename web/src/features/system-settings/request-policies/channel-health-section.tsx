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
import { useMemo, useRef, useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { JsonEditor } from '@/components/json-editor'
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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'
import { parseHttpStatusCodeRules } from '@/lib/http-status-code-rules'

import {
  SettingsControlChildren,
  SettingsControlGroup,
  SettingsForm,
  SettingsFormGrid,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { safeNumberFieldProps } from '../utils/numeric-field'
import type { HealthSettings } from './defaults'
import { useSavePolicy } from './use-save-policy'

const numericString = z.string().refine((value) => {
  const trimmed = value.trim()
  if (!trimmed) return true
  return !Number.isNaN(Number(trimmed)) && Number(trimmed) >= 0
}, 'Enter a non-negative number or leave empty')

const channelTestModes = [
  'scheduled_all',
  'auto_ban_only',
  'passive_recovery',
  'available_models',
] as const
type ChannelTestMode = (typeof channelTestModes)[number]
const MAX_CHANNEL_TEST_CONCURRENCY = 32

const createChannelHealthSchema = (
  t: (key: string, options?: Record<string, unknown>) => string
) =>
  z
    .object({
      RetryTimes: z.coerce.number().int().min(0).max(10),
      ModelRetryTimes: z.string().refine((value) => {
        try {
          const parsed: unknown = JSON.parse(value.trim() || '{}')
          return (
            parsed !== null &&
            typeof parsed === 'object' &&
            !Array.isArray(parsed) &&
            Object.entries(parsed).every(
              ([name, count]) =>
                name.trim() !== '' &&
                name === name.trim() &&
                typeof count === 'number' &&
                Number.isInteger(count) &&
                count >= 0 &&
                count <= 10
            )
          )
        } catch {
          return false
        }
      }, '请输入准确的模型名称和 0 到 10 之间的整数重试次数。'),
      ChannelDisableThreshold: numericString,
      AutomaticDisableChannelEnabled: z.boolean(),
      AutomaticEnableChannelEnabled: z.boolean(),
      AutomaticDisableKeywords: z.string(),
      AutomaticDisableStatusCodes: z.string(),
      AutomaticRetryStatusCodes: z.string(),
      perf_metrics_setting: z.object({
        exclude_errors_enabled: z.boolean(),
        excluded_status_codes: z.string().refine((value) =>
          value.split(/[,\s]+/).filter(Boolean).every((code) => /^\d{3}$/.test(code) && Number(code) >= 100 && Number(code) <= 599),
          '请输入用逗号分隔的 HTTP 状态码'),
      }),
      monitor_setting: z.object({
        channel_test_models: z.string().max(20000),
        auto_test_channel_enabled: z.boolean(),
        auto_test_channel_minutes: z.coerce
          .number()
          .int()
          .min(1, t('Interval must be at least 1 minute')),
        channel_test_concurrency: z.coerce
          .number()
          .int(t('Enter a positive integer'))
          .min(1, t('Channel test concurrency must be between 1 and 32'))
          .max(
            MAX_CHANNEL_TEST_CONCURRENCY,
            t('Channel test concurrency must be between 1 and 32')
          ),
        channel_test_mode: z.enum(channelTestModes),
        channel_test_prompt: z
          .string()
          .refine(
            (value) => [...value].length <= 20000,
            t('Test prompt must not exceed 20,000 characters')
          ),
        channel_test_max_tokens: z.coerce
          .number()
          .int(t('Enter a positive integer'))
          .min(1, t('Test output limit must be between 1 and 32,768 tokens'))
          .max(
            32768,
            t('Test output limit must be between 1 and 32,768 tokens')
          ),
      }),
    })
    .superRefine((values, ctx) => {
      if (values.monitor_setting.channel_test_mode === 'available_models' && !values.monitor_setting.channel_test_models.trim()) {
        ctx.addIssue({ code: 'custom', path: ['monitor_setting', 'channel_test_models'], message: t('Enter at least one model') })
      }
      const disableParsed = parseHttpStatusCodeRules(
        values.AutomaticDisableStatusCodes
      )
      if (!disableParsed.ok) {
        ctx.addIssue({
          code: 'custom',
          path: ['AutomaticDisableStatusCodes'],
          message: t('Invalid status code rules: {{tokens}}', {
            tokens: disableParsed.invalidTokens.join(', '),
          }),
        })
      }
    })

type RoutingReliabilitySchema = ReturnType<
  typeof createRoutingReliabilitySchema
>
type RoutingReliabilityFormValues = z.output<RoutingReliabilitySchema>
type RoutingReliabilityFormInput = z.input<RoutingReliabilitySchema>

type RoutingReliabilitySectionProps = {
  defaultValues: {
    RetryTimes: number
    ModelRetryTimes: string
    ChannelDisableThreshold: string
    AutomaticDisableChannelEnabled: boolean
    AutomaticEnableChannelEnabled: boolean
    AutomaticDisableKeywords: string
    AutomaticDisableStatusCodes: string
    AutomaticRetryStatusCodes: string
    'monitor_setting.auto_test_channel_enabled': boolean
    'monitor_setting.auto_test_channel_minutes': number
    'monitor_setting.channel_test_concurrency': number
    'monitor_setting.channel_test_mode': ChannelTestMode
    'monitor_setting.channel_test_prompt'?: string
    'monitor_setting.channel_test_max_tokens'?: number
    'monitor_setting.channel_test_models'?: string
    'perf_metrics_setting.exclude_errors_enabled'?: boolean
    'perf_metrics_setting.excluded_status_codes'?: string
  }
}

function normalizeLineEndings(value: string) {
  return value.replaceAll('\r\n', '\n')
}

type NormalizedRoutingReliabilityValues = {
  RetryTimes: number
  ModelRetryTimes: string
  ChannelDisableThreshold: string
  AutomaticDisableChannelEnabled: boolean
  AutomaticEnableChannelEnabled: boolean
  AutomaticDisableKeywords: string
  AutomaticDisableStatusCodes: string
  'monitor_setting.auto_test_channel_enabled': boolean
  'monitor_setting.auto_test_channel_minutes': number
  'monitor_setting.channel_test_concurrency': number
  'monitor_setting.channel_test_mode': ChannelTestMode
  'monitor_setting.channel_test_prompt': string
  'monitor_setting.channel_test_max_tokens': number
  'monitor_setting.channel_test_models': string
  'perf_metrics_setting.exclude_errors_enabled': boolean
  'perf_metrics_setting.excluded_status_codes': string
}

function normalizeChannelTestMode(value?: string): ChannelTestMode {
  if (value === 'auto_ban_only' || value === 'passive_recovery' || value === 'available_models') {
    return value
  }
  return 'scheduled_all'
}

const buildFormDefaults = (
  defaults: RoutingReliabilitySectionProps['defaultValues']
): RoutingReliabilityFormInput => ({
  RetryTimes: defaults.RetryTimes ?? 0,
  ModelRetryTimes: defaults.ModelRetryTimes || '{}',
  ChannelDisableThreshold: defaults.ChannelDisableThreshold ?? '',
  AutomaticDisableChannelEnabled: defaults.AutomaticDisableChannelEnabled,
  AutomaticEnableChannelEnabled: defaults.AutomaticEnableChannelEnabled,
  AutomaticDisableKeywords: normalizeLineEndings(
    defaults.AutomaticDisableKeywords ?? ''
  ),
  AutomaticDisableStatusCodes: defaults.AutomaticDisableStatusCodes ?? '',
  AutomaticRetryStatusCodes: defaults.AutomaticRetryStatusCodes ?? '',
  perf_metrics_setting: {
    exclude_errors_enabled: defaults['perf_metrics_setting.exclude_errors_enabled'] ?? false,
    excluded_status_codes: defaults['perf_metrics_setting.excluded_status_codes'] ?? '',
  },
  monitor_setting: {
    channel_test_models: defaults['monitor_setting.channel_test_models'] ?? '',
    auto_test_channel_enabled:
      defaults['monitor_setting.auto_test_channel_enabled'],
    auto_test_channel_minutes:
      defaults['monitor_setting.auto_test_channel_minutes'],
    channel_test_concurrency:
      defaults['monitor_setting.channel_test_concurrency'],
    channel_test_mode: normalizeChannelTestMode(
      defaults['monitor_setting.channel_test_mode']
    ),
    channel_test_prompt: normalizeLineEndings(
      defaults['monitor_setting.channel_test_prompt'] ?? ''
    ),
    channel_test_max_tokens:
      defaults['monitor_setting.channel_test_max_tokens'] ?? 4096,
  },
})

const normalizeDefaults = (
  defaults: RoutingReliabilitySectionProps['defaultValues']
): NormalizedRoutingReliabilityValues => ({
  RetryTimes: defaults.RetryTimes ?? 0,
  ModelRetryTimes: defaults.ModelRetryTimes || '{}',
  ChannelDisableThreshold: (defaults.ChannelDisableThreshold ?? '').trim(),
  AutomaticDisableChannelEnabled: defaults.AutomaticDisableChannelEnabled,
  AutomaticEnableChannelEnabled: defaults.AutomaticEnableChannelEnabled,
  AutomaticDisableKeywords: normalizeLineEndings(
    defaults.AutomaticDisableKeywords ?? ''
  ),
  AutomaticDisableStatusCodes: parseHttpStatusCodeRules(
    defaults.AutomaticDisableStatusCodes ?? ''
  ).normalized,
  'monitor_setting.auto_test_channel_enabled':
    defaults['monitor_setting.auto_test_channel_enabled'],
  'monitor_setting.auto_test_channel_minutes':
    defaults['monitor_setting.auto_test_channel_minutes'],
  'monitor_setting.channel_test_concurrency':
    defaults['monitor_setting.channel_test_concurrency'],
  'monitor_setting.channel_test_mode': normalizeChannelTestMode(
    defaults['monitor_setting.channel_test_mode']
  ),
  'monitor_setting.channel_test_prompt': normalizeLineEndings(
    defaults['monitor_setting.channel_test_prompt'] ?? ''
  ).trim(),
  'monitor_setting.channel_test_max_tokens':
    defaults['monitor_setting.channel_test_max_tokens'] ?? 4096,
  'monitor_setting.channel_test_models': (defaults['monitor_setting.channel_test_models'] ?? '').trim(),
  'perf_metrics_setting.exclude_errors_enabled': defaults['perf_metrics_setting.exclude_errors_enabled'] ?? false,
  'perf_metrics_setting.excluded_status_codes': defaults['perf_metrics_setting.excluded_status_codes'] ?? '',
})

const normalizeFormValues = (
  values: RoutingReliabilityFormValues
): NormalizedRoutingReliabilityValues => ({
  RetryTimes: values.RetryTimes,
  ModelRetryTimes: values.ModelRetryTimes.trim() || '{}',
  ChannelDisableThreshold: values.ChannelDisableThreshold.trim(),
  AutomaticDisableChannelEnabled: values.AutomaticDisableChannelEnabled,
  AutomaticEnableChannelEnabled: values.AutomaticEnableChannelEnabled,
  AutomaticDisableKeywords: normalizeLineEndings(
    values.AutomaticDisableKeywords
  ),
  AutomaticDisableStatusCodes: parseHttpStatusCodeRules(
    values.AutomaticDisableStatusCodes
  ).normalized,
  'monitor_setting.auto_test_channel_enabled':
    values.monitor_setting.auto_test_channel_enabled,
  'monitor_setting.auto_test_channel_minutes':
    values.monitor_setting.auto_test_channel_minutes,
  'monitor_setting.channel_test_concurrency':
    values.monitor_setting.channel_test_concurrency,
  'monitor_setting.channel_test_mode': values.monitor_setting.channel_test_mode,
  'monitor_setting.channel_test_prompt': normalizeLineEndings(
    values.monitor_setting.channel_test_prompt
  ).trim(),
  'monitor_setting.channel_test_max_tokens':
    values.monitor_setting.channel_test_max_tokens,
  'monitor_setting.channel_test_models': values.monitor_setting.channel_test_models.trim(),
  'perf_metrics_setting.exclude_errors_enabled': values.perf_metrics_setting.exclude_errors_enabled,
  'perf_metrics_setting.excluded_status_codes': values.perf_metrics_setting.excluded_status_codes.trim(),
})

export function ChannelHealthSection({
  defaultValues,
}: ChannelHealthSectionProps) {
  const { t } = useTranslation()
  const updateOption = useSavePolicy()
  const channelHealthSchema = createChannelHealthSchema(t)
  const baselineRef = useRef<NormalizedChannelHealthValues>(
    normalizeDefaults(defaultValues)
  )

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<
    ChannelHealthFormInput,
    unknown,
    ChannelHealthFormValues
  >({
    resolver: zodResolver(channelHealthSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)
  useEffect(() => {
    baselineRef.current = normalizeDefaults(defaultValues)
  }, [defaultValues])

  const autoDisableStatusCodes = form.watch('AutomaticDisableStatusCodes')
  const channelTestMode = form.watch('monitor_setting.channel_test_mode')
  const autoEnable = form.watch('AutomaticEnableChannelEnabled')
  let channelTestModeDescription: string
  switch (channelTestMode) {
    case 'available_models':
      channelTestModeDescription = '按渠道优先级测试已配置的模型，某个模型首次测试成功后停止继续测试。'
      break
    case 'auto_ban_only':
      channelTestModeDescription = t(
        'Periodically checks only channels with auto-disable enabled, excluding manually disabled channels.'
      )
      break
    case 'passive_recovery':
      channelTestModeDescription = t(
        'Does not check healthy channels. It only rechecks auto-disabled channels and restores them after they recover.'
      )
      break
    default:
      channelTestModeDescription = t(
        'Periodically checks all channels except manually disabled ones to detect failures and recover channels automatically.'
      )
  }
  const autoDisableParsed = useMemo(
    () => parseHttpStatusCodeRules(autoDisableStatusCodes),
    [autoDisableStatusCodes]
  )

  const onSubmit = async (values: ChannelHealthFormValues) => {
    const normalized = normalizeFormValues(values)
    const updates = (
      Object.keys(normalized) as Array<keyof NormalizedChannelHealthValues>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    try {
      await updateOption.mutateAsync(
        Object.fromEntries(updates.map((key) => [key, String(normalized[key])]))
      )
      baselineRef.current = normalized
    } catch (error) {
      handleServerError(error)
    }
  }

  return (
    <SettingsSection title={t('Channel health')}>
      <div className='text-muted-foreground space-y-1 text-sm'>
        <p>{t('Source: global settings. Changes take effect after saving.')}</p>
        <p>
          {form.watch('AutomaticDisableChannelEnabled')
            ? t(
                'Channels must also enable Auto Ban before automatic disabling can take effect.'
              )
            : t(
                'With these settings, automatic disabling is off for all channels.'
              )}
        </p>
        {form.watch('AutomaticEnableChannelEnabled') &&
          !form.watch('monitor_setting.auto_test_channel_enabled') && (
            <p>
              {t(
                'Scheduled recovery is off. Bulk channel tests can still re-enable automatically disabled channels.'
              )}
            </p>
          )}
      </div>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={form.formState.isSubmitting}
          />

          <div className='flex min-w-0 flex-col gap-4'>
            <div className='flex flex-col gap-1'>
              <h4 className='text-sm font-medium'>{t('Request retry')}</h4>
            </div>
            <div className='grid min-w-0 gap-6 xl:grid-cols-[minmax(12rem,24rem)_minmax(0,1fr)]'>
              <FormField
                control={form.control}
                name='RetryTimes'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Retry Times')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min='0'
                        max='10'
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Number of times to retry failed requests (0-10)')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='ModelRetryTimes'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>按模型设置重试次数</FormLabel>
                    <FormControl>
                      <div
                        role='group'
                        aria-label='按模型设置重试次数'
                      >
                        <JsonEditor
                          value={field.value}
                          onChange={field.onChange}
                          valueType='any'
                          keyLabel='模型名称'
                          keyPlaceholder='模型名称'
                          valueLabel='重试次数'
                          valuePlaceholder='重试次数'
                        />
                      </div>
                    </FormControl>
                    <FormDescription>
                      {t(
                        '使用客户端请求中的模型名称。为模型单独设置的次数会限制整个请求的重试次数；设置为 0 表示不重试。未列出的模型继续使用全局和跨分组的重试规则。'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='AutomaticRetryStatusCodes'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Auto-retry status codes')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('e.g. 401, 403, 429, 500-599')}
                        value={field.value}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Accepts comma-separated status codes and inclusive ranges.'
                      )}{' '}
                      {autoRetryParsed.ok &&
                        autoRetryParsed.normalized &&
                        autoRetryParsed.normalized !== field.value.trim() && (
                          <span className='text-muted-foreground'>
                            {t('Normalized:')} {autoRetryParsed.normalized}
                          </span>
                        )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </div>

          <Separator />

          <div className='flex min-w-0 flex-col gap-4'>
            <div className='flex flex-col gap-1'>
              <h4 className='text-sm font-medium'>
                {t('Channel health checks')}
              </h4>
            </div>
            <FormField
              control={form.control}
              name='monitor_setting.channel_test_prompt'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Channel test prompt')}</FormLabel>
                  <FormControl>
                    <Textarea
                      {...field}
                      className='min-h-28'
                      placeholder={t(
                        'Enter a question to evaluate model responses'
                      )}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Used for manual and scheduled text-model tests. Leave blank to compare 9.11 and 9.9. Test logs show the prompt and answer to administrators; regular requests do not store responses.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <div className='grid min-w-0 gap-6 lg:grid-cols-3'>
              <FormField
                control={form.control}
                name='monitor_setting.channel_test_max_tokens'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Test maximum output tokens')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        max={32768}
                        step={1}
                        {...safeNumberFieldProps(field)}
                        onChange={(event) =>
                          field.onChange(
                            event.target.value === ''
                              ? ''
                              : event.target.valueAsNumber
                          )
                        }
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Allow enough tokens for the answer and any model reasoning. Default: 4,096.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='monitor_setting.auto_test_channel_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Scheduled channel tests')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Automatically probe all channels in the background'
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
                name='monitor_setting.channel_test_mode'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Channel test mode')}</FormLabel>
                    <Select
                      items={[
                        {
                          value: 'scheduled_all',
                          label: t('Actively check all channels'),
                        },
                        {
                          value: 'auto_ban_only',
                          label: t(
                            'Actively check auto-disable-enabled channels'
                          ),
                        },
                        {
                          value: 'passive_recovery',
                          label: t('Check channels awaiting recovery only'),
                        },
                        {
                          value: 'available_models',
                          label: '测试配置的可用模型',
                        },
                      ]}
                      value={field.value}
                      onValueChange={field.onChange}
                    >
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='scheduled_all'>
                            {t('Actively check all channels')}
                          </SelectItem>
                          <SelectItem value='auto_ban_only'>
                            {t('Actively check auto-disable-enabled channels')}
                          </SelectItem>
                          <SelectItem value='passive_recovery'>
                            {t('Check channels awaiting recovery only')}
                          </SelectItem>
                          <SelectItem value='available_models'>
                            测试配置的可用模型
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {channelTestModeDescription}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='monitor_setting.channel_test_models'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>待测试模型</FormLabel>
                    <FormControl>
                        <Textarea {...field} rows={3} placeholder='每行填写一个模型名称' />
                    </FormControl>
                    <FormDescription>仅在“测试配置的可用模型”模式下生效。</FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='monitor_setting.auto_test_channel_minutes'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Test interval (minutes)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {channelTestMode === 'passive_recovery'
                        ? t(
                            'How frequently the system checks auto-disabled channels for recovery'
                          )
                        : t('How frequently the system tests all channels')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='monitor_setting.channel_test_concurrency'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Channel test concurrency')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        max={MAX_CHANNEL_TEST_CONCURRENCY}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Maximum number of channels tested at the same time (1-32)'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='AutomaticEnableChannelEnabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Re-enable on success')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Successful scheduled or bulk checks can restore automatically disabled channels. Manually disabled channels stay disabled.'
                        )}
                        {channelTestMode === 'passive_recovery' &&
                          !autoEnable && (
                            <span className='block text-amber-600 dark:text-amber-400'>
                              {t(
                                'Recovery checks will not enable channels until this switch is turned on.'
                              )}
                            </span>
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
                name='perf_metrics_setting.exclude_errors_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>从模型广场排除错误码</FormLabel>
                      <FormDescription>包含这些状态码的失败请求不会影响模型广场统计。</FormDescription>
                    </SettingsSwitchContent>
                    <FormControl><Switch checked={field.value} onCheckedChange={field.onChange} /></FormControl>
                  </SettingsSwitchItem>
                )}
              />
              <FormField
                control={form.control}
                name='perf_metrics_setting.excluded_status_codes'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>要排除的 HTTP 状态码</FormLabel>
                    <FormControl><Input {...field} placeholder='400,502,524' /></FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </div>

          <Separator />

          <div className='flex min-w-0 flex-col gap-4'>
            <h4 className='text-sm font-medium'>{t('Auto-disable rules')}</h4>
            <SettingsFormGrid>
              <FormField
                control={form.control}
                name='AutomaticDisableChannelEnabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Disable on failure')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Apply disable rules to upstream request errors and scheduled or bulk health checks'
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
                name='ChannelDisableThreshold'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Health check timeout threshold (seconds)')}
                    </FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        step={1}
                        value={field.value}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Scheduled or bulk health checks can disable a channel when this duration is exceeded, if both global and channel auto-disable are enabled.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='AutomaticDisableStatusCodes'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Auto-disable status codes')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('e.g. 401, 403, 429, 500-599')}
                        value={field.value}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Accepts comma-separated status codes and inclusive ranges.'
                      )}{' '}
                      {autoDisableParsed.ok &&
                        autoDisableParsed.normalized &&
                        autoDisableParsed.normalized !== field.value.trim() && (
                          <span className='text-muted-foreground'>
                            {t('Normalized:')} {autoDisableParsed.normalized}
                          </span>
                        )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='AutomaticDisableKeywords'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Failure keywords')}</FormLabel>
                    <FormControl>
                      <Textarea
                        rows={6}
                        placeholder={t('one keyword per line')}
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'If an upstream error contains any of these keywords (case insensitive), the channel will be disabled automatically.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SettingsFormGrid>
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
