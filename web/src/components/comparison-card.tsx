import { useId, useState } from "react"

import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { formatPercent, formatTokens, formatUsd } from "@/lib/format"
import type { Comparison } from "@/lib/types"
import { cn } from "@/lib/utils"

function differenceText(difference: number, baseline: number): string {
  if (difference === 0) {
    return "Same cost as one model"
  }
  const direction = difference > 0 ? "less" : "more"
  if (baseline <= 0) {
    return `Routing costs ${direction}`
  }
  return `${formatPercent(Math.abs(difference) / baseline)} ${direction} than one model`
}

export function ComparisonCard({
  comparison,
  description,
}: {
  comparison: Comparison
  description: string
}) {
  const selectId = useId()
  const [baseline, setBaseline] = useState<string | null>(null)
  const options = comparison.single_model
  const selected =
    options.find((option) => option.model === (baseline ?? comparison.reference_model)) ??
    options[0]
  const tokens = comparison.work_tokens

  return (
    <Card>
      <CardHeader>
        <CardTitle>Routing vs one model</CardTitle>
        <CardDescription>{description}</CardDescription>
        {options.length > 0 && (
          <CardAction className="grid gap-1.5">
            <Label htmlFor={selectId} className="text-xs text-muted-foreground">
              Compare with
            </Label>
            <Select value={selected.model} onValueChange={setBaseline}>
              <SelectTrigger id={selectId} className="w-56 font-mono text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {options.map((option) => (
                  <SelectItem key={option.model} value={option.model} className="font-mono text-xs">
                    {option.model}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className="grid gap-4">
        {selected ? (
          <dl className="grid gap-4 sm:grid-cols-3">
            <div className="grid content-start gap-1">
              <dt className="text-xs text-muted-foreground">One model</dt>
              <dd className="text-2xl font-semibold tabular-nums">{formatUsd(selected.cost_usd)}</dd>
              <dd className="truncate font-mono text-xs text-muted-foreground" title={selected.model}>
                {selected.model}
              </dd>
            </div>
            <div className="grid content-start gap-1">
              <dt className="text-xs text-muted-foreground">Routing</dt>
              <dd className="text-2xl font-semibold tabular-nums">
                {formatUsd(comparison.actual_usd)}
              </dd>
              <dd className="text-xs text-muted-foreground tabular-nums">
                API {formatUsd(comparison.api_usd)} · Subscription{" "}
                {formatUsd(comparison.subscription_value_usd)}
              </dd>
            </div>
            <div className="grid content-start gap-1">
              <dt className="text-xs text-muted-foreground">Difference</dt>
              <dd
                className={cn(
                  "text-2xl font-semibold tabular-nums",
                  selected.cost_usd > comparison.actual_usd && "text-success",
                )}
              >
                {formatUsd(Math.abs(selected.cost_usd - comparison.actual_usd))}
              </dd>
              <dd className="text-xs text-muted-foreground tabular-nums">
                {differenceText(selected.cost_usd - comparison.actual_usd, selected.cost_usd)}
              </dd>
            </div>
          </dl>
        ) : (
          <p className="text-sm text-muted-foreground">No model has prices to compare with.</p>
        )}
        <div className="grid gap-1 text-xs text-muted-foreground">
          <p className="tabular-nums">
            Work priced: input {formatTokens(tokens.input)} · output {formatTokens(tokens.output)} ·
            cache read {formatTokens(tokens.cache_read)} · cache write{" "}
            {formatTokens(tokens.cache_write)} tokens.
          </p>
          <p>Estimate: a single model would use a different number of tokens and turns.</p>
        </div>
      </CardContent>
    </Card>
  )
}
