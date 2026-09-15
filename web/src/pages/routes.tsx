import { useState } from "react"
import { Link } from "react-router"
import {
  ArrowRightIcon,
  CrownIcon,
  PencilIcon,
  PlusIcon,
  RouteIcon,
  TerminalIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react"
import { toast } from "sonner"

import { StrategyBadge } from "@/components/badges"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
import { RelativeTime } from "@/components/relative-time"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { ConnectDialog } from "@/features/routes/connect-dialog"
import { RouteSheet } from "@/features/routes/route-sheet"
import { useDeleteRoute, useModels, useProviders, useRoutes } from "@/lib/queries"
import { escalateSettings, guidedSettings } from "@/lib/routes"
import type { Model, Provider, Route } from "@/lib/types"

function targetText(target: string) {
  return target === "top" ? "top tier" : "next tier"
}

function RouteCard({
  route,
  modelById,
  providerById,
  onConnect,
  onEdit,
}: {
  route: Route
  modelById: Map<number, Model>
  providerById: Map<number, Provider>
  onConnect: () => void
  onEdit: () => void
}) {
  const deleteRoute = useDeleteRoute()
  const escalate = route.strategy === "escalate"
  const settings = escalateSettings(route)
  const modelName = (id: number) => modelById.get(id)?.model_id ?? `model ${id}`
  const guided = route.strategy === "guided" ? guidedSettings(route) : null

  const rules = [
    settings.classifier.enabled &&
      `Classifier ${modelName(settings.classifier.model_id)} → ${targetText(settings.classifier.target)}`,
    settings.failure_streak.enabled &&
      `${settings.failure_streak.threshold} failed tool calls → ${targetText(settings.failure_streak.target)}`,
  ].filter(Boolean)

  return (
    <Card size="sm">
      <CardContent className="flex flex-wrap items-start gap-4">
        <div className="grid min-w-0 flex-1 gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono font-medium">{route.name}</span>
            <StrategyBadge strategy={route.strategy} />
            {!route.name.toLowerCase().includes("claude") && (
              <span className="flex items-center gap-1 text-xs text-warning">
                <TriangleAlertIcon aria-hidden className="size-3.5" />
                Not listed by model discovery
              </span>
            )}
          </div>
          <ol className="flex flex-wrap items-center gap-1.5" aria-label="Tier chain">
            {route.tiers.map((tier, index) => {
              const model = modelById.get(tier.model_id)
              const subscription =
                model && providerById.get(model.provider_id)?.type === "anthropic-subscription"
              return (
                <li key={`${tier.model_id}-${index}`} className="flex items-center gap-1.5">
                  {index > 0 && <ArrowRightIcon aria-hidden className="size-3.5 text-muted-foreground" />}
                  <span className="flex items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-xs">
                    {subscription && (
                      <CrownIcon aria-label="Claude subscription" className="size-3.5 text-muted-foreground" />
                    )}
                    {modelName(tier.model_id)}
                    {escalate && <span className="text-muted-foreground">#{tier.label}</span>}
                  </span>
                </li>
              )
            })}
          </ol>
          <p className="text-xs text-muted-foreground">
            {escalate && (rules.length > 0 ? rules.join(" · ") : "Markers only")}
            {guided && `Director ${modelName(guided.director.model_id)}`}
            {route.settings.advisor?.model_id
              ? ` · Advisor ${modelName(route.settings.advisor.model_id)}`
              : route.settings.advisor?.off
                ? " · Advisor off"
                : ""}
            {(escalate || guided) && " · "}
            Created <RelativeTime ms={route.created_at} />
          </p>
        </div>
        <div className="flex items-center gap-1">
          <Button variant="outline" size="sm" onClick={onConnect}>
            <TerminalIcon />
            Connect Claude Code
          </Button>
          <Button variant="ghost" size="icon-sm" aria-label={`Edit ${route.name}`} onClick={onEdit}>
            <PencilIcon />
          </Button>
          <ConfirmDialog
            trigger={
              <Button variant="ghost" size="icon-sm" aria-label={`Delete ${route.name}`}>
                <Trash2Icon />
              </Button>
            }
            title={`Delete ${route.name}?`}
            description="Claude Code sessions that send this model name start to fail. Past requests stay in the ledger."
            confirmLabel="Delete route"
            pending={deleteRoute.isPending}
            onConfirm={(close) =>
              deleteRoute.mutate(route.id, {
                onSuccess: () => {
                  close()
                  toast.success(`Deleted ${route.name}`)
                },
              })
            }
          />
        </div>
      </CardContent>
    </Card>
  )
}

export function RoutesPage() {
  const routes = useRoutes()
  const models = useModels()
  const providers = useProviders()
  const [sheet, setSheet] = useState<{ open: boolean; route: Route | null }>({
    open: false,
    route: null,
  })
  const [connect, setConnect] = useState<{ open: boolean; route: Route | null }>({
    open: false,
    route: null,
  })

  const modelById = new Map((models.data ?? []).map((model) => [model.id, model]))
  const providerById = new Map((providers.data ?? []).map((provider) => [provider.id, provider]))
  const hasEnabledModels = models.data?.some((model) => model.enabled) ?? false

  const newButton = (
    <Button onClick={() => setSheet({ open: true, route: null })}>
      <PlusIcon />
      New route
    </Button>
  )

  return (
    <>
      <PageHeader
        title="Routes"
        description="Each route is a model name for Claude Code, backed by one model or an escalation chain."
        actions={newButton}
      />
      <RouteSheet
        open={sheet.open}
        route={sheet.route}
        onOpenChange={(open) => setSheet((current) => ({ ...current, open }))}
      />
      <ConnectDialog
        open={connect.open}
        route={connect.route}
        onOpenChange={(open) => setConnect((current) => ({ ...current, open }))}
      />

      {routes.isError ? (
        <QueryError error={routes.error} onRetry={() => routes.refetch()} />
      ) : routes.isPending ? (
        <div className="grid gap-3">
          {Array.from({ length: 3 }, (_, index) => (
            <Skeleton key={index} className="h-24 w-full" />
          ))}
        </div>
      ) : routes.data.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <RouteIcon />
            </EmptyMedia>
            <EmptyTitle>No routes yet</EmptyTitle>
            <EmptyDescription>
              {hasEnabledModels || models.isPending
                ? "Create a route to give Claude Code a model name to use."
                : "Routes are built from enabled models. Add a provider and enable its models first."}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            {hasEnabledModels || models.isPending ? (
              newButton
            ) : (
              <Button asChild>
                <Link to="/providers">Go to providers</Link>
              </Button>
            )}
          </EmptyContent>
        </Empty>
      ) : (
        <div className="grid gap-3">
          {routes.data.map((route) => (
            <RouteCard
              key={route.id}
              route={route}
              modelById={modelById}
              providerById={providerById}
              onConnect={() => setConnect({ open: true, route })}
              onEdit={() => setSheet({ open: true, route })}
            />
          ))}
        </div>
      )}
    </>
  )
}
