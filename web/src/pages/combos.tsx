import { useState, type SubmitEvent } from "react"
import { ArrowDownIcon, ArrowUpIcon, LayersIcon, PencilIcon, PlusIcon, Trash2Icon, XIcon } from "lucide-react"
import { toast } from "sonner"

import { ConfirmDialog } from "@/components/confirm-dialog"
import { CopyButton } from "@/components/copy-button"
import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
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
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { ModelSelect } from "@/features/routes/model-select"
import {
  useCombos,
  useCreateCombo,
  useDeleteCombo,
  useModels,
  useProviders,
  useUpdateCombo,
} from "@/lib/queries"
import type { Combo, ComboMember, ComboStrategy, Model, Provider } from "@/lib/types"

const strategies: { value: ComboStrategy; label: string; description: string }[] = [
  {
    value: "fallback",
    label: "Fallback chain",
    description: "Every request starts with the first model. The next model gets the request only when the one before it fails.",
  },
  {
    value: "round-robin",
    label: "Round robin",
    description: "Requests take turns across the models, in proportion to their weights. A request that fails moves on to the next model.",
  },
  {
    value: "least-used",
    label: "Least used",
    description:
      "Each request goes to the model carrying the fewest requests for its weight. A request that fails moves on to the next model.",
  },
]

// Fallback runs its models in a fixed order, so only the other two share traffic.
const weighted = (strategy: ComboStrategy) => strategy !== "fallback"

const maxWeight = 1000

const namePattern = /^[A-Za-z0-9][A-Za-z0-9._-]*$/

function ComboCard({
  combo,
  modelById,
  providerById,
  onEdit,
}: {
  combo: Combo
  modelById: Map<number, Model>
  providerById: Map<number, Provider>
  onEdit: () => void
}) {
  const updateCombo = useUpdateCombo()
  const deleteCombo = useDeleteCombo()
  const directName = `combo/${combo.name}`

  return (
    <Card size="sm">
      <CardContent className="flex flex-wrap items-start gap-4">
        <div className="grid min-w-0 flex-1 gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono font-medium">{combo.name}</span>
            <Badge variant="outline">
              {strategies.find((item) => item.value === combo.strategy)?.label ?? combo.strategy}
            </Badge>
            {!combo.enabled && <Badge variant="secondary">Disabled</Badge>}
          </div>
          <ol className="flex flex-wrap items-center gap-1.5" aria-label="Models">
            {combo.members.map((member, index) => {
              const model = modelById.get(member.model_id)
              const provider = model && providerById.get(model.provider_id)
              const shares = weighted(combo.strategy) && combo.members.some((other) => other.weight !== member.weight)
              return (
                <li key={member.model_id} className="flex items-center gap-1.5">
                  <span className="text-xs text-muted-foreground tabular-nums">{index + 1}.</span>
                  <span className="rounded-md border px-2 py-0.5 font-mono text-xs">
                    {model?.model_id ?? `model ${member.model_id}`}
                    {provider && <span className="text-muted-foreground"> - {provider.name}</span>}
                    {model && !model.enabled && <span className="text-muted-foreground"> (disabled)</span>}
                    {shares && <span className="text-muted-foreground"> &times;{member.weight}</span>}
                  </span>
                </li>
              )
            })}
          </ol>
          <p className="text-xs text-muted-foreground">
            Use it in Claude Code as <code className="font-mono text-foreground">{directName}</code>, or
            choose it as a model in a route.
          </p>
        </div>
        <div className="flex items-center gap-1">
          <Switch
            checked={combo.enabled}
            disabled={updateCombo.isPending}
            onCheckedChange={(enabled) => updateCombo.mutate({ id: combo.id, input: { enabled } })}
            aria-label={`Enable ${combo.name}`}
            className="mr-2"
          />
          <CopyButton value={directName} label={`Copy ${directName}`} iconOnly />
          <Button variant="ghost" size="icon-sm" aria-label={`Edit ${combo.name}`} onClick={onEdit}>
            <PencilIcon />
          </Button>
          <ConfirmDialog
            trigger={
              <Button variant="ghost" size="icon-sm" aria-label={`Delete ${combo.name}`}>
                <Trash2Icon />
              </Button>
            }
            title={`Delete ${combo.name}?`}
            description="Claude Code sessions that send this combo's name start to fail. The gateway refuses while a route uses the combo."
            confirmLabel="Delete combo"
            pending={deleteCombo.isPending}
            onConfirm={(close) =>
              deleteCombo.mutate(combo.id, {
                onSuccess: () => {
                  close()
                  toast.success(`Deleted ${combo.name}`)
                },
              })
            }
          />
        </div>
      </CardContent>
    </Card>
  )
}

function ComboForm({
  combo,
  models,
  providers,
  onDone,
}: {
  combo: Combo | null
  models: Model[]
  providers: Provider[]
  onDone: () => void
}) {
  const createCombo = useCreateCombo()
  const updateCombo = useUpdateCombo()
  const [name, setName] = useState(combo?.name ?? "")
  const [strategy, setStrategy] = useState<ComboStrategy>(combo?.strategy ?? "fallback")
  const [enabled, setEnabled] = useState(combo?.enabled ?? true)
  const [members, setMembers] = useState<ComboMember[]>(combo?.members ?? [])
  const [submitted, setSubmitted] = useState(false)

  const comboProviders = new Set(providers.filter((provider) => provider.type === "combo").map((provider) => provider.id))
  const chosen = new Set(members.map((member) => member.model_id))
  const choices = models.filter((model) => !comboProviders.has(model.provider_id) && !chosen.has(model.id))
  const modelById = new Map(models.map((model) => [model.id, model]))
  const providerById = new Map(providers.map((provider) => [provider.id, provider]))
  const pending = createCombo.isPending || updateCombo.isPending

  const problems: string[] = []
  if (!namePattern.test(name.trim())) {
    problems.push('Enter a name of letters, digits, ".", "_" and "-" that starts with a letter or digit.')
  }
  if (members.length === 0) problems.push("Add at least one model.")
  if (weighted(strategy) && members.some((member) => member.weight < 1 || member.weight > maxWeight)) {
    problems.push(`Give every weight a value between 1 and ${maxWeight}.`)
  }

  const setWeight = (index: number, value: string) =>
    setMembers((current) =>
      current.map((member, i) => (i === index ? { ...member, weight: Number(value) || 0 } : member)),
    )

  const move = (index: number, by: number) =>
    setMembers((current) => {
      const next = [...current]
      const [item] = next.splice(index, 1)
      next.splice(index + by, 0, item)
      return next
    })

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setSubmitted(true)
    if (problems.length > 0) return
    const input = { name: name.trim(), strategy, enabled, members }
    const onSuccess = (saved: Combo) => {
      toast.success(combo ? `Saved ${saved.name}` : `Created ${saved.name}`)
      onDone()
    }
    if (combo) {
      updateCombo.mutate({ id: combo.id, input }, { onSuccess })
    } else {
      createCombo.mutate(input, { onSuccess })
    }
  }

  return (
    <form onSubmit={submit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>{combo ? `Edit ${combo.name}` : "New combo"}</DialogTitle>
        <DialogDescription>
          Claude Code can use the combo as <code>combo/{name.trim() || "name"}</code>, and routes can use it
          like a model.
        </DialogDescription>
      </DialogHeader>

      <div className="grid gap-2">
        <Label htmlFor="combo-name">Name</Label>
        <Input
          id="combo-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          className="font-mono"
          autoComplete="off"
          aria-invalid={(submitted && !namePattern.test(name.trim())) || undefined}
        />
      </div>

      <div className="grid gap-2">
        <Label htmlFor="combo-strategy">Strategy</Label>
        <Select
          value={strategy}
          onValueChange={(next) => {
            const match = strategies.find((item) => item.value === next)
            if (match) setStrategy(match.value)
          }}
        >
          <SelectTrigger id="combo-strategy" className="w-full" aria-describedby="combo-strategy-hint">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {strategies.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p id="combo-strategy-hint" className="text-sm text-muted-foreground">
          {strategies.find((item) => item.value === strategy)?.description} A request moves on after a rate
          limit, a server error, a rejected key or model, or a prompt too long for the model.
        </p>
      </div>

      <div className="grid gap-2">
        <Label htmlFor="combo-add-model">Models</Label>
        {members.length > 0 && (
          <ol className="grid gap-1.5">
            {members.map((member, index) => {
              const model = modelById.get(member.model_id)
              const provider = model && providerById.get(model.provider_id)
              const name = model?.model_id ?? `model ${member.model_id}`
              return (
                <li key={member.model_id} className="flex items-center gap-2 rounded-md border px-2 py-1">
                  <span className="w-5 text-xs text-muted-foreground tabular-nums">{index + 1}.</span>
                  <span className="min-w-0 flex-1 truncate font-mono text-xs">
                    {name}
                    {provider && <span className="text-muted-foreground"> - {provider.name}</span>}
                  </span>
                  {weighted(strategy) && (
                    <Input
                      type="number"
                      min={1}
                      max={maxWeight}
                      className="h-7 w-16 text-xs"
                      aria-label={`Weight for ${name}`}
                      value={member.weight}
                      onChange={(event) => setWeight(index, event.target.value)}
                    />
                  )}
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Move ${name} up`}
                    disabled={index === 0}
                    onClick={() => move(index, -1)}
                  >
                    <ArrowUpIcon />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Move ${name} down`}
                    disabled={index === members.length - 1}
                    onClick={() => move(index, 1)}
                  >
                    <ArrowDownIcon />
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Remove ${name}`}
                    onClick={() => setMembers((current) => current.filter((item) => item.model_id !== member.model_id))}
                  >
                    <XIcon />
                  </Button>
                </li>
              )
            })}
          </ol>
        )}
        <ModelSelect
          id="combo-add-model"
          value={0}
          onChange={(id) => setMembers((current) => [...current, { model_id: id, weight: 1 }])}
          models={choices}
          providers={providers}
          invalid={submitted && members.length === 0}
        />
        {weighted(strategy) && members.length > 1 && (
          <p className="text-xs text-muted-foreground">
            Weight is a share, not a percentage: 3 against 1 sends three requests to the first model for every one
            the second gets.
          </p>
        )}
      </div>

      <div className="flex items-center gap-3">
        <Switch id="combo-enabled" checked={enabled} onCheckedChange={setEnabled} />
        <Label htmlFor="combo-enabled">Enabled</Label>
      </div>

      {submitted && problems.length > 0 && (
        <p role="alert" className="text-sm text-destructive">
          {problems.join(" ")}
        </p>
      )}

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {pending && <Spinner />}
          {combo ? "Save" : "Create combo"}
        </Button>
      </DialogFooter>
    </form>
  )
}

export function CombosPage() {
  const combos = useCombos()
  const models = useModels()
  const providers = useProviders()
  const [dialog, setDialog] = useState<{ open: boolean; combo: Combo | null }>({ open: false, combo: null })

  const modelById = new Map((models.data ?? []).map((model) => [model.id, model]))
  const providerById = new Map((providers.data ?? []).map((provider) => [provider.id, provider]))

  const newButton = (
    <Button onClick={() => setDialog({ open: true, combo: null })}>
      <PlusIcon />
      New combo
    </Button>
  )

  return (
    <>
      <PageHeader
        title="Combos"
        description="A combo sends each request to one of its models and moves on to the next model when one fails. Use it in Claude Code as combo/<name>, or as a model in a route."
        actions={newButton}
      />
      <Dialog open={dialog.open} onOpenChange={(open) => setDialog((current) => ({ ...current, open }))}>
        <DialogContent className="sm:max-w-xl">
          {dialog.open && (
            <ComboForm
              key={dialog.combo?.id ?? "new"}
              combo={dialog.combo}
              models={models.data ?? []}
              providers={providers.data ?? []}
              onDone={() => setDialog((current) => ({ ...current, open: false }))}
            />
          )}
        </DialogContent>
      </Dialog>

      {combos.isError ? (
        <QueryError error={combos.error} onRetry={() => combos.refetch()} />
      ) : combos.isPending ? (
        <div className="grid gap-3">
          {Array.from({ length: 2 }, (_, index) => (
            <Skeleton key={index} className="h-24 w-full" />
          ))}
        </div>
      ) : combos.data.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <LayersIcon />
            </EmptyMedia>
            <EmptyTitle>No combos yet</EmptyTitle>
            <EmptyDescription>
              Put models from any provider in a fallback chain or balance requests across them.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>{newButton}</EmptyContent>
        </Empty>
      ) : (
        <div className="grid gap-3">
          {combos.data.map((combo) => (
            <ComboCard
              key={combo.id}
              combo={combo}
              modelById={modelById}
              providerById={providerById}
              onEdit={() => setDialog({ open: true, combo })}
            />
          ))}
        </div>
      )}
    </>
  )
}
