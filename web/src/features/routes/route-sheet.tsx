import { useRef, useState, type SubmitEvent } from "react"
import { Link } from "react-router"
import {
  ArrowDownIcon,
  ArrowUpIcon,
  PlusIcon,
  TriangleAlertIcon,
  XIcon,
} from "lucide-react"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { AdvisorSettingsFields } from "@/features/routes/advisor-settings"
import { GuidedSettingsFields } from "@/features/routes/guided-settings"
import { ModelSelect } from "@/features/routes/model-select"
import { useCreateRoute, useModels, useProviders, useUpdateRoute } from "@/lib/queries"
import {
  advisorForm,
  advisorSettings,
  defaultEscalateSettings,
  defaultGuidedSettings,
  defaultLabel,
  escalateSettings,
  guidedSettings,
  labelPattern,
} from "@/lib/routes"
import type {
  EscalateSettings,
  EscalationTarget,
  GuidedSettings,
  Model,
  Provider,
  Route,
  RouteInput,
  Strategy,
} from "@/lib/types"

interface TierDraft {
  key: number
  modelId: number
  label: string
}

export function RouteSheet({
  open,
  onOpenChange,
  route,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  route: Route | null
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl">
        <RouteForm route={route} onDone={() => onOpenChange(false)} />
      </SheetContent>
    </Sheet>
  )
}

function TargetSelect({
  id,
  value,
  onChange,
}: {
  id: string
  value: EscalationTarget
  onChange: (target: EscalationTarget) => void
}) {
  return (
    <Select value={value} onValueChange={(next) => onChange(next === "top" ? "top" : "next")}>
      <SelectTrigger id={id} className="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="next">Next tier up</SelectItem>
        <SelectItem value="top">Top tier</SelectItem>
      </SelectContent>
    </Select>
  )
}

function tierTitle(strategy: Strategy, index: number): string {
  if (strategy === "direct") return "Model"
  if (strategy === "guided") {
    return index === 0 ? "Executor 1 · first choice" : `Executor ${index + 1} · after repeated failures`
  }
  return index === 0 ? "Tier 0 · base model" : `Tier ${index} · escalation`
}

function RouteForm({ route, onDone }: { route: Route | null; onDone: () => void }) {
  const providers = useProviders()
  const models = useModels()
  const createRoute = useCreateRoute()
  const updateRoute = useUpdateRoute()

  const [name, setName] = useState(route?.name ?? "")
  const [strategy, setStrategy] = useState<Strategy>(route?.strategy ?? "escalate")
  const [tiers, setTiers] = useState<TierDraft[]>(() =>
    route
      ? route.tiers.map((tier, index) => ({ key: index, modelId: tier.model_id, label: tier.label }))
      : [
          { key: 0, modelId: 0, label: "" },
          { key: 1, modelId: 0, label: "" },
        ],
  )
  const [settings, setSettings] = useState<EscalateSettings>(() =>
    route ? escalateSettings(route) : defaultEscalateSettings,
  )
  const [guided, setGuided] = useState<GuidedSettings>(() =>
    route ? guidedSettings(route) : defaultGuidedSettings,
  )
  const [advisor, setAdvisor] = useState(() => advisorForm(route?.settings.advisor))
  const [submitted, setSubmitted] = useState(false)
  const nextKey = useRef(tiers.length)

  const modelList = models.data ?? []
  const providerList = providers.data ?? []
  const modelById = new Map(modelList.map((model) => [model.id, model]))
  const escalate = strategy === "escalate"
  const tiered = strategy !== "direct"
  const pending = createRoute.isPending || updateRoute.isPending

  const effectiveLabels = tiers.map((tier) => {
    const model = modelById.get(tier.modelId)
    return tier.label.trim() || (model ? defaultLabel(model.model_id) : "")
  })

  const problems: string[] = []
  if (!name.trim()) {
    problems.push("Enter a route name.")
  } else if (/\s/.test(name.trim())) {
    problems.push("The route name cannot contain spaces.")
  }
  tiers.forEach((tier, index) => {
    const model = modelById.get(tier.modelId)
    if (!model) {
      problems.push(`${tierTitle(strategy, index)}: choose a model.`)
    } else if (!model.enabled) {
      problems.push(`${tierTitle(strategy, index)}: ${model.model_id} is disabled.`)
    }
    const label = tier.label.trim()
    if (tiered && label && !labelPattern.test(label)) {
      problems.push(`${tierTitle(strategy, index)}: a label may only contain a-z, 0-9, ".", "_" and "-".`)
    }
  })
  if (tiered) {
    const seen = new Set<string>()
    for (const label of effectiveLabels) {
      if (label && seen.has(label)) problems.push(`The label "${label}" is used more than once.`)
      seen.add(label)
    }
  }
  if (escalate) {
    const classifierModel = modelById.get(settings.classifier.model_id)
    if (settings.classifier.enabled && !classifierModel) {
      problems.push("Choose a classifier model.")
    } else if (settings.classifier.enabled && classifierModel && !classifierModel.enabled) {
      problems.push(`The classifier model ${classifierModel.model_id} is disabled.`)
    }
    if (settings.failure_streak.enabled && !(settings.failure_streak.threshold >= 1)) {
      problems.push("The failure threshold must be 1 or more.")
    }
  }
  if (strategy === "guided") {
    const directorModel = modelById.get(guided.director.model_id)
    if (!directorModel) {
      problems.push("Choose a director model.")
    } else if (!directorModel.enabled) {
      problems.push(`The director model ${directorModel.model_id} is disabled.`)
    }
    if (!(guided.director.max_calls_per_turn >= 1)) {
      problems.push("Director calls per turn must be 1 or more.")
    }
  }

  const changeStrategy = (next: Strategy) => {
    setStrategy(next)
    if (next === "direct") {
      setTiers((current) => current.slice(0, 1))
    } else if (next === "escalate" && tiers.length < 2) {
      setTiers((current) => [...current, { key: nextKey.current++, modelId: 0, label: "" }])
    }
  }

  const updateTier = (key: number, changes: Partial<TierDraft>) =>
    setTiers((current) => current.map((tier) => (tier.key === key ? { ...tier, ...changes } : tier)))

  const moveTier = (index: number, offset: -1 | 1) =>
    setTiers((current) => {
      const next = [...current]
      const [tier] = next.splice(index, 1)
      next.splice(index + offset, 0, tier)
      return next
    })

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    setSubmitted(true)
    if (problems.length > 0) return
    const input: RouteInput = {
      name: name.trim(),
      strategy,
      tiers: tiers.map((tier) => ({ model_id: tier.modelId, label: tier.label.trim() })),
      settings: { ...(escalate ? settings : strategy === "guided" ? guided : {}), ...advisorSettings(advisor) },
    }
    const onSuccess = (saved: Route) => {
      toast.success(route ? `Saved ${saved.name}` : `Created ${saved.name}`)
      onDone()
    }
    if (route) {
      updateRoute.mutate({ id: route.id, input }, { onSuccess })
    } else {
      createRoute.mutate(input, { onSuccess })
    }
  }

  const minTiers = escalate ? 2 : 1
  const hasEnabledModels = modelList.some((model) => model.enabled)
  const missingClaude = name.trim() !== "" && !name.toLowerCase().includes("claude")

  return (
    <form onSubmit={submit} className="flex h-full min-h-0 flex-col">
      <SheetHeader className="border-b">
        <SheetTitle>{route ? `Edit ${route.name}` : "New route"}</SheetTitle>
        <SheetDescription>
          A route turns one model name in Claude Code into a chain of provider models.
        </SheetDescription>
      </SheetHeader>

      <div className="grid flex-1 content-start gap-6 overflow-y-auto p-4">
        {providers.isPending || models.isPending ? (
          <div className="grid gap-3">
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-24 w-full" />
          </div>
        ) : (
          <>
            {!hasEnabledModels && (
              <Alert>
                <TriangleAlertIcon />
                <AlertTitle>No enabled models</AlertTitle>
                <AlertDescription>
                  <p>
                    Add a provider and enable its models on the{" "}
                    <Link to="/providers" className="underline underline-offset-4">
                      Providers
                    </Link>{" "}
                    page first.
                  </p>
                </AlertDescription>
              </Alert>
            )}

            <div className="grid gap-4 sm:grid-cols-[1fr_12rem]">
              <div className="grid gap-2">
                <Label htmlFor="route-name">Name</Label>
                <Input
                  id="route-name"
                  value={name}
                  onChange={(event) => setName(event.target.value)}
                  placeholder="claude-cheap"
                  className="font-mono"
                  autoComplete="off"
                  aria-describedby="route-name-hint"
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="route-strategy">Strategy</Label>
                <Select
                  value={strategy}
                  onValueChange={(next) => changeStrategy(next === "direct" || next === "guided" ? next : "escalate")}
                >
                  <SelectTrigger id="route-strategy" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="guided">Guided</SelectItem>
                    <SelectItem value="escalate">Escalate</SelectItem>
                    <SelectItem value="direct">Direct</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div id="route-name-hint" className="grid gap-1 text-sm sm:col-span-2">
                <p className="text-muted-foreground">
                  Claude Code sends this as the model name. Include <code>claude</code> in it, so
                  Claude Code&apos;s gateway model discovery lists the route.
                </p>
                {missingClaude && (
                  <p className="flex items-center gap-1.5 text-warning">
                    <TriangleAlertIcon aria-hidden className="size-4" />
                    Model discovery does not list names without &quot;claude&quot;.
                  </p>
                )}
                <p className="text-muted-foreground">
                  {escalate
                    ? "Escalate: a cheap base model does the work, and a stronger tier takes over a turn on a marker, a classifier verdict, or repeated tool failures."
                    : strategy === "guided"
                      ? "Guided: low-cost executor models do the work. A director model checks the session at checkpoints and tells the executor what to do next."
                      : "Direct: every request goes to one model."}
                </p>
              </div>
            </div>

            <section className="grid gap-3" aria-labelledby="tiers-heading">
              <div className="flex items-center justify-between gap-2">
                <h3 id="tiers-heading" className="font-medium">
                  {strategy === "guided" ? "Executors" : escalate ? "Tiers" : "Model"}
                </h3>
                {tiered && (
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      setTiers((current) => [
                        ...current,
                        { key: nextKey.current++, modelId: 0, label: "" },
                      ])
                    }
                  >
                    <PlusIcon />
                    Add tier
                  </Button>
                )}
              </div>
              <ol className="grid gap-3">
                {tiers.map((tier, index) => (
                  <TierRow
                    key={tier.key}
                    tier={tier}
                    index={index}
                    count={tiers.length}
                    minTiers={minTiers}
                    strategy={strategy}
                    derivedLabel={effectiveLabels[index] && !tier.label.trim() ? effectiveLabels[index] : ""}
                    models={modelList}
                    providers={providerList}
                    invalid={submitted && !modelById.get(tier.modelId)?.enabled}
                    onChange={(changes) => updateTier(tier.key, changes)}
                    onMove={(offset) => moveTier(index, offset)}
                    onRemove={() => setTiers((current) => current.filter((item) => item.key !== tier.key))}
                  />
                ))}
              </ol>
            </section>

            {escalate && (
              <>
                <section className="grid gap-3" aria-labelledby="rules-heading">
                  <h3 id="rules-heading" className="font-medium">
                    Escalation rules
                  </h3>
                  <div className="grid gap-3 rounded-lg border p-3">
                    <div className="flex items-start justify-between gap-4">
                      <div className="grid gap-1">
                        <Label htmlFor="classifier-enabled">Classifier</Label>
                        <p className="text-sm text-muted-foreground">
                          A small model checks each user turn and escalates when the user is unhappy
                          or asks for a review.
                        </p>
                      </div>
                      <Switch
                        id="classifier-enabled"
                        checked={settings.classifier.enabled}
                        onCheckedChange={(enabled) =>
                          setSettings((current) => ({
                            ...current,
                            classifier: { ...current.classifier, enabled },
                          }))
                        }
                      />
                    </div>
                    {settings.classifier.enabled && (
                      <div className="grid gap-3 sm:grid-cols-2">
                        <div className="grid gap-2">
                          <Label htmlFor="classifier-model">Classifier model</Label>
                          <ModelSelect
                            id="classifier-model"
                            value={settings.classifier.model_id}
                            onChange={(modelId) =>
                              setSettings((current) => ({
                                ...current,
                                classifier: { ...current.classifier, model_id: modelId },
                              }))
                            }
                            models={modelList}
                            providers={providerList}
                            invalid={submitted && !modelById.get(settings.classifier.model_id)?.enabled}
                            excludeSubscription
                          />
                          <p className="text-xs text-muted-foreground">
                            Subscription models are not offered, because the gateway makes this call
                            itself.
                          </p>
                        </div>
                        <div className="grid gap-2">
                          <Label htmlFor="classifier-target">Escalate to</Label>
                          <TargetSelect
                            id="classifier-target"
                            value={settings.classifier.target}
                            onChange={(target) =>
                              setSettings((current) => ({
                                ...current,
                                classifier: { ...current.classifier, target },
                              }))
                            }
                          />
                        </div>
                      </div>
                    )}
                  </div>

                  <div className="grid gap-3 rounded-lg border p-3">
                    <div className="flex items-start justify-between gap-4">
                      <div className="grid gap-1">
                        <Label htmlFor="failure-enabled">Failed tool calls</Label>
                        <p className="text-sm text-muted-foreground">
                          Escalate after several failed tool calls in a row.
                        </p>
                      </div>
                      <Switch
                        id="failure-enabled"
                        checked={settings.failure_streak.enabled}
                        onCheckedChange={(enabled) =>
                          setSettings((current) => ({
                            ...current,
                            failure_streak: { ...current.failure_streak, enabled },
                          }))
                        }
                      />
                    </div>
                    {settings.failure_streak.enabled && (
                      <div className="grid gap-3 sm:grid-cols-2">
                        <div className="grid gap-2">
                          <Label htmlFor="failure-threshold">Failures in a row</Label>
                          <Input
                            id="failure-threshold"
                            type="number"
                            min={1}
                            step={1}
                            value={settings.failure_streak.threshold || ""}
                            onChange={(event) =>
                              setSettings((current) => ({
                                ...current,
                                failure_streak: {
                                  ...current.failure_streak,
                                  threshold: Number(event.target.value),
                                },
                              }))
                            }
                            required
                          />
                        </div>
                        <div className="grid gap-2">
                          <Label htmlFor="failure-target">Escalate to</Label>
                          <TargetSelect
                            id="failure-target"
                            value={settings.failure_streak.target}
                            onChange={(target) =>
                              setSettings((current) => ({
                                ...current,
                                failure_streak: { ...current.failure_streak, target },
                              }))
                            }
                          />
                        </div>
                      </div>
                    )}
                  </div>
                </section>

                <MarkerHelp labels={effectiveLabels} />
              </>
            )}

            {strategy === "guided" && (
              <GuidedSettingsFields
                settings={guided}
                onChange={setGuided}
                models={modelList}
                providers={providerList}
                invalidDirector={submitted && !modelById.get(guided.director.model_id)?.enabled}
              />
            )}

            <AdvisorSettingsFields value={advisor} onChange={setAdvisor} models={modelList} providers={providerList} />

            {submitted && problems.length > 0 && (
              <Alert variant="destructive">
                <TriangleAlertIcon />
                <AlertTitle>Fix these before saving</AlertTitle>
                <AlertDescription>
                  <ul className="list-disc pl-4">
                    {problems.map((problem) => (
                      <li key={problem}>{problem}</li>
                    ))}
                  </ul>
                </AlertDescription>
              </Alert>
            )}
          </>
        )}
      </div>

      <SheetFooter className="flex-row justify-end border-t">
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending || providers.isPending || models.isPending}>
          {pending && <Spinner />}
          {route ? "Save route" : "Create route"}
        </Button>
      </SheetFooter>
    </form>
  )
}

function TierRow({
  tier,
  index,
  count,
  minTiers,
  strategy,
  derivedLabel,
  models,
  providers,
  invalid,
  onChange,
  onMove,
  onRemove,
}: {
  tier: TierDraft
  index: number
  count: number
  minTiers: number
  strategy: Strategy
  derivedLabel: string
  models: Model[]
  providers: Provider[]
  invalid: boolean
  onChange: (changes: Partial<TierDraft>) => void
  onMove: (offset: -1 | 1) => void
  onRemove: () => void
}) {
  const title = tierTitle(strategy, index)

  return (
    <li className="grid gap-3 rounded-lg border p-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium">{title}</span>
        {strategy !== "direct" && (
          <div className="flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Move ${title} up`}
              disabled={index === 0}
              onClick={() => onMove(-1)}
            >
              <ArrowUpIcon />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Move ${title} down`}
              disabled={index === count - 1}
              onClick={() => onMove(1)}
            >
              <ArrowDownIcon />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={`Remove ${title}`}
              disabled={count <= minTiers}
              onClick={onRemove}
            >
              <XIcon />
            </Button>
          </div>
        )}
      </div>
      <div className={strategy !== "direct" ? "grid gap-3 sm:grid-cols-[1fr_12rem]" : "grid gap-3"}>
        <div className="grid gap-2">
          <Label htmlFor={`tier-${tier.key}-model`}>Model</Label>
          <ModelSelect
            id={`tier-${tier.key}-model`}
            value={tier.modelId}
            onChange={(modelId) => onChange({ modelId })}
            models={models}
            providers={providers}
            invalid={invalid}
          />
        </div>
        {strategy !== "direct" && (
          <div className="grid gap-2">
            <Label htmlFor={`tier-${tier.key}-label`}>Label</Label>
            <Input
              id={`tier-${tier.key}-label`}
              value={tier.label}
              onChange={(event) => onChange({ label: event.target.value.toLowerCase() })}
              placeholder={derivedLabel || "from the model ID"}
              className="font-mono"
              autoComplete="off"
            />
          </div>
        )}
      </div>
    </li>
  )
}

function MarkerHelp({ labels }: { labels: string[] }) {
  return (
    <section className="grid gap-2 rounded-lg bg-muted p-3 text-sm" aria-labelledby="markers-heading">
      <h3 id="markers-heading" className="font-medium">
        Prompt markers
      </h3>
      <p className="text-muted-foreground">
        Type a marker in a Claude Code prompt to choose the tier for that turn.
      </p>
      <ul className="grid gap-1">
        <li>
          <code className="font-mono">#base</code>{" "}
          <span className="text-muted-foreground">selects tier 0{labels[0] && ` (${labels[0]})`}</span>
        </li>
        <li>
          <code className="font-mono">#up</code>{" "}
          <span className="text-muted-foreground">selects tier 1{labels[1] && ` (${labels[1]})`}</span>
        </li>
        {labels.map(
          (label, index) =>
            label && (
              <li key={`${label}-${index}`}>
                <code className="font-mono">#{label}</code>{" "}
                <span className="text-muted-foreground">selects tier {index}</span>
              </li>
            ),
        )}
      </ul>
    </section>
  )
}
