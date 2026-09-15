import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { ModelSelect } from "@/features/routes/model-select"
import type { DirectorEffort, GuidedSettings, Model, Provider } from "@/lib/types"

const efforts: { value: DirectorEffort; label: string }[] = [
  { value: "", label: "Model default" },
  { value: "low", label: "Low" },
  { value: "medium", label: "Medium" },
  { value: "high", label: "High" },
  { value: "xhigh", label: "Extra high" },
  { value: "max", label: "Max" },
]

// Radix Select reserves the empty string, so the model default uses a placeholder value.
const defaultEffort = "default"

function count(value: string): number {
  const n = Math.trunc(Number(value))
  return Number.isFinite(n) && n > 0 ? n : 0
}

function SwitchRow({
  id,
  label,
  description,
  checked,
  onChange,
}: {
  id: string
  label: string
  description: string
  checked: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="grid gap-1">
        <Label htmlFor={id}>{label}</Label>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
      <Switch id={id} checked={checked} onCheckedChange={onChange} />
    </div>
  )
}

function CountField({
  id,
  label,
  hint,
  value,
  min,
  onChange,
}: {
  id: string
  label: string
  hint: string
  value: number
  min: number
  onChange: (value: number) => void
}) {
  return (
    <div className="grid content-start gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type="number"
        inputMode="numeric"
        min={min}
        step={1}
        value={String(value)}
        onChange={(event) => onChange(count(event.target.value))}
        aria-describedby={`${id}-hint`}
      />
      <p id={`${id}-hint`} className="text-xs text-muted-foreground">
        {hint}
      </p>
    </div>
  )
}

export function GuidedSettingsFields({
  settings,
  onChange,
  models,
  providers,
  invalidDirector,
}: {
  settings: GuidedSettings
  onChange: (settings: GuidedSettings) => void
  models: Model[]
  providers: Provider[]
  invalidDirector: boolean
}) {
  const director = models.find((model) => model.id === settings.director.model_id)
  const subscriptionDirector =
    director !== undefined &&
    providers.find((provider) => provider.id === director.provider_id)?.type === "anthropic-subscription"

  const setDirector = (changes: Partial<GuidedSettings["director"]>) =>
    onChange({ ...settings, director: { ...settings.director, ...changes } })
  const setCheckpoints = (changes: Partial<GuidedSettings["checkpoints"]>) =>
    onChange({ ...settings, checkpoints: { ...settings.checkpoints, ...changes } })

  return (
    <>
      <section className="grid gap-3" aria-labelledby="director-heading">
        <h3 id="director-heading" className="font-medium">
          Director
        </h3>
        <div className="grid gap-3 rounded-lg border p-3">
          <div className="grid gap-3 sm:grid-cols-[1fr_10rem]">
            <div className="grid gap-2">
              <Label htmlFor="director-model">Director model</Label>
              <ModelSelect
                id="director-model"
                value={settings.director.model_id}
                onChange={(modelId) => setDirector({ model_id: modelId })}
                models={models}
                providers={providers}
                invalid={invalidDirector}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="director-effort">Effort</Label>
              <Select
                value={settings.director.effort || defaultEffort}
                onValueChange={(next) =>
                  setDirector({
                    effort: efforts.find((effort) => effort.value === next)?.value ?? "",
                  })
                }
              >
                <SelectTrigger id="director-effort" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {efforts.map((effort) => (
                    <SelectItem key={effort.label} value={effort.value || defaultEffort}>
                      {effort.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <p className="text-sm text-muted-foreground">
            {subscriptionDirector
              ? "The gateway cannot call a subscription model on its own. At a checkpoint, Claude Code's request goes to the director, so the director does that step itself. Executors get no written guidance."
              : "At a checkpoint, the gateway sends the director a text copy of the session and adds its guidance to the executor's request. Claude Code does not see the guidance."}
          </p>
          <div className="grid gap-3 sm:grid-cols-2">
            <CountField
              id="director-max-calls"
              label="Director calls per turn"
              hint="The limit for one prompt and its tool loop."
              value={settings.director.max_calls_per_turn}
              min={1}
              onChange={(value) => setDirector({ max_calls_per_turn: value })}
            />
            <CountField
              id="escalate-after"
              label="Move up after"
              hint="Failure checkpoints in a turn before the next executor. 0 stays on the first executor."
              value={settings.escalate_after}
              min={0}
              onChange={(value) => onChange({ ...settings, escalate_after: value })}
            />
          </div>
        </div>
      </section>

      <section className="grid gap-3" aria-labelledby="checkpoints-heading">
        <h3 id="checkpoints-heading" className="font-medium">
          Checkpoints
        </h3>
        <div className="grid gap-4 rounded-lg border p-3">
          <SwitchRow
            id="checkpoint-turn-start"
            label="Turn start"
            description="The director plans the work when you send a new prompt."
            checked={settings.checkpoints.turn_start}
            onChange={(turnStart) => setCheckpoints({ turn_start: turnStart })}
          />
          <SwitchRow
            id="checkpoint-unsure"
            label="Executor is unsure"
            description="The executor writes that it is stuck or not sure."
            checked={settings.checkpoints.unsure}
            onChange={(unsure) => setCheckpoints({ unsure })}
          />
          <SwitchRow
            id="checkpoint-review"
            label="Review after passing tests"
            description="Files changed and the latest tool results pass. The director approves the work or lists what to fix."
            checked={settings.checkpoints.review_on_success}
            onChange={(review) => setCheckpoints({ review_on_success: review })}
          />
          <div className="grid gap-3 sm:grid-cols-2">
            <CountField
              id="checkpoint-failures"
              label="Failed tool results"
              hint="New failures since the last check. 0 turns this off."
              value={settings.checkpoints.failed_results}
              min={0}
              onChange={(value) => setCheckpoints({ failed_results: value })}
            />
            <CountField
              id="checkpoint-steps"
              label="Steps without a check"
              hint="Executor steps since the last check. 0 turns this off."
              value={settings.checkpoints.steps}
              min={0}
              onChange={(value) => setCheckpoints({ steps: value })}
            />
          </div>
        </div>
      </section>
    </>
  )
}
