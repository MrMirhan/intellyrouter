import { useState } from "react"
import {
  ChevronRightIcon,
  PencilIcon,
  PlusIcon,
  RefreshCwIcon,
  ServerIcon,
  Trash2Icon,
} from "lucide-react"
import { toast } from "sonner"

import { ProviderTypeBadge } from "@/components/badges"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { ProviderDialog } from "@/features/providers/provider-dialog"
import { ProviderModels } from "@/features/providers/provider-models"
import { isUpstream, providerTypes } from "@/lib/providers"
import {
  useDeleteProvider,
  useProviders,
  useSyncModels,
  useUpdateProvider,
} from "@/lib/queries"
import type { UpstreamProvider } from "@/lib/types"
import { cn } from "@/lib/utils"

function KeyStatus({ provider }: { provider: UpstreamProvider }) {
  const info = providerTypes[provider.type]
  if (info.key === "none") return null
  if (provider.has_key) return <Badge variant="outline">Key set</Badge>
  if (info.key === "required") return <Badge variant="destructive">Key missing</Badge>
  return <Badge variant="outline">No key</Badge>
}

function baseUrlText(provider: UpstreamProvider): string {
  const info = providerTypes[provider.type]
  if (info.baseUrl === "none") return "Claude Code login passthrough"
  if (provider.base_url) return provider.base_url
  return info.defaultBaseUrl ? `${info.defaultBaseUrl} (default)` : "Default base URL"
}

function ProviderCard({
  provider,
  expanded,
  onExpandedChange,
  onEdit,
}: {
  provider: UpstreamProvider
  expanded: boolean
  onExpandedChange: (expanded: boolean) => void
  onEdit: () => void
}) {
  const updateProvider = useUpdateProvider()
  const syncModels = useSyncModels()
  const deleteProvider = useDeleteProvider()

  const sync = () =>
    syncModels.mutate(provider.id, {
      onSuccess: (models) => {
        toast.success(`Synced ${models.length} models from ${provider.name}`, {
          description: "New models arrive disabled. Enable the ones you want to route to.",
        })
        onExpandedChange(true)
      },
      onError: (error) => {
        toast.error(error.message, {
          description: "If this provider has no model list, add its models by hand.",
        })
        onExpandedChange(true)
      },
    })

  return (
    <Collapsible open={expanded} onOpenChange={onExpandedChange}>
      <Card className="gap-0 py-0">
        <div className="flex flex-wrap items-center gap-3 p-4">
          <CollapsibleTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={`${expanded ? "Hide" : "Show"} models for ${provider.name}`}
            >
              <ChevronRightIcon className={cn("transition-transform", expanded && "rotate-90")} />
            </Button>
          </CollapsibleTrigger>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{provider.name}</span>
              <ProviderTypeBadge type={provider.type} />
              <KeyStatus provider={provider} />
              <span className="text-xs text-muted-foreground">
                Direct names: <code className="font-mono text-foreground">{provider.slug}/&lt;model id&gt;</code>
              </span>
            </div>
            <p className="truncate font-mono text-xs text-muted-foreground">{baseUrlText(provider)}</p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <div className="mr-2 flex items-center gap-2">
              <Switch
                id={`provider-enabled-${provider.id}`}
                checked={provider.enabled}
                disabled={updateProvider.isPending}
                onCheckedChange={(enabled) =>
                  updateProvider.mutate({ id: provider.id, input: { enabled } })
                }
              />
              <Label htmlFor={`provider-enabled-${provider.id}`} className="text-sm">
                {provider.enabled ? "Enabled" : "Disabled"}
              </Label>
            </div>
            <Button variant="outline" size="sm" onClick={sync} disabled={syncModels.isPending}>
              {syncModels.isPending ? <Spinner /> : <RefreshCwIcon />}
              Sync models
            </Button>
            <Button variant="ghost" size="icon-sm" aria-label={`Edit ${provider.name}`} onClick={onEdit}>
              <PencilIcon />
            </Button>
            <ConfirmDialog
              trigger={
                <Button variant="ghost" size="icon-sm" aria-label={`Delete ${provider.name}`}>
                  <Trash2Icon />
                </Button>
              }
              title={`Delete ${provider.name}?`}
              description="This removes the provider and its models. The gateway refuses while a route still uses one of its models."
              confirmLabel="Delete provider"
              pending={deleteProvider.isPending}
              onConfirm={(close) =>
                deleteProvider.mutate(provider.id, {
                  onSuccess: () => {
                    close()
                    toast.success(`Deleted ${provider.name}`)
                  },
                })
              }
            />
          </div>
        </div>
        <CollapsibleContent className="border-t p-4">
          <ProviderModels provider={provider} syncing={syncModels.isPending} onSync={sync} />
        </CollapsibleContent>
      </Card>
    </Collapsible>
  )
}

export function ProvidersPage() {
  const providers = useProviders()
  const upstream = (providers.data ?? []).filter(isUpstream)
  const [dialog, setDialog] = useState<{ open: boolean; provider: UpstreamProvider | null }>({
    open: false,
    provider: null,
  })
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(new Set())

  const addButton = (
    <Button onClick={() => setDialog({ open: true, provider: null })}>
      <PlusIcon />
      Add provider
    </Button>
  )

  const setProviderExpanded = (id: number, open: boolean) =>
    setExpanded((current) => {
      const next = new Set(current)
      if (open) {
        next.add(id)
      } else {
        next.delete(id)
      }
      return next
    })

  return (
    <>
      <PageHeader
        title="Providers"
        description="Upstream LLM providers. Sync or add their models, then enable the ones routes can use."
        actions={addButton}
      />
      <ProviderDialog
        open={dialog.open}
        provider={dialog.provider}
        onOpenChange={(open) => setDialog((current) => ({ ...current, open }))}
      />

      {providers.isError ? (
        <QueryError error={providers.error} onRetry={() => providers.refetch()} />
      ) : providers.isPending ? (
        <div className="grid gap-3">
          {Array.from({ length: 3 }, (_, index) => (
            <Skeleton key={index} className="h-20 w-full" />
          ))}
        </div>
      ) : upstream.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ServerIcon />
            </EmptyMedia>
            <EmptyTitle>No providers yet</EmptyTitle>
            <EmptyDescription>
              Add a cheap provider such as DeepSeek, and your Claude subscription for escalations.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>{addButton}</EmptyContent>
        </Empty>
      ) : (
        <div className="grid gap-3">
          {upstream.map((provider) => (
            <ProviderCard
              key={provider.id}
              provider={provider}
              expanded={expanded.has(provider.id)}
              onExpandedChange={(open) => setProviderExpanded(provider.id, open)}
              onEdit={() => setDialog({ open: true, provider })}
            />
          ))}
        </div>
      )}
    </>
  )
}
