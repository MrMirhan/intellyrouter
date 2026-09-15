import { useState, type SubmitEvent } from "react"
import { Navigate, useLocation } from "react-router"
import { WaypointsIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Spinner } from "@/components/ui/spinner"
import { ApiError } from "@/lib/api"
import { useLogin, useSession } from "@/lib/queries"

function redirectTarget(state: unknown): string {
  if (state && typeof state === "object" && "from" in state && typeof state.from === "string") {
    return state.from
  }
  return "/"
}

export function LoginPage() {
  const session = useSession()
  const login = useLogin()
  const location = useLocation()
  const [token, setToken] = useState("")

  if (session.isSuccess) {
    return <Navigate to={redirectTarget(location.state)} replace />
  }

  const submit = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault()
    login.mutate(token.trim())
  }

  const error =
    login.error instanceof ApiError && login.error.status === 401
      ? "This token is not valid."
      : login.error?.message

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/40 p-6">
      <Card className="w-full max-w-sm">
        <form onSubmit={submit} className="grid gap-6">
          <CardHeader>
            <div className="mb-2 flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
              <WaypointsIcon className="size-5" />
            </div>
            <CardTitle className="text-xl">IntellyRouter</CardTitle>
            <CardDescription>Sign in to the gateway dashboard.</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-2">
            <Label htmlFor="admin-token">Admin token</Label>
            <Input
              id="admin-token"
              type="password"
              value={token}
              onChange={(event) => setToken(event.target.value)}
              placeholder="ia_..."
              autoComplete="current-password"
              aria-invalid={error ? true : undefined}
              aria-describedby="admin-token-hint"
              autoFocus
              required
            />
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <p id="admin-token-hint" className="text-sm text-muted-foreground">
              The gateway prints the admin token in its console on first start. Run{" "}
              <code className="rounded bg-muted px-1 py-0.5 text-xs">
                intellyrouter -reset-admin-token
              </code>{" "}
              to issue a new one.
            </p>
          </CardContent>
          <CardFooter>
            <Button type="submit" className="w-full" disabled={!token.trim() || login.isPending}>
              {login.isPending && <Spinner />}
              Sign in
            </Button>
          </CardFooter>
        </form>
      </Card>
    </div>
  )
}
