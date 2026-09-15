import { useCallback, useEffect, useState } from "react"
import { Link, useSearchParams } from "react-router"
import { ChevronLeftIcon, ChevronRightIcon, ScrollTextIcon, XIcon } from "lucide-react"

import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
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
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { RequestsTable } from "@/features/requests/requests-table"
import { formatCount } from "@/lib/format"
import { useRequests, useRoutes } from "@/lib/queries"
import type { RequestStatus } from "@/lib/types"

const pageSize = 50
const allValue = "all"

const statusOptions: { value: RequestStatus; label: string }[] = [
  { value: "ok", label: "OK" },
  { value: "upstream_error", label: "Upstream error" },
  { value: "error", label: "Error" },
  { value: "canceled", label: "Canceled" },
]

function parseStatus(value: string | null): RequestStatus | undefined {
  return statusOptions.find((option) => option.value === value)?.value
}

export function RequestsPage() {
  const [params, setParams] = useSearchParams()
  const route = params.get("route") ?? ""
  const status = parseStatus(params.get("status"))
  const session = params.get("session") ?? ""
  const page = Math.max(1, Math.floor(Number(params.get("page"))) || 1)

  const routes = useRoutes()
  const requests = useRequests({
    route: route || undefined,
    status,
    session_id: session || undefined,
    limit: pageSize,
    offset: (page - 1) * pageSize,
  })

  const update = useCallback(
    (changes: Record<string, string>) => {
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
    },
    [setParams],
  )

  const [sessionDraft, setSessionDraft] = useState(session)
  const [syncedSession, setSyncedSession] = useState(session)
  if (session !== syncedSession) {
    setSyncedSession(session)
    setSessionDraft(session)
  }

  useEffect(() => {
    if (sessionDraft === session) return
    const timer = setTimeout(() => update({ session: sessionDraft }), 400)
    return () => clearTimeout(timer)
  }, [sessionDraft, session, update])

  const filtered = Boolean(route || status || session)
  const total = requests.data?.total ?? 0
  const items = requests.data?.items ?? []
  const first = total === 0 ? 0 : (page - 1) * pageSize + 1
  const last = Math.min(page * pageSize, total)

  return (
    <>
      <PageHeader
        title="Requests"
        description="Every request Claude Code sent through the gateway, newest first."
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
        <div className="grid gap-1.5">
          <Label htmlFor="filter-status">Status</Label>
          <Select
            value={status ?? allValue}
            onValueChange={(value) => update({ status: value === allValue ? "" : value })}
          >
            <SelectTrigger id="filter-status" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={allValue}>All statuses</SelectItem>
              {statusOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="filter-session">Session ID</Label>
          <Input
            id="filter-session"
            value={sessionDraft}
            onChange={(event) => setSessionDraft(event.target.value)}
            placeholder="Any session"
            className="w-64 font-mono"
            autoComplete="off"
          />
        </div>
        {filtered && (
          <Button variant="ghost" onClick={() => update({ route: "", status: "", session: "" })}>
            <XIcon />
            Clear filters
          </Button>
        )}
      </div>

      {requests.isError ? (
        <QueryError error={requests.error} onRetry={() => requests.refetch()} />
      ) : requests.isSuccess && total === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ScrollTextIcon />
            </EmptyMedia>
            <EmptyTitle>{filtered ? "No matching requests" : "No requests yet"}</EmptyTitle>
            <EmptyDescription>
              {filtered
                ? "No request matches these filters."
                : "Connect Claude Code to a route, and its requests show up here."}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            {filtered ? (
              <Button variant="outline" onClick={() => update({ route: "", status: "", session: "" })}>
                Clear filters
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
          <RequestsTable
            items={items}
            loading={requests.isPending}
            stale={requests.isPlaceholderData}
          />
          <div className="flex items-center justify-between gap-4 border-t px-4 py-3 text-sm">
            <span className="text-muted-foreground tabular-nums">
              {requests.isPending
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
