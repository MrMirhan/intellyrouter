import { Link, useParams } from "react-router"
import { ArrowLeftIcon, DownloadIcon, SearchXIcon, SignpostIcon } from "lucide-react"

import { BillingBadge, LegStatusBadge, RoleBadge } from "@/components/badges"
import { ComparisonCard } from "@/components/comparison-card"
import { CopyButton } from "@/components/copy-button"
import { Kpi } from "@/components/kpi"
import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
import { RelativeTime } from "@/components/relative-time"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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
import { RequestsTable } from "@/features/requests/requests-table"
import { ApiError, api } from "@/lib/api"
import {
  formatAbsolute,
  formatCount,
  formatDuration,
  formatLatency,
  formatPercent,
  formatPlural,
  formatTokens,
  formatUsd,
} from "@/lib/format"
import { useSessionDetail } from "@/lib/queries"
import type { SessionCheckpoint, SessionModelUsage } from "@/lib/types"

function NotFound() {
  return (
    <Empty className="border">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchXIcon />
        </EmptyMedia>
        <EmptyTitle>Session not found</EmptyTitle>
        <EmptyDescription>The ledger has no requests with this session ID.</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" asChild>
          <Link to="/sessions">Back to sessions</Link>
        </Button>
      </EmptyContent>
    </Empty>
  )
}

function WorkTable({ rows }: { rows: SessionModelUsage[] }) {
  const total = rows.reduce((sum, row) => sum + row.cost_usd, 0)
  const sorted = [...rows].sort((a, b) => b.cost_usd - a.cost_usd)

  return (
    <Card className="gap-0 pb-0">
      <CardHeader className="pb-4">
        <CardTitle>Who did the work</CardTitle>
        <CardDescription>Upstream calls in this session by role and model.</CardDescription>
      </CardHeader>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="pl-6">Role</TableHead>
            <TableHead>Model</TableHead>
            <TableHead>Billing</TableHead>
            <TableHead className="text-right">Calls</TableHead>
            <TableHead className="text-right">Input</TableHead>
            <TableHead className="text-right">Output</TableHead>
            <TableHead className="text-right">Cache read</TableHead>
            <TableHead className="text-right">Cache write</TableHead>
            <TableHead className="text-right">Cost</TableHead>
            <TableHead className="pr-6">Share of cost</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sorted.length === 0 ? (
            <TableRow>
              <TableCell colSpan={10} className="py-6 text-center text-muted-foreground">
                No upstream calls were recorded.
              </TableCell>
            </TableRow>
          ) : (
            sorted.map((row) => {
              const share = total > 0 ? row.cost_usd / total : 0
              return (
                <TableRow key={`${row.role}/${row.model}/${row.billing}`}>
                  <TableCell className="pl-6">
                    <RoleBadge role={row.role} />
                  </TableCell>
                  <TableCell className="font-mono text-xs font-medium">{row.model}</TableCell>
                  <TableCell>
                    <BillingBadge billing={row.billing} />
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{formatCount(row.calls)}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatTokens(row.input_tokens)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatTokens(row.output_tokens)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatTokens(row.cache_read_tokens)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatTokens(row.cache_write_tokens)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{formatUsd(row.cost_usd)}</TableCell>
                  <TableCell className="pr-6">
                    <div className="flex items-center gap-2">
                      <div aria-hidden className="h-2 w-20 overflow-hidden rounded-full bg-muted">
                        <div className="h-full bg-chart-1" style={{ width: `${share * 100}%` }} />
                      </div>
                      <span className="text-xs tabular-nums">{formatPercent(share)}</span>
                    </div>
                  </TableCell>
                </TableRow>
              )
            })
          )}
        </TableBody>
      </Table>
    </Card>
  )
}

function Checkpoints({ items }: { items: SessionCheckpoint[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Director checkpoints and advisor questions</CardTitle>
        <CardDescription>
          Each time the director checked the work or the advisor answered the executor, with its note.
        </CardDescription>
      </CardHeader>
      <CardContent>
        {items.length === 0 ? (
          <p className="text-sm text-muted-foreground">The director and the advisor were not called in this session.</p>
        ) : (
          <ol className="grid gap-3">
            {items.map((item, index) => (
              <li key={`${item.request_id}-${index}`} className="grid gap-2 rounded-lg border p-3">
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
                  <RelativeTime ms={item.ts} />
                  <RoleBadge role={item.role} />
                  <span className="font-mono text-xs font-medium">{item.model}</span>
                  <BillingBadge billing={item.billing} />
                  <LegStatusBadge status={item.status} />
                  <span className="text-xs text-muted-foreground tabular-nums">
                    {formatLatency(item.latency_ms)}
                  </span>
                  <Link
                    to={`/requests/${item.request_id}`}
                    className="ml-auto text-xs font-medium hover:underline"
                  >
                    Request #{item.request_id}
                  </Link>
                </div>
                {item.note && (
                  <div className="flex gap-2 rounded-md bg-muted px-3 py-2">
                    <SignpostIcon
                      aria-hidden
                      className="mt-0.5 size-4 shrink-0 text-muted-foreground"
                    />
                    <p className="text-sm break-words">{item.note}</p>
                  </div>
                )}
              </li>
            ))}
          </ol>
        )}
      </CardContent>
    </Card>
  )
}

export function SessionDetailPage() {
  const { id = "" } = useParams()
  const session = useSessionDetail(id)

  const back = (
    <Button variant="ghost" size="sm" asChild>
      <Link to="/sessions">
        <ArrowLeftIcon />
        Sessions
      </Link>
    </Button>
  )

  if (id === "" || (session.error instanceof ApiError && session.error.status === 404)) {
    return (
      <>
        <div>{back}</div>
        <NotFound />
      </>
    )
  }

  if (session.isError) {
    return (
      <>
        <div>{back}</div>
        <QueryError error={session.error} onRetry={() => session.refetch()} />
      </>
    )
  }

  if (session.isPending) {
    return (
      <>
        <div>{back}</div>
        <Skeleton className="h-8 w-72" />
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }, (_, index) => (
            <Skeleton key={index} className="h-28 w-full rounded-xl" />
          ))}
        </div>
        <Skeleton className="h-48 w-full rounded-xl" />
        <Skeleton className="h-64 w-full rounded-xl" />
      </>
    )
  }

  const { summary, by_model: byModel, checkpoints, comparison, requests } = session.data
  const roleCost = (role: string) =>
    byModel.filter((row) => row.role === role).reduce((sum, row) => sum + row.cost_usd, 0)

  return (
    <>
      <div>{back}</div>
      <PageHeader
        title={
          <span className="flex flex-wrap items-baseline gap-x-3">
            Session
            <span className="font-mono text-base font-medium break-all">{summary.session_id}</span>
          </span>
        }
        description={`${formatAbsolute(summary.first_ts)} – ${formatAbsolute(summary.last_ts)} · ${formatDuration(summary.last_ts - summary.first_ts)}`}
        actions={
          <>
            <CopyButton value={summary.session_id} label="Copy ID" />
            <Button variant="outline" size="sm" asChild>
              <a href={api.sessionExportUrl(summary.session_id)} download>
                <DownloadIcon />
                Export JSONL
              </a>
            </Button>
          </>
        }
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
        <Kpi title="Requests" value={formatCount(summary.requests)}>
          <p>
            {formatPlural(summary.errors, "error")} · {formatPlural(summary.agents, "agent")}
          </p>
          {summary.routes.length > 0 && <p>Routes: {summary.routes.join(", ")}</p>}
        </Kpi>
        <Kpi title="API cost" value={formatUsd(summary.cost_usd)}>
          <p>
            Input {formatTokens(summary.input_tokens)} · Output {formatTokens(summary.output_tokens)}{" "}
            · Cache read {formatTokens(summary.cache_read_tokens)} · Cache write{" "}
            {formatTokens(summary.cache_write_tokens)}
          </p>
        </Kpi>
        <Kpi title="Subscription value" value={formatUsd(summary.subscription_value_usd)}>
          <p>Valued at API prices. It costs nothing extra but counts toward your limits.</p>
        </Kpi>
        <Kpi title="Director calls" value={formatCount(summary.director_calls)}>
          <p>Director cost {formatUsd(roleCost("director"))}</p>
        </Kpi>
        <Kpi title="Advisor calls" value={formatCount(summary.advisor_calls)}>
          <p>Advisor cost {formatUsd(roleCost("advisor"))}</p>
        </Kpi>
      </div>

      <ComparisonCard
        comparison={comparison}
        description="What the work in this session would cost on one model at API prices."
      />

      <WorkTable rows={byModel} />

      <Checkpoints items={checkpoints} />

      <section className="space-y-4" aria-labelledby="session-requests-heading">
        <div>
          <h2 id="session-requests-heading" className="font-heading text-lg font-semibold">
            Requests
          </h2>
          <p className="text-sm text-muted-foreground">Every request in this session, oldest first.</p>
        </div>
        <Card className="gap-0 py-0">
          <RequestsTable items={requests} showSession={false} />
        </Card>
      </section>
    </>
  )
}
