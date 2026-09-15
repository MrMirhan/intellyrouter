import { useState } from "react"
import { Link, useParams } from "react-router"
import { ArrowLeftIcon, CoinsIcon, SearchXIcon, TriangleAlertIcon, TrophyIcon } from "lucide-react"
import { toast } from "sonner"

import { EvalResultBadge, EvalStatusBadge } from "@/components/badges"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
import { RelativeTime } from "@/components/relative-time"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ResultSheet } from "@/features/eval/result-sheet"
import { ApiError } from "@/lib/api"
import { evalModeLabel } from "@/lib/eval"
import {
  formatAbsolute,
  formatCount,
  formatDuration,
  formatPercent,
  formatPlural,
  formatTokens,
  formatUsd,
} from "@/lib/format"
import { useCancelEvalRun, useEvalRun } from "@/lib/queries"
import type { EvalResult, EvalRunDetail, EvalSummary } from "@/lib/types"

function MarkBadge({ icon: Icon, label }: { icon: typeof TrophyIcon; label: string }) {
  return (
    <Badge variant="outline" className="border-success/40 text-success">
      <Icon />
      {label}
    </Badge>
  )
}

function SummaryTable({ summaries }: { summaries: EvalSummary[] }) {
  const compare = summaries.length > 1
  const bestPassRate = Math.max(0, ...summaries.map((summary) => summary.pass_rate))
  const solved = summaries.filter((summary) => summary.passed > 0)
  const lowestCost = Math.min(...solved.map((summary) => summary.cost_per_solved_usd))

  return (
    <Card className="gap-0 pb-0">
      <CardHeader className="pb-4">
        <CardTitle>Summary by route</CardTitle>
        <CardDescription>
          Cost per solved task is API spend plus subscription value, divided by passed tasks.
        </CardDescription>
      </CardHeader>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="pl-6">Route</TableHead>
            <TableHead className="text-right">Passed</TableHead>
            <TableHead className="text-right">Pass rate</TableHead>
            <TableHead className="text-right">API cost</TableHead>
            <TableHead className="text-right">Subscription value</TableHead>
            <TableHead className="text-right">Cost per solved</TableHead>
            <TableHead className="text-right">Escalated</TableHead>
            <TableHead className="text-right">API tokens</TableHead>
            <TableHead className="text-right">Subscription tokens</TableHead>
            <TableHead className="pr-6 text-right">Avg duration</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {summaries.length === 0 ? (
            <TableRow>
              <TableCell colSpan={10} className="py-6 text-center text-muted-foreground">
                No finished results yet.
              </TableCell>
            </TableRow>
          ) : (
            summaries.map((summary) => {
              const highestPassRate = compare && bestPassRate > 0 && summary.pass_rate === bestPassRate
              const cheapest = compare && summary.passed > 0 && summary.cost_per_solved_usd === lowestCost
              return (
                <TableRow key={summary.route}>
                  <TableCell className="pl-6 font-mono text-xs font-medium">{summary.route}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatCount(summary.passed)} / {formatCount(summary.tasks)}
                  </TableCell>
                  <TableCell className="text-right">
                    <span className="inline-flex items-center gap-2">
                      {highestPassRate && <MarkBadge icon={TrophyIcon} label="Highest" />}
                      <span className="tabular-nums">{formatPercent(summary.pass_rate)}</span>
                    </span>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{formatUsd(summary.cost_usd)}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatUsd(summary.subscription_value_usd)}
                  </TableCell>
                  <TableCell className="text-right">
                    <span className="inline-flex items-center gap-2">
                      {cheapest && <MarkBadge icon={CoinsIcon} label="Lowest" />}
                      <span className="tabular-nums">
                        {summary.passed > 0 ? formatUsd(summary.cost_per_solved_usd) : "—"}
                      </span>
                    </span>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatCount(summary.escalated_requests)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{formatTokens(summary.api_tokens)}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {formatTokens(summary.subscription_tokens)}
                  </TableCell>
                  <TableCell className="pr-6 text-right tabular-nums">
                    {formatDuration(summary.avg_duration_ms)}
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

function ResultsMatrix({
  run,
  onSelect,
}: {
  run: EvalRunDetail
  onSelect: (result: EvalResult) => void
}) {
  const byTask = new Map<string, Map<string, EvalResult>>()
  for (const result of run.results) {
    const row = byTask.get(result.task) ?? new Map<string, EvalResult>()
    row.set(result.route, result)
    byTask.set(result.task, row)
  }
  const tasks = [...new Set([...run.tasks, ...run.results.map((result) => result.task)])]
  const routes = [...new Set([...run.routes, ...run.results.map((result) => result.route)])]

  return (
    <Card className="gap-0 pb-0">
      <CardHeader className="pb-4">
        <CardTitle>Results</CardTitle>
        <CardDescription>
          Rows are tasks and columns are routes. Select a cell to read the Claude Code and test
          output.
        </CardDescription>
      </CardHeader>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="sticky left-0 z-10 bg-card pl-6">Task</TableHead>
            {routes.map((route) => (
              <TableHead key={route} className="font-mono text-xs">
                {route}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {tasks.map((task) => (
            <TableRow key={task}>
              <TableHead scope="row" className="sticky left-0 z-10 bg-card pl-6 font-mono text-xs">
                {task}
              </TableHead>
              {routes.map((route) => {
                const result = byTask.get(task)?.get(route)
                return (
                  <TableCell key={route} className="p-1 align-top">
                    {result ? (
                      <button
                        type="button"
                        onClick={() => onSelect(result)}
                        aria-label={`${task} on ${route}: ${result.passed ? "passed" : "failed"}. Show output`}
                        className="grid w-full min-w-32 justify-items-start gap-1 rounded-md p-2 text-left hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                      >
                        <EvalResultBadge passed={result.passed} />
                        <span className="text-xs tabular-nums">
                          {formatUsd(result.cost_usd)}
                          {result.subscription_value_usd > 0 &&
                            ` + ${formatUsd(result.subscription_value_usd)} subscription`}
                        </span>
                        <span className="text-xs text-muted-foreground tabular-nums">
                          {formatDuration(result.duration_ms)}
                        </span>
                      </button>
                    ) : (
                      <span className="block p-2 text-xs text-muted-foreground">
                        {run.status === "running" ? "Pending" : "—"}
                      </span>
                    )}
                  </TableCell>
                )
              })}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </Card>
  )
}

export function EvalRunPage() {
  const { id } = useParams()
  const runId = Number(id)
  const valid = Number.isInteger(runId) && runId > 0
  const run = useEvalRun(runId)
  const cancelRun = useCancelEvalRun()
  const [sheet, setSheet] = useState<{ open: boolean; result: EvalResult | null }>({
    open: false,
    result: null,
  })

  const back = (
    <div>
      <Button variant="ghost" size="sm" asChild>
        <Link to="/eval">
          <ArrowLeftIcon />
          Eval
        </Link>
      </Button>
    </div>
  )

  if (!valid || (run.error instanceof ApiError && run.error.status === 404)) {
    return (
      <>
        {back}
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchXIcon />
            </EmptyMedia>
            <EmptyTitle>Run not found</EmptyTitle>
            <EmptyDescription>There is no eval run with this ID.</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" asChild>
              <Link to="/eval">Back to eval</Link>
            </Button>
          </EmptyContent>
        </Empty>
      </>
    )
  }

  if (run.isError) {
    return (
      <>
        {back}
        <QueryError error={run.error} onRetry={() => run.refetch()} />
      </>
    )
  }

  if (run.isPending) {
    return (
      <>
        {back}
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-48 w-full" />
        <Skeleton className="h-64 w-full" />
      </>
    )
  }

  const data = run.data
  const running = data.status === "running"

  return (
    <>
      {back}
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            Eval run #{data.id}
            <EvalStatusBadge status={data.status} />
          </span>
        }
        description={`Started ${formatAbsolute(data.created_at)} · ${evalModeLabel(data.mode)} · ${formatCount(data.parallel)} in parallel`}
        actions={
          running && (
            <ConfirmDialog
              trigger={<Button variant="outline">Cancel run</Button>}
              title={`Cancel run #${data.id}?`}
              description="Claude Code sessions that are still running stop, and the run is marked as canceled."
              confirmLabel="Cancel run"
              pending={cancelRun.isPending}
              onConfirm={(close) =>
                cancelRun.mutate(data.id, {
                  onSuccess: () => {
                    close()
                    toast.success(`Canceling run #${data.id}`)
                  },
                })
              }
            />
          )
        }
      />

      {data.error && (
        <Alert variant="destructive">
          <TriangleAlertIcon />
          <AlertTitle>The run reported an error</AlertTitle>
          <AlertDescription className="break-words">{data.error}</AlertDescription>
        </Alert>
      )}

      <Card size="sm">
        <CardContent className="grid gap-3">
          <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
            <span className="tabular-nums">
              {formatCount(data.done)} of {formatPlural(data.total, "Claude Code run")} finished
            </span>
            <span className="text-muted-foreground">
              {running ? (
                "Updates every 3 seconds"
              ) : data.finished_at > 0 ? (
                <>
                  Finished <RelativeTime ms={data.finished_at} />
                </>
              ) : null}
            </span>
          </div>
          <Progress
            value={data.total > 0 ? (data.done / data.total) * 100 : 0}
            aria-label="Run progress"
          />
        </CardContent>
      </Card>

      <SummaryTable summaries={data.summaries} />
      <ResultsMatrix run={data} onSelect={(result) => setSheet({ open: true, result })} />
      <ResultSheet
        open={sheet.open}
        result={sheet.result}
        onOpenChange={(open) => setSheet((current) => ({ ...current, open }))}
      />
    </>
  )
}
