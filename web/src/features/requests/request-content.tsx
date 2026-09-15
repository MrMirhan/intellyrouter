import { Link } from "react-router"
import { FileTextIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Spinner } from "@/components/ui/spinner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ContentBlocks, Expandable, MessageList } from "@/features/requests/content-blocks"
import { formatCount, formatPlural } from "@/lib/format"
import type { RequestContent } from "@/lib/types"

export function ContentHint() {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border px-4 py-3 text-sm">
      <FileTextIcon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
      <p className="text-muted-foreground">
        No content was kept for this request. Turn on content capture to keep prompts and responses.
      </p>
      <Button variant="outline" size="sm" className="ml-auto" asChild>
        <Link to="/settings">Settings</Link>
      </Button>
    </div>
  )
}

export function RequestContentView({
  content,
  loadingAll,
  onLoadAll,
}: {
  content: RequestContent
  loadingAll: boolean
  onLoadAll: () => void
}) {
  const { request, response } = content
  const messages = request.messages ?? []
  const tools = request.tools ?? []
  const hidden = content.message_count - messages.length

  return (
    <Tabs defaultValue="response">
      <TabsList>
        <TabsTrigger value="response">Response</TabsTrigger>
        <TabsTrigger value="request">Request</TabsTrigger>
      </TabsList>
      <TabsContent value="response">
        <Card>
          <CardContent className="grid gap-3">
            {response ? (
              <>
                <p className="text-xs text-muted-foreground">
                  <code className="font-mono">{response.model}</code>
                  {response.stop_reason && (
                    <>
                      {" "}
                      · Stop reason <code className="font-mono">{response.stop_reason}</code>
                    </>
                  )}
                </p>
                <ContentBlocks content={response.content} />
              </>
            ) : (
              <p className="text-sm text-muted-foreground">
                No final response was kept. The request possibly failed or was canceled.
              </p>
            )}
          </CardContent>
        </Card>
      </TabsContent>
      <TabsContent value="request">
        <Card>
          <CardContent className="grid gap-4">
            {request.system && request.system.length > 0 && (
              <Expandable title="System prompt">
                <ContentBlocks content={request.system} />
              </Expandable>
            )}
            {tools.length > 0 && (
              <Expandable title={`Tools (${formatCount(tools.length)})`}>
                <ul className="flex flex-wrap gap-1.5">
                  {tools.map((tool, index) => (
                    <li key={index}>
                      <Badge variant="outline" className="font-mono">
                        {tool.name}
                      </Badge>
                    </li>
                  ))}
                </ul>
              </Expandable>
            )}
            <div className="flex flex-wrap items-center justify-between gap-2">
              <p className="text-sm text-muted-foreground">
                {hidden > 0
                  ? `Showing the last ${formatCount(messages.length)} of ${formatCount(content.message_count)} messages.`
                  : formatPlural(messages.length, "message")}
              </p>
              {hidden > 0 && (
                <Button variant="outline" size="sm" disabled={loadingAll} onClick={onLoadAll}>
                  {loadingAll && <Spinner />}
                  Load all {formatCount(content.message_count)} messages
                </Button>
              )}
            </div>
            <MessageList messages={messages} />
          </CardContent>
        </Card>
      </TabsContent>
    </Tabs>
  )
}
