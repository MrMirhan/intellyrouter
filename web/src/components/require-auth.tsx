import { Navigate, Outlet, useLocation } from "react-router"

import { PageSpinner } from "@/components/page-spinner"
import { QueryError } from "@/components/query-error"
import { ApiError } from "@/lib/api"
import { useSession } from "@/lib/queries"

export function RequireAuth() {
  const session = useSession()
  const location = useLocation()

  if (session.isPending) {
    return <PageSpinner />
  }
  if (session.isError) {
    if (session.error instanceof ApiError && session.error.status === 401) {
      return (
        <Navigate to="/login" replace state={{ from: `${location.pathname}${location.search}` }} />
      )
    }
    return (
      <div className="flex min-h-svh items-center justify-center p-6">
        <QueryError error={session.error} onRetry={() => session.refetch()} />
      </div>
    )
  }
  return <Outlet />
}
