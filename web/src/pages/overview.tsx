import type { ReactNode } from "react"
import { Link, useSearchParams } from "react-router"
import { ActivityIcon, GaugeIcon } from "lucide-react"
import { Bar, BarChart, CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts"

import { BillingBadge } from "@/components/badges"
import { ComparisonCard } from "@/components/comparison-card"
import { Kpi } from "@/components/kpi"
import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
import { RelativeTime } from "@/components/relative-time"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  formatBucket,
  formatCount,
  formatPercent,
  formatTokens,
  formatUsd,
} from "@/lib/format"
import { useStats, useSubscriptionLimits } from "@/lib/queries"
import type { Stats, StatsPoint, StatsRange } from "@/lib/types"

const ranges: { value: StatsRange; label: string }[] = [
  { value: "24h", label: "last 24 hours" },
  { value: "7d", label: "last 7 days" },
  { value: "30d", label: "last 30 days" },
]

const spendConfig: ChartConfig = {
  cost_usd: { label: "API spend", color: "var(--chart-1)" },
  reference_cost_usd: { label: "Reference cost", color: "var(--chart-2)" },
  subscription_value_usd: { label: "Subscription value", color: "var(--chart-3)" },
}

const tokenConfig: ChartConfig = {
  api_tokens: { label: "API tokens", color: "var(--chart-1)" },
  subscription_tokens: { label: "Subscription tokens", color: "var(--chart-3)" },
}

function fillSeries(stats: Stats, until: number): StatsPoint[] {
  const byTs = new Map(stats.series.map((point) => [point.ts, point]))
  const points: StatsPoint[] = []
  for (let ts = stats.since; stats.bucket_ms > 0 && ts <= until; ts += stats.bucket_ms) {
    points.push(
      byTs.get(ts) ?? {
        ts,
        requests: 0,
        cost_usd: 0,
        subscription_value_usd: 0,
        reference_cost_usd: 0,
        api_tokens: 0,
        subscription_tokens: 0,
      },
    )
  }
  return points
}

function TooltipRow({
  color,
  label,
  value,
}: {
  color: string | undefined
  label: ReactNode
  value: string
}) {
  return (
    <>
      <span aria-hidden className="size-2.5 shrink-0 rounded-[2px]" style={{ backgroundColor: color }} />
      <span className="flex flex-1 items-center justify-between gap-4 leading-none">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-mono font-medium text-foreground tabular-nums">{value}</span>
      </span>
    </>
  )
}

function TokenSplit({ api, subscription }: { api: number; subscription: number }) {
  const total = api + subscription
  if (total === 0) {
    return <div className="h-2 rounded-full bg-muted" />
  }
  return (
    <div
      role="img"
      aria-label={`${formatPercent(api / total)} of tokens on API models, ${formatPercent(subscription / total)} on the Claude subscription`}
      className="flex h-2 gap-0.5 overflow-hidden rounded-full"
    >
      {api > 0 && <div className="bg-chart-1" style={{ width: `${(api / total) * 100}%` }} />}
      {subscription > 0 && (
        <div className="bg-chart-3" style={{ width: `${(subscription / total) * 100}%` }} />
      )}
    </div>
  )
}

function Kpis({ stats, rangeLabel }: { stats: Stats; rangeLabel: string }) {
  const t = stats.totals
  const tokens = t.input_tokens + t.output_tokens + t.cache_read_tokens + t.cache_write_tokens
  const overhead = t.director_cost_usd + t.advisor_cost_usd + t.classifier_cost_usd

  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <Kpi title="API spend" value={formatUsd(t.cost_usd)}>
        <p>Real API cost in the {rangeLabel}.</p>
      </Kpi>
      <Kpi title="Estimated savings" value={formatUsd(t.savings_usd)}>
        <p>
          Estimated against a reference cost of{" "}
          <span className="font-medium text-foreground tabular-nums">
            {formatUsd(t.reference_cost_usd)}
          </span>
          .
        </p>
      </Kpi>
      <Kpi title="Claude subscription usage" value={formatUsd(t.subscription_value_usd)}>
        <p>Valued at API prices. It costs nothing extra but counts toward your limits.</p>
      </Kpi>
      <Kpi title="Requests" value={formatCount(t.requests)}>
        <p>
          {formatCount(t.errors)} errors ·{" "}
          {t.requests > 0 ? `${formatPercent(t.errors / t.requests)} error rate` : "no traffic"}
        </p>
      </Kpi>
      <Kpi
        title="Escalation rate"
        value={t.escalate_requests > 0 ? formatPercent(t.escalated_requests / t.escalate_requests) : "—"}
      >
        <p>
          {formatCount(t.escalated_requests)} of {formatCount(t.escalate_requests)} requests on
          escalate routes moved up a tier.
        </p>
      </Kpi>
      <Kpi title="API vs subscription tokens" value={formatTokens(t.api_tokens + t.subscription_tokens)}>
        <TokenSplit api={t.api_tokens} subscription={t.subscription_tokens} />
        <div className="flex flex-wrap justify-between gap-2">
          <span className="flex items-center gap-1.5">
            <span aria-hidden className="size-2 rounded-[2px] bg-chart-1" />
            API {formatTokens(t.api_tokens)}
          </span>
          <span className="flex items-center gap-1.5">
            <span aria-hidden className="size-2 rounded-[2px] bg-chart-3" />
            Subscription {formatTokens(t.subscription_tokens)}
          </span>
        </div>
      </Kpi>
      <Kpi title="Routing overhead" value={formatUsd(overhead)}>
        <p>
          Director {formatUsd(t.director_cost_usd)} · Advisor {formatUsd(t.advisor_cost_usd)} · Classifier{" "}
          {formatUsd(t.classifier_cost_usd)}
          {t.cost_usd > 0 && ` · ${formatPercent(overhead / t.cost_usd)} of API spend`}
        </p>
      </Kpi>
      <Kpi title="Tokens processed" value={formatTokens(tokens)}>
        <p>
          Input {formatTokens(t.input_tokens)} · Output {formatTokens(t.output_tokens)} · Cache read{" "}
          {formatTokens(t.cache_read_tokens)} · Cache write {formatTokens(t.cache_write_tokens)}
        </p>
      </Kpi>
    </div>
  )
}

function SpendChart({ points, bucketMs }: { points: StatsPoint[]; bucketMs: number }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Spend vs reference</CardTitle>
        <CardDescription>
          API spend, what the reference model would have cost, and subscription usage at API
          prices.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ChartContainer config={spendConfig} className="aspect-auto h-64 w-full">
          <LineChart data={points} margin={{ top: 8, right: 8, left: 0 }} accessibilityLayer>
            <CartesianGrid vertical={false} />
            <XAxis
              dataKey="ts"
              tickLine={false}
              axisLine={false}
              tickMargin={8}
              minTickGap={32}
              tickFormatter={(value) => formatBucket(Number(value), bucketMs)}
            />
            <YAxis
              tickLine={false}
              axisLine={false}
              width="auto"
              tickFormatter={(value) => formatUsd(Number(value))}
            />
            <ChartTooltip
              content={
                <ChartTooltipContent
                  labelFormatter={(_, payload) => formatBucket(Number(payload[0]?.payload?.ts), bucketMs)}
                  formatter={(value, name, item) => (
                    <TooltipRow
                      color={item.color}
                      label={spendConfig[String(name)]?.label ?? name}
                      value={formatUsd(Number(value))}
                    />
                  )}
                />
              }
            />
            <ChartLegend content={<ChartLegendContent />} />
            {Object.keys(spendConfig).map((key) => (
              <Line
                key={key}
                dataKey={key}
                type="monotone"
                stroke={`var(--color-${key})`}
                strokeWidth={2}
                dot={false}
              />
            ))}
          </LineChart>
        </ChartContainer>
      </CardContent>
    </Card>
  )
}

function TokensChart({ points, bucketMs }: { points: StatsPoint[]; bucketMs: number }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>API vs subscription tokens</CardTitle>
        <CardDescription>
          Tokens on API-billed models next to tokens on your Claude subscription.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ChartContainer config={tokenConfig} className="aspect-auto h-64 w-full">
          <BarChart data={points} margin={{ top: 8, right: 8, left: 0 }} accessibilityLayer>
            <CartesianGrid vertical={false} />
            <XAxis
              dataKey="ts"
              tickLine={false}
              axisLine={false}
              tickMargin={8}
              minTickGap={32}
              tickFormatter={(value) => formatBucket(Number(value), bucketMs)}
            />
            <YAxis
              tickLine={false}
              axisLine={false}
              width={48}
              tickFormatter={(value) => formatTokens(Number(value))}
            />
            <ChartTooltip
              content={
                <ChartTooltipContent
                  labelFormatter={(_, payload) => formatBucket(Number(payload[0]?.payload?.ts), bucketMs)}
                  formatter={(value, name, item) => (
                    <TooltipRow
                      color={item.color}
                      label={tokenConfig[String(name)]?.label ?? name}
                      value={formatCount(Number(value))}
                    />
                  )}
                />
              }
            />
            <ChartLegend content={<ChartLegendContent />} />
            <Bar
              dataKey="api_tokens"
              stackId="tokens"
              fill="var(--color-api_tokens)"
              stroke="var(--card)"
              strokeWidth={1}
            />
            <Bar
              dataKey="subscription_tokens"
              stackId="tokens"
              fill="var(--color-subscription_tokens)"
              stroke="var(--card)"
              strokeWidth={1}
              radius={[4, 4, 0, 0]}
            />
          </BarChart>
        </ChartContainer>
      </CardContent>
    </Card>
  )
}

function RouteTable({ stats }: { stats: Stats }) {
  const rows = [...stats.by_route].sort((a, b) => b.requests - a.requests)
  return (
    <Card className="gap-0 pb-0">
      <CardHeader className="pb-4">
        <CardTitle>By route</CardTitle>
      </CardHeader>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="pl-6">Route</TableHead>
            <TableHead className="text-right">Requests</TableHead>
            <TableHead className="text-right">API spend</TableHead>
            <TableHead className="text-right">Subscription value</TableHead>
            <TableHead className="text-right">Reference cost</TableHead>
            <TableHead className="pr-6 text-right">Est. savings</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={6} className="py-6 text-center text-muted-foreground">
                No route traffic in this range.
              </TableCell>
            </TableRow>
          ) : (
            rows.map((row) => (
              <TableRow key={row.route}>
                <TableCell className="pl-6">
                  <Link
                    to={`/requests?route=${encodeURIComponent(row.route)}`}
                    className="font-mono text-xs font-medium hover:underline"
                  >
                    {row.route || "(no route)"}
                  </Link>
                </TableCell>
                <TableCell className="text-right tabular-nums">{formatCount(row.requests)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatUsd(row.cost_usd)}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatUsd(row.subscription_value_usd)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatUsd(row.reference_cost_usd)}
                </TableCell>
                <TableCell className="pr-6 text-right tabular-nums">
                  {row.reference_cost_usd > 0 ? formatUsd(row.reference_cost_usd - row.cost_usd) : "—"}
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </Card>
  )
}

function ModelTable({ stats }: { stats: Stats }) {
  const rows = [...stats.by_model].sort((a, b) => b.calls - a.calls)
  return (
    <Card className="gap-0 pb-0">
      <CardHeader className="pb-4">
        <CardTitle>By model</CardTitle>
        <CardDescription>Every upstream call, including classifier calls.</CardDescription>
      </CardHeader>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="pl-6">Model</TableHead>
            <TableHead>Billing</TableHead>
            <TableHead className="text-right">Calls</TableHead>
            <TableHead className="text-right">Input</TableHead>
            <TableHead className="text-right">Output</TableHead>
            <TableHead className="text-right">Cache read</TableHead>
            <TableHead className="text-right">Cache write</TableHead>
            <TableHead className="pr-6 text-right">API cost</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={8} className="py-6 text-center text-muted-foreground">
                No model calls in this range.
              </TableCell>
            </TableRow>
          ) : (
            rows.map((row) => (
              <TableRow key={`${row.provider}/${row.model}/${row.billing}`}>
                <TableCell className="pl-6">
                  <div className="font-mono text-xs font-medium">{row.model}</div>
                  <div className="text-xs text-muted-foreground">{row.provider}</div>
                </TableCell>
                <TableCell>
                  <BillingBadge billing={row.billing} />
                </TableCell>
                <TableCell className="text-right tabular-nums">{formatCount(row.calls)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatTokens(row.input_tokens)}</TableCell>
                <TableCell className="text-right tabular-nums">{formatTokens(row.output_tokens)}</TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatTokens(row.cache_read_tokens)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatTokens(row.cache_write_tokens)}
                </TableCell>
                <TableCell className="pr-6 text-right tabular-nums">{formatUsd(row.cost_usd)}</TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </Card>
  )
}

function RateLimitsCard() {
  const limits = useSubscriptionLimits()
  const headers = Object.entries(limits.data?.headers ?? {}).sort(([a], [b]) => a.localeCompare(b))
  const capturedAt = limits.data?.captured_at ?? 0

  return (
    <Card>
      <CardHeader>
        <CardTitle>Claude subscription limits</CardTitle>
        <CardDescription>
          Latest rate-limit headers from Anthropic on subscription traffic
          {capturedAt > 0 && (
            <>
              , captured <RelativeTime ms={capturedAt} />
            </>
          )}
          .
        </CardDescription>
      </CardHeader>
      <CardContent>
        {limits.isError ? (
          <QueryError error={limits.error} onRetry={() => limits.refetch()} />
        ) : limits.isPending ? (
          <div className="grid gap-2">
            {Array.from({ length: 4 }, (_, index) => (
              <Skeleton key={index} className="h-5 w-full" />
            ))}
          </div>
        ) : headers.length === 0 ? (
          <Empty className="p-6">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <GaugeIcon />
              </EmptyMedia>
              <EmptyTitle>No subscription traffic yet</EmptyTitle>
              <EmptyDescription>
                Limits appear after a Claude subscription tier serves its first request.
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button variant="outline" size="sm" asChild>
                <Link to="/routes">Set up a route</Link>
              </Button>
            </EmptyContent>
          </Empty>
        ) : (
          <dl className="grid">
            {headers.map(([name, value]) => (
              <div key={name} className="flex items-baseline justify-between gap-4 border-b py-2 last:border-0">
                <dt className="font-mono text-xs text-muted-foreground">
                  {name.replace(/^anthropic-ratelimit-/, "")}
                </dt>
                <dd className="text-right font-mono text-xs break-all">{value}</dd>
              </div>
            ))}
          </dl>
        )}
      </CardContent>
    </Card>
  )
}

export function OverviewPage() {
  const [params, setParams] = useSearchParams()
  const range = ranges.find((item) => item.value === params.get("range")) ?? ranges[0]
  const stats = useStats(range.value)

  const points = stats.data ? fillSeries(stats.data, stats.dataUpdatedAt) : []

  return (
    <>
      <PageHeader
        title="Overview"
        description="Spend, savings and routing across all requests."
        actions={
          <ToggleGroup
            type="single"
            variant="outline"
            value={range.value}
            aria-label="Time range"
            onValueChange={(value) => {
              const match = ranges.find((item) => item.value === value)
              if (match) setParams({ range: match.value }, { replace: true })
            }}
          >
            {ranges.map((item) => (
              <ToggleGroupItem key={item.value} value={item.value} aria-label={item.label}>
                {item.value}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
        }
      />

      {stats.isError ? (
        <QueryError error={stats.error} onRetry={() => stats.refetch()} />
      ) : stats.isPending ? (
        <>
          <Skeleton className="h-48 w-full rounded-xl" />
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {Array.from({ length: 8 }, (_, index) => (
              <Skeleton key={index} className="h-32 w-full rounded-xl" />
            ))}
          </div>
          <div className="grid gap-4 lg:grid-cols-2">
            <Skeleton className="h-80 w-full rounded-xl" />
            <Skeleton className="h-80 w-full rounded-xl" />
          </div>
        </>
      ) : (
        <>
          {stats.data.totals.requests > 0 && (
            <ComparisonCard
              comparison={stats.data.totals.comparison}
              description={`What the work in the ${range.label} would cost on one model at API prices.`}
            />
          )}
          <Kpis stats={stats.data} rangeLabel={range.label} />
          {stats.data.totals.requests === 0 ? (
            <Empty className="border">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ActivityIcon />
                </EmptyMedia>
                <EmptyTitle>No requests in the {range.label}</EmptyTitle>
                <EmptyDescription>
                  Connect Claude Code to a route, and charts and breakdowns fill in here.
                </EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button asChild>
                  <Link to="/routes">Connect Claude Code</Link>
                </Button>
              </EmptyContent>
            </Empty>
          ) : (
            <>
              <div className="grid gap-4 lg:grid-cols-2">
                <SpendChart points={points} bucketMs={stats.data.bucket_ms} />
                <TokensChart points={points} bucketMs={stats.data.bucket_ms} />
              </div>
              <RouteTable stats={stats.data} />
              <ModelTable stats={stats.data} />
            </>
          )}
        </>
      )}

      <RateLimitsCard />
    </>
  )
}
