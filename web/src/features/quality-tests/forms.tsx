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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useForm, useFormContext } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { handleServerError } from '@/lib/handle-server-error'

import {
  qualityGet,
  qualityWrite,
  type QualityJudge,
  type QualityTarget,
  type QualityTest,
  type QualityTokenOption,
} from './api'

const targetSchema = z.object({
  token_id: z.number().positive(),
  group: z.string().min(1),
  model: z.string().min(1),
  endpoint: z.enum(['chat', 'responses']),
})
const testSchema = targetSchema.extend({
  name: z.string().trim().min(1).max(200),
  prompt: z.string().trim().min(1).max(20000),
  expected_answer: z.string().trim().min(1).max(20000),
  interval_minutes: z.number().int().min(1).max(10080),
  enabled: z.boolean(),
  public: z.boolean(),
})
const judgeSchema = targetSchema.extend({
  instructions: z.string().trim().min(1).max(20000),
})
type TestValues = z.infer<typeof testSchema>

// Both forms share the same token-scoped target fields. Lists are derived from
// server-authorized options; changing a parent clears the dependent selection.
function TargetFields({ options }: { options: QualityTokenOption[] }) {
  const { t } = useTranslation()
  const form = useFormContext<QualityTarget>()
  const token = options.find((item) => item.id === form.watch('token_id'))
  const groups = token?.groups ?? []
  const models =
    groups.find((item) => item.name === form.watch('group'))?.models ?? []
  return (
    <div className='grid gap-4 sm:grid-cols-2'>
      <FormField
        control={form.control}
        name='token_id'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Administrator API key')}</FormLabel>
            <FormControl>
              <NativeSelect
                className='w-full'
                value={field.value || ''}
                onChange={(event) => {
                  field.onChange(Number(event.target.value))
                  form.setValue('group', '')
                  form.setValue('model', '')
                }}
              >
                <option value=''>{t('Select an API key')}</option>
                {options.map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name} · {item.key}
                  </option>
                ))}
              </NativeSelect>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name='group'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Group')}</FormLabel>
            <FormControl>
              <NativeSelect
                className='w-full'
                {...field}
                onChange={(event) => {
                  field.onChange(event)
                  form.setValue('model', '')
                }}
              >
                <option value=''>{t('Select a group')}</option>
                {groups.map((item) => (
                  <option key={item.name} value={item.name}>
                    {item.name}
                  </option>
                ))}
              </NativeSelect>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name='model'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Model')}</FormLabel>
            <FormControl>
              <NativeSelect className='w-full' {...field}>
                <option value=''>{t('Select a model')}</option>
                {models.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </NativeSelect>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name='endpoint'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('API format')}</FormLabel>
            <FormControl>
              <NativeSelect className='w-full' {...field}>
                <option value='chat'>Chat Completions</option>
                <option value='responses'>Responses</option>
              </NativeSelect>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
    </div>
  )
}

export function QualityTaskForm({
  task,
  options,
  onClose,
}: {
  task?: QualityTest
  options: QualityTokenOption[]
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<TestValues>({
    resolver: zodResolver(testSchema),
    defaultValues: task ?? {
      token_id: 0,
      group: '',
      model: '',
      endpoint: 'chat',
      name: '',
      prompt: '',
      expected_answer: '',
      interval_minutes: 30,
      enabled: false,
      public: false,
    },
  })
  const save = useMutation({
    mutationFn: (values: TestValues) =>
      qualityWrite(
        task ? `/api/quality-tests/${task.id}` : '/api/quality-tests',
        task ? 'put' : 'post',
        values
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['quality-tests'] })
      onClose()
    },
    onError: (error) => handleServerError(error),
  })
  return (
    <Form {...form}>
      <form
        className='space-y-5'
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
      >
        <FormField
          control={form.control}
          name='name'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Name')}</FormLabel>
              <FormControl>
                <Input {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <TargetFields options={options} />
        <FormField
          control={form.control}
          name='prompt'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Test question')}</FormLabel>
              <FormControl>
                <Textarea rows={4} {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='expected_answer'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Reference answer')}</FormLabel>
              <FormControl>
                <Textarea rows={3} {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='interval_minutes'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Test interval (minutes)')}</FormLabel>
              <FormControl>
                <Input
                  type='number'
                  min={1}
                  max={10080}
                  {...field}
                  onChange={(event) =>
                    field.onChange(event.target.valueAsNumber)
                  }
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='enabled'
          render={({ field }) => (
            <FormItem className='flex items-center justify-between'>
              <FormLabel>{t('Enable scheduled tests')}</FormLabel>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='public'
          render={({ field }) => (
            <FormItem className='flex items-center justify-between'>
              <FormLabel>{t('Show results to signed-in users')}</FormLabel>
              <FormControl>
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            'Model calls and judge calls are billed to their selected API keys. IP restrictions also apply to local scheduled requests.'
          )}
        </p>
        <div className='flex justify-end gap-2'>
          <Button variant='outline' type='button' onClick={onClose}>
            {t('Cancel')}
          </Button>
          <Button type='submit' disabled={save.isPending}>
            {t('Save')}
          </Button>
        </div>
      </form>
    </Form>
  )
}

function JudgeForm({
  judge,
  options,
}: {
  judge: QualityJudge
  options: QualityTokenOption[]
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<QualityJudge>({
    resolver: zodResolver(judgeSchema),
    defaultValues: judge,
  })
  const save = useMutation({
    mutationFn: (values: QualityJudge) =>
      qualityWrite('/api/quality-tests/judge', 'put', values),
    onSuccess: () => {
      form.reset(form.getValues())
      void queryClient.invalidateQueries({
        queryKey: ['quality-tests', 'judge'],
      })
    },
    onError: (error) => handleServerError(error),
  })
  return (
    <Form {...form}>
      <form
        className='space-y-4'
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
      >
        <TargetFields options={options} />
        <FormField
          control={form.control}
          name='instructions'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Judge instructions')}</FormLabel>
              <FormControl>
                <Textarea rows={3} {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <p className='text-muted-foreground text-xs'>
          {t(
            'Matching answers pass directly. Other answers are evaluated by the judge model; unclear decisions remain pending verification.'
          )}
        </p>
        <Button
          type='submit'
          disabled={save.isPending || !form.formState.isDirty}
        >
          {t('Save judge configuration')}
        </Button>
      </form>
    </Form>
  )
}

export function QualityJudgePanel({
  options,
}: {
  options: QualityTokenOption[]
}) {
  const { t } = useTranslation()
  const judge = useQuery({
    queryKey: ['quality-tests', 'judge'],
    queryFn: () => qualityGet<QualityJudge>('/api/quality-tests/judge'),
  })
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Shared judge configuration')}</CardTitle>
      </CardHeader>
      <CardContent>
        {judge.isError && <ErrorState onRetry={() => void judge.refetch()} />}
        {judge.data && <JudgeForm judge={judge.data} options={options} />}
      </CardContent>
    </Card>
  )
}
