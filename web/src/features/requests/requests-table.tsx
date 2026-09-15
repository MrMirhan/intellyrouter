import { Link, useNavigate } from "react-router"
import { FileTextIcon } from "lucide-react"

import { RequestStatusBadge, RoleBadge, StrategyBadge } from "@/components/badges"
import { RelativeTime } from "@/components/relative-time"
import { TableSkeleton } from "@/components/table-skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { formatLatency, formatUsd } from "@/lib/format"
import type { RequestRecord } from "@/lib/types"
import { cn } from "@/lib/utils"

export function RequestsTable({
  items,
  loading = false,
  stale = false,
  showSession = true,
}: {
  items: RequestRecord[]
  loading?: boolean
  stale?: boolean
  showSession?: boolean
}) {
  const navigate = useNavigate()

  return (
    <Table className={cn(stale && "opacity-60")}>
      <TableHeader>
        <TableRow>
          <TableHead>Time</TableHead>
          <TableHead>Route</TableHead>
          <TableHead>Strategy</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Answered by</TableHead>
          <TableHead className="text-right">Cost</TableHead>
          <TableHead className="text-right">Subscription value</TableHead>
          <TableHead className="text-right">Latency</TableHead>
          {showSession && <TableHead>Session</TableHead>}
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableSkeleton columns={showSession ? 9 : 8} rows={10} />
        ) : (
          items.map((item) => (
            <TableRow
              key={item.id}
              className="cursor-pointer"
              onClick={() => navigate(`/requests/${item.id}`)}
            >
              <TableCell>
                <RelativeTime ms={item.ts} />
              </TableCell>
              <TableCell>
                <span className="flex items-center gap-1.5">
                  <Link
                    to={`/requests/${item.id}`}
                    className="font-medium hover:underline"
                    onClick={(event) => event.stopPropagation()}
                  >
                    {item.route || "(no route)"}
                  </Link>
                  {item.captured && (
                    <span title="Content captured" className="text-muted-foreground">
                      <FileTextIcon aria-hidden className="size-3.5" />
                      <span className="sr-only">Content captured</span>
                    </span>
                  )}
                </span>
                {item.client_model && item.client_model !== item.route && (
                  <div className="text-xs text-muted-foreground">{item.client_model}</div>
                )}
              </TableCell>
              <TableCell>
                <StrategyBadge strategy={item.strategy} />
              </TableCell>
              <TableCell>
                <span className="flex items-center gap-2">
                  <RequestStatusBadge status={item.status} />
                  {item.http_status !== 200 && item.http_status > 0 && (
                    <span className="text-xs text-muted-foreground tabular-nums">
                      {item.http_status}
                    </span>
                  )}
                </span>
              </TableCell>
              <TableCell>
                {item.answer_model ? (
                  <span className="flex items-center gap-2">
                    <span className="font-mono text-xs">{item.answer_model}</span>
                    {item.models.at(-1)?.role === "director" && <RoleBadge role="director" />}
                  </span>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </TableCell>
              <TableCell className="text-right tabular-nums">{formatUsd(item.cost_usd)}</TableCell>
              <TableCell className="text-right tabular-nums">
                {formatUsd(item.subscription_value_usd)}
              </TableCell>
              <TableCell className="text-right tabular-nums">
                {formatLatency(item.latency_ms)}
              </TableCell>
              {showSession && (
                <TableCell>
                  {item.session_id ? (
                    <Link
                      to={`/sessions/${encodeURIComponent(item.session_id)}`}
                      className="block max-w-40 truncate font-mono text-xs hover:underline"
                      title={`Open session ${item.session_id}`}
                      onClick={(event) => event.stopPropagation()}
                    >
                      {item.session_id}
                    </Link>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </TableCell>
              )}
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}
