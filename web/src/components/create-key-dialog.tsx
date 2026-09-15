import { useState, type SubmitEvent } from "react"
import { TriangleAlertIcon } from "lucide-react"

import { CopyButton } from "@/components/copy-button"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Spinner } from "@/components/ui/spinner"
import { useCreateKey } from "@/lib/queries"
import type { CreatedGatewayKey } from "@/lib/types"

export function CreateKeyDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <CreateKeyForm onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

function CreateKeyForm({ onDone }: { onDone: () => void }) {
  const createKey = useCreateKey()
  const [name, setName] = useState("")
  const [created, setCreated] = useState<CreatedGatewayKey | null>(null)

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    createKey.mutate(name.trim(), {
      onSuccess: (key) => {
        setCreated(key)
        createKey.reset()
      },
    })
  }

  if (created) {
    return (
      <>
        <DialogHeader>
          <DialogTitle>Key created</DialogTitle>
          <DialogDescription>
            Use this key as <code>ANTHROPIC_AUTH_TOKEN</code> or in the <code>x-intelly-key</code>{" "}
            header.
          </DialogDescription>
        </DialogHeader>
        <Alert>
          <TriangleAlertIcon />
          <AlertTitle>Copy this key now</AlertTitle>
          <AlertDescription>
            The gateway shows the full key only once. You cannot read it again later.
          </AlertDescription>
        </Alert>
        <div className="flex items-center gap-2">
          <Input
            readOnly
            value={created.key}
            aria-label={`Gateway key ${created.name}`}
            className="font-mono"
            autoComplete="off"
            onFocus={(event) => event.currentTarget.select()}
          />
          <CopyButton value={created.key} />
        </div>
        <DialogFooter>
          <Button onClick={onDone}>Done</Button>
        </DialogFooter>
      </>
    )
  }

  return (
    <form onSubmit={submit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>Create gateway key</DialogTitle>
        <DialogDescription>
          Claude Code uses a gateway key to authenticate to IntellyRouter.
        </DialogDescription>
      </DialogHeader>
      <div className="grid gap-2">
        <Label htmlFor="key-name">Name</Label>
        <Input
          id="key-name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="laptop"
          autoComplete="off"
          required
        />
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          Cancel
        </Button>
        <Button type="submit" disabled={!name.trim() || createKey.isPending}>
          {createKey.isPending && <Spinner />}
          Create key
        </Button>
      </DialogFooter>
    </form>
  )
}
