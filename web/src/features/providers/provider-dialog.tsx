import { useState, type SubmitEvent } from "react"
import { ArrowLeftIcon, InfoIcon } from "lucide-react"
import { toast } from "sonner"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import {
  providerPresets,
  providerTypeList,
  providerTypes,
  type ProviderPreset,
} from "@/lib/providers"
import { useCreateProvider, useUpdateProvider } from "@/lib/queries"
import type { Provider, ProviderType } from "@/lib/types"

export function ProviderDialog({
  open,
  onOpenChange,
  provider,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  provider: Provider | null
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        {provider ? (
          <ProviderForm provider={provider} onDone={() => onOpenChange(false)} />
        ) : (
          <AddProvider onDone={() => onOpenChange(false)} />
        )}
      </DialogContent>
    </Dialog>
  )
}

function AddProvider({ onDone }: { onDone: () => void }) {
  const [preset, setPreset] = useState<ProviderPreset | null>(null)

  if (preset) {
    return <ProviderForm preset={preset} onBack={() => setPreset(null)} onDone={onDone} />
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Add provider</DialogTitle>
        <DialogDescription>Pick a preset to fill in the details, or start from Custom.</DialogDescription>
      </DialogHeader>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
        {providerPresets.map((item) => (
          <button
            key={item.id}
            type="button"
            onClick={() => setPreset(item)}
            className="grid gap-0.5 rounded-lg border p-3 text-left transition-colors hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          >
            <span className="text-sm font-medium">{item.label}</span>
            <span className="text-xs text-muted-foreground">
              {item.id === "custom" ? "Any provider type" : providerTypes[item.type].label}
            </span>
          </button>
        ))}
      </div>
    </>
  )
}

function ProviderForm({
  provider,
  preset,
  onBack,
  onDone,
}: {
  provider?: Provider
  preset?: ProviderPreset
  onBack?: () => void
  onDone: () => void
}) {
  const createProvider = useCreateProvider()
  const updateProvider = useUpdateProvider()
  const [type, setType] = useState<ProviderType>(provider?.type ?? preset?.type ?? "anthropic-compatible")
  const [name, setName] = useState(provider?.name ?? preset?.name ?? "")
  const [baseUrl, setBaseUrl] = useState(provider?.base_url ?? preset?.baseUrl ?? "")
  const [apiKey, setApiKey] = useState("")
  const [enabled, setEnabled] = useState(provider?.enabled ?? true)

  const info = providerTypes[type]
  const showBaseUrl = info.baseUrl !== "none"
  const showKey = info.key !== "none"
  const keepsKey = Boolean(provider?.has_key)
  const keyRequired = info.key === "required" && !keepsKey
  const pending = createProvider.isPending || updateProvider.isPending

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    const key = showKey && apiKey.trim() ? { api_key: apiKey.trim() } : {}
    if (provider) {
      updateProvider.mutate(
        {
          id: provider.id,
          input: { type, name: name.trim(), base_url: showBaseUrl ? baseUrl.trim() : "", enabled, ...key },
        },
        {
          onSuccess: (saved) => {
            toast.success(`Saved ${saved.name}`)
            onDone()
          },
        },
      )
      return
    }
    createProvider.mutate(
      {
        type,
        name: name.trim(),
        enabled,
        ...(showBaseUrl && baseUrl.trim() ? { base_url: baseUrl.trim() } : {}),
        ...key,
      },
      {
        onSuccess: (saved) => {
          toast.success(`Added ${saved.name}`, {
            description: "Sync its models next, then enable the ones you want to route to.",
          })
          onDone()
        },
      },
    )
  }

  return (
    <form onSubmit={submit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>{provider ? `Edit ${provider.name}` : `Add ${preset?.label ?? "provider"}`}</DialogTitle>
        <DialogDescription>{info.description}</DialogDescription>
      </DialogHeader>

      {preset?.hint && <p className="text-sm text-muted-foreground">{preset.hint}</p>}

      <div className="grid gap-2">
        <Label htmlFor="provider-type">Type</Label>
        <Select
          value={type}
          onValueChange={(value) => {
            const match = providerTypeList.find((item) => item === value)
            if (match) setType(match)
          }}
        >
          <SelectTrigger id="provider-type" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {providerTypeList.map((item) => (
              <SelectItem key={item} value={item}>
                {providerTypes[item].label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="grid gap-2">
        <Label htmlFor="provider-name">Name</Label>
        <Input
          id="provider-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          autoComplete="off"
          required
        />
      </div>

      {type === "anthropic-subscription" && (
        <Alert>
          <InfoIcon />
          <AlertDescription>
            Claude Code keeps its own <code>/login</code>. On routes that use this provider, the
            gateway passes that login through to Claude and never stores the token. No API key or
            base URL is needed.
          </AlertDescription>
        </Alert>
      )}

      {showBaseUrl && (
        <div className="grid gap-2">
          <Label htmlFor="provider-base-url">
            Base URL{info.baseUrl === "optional" && <span className="text-muted-foreground"> (optional)</span>}
          </Label>
          <Input
            id="provider-base-url"
            type="url"
            value={baseUrl}
            onChange={(event) => setBaseUrl(event.target.value)}
            placeholder={info.defaultBaseUrl ?? (info.baseUrl === "optional" ? "Leave blank for the default" : "https://")}
            className="font-mono"
            autoComplete="off"
            required={info.baseUrl === "required"}
          />
        </div>
      )}

      {showKey && (
        <div className="grid gap-2">
          <Label htmlFor="provider-api-key">
            API key{!keyRequired && <span className="text-muted-foreground"> (optional)</span>}
          </Label>
          <Input
            id="provider-api-key"
            type="password"
            value={apiKey}
            onChange={(event) => setApiKey(event.target.value)}
            placeholder={keepsKey ? "Leave blank to keep the current key" : undefined}
            autoComplete="off"
            required={keyRequired}
          />
          {provider && (
            <p className="text-sm text-muted-foreground">
              {keepsKey
                ? "The stored key is never shown. A new value replaces it."
                : "No key is stored for this provider."}
            </p>
          )}
        </div>
      )}

      <div className="flex items-center gap-3">
        <Switch id="provider-enabled" checked={enabled} onCheckedChange={setEnabled} />
        <Label htmlFor="provider-enabled">Enabled</Label>
      </div>

      <DialogFooter className="sm:justify-between">
        {onBack ? (
          <Button type="button" variant="ghost" onClick={onBack}>
            <ArrowLeftIcon />
            Presets
          </Button>
        ) : (
          <span />
        )}
        <div className="flex flex-col-reverse gap-2 sm:flex-row">
          <Button type="button" variant="outline" onClick={onDone}>
            Cancel
          </Button>
          <Button type="submit" disabled={pending}>
            {pending && <Spinner />}
            {provider ? "Save" : "Add provider"}
          </Button>
        </div>
      </DialogFooter>
    </form>
  )
}
