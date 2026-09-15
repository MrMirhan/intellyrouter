import { Link, useNavigate } from "react-router"
import { FlaskConicalIcon } from "lucide-react"
import { toast } from "sonner"

import { EvalStatusBadge } from "@/components/badges"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { QueryError } from "@/components/query-error"
import { RelativeTime } from "@/components/relative-time"
import { TableSkeleton } from "@/components/table-skeleton"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Progress } from "@/components/ui/progress"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { evalModeLabel } from "@/lib/eval"
import { formatCount } from "@/lib/format"
import { useCancelEvalRun, useDeleteEvalRun, useEvalRuns } from "@/lib/queries"

export function EvalRunsTable() {
  const navigate = useNavigate()
  const runs = useEvalRuns()
  const cancelRun = useCancelEvalRun()
  const deleteRun = useDeleteEvalRun()

  return (
    <section className="grid gap-3" aria-labelledby="eval-runs-heading">
      <h2 id="eval-runs-heading" className="font-heading text-lg font-semibold">
        Runs
      </h2>
      {runs.isError ? (
        <QueryError error={runs.error} onRetry={() => runs.refetch()} />
      ) : runs.data?.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FlaskConicalIcon />
            </EmptyMedia>
            <EmptyTitle>No eval runs yet</EmptyTitle>
            <EmptyDescription>
              Start a run above to compare routes on the same tasks.
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <Card className="gap-0 py-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Run</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Mode</TableHead>
                <TableHead>Routes</TableHead>
                <TableHead>Progress</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.isPending ? (
                <TableSkeleton columns={6} />
              ) : (
                runs.data.map((run) => {
                  const running = run.status === "running"
                  return (
                    <TableRow
                      key={run.id}
                      className="cursor-pointer"
                      onClick={() => navigate(`/eval/${run.id}`)}
                    >
                      <TableCell>
                        <Link
                          to={`/eval/${run.id}`}
                          className="font-medium hover:underline"
                          onClick={(event) => event.stopPropagation()}
                        >
                          Run #{run.id}
                        </Link>
                        <div className="text-xs text-muted-foreground">
                          <RelativeTime ms={run.created_at} />
                        </div>
                      </TableCell>
                      <TableCell>
                        <EvalStatusBadge status={run.status} />
                        {run.error && (
                          <div className="max-w-56 truncate text-xs text-destructive" title={run.error}>
                            {run.error}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className="text-sm">{evalModeLabel(run.mode)}</TableCell>
                      <TableCell>
                        <div className="flex max-w-72 flex-wrap gap-1">
                          {run.routes.map((route) => (
                            <span key={route} className="rounded-md border px-1.5 py-0.5 font-mono text-xs">
                              {route}
                            </span>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex min-w-36 items-center gap-2">
                          <Progress
                            value={run.total > 0 ? (run.done / run.total) * 100 : 0}
                            aria-label={`Run #${run.id} progress`}
                          />
                          <span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums">
                            {formatCount(run.done)}/{formatCount(run.total)}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell onClick={(event) => event.stopPropagation()}>
                        <div className="flex items-center justify-end gap-1">
                          <Button variant="outline" size="sm" asChild>
                            <Link to={`/eval/${run.id}`}>Open</Link>
                          </Button>
                          {running ? (
                            <ConfirmDialog
                              trigger={
                                <Button variant="ghost" size="sm">
                                  Cancel
                                </Button>
                              }
                              title={`Cancel run #${run.id}?`}
                              description="Claude Code sessions that are still running stop, and the run is marked as canceled."
                              confirmLabel="Cancel run"
                              pending={cancelRun.isPending}
                              onConfirm={(close) =>
                                cancelRun.mutate(run.id, {
                                  onSuccess: () => {
                                    close()
                                    toast.success(`Canceling run #${run.id}`)
                                  },
                                })
                              }
                            />
                          ) : (
                            <ConfirmDialog
                              trigger={
                                <Button variant="ghost" size="sm">
                                  Delete
                                </Button>
                              }
                              title={`Delete run #${run.id}?`}
                              description="This removes the run and its results. It cannot be undone."
                              confirmLabel="Delete run"
                              pending={deleteRun.isPending}
                              onConfirm={(close) =>
                                deleteRun.mutate(run.id, {
                                  onSuccess: () => {
                                    close()
                                    toast.success(`Deleted run #${run.id}`)
                                  },
                                })
                              }
                            />
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          </Table>
        </Card>
      )}
    </section>
  )
}
