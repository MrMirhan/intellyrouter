import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { defaultEffort, efforts } from "@/features/routes/effort"
import { CountField } from "@/features/routes/guided-settings"
import type { AdvisorForm } from "@/lib/routes"
import type { Model, Provider } from "@/lib/types"

export function AdvisorSettingsFields({
  value,
  onChange,
  models,
  providers,
}: {
  value: AdvisorForm
  onChange: (value: AdvisorForm) => void
  models: Model[]
  providers: Provider[]
}) {
  const providerById = new Map(providers.map((provider) => [provider.id, provider]))
  const options = models
    .filter((model) => model.enabled || String(model.id) === value.choice)
    .map((model) => ({ model, provider: providerById.get(model.provider_id) }))
    .sort(
      (a, b) =>
        a.model.model_id.localeCompare(b.model.model_id) ||
        (a.provider?.name ?? "").localeCompare(b.provider?.name ?? ""),
    )
  const modelChosen = value.choice !== "client" && value.choice !== "off"

  return (
    <section className="grid gap-3" aria-labelledby="advisor-heading">
      <h3 id="advisor-heading" className="font-medium">
        Advisor
      </h3>
      <div className="grid gap-3 rounded-lg border p-3">
        <div className="grid gap-3 sm:grid-cols-[1fr_10rem]">
          <div className="grid gap-2">
            <Label htmlFor="route-advisor">Advisor model</Label>
            <Select value={value.choice} onValueChange={(choice) => onChange({ ...value, choice })}>
              <SelectTrigger id="route-advisor" className="w-full" aria-describedby="route-advisor-hint">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="client">No advisor, or the one set in Claude Code</SelectItem>
                <SelectItem value="off">Off</SelectItem>
                {options.map(({ model, provider }) => (
                  <SelectItem key={model.id} value={String(model.id)}>
                    <span className="font-mono text-xs">{model.model_id}</span>
                    <span className="text-muted-foreground">- {provider?.name ?? `provider ${model.provider_id}`}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {modelChosen && (
            <div className="grid gap-2">
              <Label htmlFor="advisor-effort">Effort</Label>
              <Select
                value={value.effort || defaultEffort}
                onValueChange={(next) =>
                  onChange({ ...value, effort: efforts.find((effort) => effort.value === next)?.value ?? "" })
                }
              >
                <SelectTrigger id="advisor-effort" className="w-full">
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
          )}
        </div>
        {modelChosen && (
          <CountField
            id="advisor-max-calls"
            label="Questions per turn"
            hint="The advisor answers at most this many questions in one turn."
            value={value.max_calls_per_turn}
            min={1}
            onChange={(max) => onChange({ ...value, max_calls_per_turn: max })}
          />
        )}
        <p id="route-advisor-hint" className="text-sm text-muted-foreground">
          The executor gets an ask_advisor tool. When it is stuck, unsure, or about to report the task
          done, it asks the advisor. The gateway answers with the model you choose here, on any provider,
          and continues the same response; Claude Code does not see the question. On a guided route, the
          executor asks the advisor instead of the director. A model on your Claude subscription answers
          through Claude Code on the gateway machine and uses your plan limits. Steps on your Claude
          subscription cannot get this tool: there Claude Code&apos;s own advisor runs, when it is turned on
          in Claude Code and this is a Claude model. Off removes Claude Code&apos;s own advisor tool.
        </p>
      </div>
    </section>
  )
}
