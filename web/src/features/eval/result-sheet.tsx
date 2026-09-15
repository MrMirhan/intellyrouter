import { EvalResultBadge } from "@/components/badges"
import { DetailField } from "@/components/detail-field"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"
import { formatCount, formatDuration, formatTokens, formatUsd } from "@/lib/format"
import type { EvalResult } from "@/lib/types"

function OutputBlock({ title, text }: { title: string; text: string }) {
  return (
    <section className="grid gap-2">
      <h3 className="text-sm font-medium">{title}</h3>
      {text ? (
        <pre className="max-h-[28rem] overflow-auto rounded-md bg-muted p-3 font-mono text-xs break-words whitespace-pre-wrap">
          {text}
        </pre>
      ) : (
        <p className="text-sm text-muted-foreground">No output.</p>
      )}
    </section>
  )
}

export function ResultSheet({
  result,
  open,
  onOpenChange,
}: {
  result: EvalResult | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-3xl">
        {result && (
          <>
            <SheetHeader className="border-b">
              <SheetTitle className="flex flex-wrap items-center gap-2">
                <span className="font-mono">{result.task}</span>
                <span className="font-normal text-muted-foreground">on</span>
                <span className="font-mono">{result.route}</span>
                <EvalResultBadge passed={result.passed} />
              </SheetTitle>
              <SheetDescription>Output from Claude Code and from the task's tests.</SheetDescription>
            </SheetHeader>
            <div className="grid flex-1 content-start gap-6 overflow-y-auto p-4">
              <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
                <DetailField label="Duration">
                  <span className="tabular-nums">{formatDuration(result.duration_ms)}</span>
                </DetailField>
                <DetailField label="Requests">
                  <span className="tabular-nums">{formatCount(result.requests)}</span>
                </DetailField>
                <DetailField label="Escalated requests">
                  <span className="tabular-nums">{formatCount(result.escalated_requests)}</span>
                </DetailField>
                <DetailField label="API cost">
                  <span className="tabular-nums">{formatUsd(result.cost_usd)}</span>
                </DetailField>
                <DetailField label="Subscription value">
                  <span className="tabular-nums">{formatUsd(result.subscription_value_usd)}</span>
                </DetailField>
                <DetailField label="API tokens">
                  <span className="tabular-nums">{formatTokens(result.api_tokens)}</span>
                </DetailField>
                <DetailField label="Subscription tokens">
                  <span className="tabular-nums">{formatTokens(result.subscription_tokens)}</span>
                </DetailField>
              </dl>
              {result.error && <OutputBlock title="Error" text={result.error} />}
              <OutputBlock title="Claude Code output" text={result.claude_output} />
              <OutputBlock title="Test output" text={result.test_output} />
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}
