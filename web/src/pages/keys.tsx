import { useState } from "react"
import { KeyRoundIcon, PlusIcon } from "lucide-react"
import { toast } from "sonner"

import { ConfirmDialog } from "@/components/confirm-dialog"
import { CreateKeyDialog } from "@/components/create-key-dialog"
import { PageHeader } from "@/components/page-header"
import { QueryError } from "@/components/query-error"
import { RelativeTime } from "@/components/relative-time"
import { TableSkeleton } from "@/components/table-skeleton"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useKeys, useRevokeKey } from "@/lib/queries"

export function KeysPage() {
  const keys = useKeys()
  const revokeKey = useRevokeKey()
  const [createOpen, setCreateOpen] = useState(false)

  const createButton = (
    <Button onClick={() => setCreateOpen(true)}>
      <PlusIcon />
      Create key
    </Button>
  )

  return (
    <>
      <PageHeader
        title="Gateway keys"
        description="Keys that Claude Code uses to authenticate to the gateway. The full key is shown once, when you create it."
        actions={createButton}
      />
      <CreateKeyDialog open={createOpen} onOpenChange={setCreateOpen} />

      {keys.isError ? (
        <QueryError error={keys.error} onRetry={() => keys.refetch()} />
      ) : keys.data?.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <KeyRoundIcon />
            </EmptyMedia>
            <EmptyTitle>No gateway keys yet</EmptyTitle>
            <EmptyDescription>Create a key, then use it to connect Claude Code.</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>{createButton}</EmptyContent>
        </Empty>
      ) : (
        <Card className="py-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Prefix</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {keys.isPending ? (
                <TableSkeleton columns={6} />
              ) : (
                keys.data.map((key) => (
                  <TableRow key={key.id}>
                    <TableCell className="font-medium">{key.name}</TableCell>
                    <TableCell>
                      <code className="text-xs">{key.prefix}…</code>
                    </TableCell>
                    <TableCell>
                      <RelativeTime ms={key.created_at} />
                    </TableCell>
                    <TableCell>
                      <RelativeTime ms={key.last_used_at} />
                    </TableCell>
                    <TableCell>
                      {key.revoked_at > 0 ? (
                        <span className="flex items-center gap-2">
                          <Badge variant="secondary">Revoked</Badge>
                          <RelativeTime ms={key.revoked_at} />
                        </span>
                      ) : (
                        <Badge variant="outline" className="border-success/40 text-success">
                          Active
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      {key.revoked_at === 0 && (
                        <ConfirmDialog
                          trigger={
                            <Button variant="ghost" size="sm">
                              Revoke
                            </Button>
                          }
                          title={`Revoke "${key.name}"?`}
                          description="Claude Code sessions that use this key stop working at once. This cannot be undone."
                          confirmLabel="Revoke key"
                          pending={revokeKey.isPending}
                          onConfirm={(close) =>
                            revokeKey.mutate(key.id, {
                              onSuccess: () => {
                                close()
                                toast.success(`Revoked "${key.name}"`)
                              },
                            })
                          }
                        />
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </Card>
      )}
    </>
  )
}
