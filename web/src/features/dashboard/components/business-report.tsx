import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  CartesianGrid,
  Line,
  LineChart,
  XAxis,
  YAxis,
} from 'recharts'
import { Download, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@/components/ui/chart'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { api } from '@/lib/api'

type TrendPoint = {
  date: string
  revenue: number
}

type Report = {
  updated_at: number
  overview: Record<string, number>
  periods: Record<string, number>
  trend: TrendPoint[]
  payment_quality: Record<string, number>
  referral: Record<string, number>
  gifts: Record<string, number>
}

type RangePreset =
  | 'today'
  | 'last-7-days'
  | 'last-30-days'
  | 'this-month'
  | 'last-month'
  | 'custom'

type DateRange = {
  start: string
  end: string
}

const money = (value?: number) =>
  value == null || Number.isNaN(value) ? '—' : Math.round(value).toLocaleString()

function getShanghaiDate() {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(new Date())
  const values = Object.fromEntries(
    parts.map((part) => [part.type, part.value])
  )
  return `${values.year}-${values.month}-${values.day}`
}

function shiftDate(date: string, days: number) {
  const value = new Date(`${date}T00:00:00Z`)
  value.setUTCDate(value.getUTCDate() + days)
  return value.toISOString().slice(0, 10)
}

function getRange(
  preset: RangePreset,
  today: string,
  customStart: string,
  customEnd: string
): DateRange {
  if (preset === 'today') {
    return { start: today, end: today }
  }
  if (preset === 'last-7-days') {
    return { start: shiftDate(today, -6), end: today }
  }
  if (preset === 'this-month') {
    return { start: `${today.slice(0, 7)}-01`, end: today }
  }
  if (preset === 'last-month') {
    const currentMonthStart = `${today.slice(0, 7)}-01`
    const lastMonthEnd = shiftDate(currentMonthStart, -1)
    return {
      start: `${lastMonthEnd.slice(0, 7)}-01`,
      end: lastMonthEnd,
    }
  }
  if (preset === 'custom') {
    return { start: customStart, end: customEnd }
  }
  return { start: shiftDate(today, -29), end: today }
}

export function BusinessReport() {
  const { t } = useTranslation()
  const today = useMemo(() => getShanghaiDate(), [])
  const [rangePreset, setRangePreset] = useState<RangePreset>('last-30-days')
  const [customStart, setCustomStart] = useState(() => shiftDate(today, -29))
  const [customEnd, setCustomEnd] = useState(today)
  const range = useMemo(
    () => getRange(rangePreset, today, customStart, customEnd),
    [customEnd, customStart, rangePreset, today]
  )
  const rangeIsValid = Boolean(range.start && range.end) && range.start <= range.end
  const queryString = new URLSearchParams({
    start: range.start,
    end: range.end,
  }).toString()
  const query = useQuery({
    queryKey: ['business-report', range.start, range.end],
    queryFn: async () =>
      (await api.get<{ data: Report }>(`/api/dashboard/business-report?${queryString}`))
        .data.data,
    enabled: rangeIsValid,
    refetchInterval: 60_000,
  })

  const chartConfig = {
    revenue: {
      label: t('Revenue'),
      color: 'var(--chart-1)',
    },
  } satisfies ChartConfig

  if (!rangeIsValid) {
    return (
      <div className='text-destructive text-sm'>
        {t('Select a valid date range')}
      </div>
    )
  }
  if (query.isLoading) {
    return <div>{t('Loading...')}</div>
  }

  const data = query.data
  if (!data) {
    return <div>—</div>
  }

  const cards: Array<[string, number]> = [
    ['Registered users', data.overview.registered_users],
    ['Paid users', data.overview.paid_users],
    ['Paid conversion rate', data.overview.paid_conversion],
    ['Cumulative revenue', data.overview.revenue],
  ]
  const exportUrl = `/api/dashboard/business-report/export?${queryString}`

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap justify-end gap-2'>
        <span className='text-muted-foreground self-center text-xs'>
          {t('Updated')}{' '}
          {new Date(data.updated_at * 1000).toLocaleString(undefined, {
            timeZone: 'Asia/Shanghai',
          })}
        </span>
        <Button
          variant='outline'
          size='sm'
          onClick={() => window.open(exportUrl, '_blank')}
        >
          <Download className='size-4' />
          {t('Export CSV')}
        </Button>
        <Button
          variant='ghost'
          size='icon-sm'
          aria-label={t('Refresh')}
          onClick={() => query.refetch()}
        >
          <RefreshCw className='size-4' />
        </Button>
      </div>

      <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        {cards.map(([key, value]) => (
          <Card key={key}>
            <CardHeader>
              <CardTitle className='text-muted-foreground text-sm'>
                {t(key)}
              </CardTitle>
            </CardHeader>
            <CardContent className='text-2xl font-semibold'>
              {key === 'Paid conversion rate'
                ? `${value.toFixed(1)}%`
                : money(value)}
            </CardContent>
          </Card>
        ))}
      </div>

      <div className='grid gap-3 md:grid-cols-2'>
        <Card>
          <CardHeader>
            <CardTitle>{t('Revenue periods')}</CardTitle>
          </CardHeader>
          <CardContent className='grid grid-cols-2 gap-3'>
            {Object.entries(data.periods).map(([key, value]) => (
              <div key={key}>
                <div className='text-muted-foreground text-sm'>{t(key)}</div>
                <div className='font-semibold'>{money(value)}</div>
              </div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('Payment quality')}</CardTitle>
          </CardHeader>
          <CardContent className='grid grid-cols-3 gap-3'>
            {Object.entries(data.payment_quality).map(([key, value]) => (
              <div key={key}>
                <div className='text-muted-foreground text-sm'>{t(key)}</div>
                <div className='font-semibold'>{money(value)}</div>
              </div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('Referral distribution')}</CardTitle>
          </CardHeader>
          <CardContent className='grid grid-cols-2 gap-3'>
            {Object.entries(data.referral).map(([key, value]) => (
              <div key={key}>
                <div className='text-muted-foreground text-sm'>{t(key)}</div>
                <div className='font-semibold'>{money(value)}</div>
              </div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('Gift amount')}</CardTitle>
          </CardHeader>
          <CardContent className='text-2xl font-semibold'>
            {money(data.gifts.amount)}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className='gap-3'>
          <CardTitle>{t('Revenue trend')}</CardTitle>
          <div className='flex flex-col gap-2'>
            <Tabs
              value={rangePreset}
              onValueChange={(value) => {
                if (value) {
                  setRangePreset(value as RangePreset)
                }
              }}
            >
              <TabsList className='h-auto w-full flex-wrap justify-start gap-1 sm:w-fit'>
                <TabsTrigger value='today'>{t('Today')}</TabsTrigger>
                <TabsTrigger value='last-7-days'>
                  {t('Last 7 days')}
                </TabsTrigger>
                <TabsTrigger value='last-30-days'>
                  {t('Last 30 days')}
                </TabsTrigger>
                <TabsTrigger value='this-month'>{t('This month')}</TabsTrigger>
                <TabsTrigger value='last-month'>{t('Last month')}</TabsTrigger>
                <TabsTrigger value='custom'>{t('Custom range')}</TabsTrigger>
              </TabsList>
            </Tabs>
            {rangePreset === 'custom' && (
              <div className='flex flex-wrap items-end gap-2'>
                <label className='grid gap-1 text-xs'>
                  <span className='text-muted-foreground'>
                    {t('Start date')}
                  </span>
                  <Input
                    type='date'
                    value={customStart}
                    onChange={(event) => setCustomStart(event.target.value)}
                    className='w-[145px]'
                  />
                </label>
                <label className='grid gap-1 text-xs'>
                  <span className='text-muted-foreground'>
                    {t('End date')}
                  </span>
                  <Input
                    type='date'
                    value={customEnd}
                    onChange={(event) => setCustomEnd(event.target.value)}
                    className='w-[145px]'
                  />
                </label>
                {!rangeIsValid && (
                  <span className='text-destructive text-xs'>
                    {t('Select a valid date range')}
                  </span>
                )}
              </div>
            )}
          </div>
        </CardHeader>
        <CardContent>
          <ChartContainer config={chartConfig} className='h-[320px] w-full'>
            <LineChart
              accessibilityLayer
              data={data.trend}
              margin={{ top: 12, right: 12, left: 12, bottom: 8 }}
            >
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey='date'
                tickLine={false}
                axisLine={false}
                tickMargin={8}
                tickFormatter={(value) => String(value).slice(5)}
              />
              <YAxis
                tickLine={false}
                axisLine={false}
                width={64}
                tickFormatter={(value) => money(Number(value))}
              />
              <ChartTooltip
                cursor={false}
                content={
                  <ChartTooltipContent
                    formatter={(value) => money(Number(value))}
                  />
                }
              />
              <Line
                type='monotone'
                dataKey='revenue'
                stroke='var(--color-revenue)'
                strokeWidth={2}
                dot={false}
              />
            </LineChart>
          </ChartContainer>
        </CardContent>
      </Card>
    </div>
  )
}
