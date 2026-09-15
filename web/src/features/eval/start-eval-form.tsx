import { useState, type SubmitEvent } from "react"
import { Link, useNavigate } from "react-router"
import { ChevronRightIcon, InfoIcon, PlayIcon, TriangleAlertIcon } from "lucide-react"
import { toast } from "sonner"

import { StrategyBadge } from "@/components/badges"
import { QueryError } from "@/components/query-error"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
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
import { evalDifficultyLabels, evalLanguageLabels, evalModes } from "@/lib/eval"
import { formatCount, formatPlural } from "@/lib/format"
import {
  useCreateEvalRun,
  useEvalRuns,
  useEvalTasks,
  useModels,
  useProviders,
  useRoutes,
} from "@/lib/queries"
import { routeModelIds } from "@/lib/routes"
import type { EvalTask } from "@/lib/types"
import { cn } from "@/lib/utils"

const allValue = "all"
const parallelOptions = ["1", "2", "3", "4"]

function withValue(set: ReadonlySet<string>, value: string, present: boolean): ReadonlySet<string> {
  const next = new Set(set)
  if (present) {
    next.add(value)
  } else {
    next.delete(value)
  }
  return next
}

function TaskRow({
  task,
  included,
  onIncludedChange,
}: {
  task: EvalTask
  included: boolean
  onIncludedChange: (included: boolean) => void
}) {
  const [open, setOpen] = useState(false)
  const checkboxId = `eval-task-${task.id}`

  return (
    <li>
      <Collapsible open={open} onOpenChange={setOpen} className="grid gap-2 px-3 py-2">
        <div className="flex flex-wrap items-center gap-2">
          <Checkbox
            id={checkboxId}
            checked={included}
            onCheckedChange={(checked) => onIncludedChange(checked === true)}
          />
          <Label htmlFor={checkboxId} className="font-mono text-xs">
            {task.id}
          </Label>
          <Badge variant="outline">{evalLanguageLabels[task.language] ?? task.language}</Badge>
          <Badge variant="secondary">{evalDifficultyLabels[task.difficulty] ?? task.difficulty}</Badge>
          <CollapsibleTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="ml-auto"
              aria-label={`${open ? "Hide" : "Show"} prompt for ${task.id}`}
            >
              <ChevronRightIcon className={cn("transition-transform", open && "rotate-90")} />
              {open ? "Hide prompt" : "Show prompt"}
            </Button>
          </CollapsibleTrigger>
        </div>
        <CollapsibleContent>
          <pre className="max-h-60 overflow-auto rounded-md bg-muted p-3 font-mono text-xs whitespace-pre-wrap">
            {task.prompt}
          </pre>
        </CollapsibleContent>
      </Collapsible>
    </li>
  )
}

export function StartEvalForm() {
  const navigate = useNavigate()
  const tasks = useEvalTasks()
  const routes = useRoutes()
  const runs = useEvalRuns()
  const models = useModels()
  const providers = useProviders()
  const createRun = useCreateEvalRun()

  const [excluded, setExcluded] = useState<ReadonlySet<string>>(new Set())
  const [language, setLanguage] = useState(allValue)
  const [difficulty, setDifficulty] = useState(allValue)
  const [chosenRoutes, setChosenRoutes] = useState<ReadonlySet<string>>(new Set())
  const [mode, setMode] = useState(evalModes[0].value)
  const [parallel, setParallel] = useState(parallelOptions[0])
  const [understood, setUnderstood] = useState(false)

  const taskList = tasks.data?.tasks ?? []
  const visibleTasks = taskList.filter(
    (task) =>
      (language === allValue || task.language === language) &&
      (difficulty === allValue || task.difficulty === difficulty),
  )
  const selectedTasks = taskList.filter((task) => !excluded.has(task.id))
  const hiddenSelected =
    selectedTasks.length - visibleTasks.filter((task) => !excluded.has(task.id)).length
  const selectedRoutes = (routes.data ?? []).filter((route) => chosenRoutes.has(route.name))
  const subscriptionProviders = new Set(
    (providers.data ?? [])
      .filter((provider) => provider.type === "anthropic-subscription")
      .map((provider) => provider.id),
  )
  const subscriptionModels = new Set(
    (models.data ?? [])
      .filter((model) => subscriptionProviders.has(model.provider_id))
      .map((model) => model.id),
  )
  // Gateway key mode starts Claude Code without a Claude login, so subscription models fail.
  const loginRoutes = selectedRoutes.filter((route) =>
    routeModelIds(route).some((id) => subscriptionModels.has(id)),
  )
  const needsLogin = mode === "key" && loginRoutes.length > 0
  const runCount = selectedTasks.length * selectedRoutes.length
  const activeRun = runs.data?.find((run) => run.status === "running")

  const tasksError = tasks.data?.error ?? ""
  const missingClaude = tasks.isSuccess && tasks.data.claude_path === ""
  const blocked = tasksError !== "" || missingClaude
  const canStart =
    tasks.isSuccess &&
    !blocked &&
    !activeRun &&
    selectedTasks.length > 0 &&
    selectedRoutes.length > 0 &&
    !needsLogin &&
    understood &&
    !createRun.isPending

  const setVisibleTasks = (include: boolean) =>
    setExcluded((current) => {
      const next = new Set(current)
      for (const task of visibleTasks) {
        if (include) {
          next.delete(task.id)
        } else {
          next.add(task.id)
        }
      }
      return next
    })

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    createRun.mutate(
      {
        routes: selectedRoutes.map((route) => route.name),
        task_ids: selectedTasks.map((task) => task.id),
        mode,
        parallel: Number(parallel),
        confirm: true,
      },
      {
        onSuccess: (run) => {
          toast.success(`Eval run #${run.id} started`)
          navigate(`/eval/${run.id}`)
        },
      },
    )
  }

  return (
    <Card>
      <form onSubmit={submit} className="grid gap-6">
        <CardHeader>
          <CardTitle>Start a run</CardTitle>
          <CardDescription>Pick tasks and routes, check the plan, then start.</CardDescription>
        </CardHeader>

        <CardContent className="grid gap-6">
          {tasks.isError ? (
            <QueryError error={tasks.error} onRetry={() => tasks.refetch()} />
          ) : (
            <>
              {blocked && (
                <Alert variant="destructive">
                  <TriangleAlertIcon />
                  <AlertTitle>Evals cannot start</AlertTitle>
                  <AlertDescription>
                    {tasksError && (
                      <>
                        <p>{tasksError}</p>
                        <p>
                          Start the gateway with <code>-eval-tasks &lt;dir&gt;</code> pointing at a
                          folder of eval tasks.
                        </p>
                      </>
                    )}
                    {missingClaude && (
                      <p>
                        The gateway did not find the Claude Code binary. Install Claude Code, then
                        restart the gateway.
                      </p>
                    )}
                  </AlertDescription>
                </Alert>
              )}

              {activeRun && (
                <Alert>
                  <InfoIcon />
                  <AlertTitle>Run #{activeRun.id} is in progress</AlertTitle>
                  <AlertDescription>
                    <p>
                      Only one run can be active at a time.{" "}
                      <Link to={`/eval/${activeRun.id}`} className="underline underline-offset-4">
                        Open the run
                      </Link>{" "}
                      to follow or cancel it.
                    </p>
                  </AlertDescription>
                </Alert>
              )}

              {tasks.isSuccess && !blocked && (
                <p className="text-xs break-all text-muted-foreground">
                  Tasks from <code>{tasks.data.tasks_dir}</code> · Claude Code at{" "}
                  <code>{tasks.data.claude_path}</code>
                </p>
              )}

              <section className="grid gap-3" aria-labelledby="eval-tasks-heading">
                <div className="flex flex-wrap items-end gap-3">
                  <div className="mr-auto grid gap-1">
                    <h3 id="eval-tasks-heading" className="font-medium">
                      Tasks
                    </h3>
                    <p className="text-sm text-muted-foreground tabular-nums">
                      {formatCount(selectedTasks.length)} of {formatPlural(taskList.length, "task")}{" "}
                      selected
                      {hiddenSelected > 0 && ` (${formatCount(hiddenSelected)} hidden by filters)`}
                    </p>
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor="eval-language">Language</Label>
                    <Select value={language} onValueChange={setLanguage}>
                      <SelectTrigger id="eval-language" className="w-36">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value={allValue}>All languages</SelectItem>
                        {Object.entries(evalLanguageLabels).map(([value, label]) => (
                          <SelectItem key={value} value={value}>
                            {label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid gap-1.5">
                    <Label htmlFor="eval-difficulty">Difficulty</Label>
                    <Select value={difficulty} onValueChange={setDifficulty}>
                      <SelectTrigger id="eval-difficulty" className="w-36">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value={allValue}>All difficulties</SelectItem>
                        {Object.entries(evalDifficultyLabels).map(([value, label]) => (
                          <SelectItem key={value} value={value}>
                            {label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={visibleTasks.length === 0}
                    onClick={() => setVisibleTasks(true)}
                  >
                    Select all
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={visibleTasks.length === 0}
                    onClick={() => setVisibleTasks(false)}
                  >
                    Select none
                  </Button>
                </div>

                {tasks.isPending ? (
                  <div className="grid gap-2">
                    {Array.from({ length: 4 }, (_, index) => (
                      <Skeleton key={index} className="h-10 w-full" />
                    ))}
                  </div>
                ) : visibleTasks.length === 0 ? (
                  <p className="rounded-lg border px-3 py-6 text-center text-sm text-muted-foreground">
                    {taskList.length === 0 ? "No tasks found." : "No tasks match these filters."}
                  </p>
                ) : (
                  <ul className="max-h-96 divide-y overflow-y-auto rounded-lg border">
                    {visibleTasks.map((task) => (
                      <TaskRow
                        key={task.id}
                        task={task}
                        included={!excluded.has(task.id)}
                        onIncludedChange={(included) =>
                          setExcluded((current) => withValue(current, task.id, !included))
                        }
                      />
                    ))}
                  </ul>
                )}
              </section>

              <section className="grid gap-3" aria-labelledby="eval-routes-heading">
                <div className="grid gap-1">
                  <h3 id="eval-routes-heading" className="font-medium">
                    Routes
                  </h3>
                  <p className="text-sm text-muted-foreground">
                    Every selected route runs every selected task.
                  </p>
                </div>
                {routes.isError ? (
                  <QueryError error={routes.error} onRetry={() => routes.refetch()} />
                ) : routes.isPending ? (
                  <Skeleton className="h-16 w-full" />
                ) : routes.data.length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    No routes yet.{" "}
                    <Link to="/routes" className="underline underline-offset-4">
                      Create a route
                    </Link>{" "}
                    first.
                  </p>
                ) : (
                  <ul className="grid gap-2 sm:grid-cols-2">
                    {routes.data.map((route) => {
                      const checkboxId = `eval-route-${route.id}`
                      return (
                        <li key={route.id} className="flex items-center gap-3 rounded-lg border px-3 py-2">
                          <Checkbox
                            id={checkboxId}
                            checked={chosenRoutes.has(route.name)}
                            onCheckedChange={(checked) =>
                              setChosenRoutes((current) =>
                                withValue(current, route.name, checked === true),
                              )
                            }
                          />
                          <Label htmlFor={checkboxId} className="font-mono text-xs">
                            {route.name}
                          </Label>
                          <span className="ml-auto">
                            <StrategyBadge strategy={route.strategy} />
                          </span>
                        </li>
                      )
                    })}
                  </ul>
                )}
              </section>

              <div className="grid gap-4 sm:grid-cols-[1fr_10rem]">
                <div className="grid gap-2">
                  <Label htmlFor="eval-mode">Mode</Label>
                  <Select
                    value={mode}
                    onValueChange={(value) => {
                      const match = evalModes.find((item) => item.value === value)
                      if (match) setMode(match.value)
                    }}
                  >
                    <SelectTrigger id="eval-mode" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {evalModes.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          {item.option}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="eval-parallel">Parallel runs</Label>
                  <Select value={parallel} onValueChange={setParallel}>
                    <SelectTrigger id="eval-parallel" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {parallelOptions.map((value) => (
                        <SelectItem key={value} value={value}>
                          {value}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              {needsLogin && (
                <Alert variant="destructive">
                  <TriangleAlertIcon />
                  <AlertTitle>Gateway key mode cannot run these routes</AlertTitle>
                  <AlertDescription>
                    <p>
                      {loginRoutes.map((route) => route.name).join(", ")} use Claude subscription
                      models. Gateway key mode starts Claude Code without your Claude login, so
                      those calls fail.
                    </p>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="mt-2"
                      onClick={() => setMode("subscription")}
                    >
                      Use Claude subscription mode
                    </Button>
                  </AlertDescription>
                </Alert>
              )}

              <p className="text-sm font-medium tabular-nums" aria-live="polite">
                {formatPlural(selectedTasks.length, "task")} ×{" "}
                {formatPlural(selectedRoutes.length, "route")} ={" "}
                {formatPlural(runCount, "Claude Code run")}
              </p>

              <Alert>
                <TriangleAlertIcon />
                <AlertTitle>Before you start</AlertTitle>
                <AlertDescription>
                  <ul className="list-disc pl-4">
                    <li>
                      Models edit files and run go and python3 commands on this machine, in
                      temporary copies of the tasks.
                    </li>
                    <li>API calls cost money.</li>
                    <li>
                      Subscription mode and Claude tiers use your plan limits. Opus runs can take a
                      large share of the 5-hour limit.
                    </li>
                  </ul>
                </AlertDescription>
              </Alert>

              <div className="flex items-start gap-3">
                <Checkbox
                  id="eval-understood"
                  checked={understood}
                  onCheckedChange={(checked) => setUnderstood(checked === true)}
                />
                <Label htmlFor="eval-understood" className="leading-snug">
                  I understand that this run executes commands on this machine, costs money, and
                  uses plan limits.
                </Label>
              </div>
            </>
          )}
        </CardContent>

        <CardFooter>
          <Button type="submit" disabled={!canStart}>
            {createRun.isPending ? <Spinner /> : <PlayIcon />}
            Start run
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}
