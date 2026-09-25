import { useState, type ReactNode } from "react"
import { BrainIcon, ChevronRightIcon, ImageIcon, WrenchIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { formatCount } from "@/lib/format"
import type { ContentBlock, MessageParam } from "@/lib/types"
import { cn } from "@/lib/utils"

const truncateAt = 2000

const roleLabels: Record<string, string> = {
  user: "User",
  assistant: "Assistant",
}

export function Expandable({ title, children }: { title: ReactNode; children: ReactNode }) {
  return (
    <Collapsible className="rounded-md border">
      <CollapsibleTrigger className="group/expandable flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm font-medium outline-none hover:bg-muted/50 focus-visible:ring-3 focus-visible:ring-ring/50">
        <ChevronRightIcon
          aria-hidden
          className="size-4 shrink-0 text-muted-foreground transition-transform group-data-[state=open]/expandable:rotate-90"
        />
        {title}
      </CollapsibleTrigger>
      <CollapsibleContent className="grid gap-2 border-t px-3 py-2">{children}</CollapsibleContent>
    </Collapsible>
  )
}

export function TruncatedText({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false)
  const long = text.length > truncateAt

  return (
    <div className="grid gap-1">
      <pre className="font-mono text-xs whitespace-pre-wrap break-words">
        {long && !expanded ? `${text.slice(0, truncateAt)}…` : text}
      </pre>
      {long && (
        <Button
          type="button"
          variant="link"
          size="sm"
          className="h-auto justify-self-start px-0"
          aria-expanded={expanded}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? "Show less" : `Show all (${formatCount(text.length)} characters)`}
        </Button>
      )}
    </div>
  )
}

function PlainText({ text, className }: { text: string; className?: string }) {
  return <p className={cn("text-sm whitespace-pre-wrap break-words", className)}>{text}</p>
}

function ToolResult({ content }: { content: string | ContentBlock[] }) {
  if (typeof content === "string") {
    return <TruncatedText text={content} />
  }
  return content.map((block, index) =>
    block.type === "text" ? (
      <TruncatedText key={index} text={block.text ?? ""} />
    ) : (
      <Block key={index} block={block} />
    ),
  )
}

function Block({ block }: { block: ContentBlock }) {
  switch (block.type) {
    case "text":
      return <PlainText text={block.text ?? ""} />
    case "thinking":
      return (
        <Expandable
          title={
            <>
              <BrainIcon aria-hidden className="size-4 text-muted-foreground" />
              Thinking
            </>
          }
        >
          <PlainText text={block.thinking ?? ""} className="text-muted-foreground" />
        </Expandable>
      )
    case "redacted_thinking":
      return (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <BrainIcon aria-hidden className="size-4" />
          The provider redacted this thinking.
        </p>
      )
    case "tool_use":
      return (
        <div className="grid gap-2 rounded-md border px-3 py-2">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <WrenchIcon aria-hidden className="size-4 text-muted-foreground" />
            <span className="text-muted-foreground">Tool call</span>
            <code className="font-mono text-xs font-medium">{block.name}</code>
          </div>
          <TruncatedText text={JSON.stringify(block.input ?? {}, null, 2)} />
        </div>
      )
    case "tool_result":
      return (
        <Expandable
          title={
            <>
              <WrenchIcon aria-hidden className="size-4 text-muted-foreground" />
              Tool result
              {block.is_error && <Badge variant="destructive">Error</Badge>}
            </>
          }
        >
          <ToolResult content={block.content ?? ""} />
        </Expandable>
      )
    case "image":
      return (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <ImageIcon aria-hidden className="size-4" />
          Image{block.source?.media_type && ` (${block.source.media_type})`}
        </p>
      )
    default:
      return (
        <Expandable title={<code className="font-mono text-xs">{block.type}</code>}>
          <TruncatedText text={JSON.stringify(block, null, 2)} />
        </Expandable>
      )
  }
}

export function ContentBlocks({ content }: { content: string | ContentBlock[] | null | undefined }) {
  if (typeof content === "string") {
    return <PlainText text={content} />
  }
  if (!Array.isArray(content) || content.length === 0) {
    return <p className="text-sm text-muted-foreground">No content.</p>
  }
  return (
    <div className="grid gap-2">
      {content.map((block, index) => (
        <Block key={index} block={block} />
      ))}
    </div>
  )
}

export function MessageList({ messages }: { messages: MessageParam[] }) {
  return (
    <ol className="grid gap-3">
      {messages.map((message, index) => (
        <li
          key={index}
          className={cn(
            "grid gap-2 rounded-lg border p-3",
            message.role === "assistant" && "bg-muted/40",
          )}
        >
          <span className="text-xs font-medium text-muted-foreground">
            {roleLabels[message.role] ?? message.role}
          </span>
          <ContentBlocks content={message.content} />
        </li>
      ))}
    </ol>
  )
}
