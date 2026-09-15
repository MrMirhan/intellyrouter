import { PageHeader } from "@/components/page-header"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { EvalRunsTable } from "@/features/eval/eval-runs-table"
import { StartEvalForm } from "@/features/eval/start-eval-form"

export function EvalPage() {
  return (
    <>
      <PageHeader
        title="Eval"
        description="Compare routes on the same coding tasks: pass rate, cost, and escalations."
      />
      <Card size="sm">
        <CardHeader>
          <CardTitle>How a run works</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-2 text-sm text-muted-foreground">
          <p>
            A run starts headless Claude Code on each selected task, through the gateway, once for
            every selected route. When Claude Code finishes, the gateway runs the task's tests and
            reads the cost from the request ledger. This way you can compare, for example, a cheap
            model alone, Opus 5 alone, and an escalate route.
          </p>
          <p>
            The tasks are one-shot: nobody tells the model &quot;I am not satisfied&quot;, so the
            classifier rarely triggers. An escalate route mostly shows the quality of its base model,
            plus escalations after failed tool calls.
          </p>
        </CardContent>
      </Card>
      <StartEvalForm />
      <EvalRunsTable />
    </>
  )
}
