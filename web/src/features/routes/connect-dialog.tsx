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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCreateKey, useRoutes } from "@/lib/queries"
import type { Route } from "@/lib/types"

const betasLine = "export CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1"

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

  const routeNames = [
    ...new Set([route.name, ...(routes.data ?? []).map((item) => item.name)]),
  ]
  const origin = shellQuote(window.location.origin)
  const keyText = key.trim() || "<gateway key>"
  const modelLines = [
    `export ANTHROPIC_MODEL=${shellQuote(route.name)}`,
    `export ANTHROPIC_DEFAULT_OPUS_MODEL=${shellQuote(route.name)}`,
    `export ANTHROPIC_DEFAULT_SONNET_MODEL=${shellQuote(route.name)}`,
    `export ANTHROPIC_DEFAULT_HAIKU_MODEL=${shellQuote(haikuRoute)}`,
    `export CLAUDE_CODE_SUBAGENT_MODEL=${shellQuote(subagentRoute)}`,
  ]
  const gatewaySnippet = [
    `export ANTHROPIC_BASE_URL=${origin}`,
    `export ANTHROPIC_AUTH_TOKEN=${shellQuote(keyText)}`,
    ...modelLines,
    "export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1",
  ].join("\n")
  const subscriptionSnippet = [
    `export ANTHROPIC_BASE_URL=${origin}`,
    `export ANTHROPIC_CUSTOM_HEADERS=${shellQuote(`x-intelly-key: ${keyText}`)}`,
    ...modelLines,
  ].join("\n")

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
          Run these commands in the shell where you start Claude Code.
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
          <Snippet text={gatewaySnippet} label="Gateway key mode commands" />
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
          <Snippet text={subscriptionSnippet} label="Claude subscription mode commands" />
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
            add this line when a non-Claude provider rejects beta fields in requests.
          </span>
        </p>
        <Snippet text={betasLine} label="Optional command" />
      </div>
    </div>
  )
}
