import { useMemo, useState, type SubmitEvent } from "react"
import { TriangleAlertIcon } from "lucide-react"
import { toast } from "sonner"

import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
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
import { useModels, useRoutes, useSettings, useUpdateSettings } from "@/lib/queries"

const defaultReferenceModel = "claude-fable-5-1"
// Route names cannot contain whitespace, so this value never matches a route.
const noneValue = " none"

function ReferenceModelCard({ current }: { current: string }) {
  const models = useModels()
  const update = useUpdateSettings()
  const [draft, setDraft] = useState<string | null>(null)

  const value = draft ?? current
  const modelIds = useMemo(
    () => [...new Set(models.data?.map((model) => model.model_id))].sort(),
    [models.data],
  )

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    update.mutate(
      { reference_model: value.trim() },
      {
        onSuccess: () => {
          setDraft(null)
          toast.success("Reference model saved")
        },
      },
    )
  }

  return (
    <Card>
      <form onSubmit={submit} className="grid gap-6">
        <CardHeader>
          <CardTitle>Savings reference model</CardTitle>
          <CardDescription>
            Estimated savings compare what the API-billed work cost with what the same work would
            cost on a stronger model. Escalate routes compare against their top tier. Direct routes
            have no higher tier, so the gateway prices their work on this model.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid max-w-md gap-2">
          <Label htmlFor="reference-model">Reference model ID</Label>
          <Input
            id="reference-model"
            value={value}
            onChange={(event) => setDraft(event.target.value)}
            list="reference-model-options"
            className="font-mono"
            autoComplete="off"
            aria-describedby="reference-model-hint"
            required
          />
          <datalist id="reference-model-options">
            {modelIds.map((id) => (
              <option key={id} value={id} />
            ))}
          </datalist>
          <p id="reference-model-hint" className="text-sm text-muted-foreground">
            The default is <code className="text-xs">{defaultReferenceModel}</code>. Changes apply
            to new requests only.
          </p>
        </CardContent>
        <CardFooter className="gap-2">
          <Button
            type="submit"
            disabled={!value.trim() || value.trim() === current || update.isPending}
          >
            {update.isPending && <Spinner />}
            Save
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={value === defaultReferenceModel}
            onClick={() => setDraft(defaultReferenceModel)}
          >
            Use default
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}

function FallbackRouteCard({ current }: { current: string }) {
  const routes = useRoutes()
  const update = useUpdateSettings()
  const [draft, setDraft] = useState<string | null>(null)

  const value = draft ?? current

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    update.mutate(
      { fallback_route: value },
      {
        onSuccess: () => {
          setDraft(null)
          toast.success(value ? `Fallback route set to ${value}` : "Fallback route removed")
        },
      },
    )
  }

  return (
    <Card>
      <form onSubmit={submit} className="grid gap-6">
        <CardHeader>
          <CardTitle>Fallback route</CardTitle>
          <CardDescription>
            Requests with a model name that is not a route (for example Claude Code background
            calls) use this route. With None they fail with 404.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid max-w-md gap-2">
          <Label htmlFor="fallback-route">Route</Label>
          <Select
            value={value || noneValue}
            onValueChange={(next) => setDraft(next === noneValue ? "" : next)}
          >
            <SelectTrigger id="fallback-route" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={noneValue}>None</SelectItem>
              {routes.data?.map((route) => (
                <SelectItem key={route.id} value={route.name}>
                  {route.name}
                </SelectItem>
              ))}
              {current && !routes.data?.some((route) => route.name === current) && (
                <SelectItem value={current}>{current}</SelectItem>
              )}
            </SelectContent>
          </Select>
        </CardContent>
        <CardFooter>
          <Button type="submit" disabled={value === current || update.isPending}>
            {update.isPending && <Spinner />}
            Save
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}

function ContentCaptureCard({
  enabled,
  retentionDays,
}: {
  enabled: boolean
  retentionDays: number
}) {
  const update = useUpdateSettings()
  const [enabledDraft, setEnabledDraft] = useState<boolean | null>(null)
  const [daysDraft, setDaysDraft] = useState<string | null>(null)

  const capture = enabledDraft ?? enabled
  const daysText = daysDraft ?? String(retentionDays)
  const days = Number(daysText)
  const validDays = daysText.trim() !== "" && Number.isInteger(days) && days >= 1 && days <= 365
  const changed = capture !== enabled || days !== retentionDays

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    update.mutate(
      { capture_content: capture, capture_retention_days: days },
      {
        onSuccess: () => {
          setEnabledDraft(null)
          setDaysDraft(null)
          toast.success(capture ? "Content capture is on" : "Content capture is off")
        },
      },
    )
  }

  return (
    <Card>
      <form onSubmit={submit} className="grid gap-6">
        <CardHeader>
          <CardTitle>Content capture</CardTitle>
          <CardDescription>
            Keep the prompts and responses of each request. You can then read them on the request
            page and export them.
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <Alert>
            <TriangleAlertIcon />
            <AlertDescription>
              Content capture stores prompts, code, tool output and responses in the gateway data
              directory. Keep it off for sensitive work.
            </AlertDescription>
          </Alert>
          <div className="flex max-w-md items-start justify-between gap-4">
            <div className="grid gap-1">
              <Label htmlFor="capture-content">Capture content</Label>
              <p id="capture-content-hint" className="text-sm text-muted-foreground">
                Applies to new requests only.
              </p>
            </div>
            <Switch
              id="capture-content"
              checked={capture}
              onCheckedChange={setEnabledDraft}
              aria-describedby="capture-content-hint"
            />
          </div>
          <div className="grid max-w-md gap-2">
            <Label htmlFor="capture-retention">Keep content for (days)</Label>
            <Input
              id="capture-retention"
              type="number"
              inputMode="numeric"
              min={1}
              max={365}
              step={1}
              value={daysText}
              onChange={(event) => setDaysDraft(event.target.value)}
              className="w-32"
              aria-describedby="capture-retention-hint"
              aria-invalid={!validDays}
              required
            />
            <p id="capture-retention-hint" className="text-sm text-muted-foreground">
              From 1 to 365 days. The gateway deletes older content.
            </p>
          </div>
        </CardContent>
        <CardFooter>
          <Button type="submit" disabled={!changed || !validDays || update.isPending}>
            {update.isPending && <Spinner />}
            Save
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}

export function SettingsPage() {
  const settings = useSettings()

  return (
    <>
      <PageHeader title="Settings" description="Gateway-wide options." />
      {settings.isError ? (
        <QueryError error={settings.error} onRetry={() => settings.refetch()} />
      ) : settings.isPending ? (
        <>
          <Skeleton className="h-64 w-full rounded-xl" />
          <Skeleton className="h-48 w-full rounded-xl" />
          <Skeleton className="h-64 w-full rounded-xl" />
        </>
      ) : (
        <>
          <ReferenceModelCard current={settings.data.reference_model} />
          <FallbackRouteCard current={settings.data.fallback_route} />
          <ContentCaptureCard
            enabled={settings.data.capture_content}
            retentionDays={settings.data.capture_retention_days}
          />
        </>
      )}
    </>
  )
}
