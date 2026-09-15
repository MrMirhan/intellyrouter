import { SubscriptionBadge } from "@/components/badges"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
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
  const groups = providers
    .filter((provider) => !(excludeSubscription && provider.type === "anthropic-subscription"))
    .map((provider) => ({
      provider,
      models: models
        .filter((model) => model.provider_id === provider.id && (model.enabled || model.id === value))
        .sort((a, b) => a.model_id.localeCompare(b.model_id)),
    }))
    .filter((group) => group.models.length > 0)

  return (
    <Select value={value ? String(value) : ""} onValueChange={(next) => onChange(Number(next))}>
      <SelectTrigger id={id} className="w-full" aria-invalid={invalid || undefined}>
        <SelectValue placeholder="Select a model" />
      </SelectTrigger>
      <SelectContent>
        {groups.length === 0 ? (
          <p className="px-2 py-1.5 text-sm text-muted-foreground">No enabled models</p>
        ) : (
          groups.map((group) => (
            <SelectGroup key={group.provider.id}>
              <SelectLabel className="flex items-center gap-2">
                {group.provider.name}
                {group.provider.type === "anthropic-subscription" && <SubscriptionBadge />}
              </SelectLabel>
              {group.models.map((model) => (
                <SelectItem key={model.id} value={String(model.id)}>
                  <span className="font-mono text-xs">{model.model_id}</span>
                  {!model.enabled && <span className="text-muted-foreground">(disabled)</span>}
                </SelectItem>
              ))}
            </SelectGroup>
          ))
        )}
      </SelectContent>
    </Select>
  )
}
