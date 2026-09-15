import { useState, type SubmitEvent } from "react"
import { toast } from "sonner"

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
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { useCreateModel, useUpdateModel } from "@/lib/queries"
import type { Model } from "@/lib/types"

const priceFields = [
  { key: "price_in", label: "Input" },
  { key: "price_out", label: "Output" },
  { key: "price_cache_read", label: "Cache read" },
  { key: "price_cache_write", label: "Cache write" },
] as const

type PriceKey = (typeof priceFields)[number]["key"]

function toNumber(value: string): number {
  return value.trim() === "" ? 0 : Number(value)
}

export function ModelDialog({
  providerId,
  model,
  open,
  onOpenChange,
}: {
  providerId: number
  model: Model | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <ModelForm providerId={providerId} model={model} onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

function ModelForm({
  providerId,
  model,
  onDone,
}: {
  providerId: number
  model: Model | null
  onDone: () => void
}) {
  const createModel = useCreateModel()
  const updateModel = useUpdateModel()
  const [modelId, setModelId] = useState(model?.model_id ?? "")
  const [displayName, setDisplayName] = useState(model?.display_name ?? "")
  const [context, setContext] = useState(model?.context ? String(model.context) : "")
  const [prices, setPrices] = useState<Record<PriceKey, string>>({
    price_in: model ? String(model.price_in) : "",
    price_out: model ? String(model.price_out) : "",
    price_cache_read: model ? String(model.price_cache_read) : "",
    price_cache_write: model ? String(model.price_cache_write) : "",
  })
  const [enabled, setEnabled] = useState(true)
  const pending = createModel.isPending || updateModel.isPending

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    const fields = {
      display_name: displayName.trim(),
      context: toNumber(context),
      price_in: toNumber(prices.price_in),
      price_out: toNumber(prices.price_out),
      price_cache_read: toNumber(prices.price_cache_read),
      price_cache_write: toNumber(prices.price_cache_write),
    }
    if (model) {
      updateModel.mutate(
        { id: model.id, input: fields },
        {
          onSuccess: (saved) => {
            toast.success(`Saved ${saved.model_id}`)
            onDone()
          },
        },
      )
      return
    }
    createModel.mutate(
      { providerId, input: { model_id: modelId.trim(), enabled, ...fields } },
      {
        onSuccess: (saved) => {
          toast.success(`Added ${saved.model_id}`)
          onDone()
        },
      },
    )
  }

  return (
    <form onSubmit={submit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>{model ? `Edit ${model.model_id}` : "Add model"}</DialogTitle>
        <DialogDescription>
          {model
            ? "Prices drive the cost and savings figures."
            : "Add a model by hand when the provider has no model list."}
        </DialogDescription>
      </DialogHeader>

      {!model && (
        <div className="grid gap-2">
          <Label htmlFor="model-id">Model ID</Label>
          <Input
            id="model-id"
            value={modelId}
            onChange={(event) => setModelId(event.target.value)}
            placeholder="deepseek-v4-flash"
            className="font-mono"
            autoComplete="off"
            required
          />
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="grid gap-2">
          <Label htmlFor="model-display-name">Display name</Label>
          <Input
            id="model-display-name"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            autoComplete="off"
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="model-context">Context window (tokens)</Label>
          <Input
            id="model-context"
            type="number"
            min={0}
            step={1}
            value={context}
            onChange={(event) => setContext(event.target.value)}
          />
        </div>
      </div>

      <fieldset className="grid gap-3">
        <legend className="mb-2 text-sm font-medium">
          Prices <span className="font-normal text-muted-foreground">(USD per 1M tokens)</span>
        </legend>
        <div className="grid grid-cols-2 gap-4">
          {priceFields.map((field) => (
            <div key={field.key} className="grid gap-2">
              <Label htmlFor={`model-${field.key}`}>{field.label}</Label>
              <Input
                id={`model-${field.key}`}
                type="number"
                min={0}
                step="any"
                value={prices[field.key]}
                onChange={(event) =>
                  setPrices((current) => ({ ...current, [field.key]: event.target.value }))
                }
                placeholder="0"
              />
            </div>
          ))}
        </div>
      </fieldset>

      {!model && (
        <div className="flex items-center gap-3">
          <Switch id="model-enabled" checked={enabled} onCheckedChange={setEnabled} />
          <Label htmlFor="model-enabled">Enabled</Label>
        </div>
      )}

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {pending && <Spinner />}
          {model ? "Save" : "Add model"}
        </Button>
      </DialogFooter>
    </form>
  )
}
