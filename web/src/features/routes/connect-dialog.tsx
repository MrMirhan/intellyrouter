import { useState, type SubmitEvent } from "react"
import { Link } from "react-router"
import { InfoIcon, TriangleAlertIcon } from "lucide-react"

import { CopyButton } from "@/components/copy-button"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { useCreateKey, useModels, useProviders, useRoutes } from "@/lib/queries"
import { routeModelIds } from "@/lib/routes"
import type { Route } from "@/lib/types"

type EnvVar = [name: string, value: string]
type PickerRow = { model: string; label: string; description: string }
type SnippetFormat = "shell" | "settings"

const betasEnv: EnvVar[] = [["CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS", "1"]]

function exportLines(env: EnvVar[]): string {
  return env.map(([name, value]) => `export ${name}=${shellQuote(value)}`).join("\n")
}

function settingsJSON(env: EnvVar[], advisorModel: string, pickerRows: PickerRow[]): string {
  return JSON.stringify(
    {
      ...(advisorModel ? { advisorModel } : {}),
      env: Object.fromEntries(env),
      ...(pickerRows.length > 0 ? { modelPicker: { options: pickerRows } } : {}),
    },
    null,
    2,
  )
}

function shellQuote(value: string): string {
  return /^[\w.:/@%+=,-]+$/.test(value) ? value : `'${value.replaceAll("'", `'\\''`)}'`
}

function Snippet({ text, label }: { text: string; label: string }) {
  return (
    <div className="relative min-w-0">
      <pre
        aria-label={label}
        className="overflow-x-auto rounded-lg bg-muted p-3 pr-28 font-mono text-xs leading-relaxed"
      >
        <code>{text}</code>
      </pre>
      <div className="absolute top-2 right-2">
        <CopyButton value={text} />
      </div>
    </div>
  )
}

function EnvSnippet({
  env,
  format,
  label,
  advisorModel = "",
  pickerRows = [],
}: {
  env: EnvVar[]
  format: SnippetFormat
  label: string
  advisorModel?: string
  pickerRows?: PickerRow[]
}) {
  const start = advisorModel ? `\n# Start Claude Code with: claude --advisor ${advisorModel}` : ""
  return format === "shell" ? (
    <Snippet text={exportLines(env) + start} label={`${label} commands`} />
  ) : (
    <Snippet text={settingsJSON(env, advisorModel, pickerRows)} label={`${label} settings.json`} />
  )
}

function RouteSlotSelect({
  id,
  label,
  value,
  routeNames,
  onChange,
}: {
  id: string
  label: string
  value: string
  routeNames: string[]
  onChange: (name: string) => void
}) {
  return (
    <div className="grid min-w-0 gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {routeNames.map((name) => (
            <SelectItem key={name} value={name}>
              {name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}

export function ConnectDialog({
  route,
  open,
  onOpenChange,
}: {
  route: Route | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-2xl">
        {route && <ConnectBody route={route} />}
      </DialogContent>
    </Dialog>
  )
}

function ConnectBody({ route }: { route: Route }) {
  const routes = useRoutes()
  const createKey = useCreateKey()
  const [key, setKey] = useState("")
  const [keyName, setKeyName] = useState(`claude-code-${route.name}`)
  const [createdName, setCreatedName] = useState("")
  const [haikuRoute, setHaikuRoute] = useState(route.name)
  const [subagentRoute, setSubagentRoute] = useState(route.name)
  const [format, setFormat] = useState<SnippetFormat>("shell")
  const [longContext, setLongContext] = useState(true)
  const [advisor, setAdvisor] = useState(
    route.settings.advisor?.model_id ? String(route.settings.advisor.model_id) : "off",
  )
  const models = useModels()
  const providers = useProviders()

  const routeNames = [
    ...new Set([route.name, ...(routes.data ?? []).map((item) => item.name)]),
  ]
  const origin = window.location.origin
  const keyText = key.trim() || "<gateway key>"
  // Claude Code strips "[1m]" before it sends the model name, and sizes the session at 1M
  // instead of the 200K it assumes for names it does not know.
  const slot = (name: string) => (longContext ? `${name}[1m]` : name)
  const strategyByName = new Map((routes.data ?? []).map((item) => [item.name, item.strategy]))
  const pickerRows: PickerRow[] = routeNames.map((name) => ({
    model: slot(name),
    label: name,
    description: `IntellyRouter ${strategyByName.get(name) ?? route.strategy} route`,
  }))
  const claudeProviders = new Set(
    (providers.data ?? [])
      .filter((provider) => provider.type === "anthropic" || provider.type === "anthropic-subscription")
      .map((provider) => provider.id),
  )
  const routeModels = routeModelIds(route).map((id) => (models.data ?? []).find((model) => model.id === id))
  // Other providers reject the advisor server tool.
  const advisorAvailable =
    routeModels.length > 0 && routeModels.every((model) => model !== undefined && claudeProviders.has(model.provider_id))
  const providerById = new Map((providers.data ?? []).map((provider) => [provider.id, provider]))
  const advisorChoices = (models.data ?? [])
    .filter((model) => model.enabled)
    .map((model) => ({ model, provider: providerById.get(model.provider_id) }))
    .sort((a, b) => a.model.model_id.localeCompare(b.model.model_id))
  const advisorChoice = advisorChoices.find(({ model }) => String(model.id) === advisor)
  const advisorModel =
    advisorAvailable && advisorChoice && claudeProviders.has(advisorChoice.model.provider_id)
      ? advisorChoice.model.model_id
      : ""
  const advisorEnv: EnvVar[] = advisorModel ? [["CLAUDE_CODE_ENABLE_EXPERIMENTAL_ADVISOR_TOOL", "1"]] : []
  const modelEnv: EnvVar[] = [
    ["ANTHROPIC_MODEL", slot(route.name)],
    ["ANTHROPIC_DEFAULT_OPUS_MODEL", slot(route.name)],
    ["ANTHROPIC_DEFAULT_SONNET_MODEL", slot(route.name)],
    ["ANTHROPIC_DEFAULT_HAIKU_MODEL", slot(haikuRoute)],
    ["CLAUDE_CODE_SUBAGENT_MODEL", slot(subagentRoute)],
  ]
  const gatewayEnv: EnvVar[] = [
    ["ANTHROPIC_BASE_URL", origin],
    ["ANTHROPIC_AUTH_TOKEN", keyText],
    ...modelEnv,
    ["CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY", "1"],
    ...advisorEnv,
  ]
  const subscriptionEnv: EnvVar[] = [
    ["ANTHROPIC_BASE_URL", origin],
    ["ANTHROPIC_CUSTOM_HEADERS", `x-intelly-key: ${keyText}`],
    ...modelEnv,
    ...advisorEnv,
  ]

  const create = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    createKey.mutate(keyName.trim(), {
      onSuccess: (created) => {
        setKey(created.key)
        setCreatedName(created.name)
        createKey.reset()
      },
    })
  }

  return (
    <div className="grid min-w-0 gap-5">
      <DialogHeader>
        <DialogTitle>Connect Claude Code to {route.name}</DialogTitle>
        <DialogDescription>
          Set these variables in the shell where you start Claude Code, or in a Claude Code settings file.
        </DialogDescription>
      </DialogHeader>

      <div className="grid gap-3">
        <div className="grid gap-2">
          <Label htmlFor="connect-key">Gateway key</Label>
          <Input
            id="connect-key"
            value={key}
            onChange={(event) => setKey(event.target.value)}
            placeholder="ik_..."
            className="font-mono"
            autoComplete="off"
            aria-describedby="connect-key-hint"
          />
          <p id="connect-key-hint" className="text-sm text-muted-foreground">
            The gateway cannot show an existing key again. Paste a key you saved, or create a new
            one here.{" "}
            <Link to="/keys" className="underline underline-offset-4">
              Manage keys
            </Link>
          </p>
        </div>
        <form onSubmit={create} className="flex flex-wrap items-end gap-2">
          <div className="grid min-w-56 flex-1 gap-2">
            <Label htmlFor="connect-key-name">New key name</Label>
            <Input
              id="connect-key-name"
              value={keyName}
              onChange={(event) => setKeyName(event.target.value)}
              autoComplete="off"
              required
            />
          </div>
          <Button type="submit" variant="outline" disabled={!keyName.trim() || createKey.isPending}>
            {createKey.isPending && <Spinner />}
            Create a new key
          </Button>
        </form>
        {createdName && key && (
          <Alert>
            <TriangleAlertIcon />
            <AlertTitle>Key “{createdName}” created</AlertTitle>
            <AlertDescription>
              It is filled into the commands below. Copy them now: the full key is not shown again.
            </AlertDescription>
          </Alert>
        )}
      </div>

      <section className="grid gap-3 rounded-lg border p-3" aria-labelledby="slots-heading">
        <div className="grid gap-1">
          <h3 id="slots-heading" className="text-sm font-medium">
            Model slots
          </h3>
          <p className="text-sm text-muted-foreground">
            Claude Code also sends background and subagent calls under other model names, and the
            gateway answers 404 for names that are not routes. The commands map every slot to a
            route. A cheap direct route fits background calls. You can also set a fallback route
            in{" "}
            <Link to="/settings" className="underline underline-offset-4">
              Settings
            </Link>
            .
          </p>
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <RouteSlotSelect
            id="slot-haiku"
            label="Haiku slot (background calls)"
            value={haikuRoute}
            routeNames={routeNames}
            onChange={setHaikuRoute}
          />
          <RouteSlotSelect
            id="slot-subagent"
            label="Subagent slot"
            value={subagentRoute}
            routeNames={routeNames}
            onChange={setSubagentRoute}
          />
        </div>
      </section>

      <div className="flex items-start justify-between gap-4 rounded-lg border p-3">
        <div className="grid gap-1">
          <Label htmlFor="connect-long-context">1M context window</Label>
          <p className="text-sm text-muted-foreground">
            Claude Code does not know route names, so it compacts the conversation at 200K tokens.
            With this on, the model names end in <code>[1m]</code> and Claude Code compacts near
            1M. Turn it off when a model in the route has a smaller window, such as Claude Haiku
            4.5.
          </p>
        </div>
        <Switch id="connect-long-context" checked={longContext} onCheckedChange={setLongContext} />
      </div>

      <div className="flex flex-wrap items-start justify-between gap-4 rounded-lg border p-3">
        <div className="grid min-w-0 flex-1 gap-1">
          <Label htmlFor="connect-advisor">Advisor</Label>
          <p className="text-sm text-muted-foreground">
            {advisorAvailable
              ? "Claude Code gives the model an advisor tool. When the model is stuck, or before a large change, it asks the advisor, which reads the whole session. Anthropic runs the advisor, so it must be a model on an Anthropic provider. When the model that serves a step rejects this advisor, the gateway retries that step without it. Advisor calls use your plan limits."
              : "Available when every model in the route is a Claude model. Other providers do not support the advisor tool."}
          </p>
        </div>
        <Select value={advisorAvailable ? advisor : "off"} onValueChange={setAdvisor} disabled={!advisorAvailable}>
          <SelectTrigger id="connect-advisor" className="w-72">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="off">Off</SelectItem>
            {advisorChoices.map(({ model, provider }) => {
              const anthropic = claudeProviders.has(model.provider_id)
              return (
                <SelectItem key={model.id} value={String(model.id)} disabled={!anthropic}>
                  <span className="font-mono text-xs">{model.model_id}</span>
                  <span className="text-muted-foreground">- {provider?.name ?? `provider ${model.provider_id}`}</span>
                  {!anthropic && <span className="text-muted-foreground">(not an Anthropic provider)</span>}
                </SelectItem>
              )
            })}
          </SelectContent>
        </Select>
      </div>

      <div className="grid gap-2">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p id="snippet-format-label" className="text-sm font-medium">
            Setup format
          </p>
          <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            value={format}
            onValueChange={(next) => {
              if (next === "shell" || next === "settings") setFormat(next)
            }}
            aria-labelledby="snippet-format-label"
          >
            <ToggleGroupItem value="shell">Shell exports</ToggleGroupItem>
            <ToggleGroupItem value="settings">settings.json</ToggleGroupItem>
          </ToggleGroup>
        </div>
        {format === "settings" && (
          <p className="text-sm text-muted-foreground">
            Add the <code>env</code> block to <code>~/.claude/settings.json</code> to use the
            gateway in every project, or to <code>.claude/settings.local.json</code> for one
            project. When the file has other settings, merge only the <code>env</code> block. Do
            not put it in a shared <code>.claude/settings.json</code>: that file goes into git, and
            the gateway key is a secret. Restart Claude Code after you change the file.
          </p>
        )}
      </div>

      <Tabs defaultValue="gateway-key" className="min-w-0">
        <TabsList>
          <TabsTrigger value="gateway-key">Gateway key</TabsTrigger>
          <TabsTrigger value="subscription">Claude subscription</TabsTrigger>
        </TabsList>
        <TabsContent value="gateway-key" className="grid min-w-0 gap-3 pt-2">
          <p className="text-sm text-muted-foreground">
            Claude Code authenticates to the gateway with the gateway key. Use this mode when the
            route runs on API providers only.
          </p>
          <EnvSnippet env={gatewayEnv} format={format} label="Gateway key mode" advisorModel={advisorModel} />
          <p className="text-sm text-muted-foreground">
            With model discovery on, <code>/model</code> in Claude Code lists every route whose
            name contains <code>claude</code>.
          </p>
        </TabsContent>
        <TabsContent value="subscription" className="grid min-w-0 gap-3 pt-2">
          <p className="text-sm text-muted-foreground">
            Claude Code keeps your own Claude login, and the gateway key travels in a separate
            header. Use this mode when a tier runs on your Claude subscription.
          </p>
          <EnvSnippet
            env={subscriptionEnv}
            format={format}
            label="Claude subscription mode"
            advisorModel={advisorModel}
            pickerRows={pickerRows}
          />
          <p className="text-sm text-muted-foreground">
            Claude Code does not run model discovery with a Claude login. The settings.json format
            adds every route to <code>/model</code> with <code>modelPicker</code>. Claude Code
            reads <code>modelPicker</code> only from user settings (<code>~/.claude/settings.json</code>),
            not from project settings.
          </p>
          <Alert>
            <InfoIcon />
            <AlertTitle>Do not set ANTHROPIC_AUTH_TOKEN or ANTHROPIC_API_KEY</AlertTitle>
            <AlertDescription>
              Run <code>/login</code> in Claude Code instead. The gateway passes that login through
              to Claude and never stores it.
            </AlertDescription>
          </Alert>
        </TabsContent>
      </Tabs>

      <div className="grid gap-2 rounded-lg border p-3">
        <p className="text-sm">
          <span className="font-medium">Optional:</span>{" "}
          <span className="text-muted-foreground">
            {format === "shell" ? "add this line" : "add this variable to the env block"} when a
            non-Claude provider rejects beta fields in requests.
          </span>
        </p>
        <EnvSnippet env={betasEnv} format={format} label="Optional" />
      </div>
    </div>
  )
}
