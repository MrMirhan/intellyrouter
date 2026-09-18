import { useMemo, useState } from "react"
import {
  BoxesIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  PencilIcon,
  PlusIcon,
  RefreshCwIcon,
  SearchIcon,
} from "lucide-react"

import { QueryError } from "@/components/query-error"
import { Badge } from "@/components/ui/badge"
import { TableSkeleton } from "@/components/table-skeleton"
import { Button } from "@/components/ui/button"
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
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCaption,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ModelDialog } from "@/features/providers/model-dialog"
import { formatCount, formatTokens, formatUsd } from "@/lib/format"
import { useModels, useUpdateModel } from "@/lib/queries"
import type { Model, Provider } from "@/lib/types"

const pageSize = 25

export function ProviderModels({
  provider,
  syncing,
  onSync,
}: {
  provider: Provider
  syncing: boolean
  onSync: () => void
}) {
  const models = useModels(provider.id)
  const updateModel = useUpdateModel()
  const [search, setSearch] = useState("")
  const [enabledOnly, setEnabledOnly] = useState(false)
  const [page, setPage] = useState(1)
  const [dialog, setDialog] = useState<{ open: boolean; model: Model | null }>({
    open: false,
    model: null,
  })

  const all = models.data ?? []
  const enabledCount = all.filter((model) => model.enabled).length
  const rows = useMemo(() => {
    const term = search.trim().toLowerCase()
    return (models.data ?? [])
      .filter(
        (model) =>
          (!enabledOnly || model.enabled) &&
          (!term ||
            model.model_id.toLowerCase().includes(term) ||
            model.display_name.toLowerCase().includes(term)),
      )
      .sort((a, b) => Number(b.enabled) - Number(a.enabled) || a.model_id.localeCompare(b.model_id))
  }, [models.data, search, enabledOnly])

  const pageCount = Math.max(1, Math.ceil(rows.length / pageSize))
  const currentPage = Math.min(page, pageCount)
  const visible = rows.slice((currentPage - 1) * pageSize, currentPage * pageSize)
  const addButton = (
    <Button variant="outline" size="sm" onClick={() => setDialog({ open: true, model: null })}>
      <PlusIcon />
      Add model manually
    </Button>
  )

  if (models.isError) {
    return <QueryError error={models.error} onRetry={() => models.refetch()} />
  }

  return (
    <div className="grid gap-3">
      <ModelDialog
        providerId={provider.id}
        model={dialog.model}
        open={dialog.open}
        onOpenChange={(open) => setDialog((current) => ({ ...current, open }))}
      />

      {models.isSuccess && all.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BoxesIcon />
            </EmptyMedia>
            <EmptyTitle>No models yet</EmptyTitle>
            <EmptyDescription>
              Sync the model list from {provider.name}. If the provider has no model list, add
              models by hand.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent className="flex-row justify-center">
            <Button size="sm" onClick={onSync} disabled={syncing}>
              {syncing ? <Spinner /> : <RefreshCwIcon />}
              Sync models
            </Button>
            {addButton}
          </EmptyContent>
        </Empty>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-3">
            <div className="relative w-full max-w-xs">
              <SearchIcon
                aria-hidden
                className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
              />
              <Input
                value={search}
                onChange={(event) => {
                  setSearch(event.target.value)
                  setPage(1)
                }}
                placeholder="Filter models"
                aria-label={`Filter ${provider.name} models`}
                className="pl-8"
              />
            </div>
            <div className="flex items-center gap-2">
              <Switch
                id={`enabled-only-${provider.id}`}
                checked={enabledOnly}
                onCheckedChange={(checked) => {
                  setEnabledOnly(checked)
                  setPage(1)
                }}
              />
              <Label htmlFor={`enabled-only-${provider.id}`}>Enabled only</Label>
            </div>
            <span className="text-sm text-muted-foreground tabular-nums">
              {formatCount(enabledCount)} enabled of {formatCount(all.length)}
            </span>
            <div className="ml-auto">{addButton}</div>
          </div>

          <div className="rounded-lg border">
            <Table>
              <TableCaption className="sr-only">
                Models for {provider.name}. Prices are in USD per 1M tokens.
              </TableCaption>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-16">Enabled</TableHead>
                  <TableHead>Model</TableHead>
                  <TableHead className="w-20">Images</TableHead>
                  <TableHead className="text-right">Context</TableHead>
                  <TableHead className="text-right">Input</TableHead>
                  <TableHead className="text-right">Output</TableHead>
                  <TableHead className="text-right">Cache read</TableHead>
                  <TableHead className="text-right">Cache write</TableHead>
                  <TableHead className="w-12">
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {models.isPending ? (
                  <TableSkeleton columns={9} />
                ) : visible.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={9} className="py-6 text-center text-muted-foreground">
                      No models match this filter.
                    </TableCell>
                  </TableRow>
                ) : (
                  visible.map((model) => (
                    <TableRow key={model.id}>
                      <TableCell>
                        <Switch
                          checked={model.enabled}
                          disabled={updateModel.isPending && updateModel.variables?.id === model.id}
                          onCheckedChange={(checked) =>
                            updateModel.mutate({ id: model.id, input: { enabled: checked } })
                          }
                          aria-label={`Enable ${model.model_id}`}
                        />
                      </TableCell>
                      <TableCell className="max-w-72">
                        <div className="truncate font-mono text-xs font-medium" title={model.model_id}>
                          {model.model_id}
                        </div>
                        {model.display_name && model.display_name !== model.model_id && (
                          <div className="truncate text-xs text-muted-foreground" title={model.display_name}>
                            {model.display_name}
                          </div>
                        )}
                      </TableCell>
                      <TableCell>
                        {model.vision ? (
                          <Badge variant="secondary">Images</Badge>
                        ) : (
                          <span className="text-xs text-muted-foreground">—</span>
                        )}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {model.context > 0 ? formatTokens(model.context) : "—"}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">{formatUsd(model.price_in)}</TableCell>
                      <TableCell className="text-right tabular-nums">{formatUsd(model.price_out)}</TableCell>
                      <TableCell className="text-right tabular-nums">
                        {formatUsd(model.price_cache_read)}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {formatUsd(model.price_cache_write)}
                      </TableCell>
                      <TableCell>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`Edit ${model.model_id}`}
                          onClick={() => setDialog({ open: true, model })}
                        >
                          <PencilIcon />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-2 text-sm text-muted-foreground">
            <span>Prices are in USD per 1M tokens.</span>
            {pageCount > 1 && (
              <div className="flex items-center gap-2">
                <span className="tabular-nums">
                  Page {currentPage} of {pageCount}
                </span>
                <Button
                  variant="outline"
                  size="icon-sm"
                  aria-label="Previous page"
                  disabled={currentPage <= 1}
                  onClick={() => setPage(currentPage - 1)}
                >
                  <ChevronLeftIcon />
                </Button>
                <Button
                  variant="outline"
                  size="icon-sm"
                  aria-label="Next page"
                  disabled={currentPage >= pageCount}
                  onClick={() => setPage(currentPage + 1)}
                >
                  <ChevronRightIcon />
                </Button>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  )
}
