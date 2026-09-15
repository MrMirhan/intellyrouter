import { CrownIcon } from "lucide-react"

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { Model, Provider } from "@/lib/types"

export function ModelSelect({
  id,
  value,
  onChange,
  models,
  providers,
  excludeSubscription = false,
  invalid = false,
}: {
  id: string
  value: number
  onChange: (modelId: number) => void
  models: Model[]
  providers: Provider[]
  excludeSubscription?: boolean
  invalid?: boolean
}) {
  const providerById = new Map(providers.map((provider) => [provider.id, provider]))
  const options = models
    .filter((model) => model.enabled || model.id === value)
    .map((model) => ({ model, provider: providerById.get(model.provider_id) }))
    .filter(({ provider }) => !(excludeSubscription && provider?.type === "anthropic-subscription"))
    .sort(
      (a, b) =>
        a.model.model_id.localeCompare(b.model.model_id) ||
        (a.provider?.name ?? "").localeCompare(b.provider?.name ?? ""),
    )

  return (
    <Select value={value ? String(value) : ""} onValueChange={(next) => onChange(Number(next))}>
      <SelectTrigger id={id} className="w-full" aria-invalid={invalid || undefined}>
        <SelectValue placeholder="Select a model" />
      </SelectTrigger>
      <SelectContent>
        {options.length === 0 ? (
          <p className="px-2 py-1.5 text-sm text-muted-foreground">No enabled models</p>
        ) : (
          options.map(({ model, provider }) => (
            <SelectItem key={model.id} value={String(model.id)}>
              <span className="font-mono text-xs">{model.model_id}</span>
              <span className="text-muted-foreground">- {provider?.name ?? `provider ${model.provider_id}`}</span>
              {provider?.type === "anthropic-subscription" && (
                <CrownIcon aria-label="Claude subscription" className="size-3.5 text-muted-foreground" />
              )}
              {!model.enabled && <span className="text-muted-foreground">(disabled)</span>}
            </SelectItem>
          ))
        )}
      </SelectContent>
    </Select>
  )
}
