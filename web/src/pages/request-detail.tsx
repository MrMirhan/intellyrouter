import { useState, type ReactNode } from "react"
import { Link, useParams } from "react-router"
import {
  ArrowLeftIcon,
  DownloadIcon,
  SearchXIcon,
  SignpostIcon,
  TriangleAlertIcon,
} from "lucide-react"

import {
  BillingBadge,
  LegStatusBadge,
  RequestStatusBadge,
  RoleBadge,
  StrategyBadge,
} from "@/components/badges"
import { DetailField } from "@/components/detail-field"
import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
import { RelativeTime } from "@/components/relative-time"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { ContentBlocks, Expandable, TruncatedText } from "@/features/requests/content-blocks"
import { ContentHint, RequestContentView } from "@/features/requests/request-content"
import { ApiError, api } from "@/lib/api"
import { formatAbsolute, formatCount, formatLatency, formatTokens, formatUsd } from "@/lib/format"
import { useRequest, useRequestContent } from "@/lib/queries"
import type { ContentLeg, Leg, RequestRecord } from "@/lib/types"

const contentTail = 30

function Tokens({ value }: { value: number }) {
  return (
    <span className="tabular-nums" title={`${formatCount(value)} tokens`}>
      {formatTokens(value)}
    </span>
  )
}

function NotFound() {
  return (
    <Empty className="border">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchXIcon />
        </EmptyMedia>
        <EmptyTitle>Request not found</EmptyTitle>
        <EmptyDescription>The ledger has no request with this ID.</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" asChild>
          <Link to="/requests">Back to requests</Link>
        </Button>
      </EmptyContent>
    </Empty>
  )
}

function LegItem({ leg, last, content }: { leg: Leg; last: boolean; content?: ContentLeg }) {
  return (
    <li className="grid grid-cols-[2rem_1fr] gap-3">
      <div className="flex flex-col items-center">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-full border bg-background text-xs font-medium tabular-nums">
          {leg.seq}
        </span>
        {!last && <span aria-hidden className="w-px flex-1 bg-border" />}
      </div>
      <Card size="sm" className="mb-4">
        <CardContent className="grid gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <RoleBadge role={leg.role} />
            <BillingBadge billing={leg.billing} />
            <LegStatusBadge status={leg.status} />
            <span className="min-w-0 text-sm">
              <span className="text-muted-foreground">{leg.provider}</span>
              <span className="text-muted-foreground"> / </span>
              <span className="font-mono font-medium">{leg.model}</span>
            </span>
          </div>
          {leg.note && (
            <div className="flex gap-2 rounded-md bg-muted px-3 py-2">
              <SignpostIcon aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
              <p className="text-sm font-medium break-words">{leg.note}</p>
            </div>
          )}
          <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-7">
            <DetailField label="Input">
              <Tokens value={leg.input_tokens} />
            </DetailField>
            <DetailField label="Output">
              <Tokens value={leg.output_tokens} />
            </DetailField>
            <DetailField label="Cache read">
              <Tokens value={leg.cache_read_tokens} />
            </DetailField>
            <DetailField label="Cache write">
              <Tokens value={leg.cache_write_tokens} />
            </DetailField>
            <DetailField label="Cost">
              <span className="tabular-nums">{formatUsd(leg.cost_usd)}</span>
            </DetailField>
            <DetailField label="Latency">
              <span className="tabular-nums">{formatLatency(leg.latency_ms)}</span>
            </DetailField>
            <DetailField label="Stop reason">
              {leg.stop_reason ? (
                <code className="text-xs">{leg.stop_reason}</code>
              ) : (
                <span className="text-muted-foreground">—</span>
              )}
            </DetailField>
          </dl>
          {content?.input && (
            <Expandable title="Added by the gateway">
              <TruncatedText text={content.input} />
            </Expandable>
          )}
          {content?.output && (
            <Expandable title="Output">
              <ContentBlocks content={content.output.content} />
            </Expandable>
          )}
        </CardContent>
      </Card>
    </li>
  )
}

export function RequestDetailPage() {
  const { id } = useParams()
  const requestId = Number(id)
  const valid = Number.isInteger(requestId) && requestId > 0
  const request = useRequest(requestId)

  const back = (
    <Button variant="ghost" size="sm" asChild>
      <Link to="/requests">
        <ArrowLeftIcon />
        Requests
      </Link>
    </Button>
  )

  if (!valid || (request.error instanceof ApiError && request.error.status === 404)) {
    return (
      <>
        <div>{back}</div>
        <NotFound />
      </>
    )
  }

  if (request.isError) {
    return (
      <>
        <div>{back}</div>
        <QueryError error={request.error} onRetry={() => request.refetch()} />
      </>
    )
  }

  if (request.isPending) {
    return (
      <>
        <div>{back}</div>
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-48 w-full" />
        <Skeleton className="h-64 w-full" />
      </>
    )
  }

  return <RequestDetail key={request.data.id} data={request.data} back={back} />
}

function RequestDetail({ data, back }: { data: RequestRecord; back: ReactNode }) {
  const [tail, setTail] = useState(contentTail)
  const content = useRequestContent(data.id, tail, data.captured)
  const legs = [...(data.legs ?? [])].sort((a, b) => a.seq - b.seq)
  const saving = data.reference_cost_usd > 0 ? data.reference_cost_usd - data.cost_usd : null

  return (
    <>
      <div>{back}</div>
      <PageHeader
        title={
          <span className="flex flex-wrap items-center gap-3">
            Request #{data.id}
            <RequestStatusBadge status={data.status} />
          </span>
        }
        description={formatAbsolute(data.ts)}
        actions={
          data.captured && (
            <Button variant="outline" size="sm" asChild>
              <a href={api.requestExportUrl(data.id)} download>
                <DownloadIcon />
                Export JSON
              </a>
            </Button>
          )
        }
      />

      {data.error && (
        <Alert variant="destructive">
          <TriangleAlertIcon />
          <AlertTitle>Request failed{data.http_status > 0 && ` with HTTP ${data.http_status}`}</AlertTitle>
          <AlertDescription className="break-words">{data.error}</AlertDescription>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Summary</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-2 gap-x-6 gap-y-4 md:grid-cols-4">
            <DetailField label="Time">
              <RelativeTime ms={data.ts} />
            </DetailField>
            <DetailField label="Route">
              <span className="font-medium">{data.route || "—"}</span>
            </DetailField>
            <DetailField label="Strategy">
              <StrategyBadge strategy={data.strategy} />
            </DetailField>
            <DetailField label="Client model">
              <code className="text-xs">{data.client_model || "—"}</code>
            </DetailField>
            <DetailField label="API cost">
              <span className="tabular-nums">{formatUsd(data.cost_usd)}</span>
            </DetailField>
            <DetailField label="Subscription value">
              <span className="tabular-nums">{formatUsd(data.subscription_value_usd)}</span>
            </DetailField>
            <DetailField label="Reference cost">
              <span className="tabular-nums">{formatUsd(data.reference_cost_usd)}</span>
            </DetailField>
            <DetailField label="Estimated saving">
              {saving === null ? (
                <span className="text-muted-foreground">—</span>
              ) : (
                <span className="tabular-nums">{formatUsd(saving)}</span>
              )}
            </DetailField>
            <DetailField label="Latency">
              <span className="tabular-nums">{formatLatency(data.latency_ms)}</span>
            </DetailField>
            <DetailField label="HTTP status">
              <span className="tabular-nums">{data.http_status || "—"}</span>
            </DetailField>
            <DetailField label="Streaming">{data.stream ? "Yes" : "No"}</DetailField>
            <DetailField label="Agent">
              <code className="text-xs">{data.agent_id || "—"}</code>
            </DetailField>
            <DetailField label="Session">
              {data.session_id ? (
                <Link
                  to={`/sessions/${encodeURIComponent(data.session_id)}`}
                  className="font-mono text-xs hover:underline"
                >
                  {data.session_id}
                </Link>
              ) : (
                "—"
              )}
            </DetailField>
          </dl>
        </CardContent>
      </Card>

      <section className="space-y-4" aria-labelledby="legs-heading">
        <div>
          <h2 id="legs-heading" className="font-heading text-lg font-semibold">
            Upstream calls
          </h2>
          <p className="text-sm text-muted-foreground">
            Each call the gateway made for this request, in order. The note explains why a tier was
            chosen.
          </p>
        </div>
        {legs.length === 0 ? (
          <p className="text-sm text-muted-foreground">No upstream calls were recorded.</p>
        ) : (
          <ol>
            {legs.map((leg, index) => (
              <LegItem
                key={leg.seq}
                leg={leg}
                last={index === legs.length - 1}
                content={content.data?.legs.find((item) => item.seq === leg.seq)}
              />
            ))}
          </ol>
        )}
      </section>

      <section className="space-y-4" aria-labelledby="content-heading">
        <div>
          <h2 id="content-heading" className="font-heading text-lg font-semibold">
            Content
          </h2>
          <p className="text-sm text-muted-foreground">
            What Claude Code sent and the final response it got.
          </p>
        </div>
        {!data.captured ? (
          <ContentHint />
        ) : content.isError ? (
          <QueryError error={content.error} onRetry={() => content.refetch()} />
        ) : content.isPending ? (
          <Skeleton className="h-64 w-full rounded-xl" />
        ) : content.data === null ? (
          <ContentHint />
        ) : (
          <RequestContentView
            content={content.data}
            loadingAll={content.isPlaceholderData}
            onLoadAll={() => setTail(0)}
          />
        )}
      </section>
    </>
  )
}
