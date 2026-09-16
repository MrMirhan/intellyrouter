import { Link, useSearchParams } from "react-router"
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  CrownIcon,
  FileTextIcon,
  MessagesSquareIcon,
  XIcon,
} from "lucide-react"

import { CopyButton } from "@/components/copy-button"
import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
import { RelativeTime } from "@/components/relative-time"
import { TableSkeleton } from "@/components/table-skeleton"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { formatCount, formatDuration, formatUsd } from "@/lib/format"
import { useRoutes, useSessions } from "@/lib/queries"
import type { SessionSummary } from "@/lib/types"
import { cn } from "@/lib/utils"

const pageSize = 50
const allValue = "all"

function ModelChips({ models }: { models: SessionSummary["models"] }) {
  if (models.length === 0) {
    return <span className="text-muted-foreground">—</span>
  }
  return (
    <ul className="flex max-w-80 flex-wrap gap-1">
      {models.map((item) => (
        <li key={`${item.model}/${item.billing}`}>
          <Badge
            variant={item.billing === "subscription" ? "secondary" : "outline"}
            className="font-mono"
          >
            {item.billing === "subscription" && (
              <>
                <CrownIcon aria-hidden />
                <span className="sr-only">Subscription:</span>
              </>
            )}
            {item.model}
            <span className="text-muted-foreground tabular-nums">×{formatCount(item.calls)}</span>
          </Badge>
        </li>
      ))}
    </ul>
  )
}

export function SessionsPage() {
  const [params, setParams] = useSearchParams()
  const route = params.get("route") ?? ""
  const page = Math.max(1, Math.floor(Number(params.get("page"))) || 1)

  const routes = useRoutes()
  const sessions = useSessions({
    route: route || undefined,
    limit: pageSize,
    offset: (page - 1) * pageSize,
  })

  const update = (changes: Record<string, string>) => {
    setParams(
      (current) => {
        const next = new URLSearchParams(current)
        for (const [key, value] of Object.entries(changes)) {
          if (value) {
            next.set(key, value)
          } else {
            next.delete(key)
          }
        }
        if (!("page" in changes)) {
          next.delete("page")
        }
        return next
      },
      { replace: true },
    )
  }

  const total = sessions.data?.total ?? 0
  const items = sessions.data?.items ?? []
  const first = total === 0 ? 0 : (page - 1) * pageSize + 1
  const last = Math.min(page * pageSize, total)

  return (
    <>
      <PageHeader
        title="Sessions"
        description="Claude Code sessions that went through the gateway, most recent activity first."
      />

      <div className="flex flex-wrap items-end gap-3">
        <div className="grid gap-1.5">
          <Label htmlFor="filter-route">Route</Label>
          <Select
            value={route || allValue}
            onValueChange={(value) => update({ route: value === allValue ? "" : value })}
          >
            <SelectTrigger id="filter-route" className="w-48">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={allValue}>All routes</SelectItem>
              {routes.data?.map((item) => (
                <SelectItem key={item.id} value={item.name}>
                  {item.name}
                </SelectItem>
              ))}
              {route && !routes.data?.some((item) => item.name === route) && (
                <SelectItem value={route}>{route}</SelectItem>
              )}
            </SelectContent>
          </Select>
        </div>
        {route && (
          <Button variant="ghost" onClick={() => update({ route: "" })}>
            <XIcon />
            Clear filter
          </Button>
        )}
      </div>

      {sessions.isError ? (
        <QueryError error={sessions.error} onRetry={() => sessions.refetch()} />
      ) : sessions.isSuccess && total === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <MessagesSquareIcon />
            </EmptyMedia>
            <EmptyTitle>{route ? "No sessions on this route" : "No sessions yet"}</EmptyTitle>
            <EmptyDescription>
              {route
                ? "No session sent requests to this route."
                : "Sessions appear after Claude Code sends requests with its session header."}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            {route ? (
              <Button variant="outline" onClick={() => update({ route: "" })}>
                Clear filter
              </Button>
            ) : (
              <Button asChild>
                <Link to="/routes">Connect Claude Code</Link>
              </Button>
            )}
          </EmptyContent>
        </Empty>
      ) : (
        <Card className="gap-0 py-0">
          <Table className={cn(sessions.isPlaceholderData && "opacity-60")}>
            <TableHeader>
              <TableRow>
                <TableHead>Last activity</TableHead>
                <TableHead>Session</TableHead>
                <TableHead>Duration</TableHead>
                <TableHead className="text-right">Requests</TableHead>
                <TableHead>Models</TableHead>
                <TableHead className="text-right">Director calls</TableHead>
                <TableHead className="text-right">Advisor calls</TableHead>
                <TableHead className="text-right">API cost</TableHead>
                <TableHead className="text-right">Subscription value</TableHead>
                <TableHead className="text-right">Errors</TableHead>
                <TableHead>Captured</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sessions.isPending ? (
                <TableSkeleton columns={11} rows={10} />
              ) : (
                items.map((item) => (
                  <TableRow key={item.session_id}>
                    <TableCell>
                      <RelativeTime ms={item.last_ts} />
                    </TableCell>
                    <TableCell>
                      <span className="flex items-center gap-1">
                        <Link
                          to={`/sessions/${encodeURIComponent(item.session_id)}`}
                          className="font-mono text-xs font-medium hover:underline"
                          title={item.session_id}
                          aria-label={`Session ${item.session_id}`}
                        >
                          {item.session_id.slice(0, 8)}
                        </Link>
                        <CopyButton value={item.session_id} label="Copy session ID" iconOnly />
                      </span>
                    </TableCell>
                    <TableCell className="whitespace-nowrap tabular-nums">
                      {formatDuration(item.last_ts - item.first_ts)}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatCount(item.requests)}
                    </TableCell>
                    <TableCell>
                      <ModelChips models={item.models} />
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatCount(item.director_calls)}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatCount(item.advisor_calls)}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatUsd(item.cost_usd)}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatUsd(item.subscription_value_usd)}
                    </TableCell>
                    <TableCell
                      className={cn("text-right tabular-nums", item.errors > 0 && "text-destructive")}
                    >
                      {formatCount(item.errors)}
                    </TableCell>
                    <TableCell>
                      {item.captured_requests > 0 ? (
                        <span
                          className="inline-flex items-center gap-1 text-muted-foreground tabular-nums"
                          title={`${formatCount(item.captured_requests)} requests with captured content`}
                        >
                          <FileTextIcon aria-hidden className="size-3.5" />
                          {formatCount(item.captured_requests)}
                          <span className="sr-only"> requests with captured content</span>
                        </span>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
          <div className="flex items-center justify-between gap-4 border-t px-4 py-3 text-sm">
            <span className="text-muted-foreground tabular-nums">
              {sessions.isPending
                ? "Loading…"
                : `Showing ${formatCount(first)}–${formatCount(last)} of ${formatCount(total)}`}
            </span>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={page <= 1}
                onClick={() => update({ page: String(page - 1) })}
              >
                <ChevronLeftIcon />
                Previous
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={last >= total}
                onClick={() => update({ page: String(page + 1) })}
              >
                Next
                <ChevronRightIcon />
              </Button>
            </div>
          </div>
        </Card>
      )}
    </>
  )
}
