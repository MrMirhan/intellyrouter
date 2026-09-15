import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { Model, Provider } from "@/lib/types"

export function AdvisorSettingsFields({
  value,
  onChange,
  models,
  providers,
}: {
  value: string
  onChange: (value: string) => void
  models: Model[]
  providers: Provider[]
}) {
  const providerById = new Map(providers.map((provider) => [provider.id, provider]))
  const options = models
    .filter((model) => model.enabled || String(model.id) === value)
    .map((model) => {
      const provider = providerById.get(model.provider_id)
      return {
        model,
        provider,
        anthropic: provider?.type === "anthropic" || provider?.type === "anthropic-subscription",
      }
    })
    .sort(
      (a, b) =>
        Number(b.anthropic) - Number(a.anthropic) ||
        a.model.model_id.localeCompare(b.model.model_id) ||
        (a.provider?.name ?? "").localeCompare(b.provider?.name ?? ""),
    )

  return (
    <section className="grid gap-3" aria-labelledby="advisor-heading">
      <h3 id="advisor-heading" className="font-medium">
        Advisor
      </h3>
      <div className="grid gap-3 rounded-lg border p-3">
        <div className="grid gap-2">
          <Label htmlFor="route-advisor">Advisor model</Label>
          <Select value={value} onValueChange={onChange}>
            <SelectTrigger id="route-advisor" className="w-full" aria-describedby="route-advisor-hint">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="client">Use the advisor set in Claude Code</SelectItem>
              <SelectItem value="off">Off</SelectItem>
              {options.map(({ model, provider, anthropic }) => (
                <SelectItem key={model.id} value={String(model.id)} disabled={!anthropic}>
                  <span className="font-mono text-xs">{model.model_id}</span>
                  <span className="text-muted-foreground">- {provider?.name ?? `provider ${model.provider_id}`}</span>
                  {!anthropic && <span className="text-muted-foreground">(not an Anthropic provider)</span>}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <p id="route-advisor-hint" className="text-sm text-muted-foreground">
          With Claude Code&apos;s advisor tool, the model asks a second model when it is stuck or before a
          large change. The model you choose here replaces the advisor that Claude Code asks for on this
          route, and Off removes the tool. Anthropic runs the advisor, so it must be a model on an Anthropic
          provider. Claude Code must have the advisor turned on: see Connect Claude Code on the Routes page.
          When a model rejects the advisor, the gateway sends that request again without it.
        </p>
      </div>
    </section>
  )
}
