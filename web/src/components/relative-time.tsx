import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { formatAbsolute, formatRelative } from "@/lib/format"

export function RelativeTime({ ms, empty = "Never" }: { ms: number; empty?: string }) {
  if (ms === 0) {
    return <span className="text-muted-foreground">{empty}</span>
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <time
          dateTime={new Date(ms).toISOString()}
          tabIndex={0}
          className="whitespace-nowrap underline decoration-muted-foreground/40 decoration-dotted underline-offset-4"
        >
          {formatRelative(ms)}
        </time>
      </TooltipTrigger>
      <TooltipContent>{formatAbsolute(ms)}</TooltipContent>
    </Tooltip>
  )
}
