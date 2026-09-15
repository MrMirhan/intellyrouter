import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider } from "react-router"
import { toast } from "sonner"

import "@/index.css"
import { ThemeProvider } from "@/components/theme-provider"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { ApiError } from "@/lib/api"
import { queryKeys } from "@/lib/queries"
import { router } from "@/router"

function redirectOnUnauthorized(error: Error): boolean {
  if (!(error instanceof ApiError) || error.status !== 401) return false
  const { pathname, search } = router.state.location
  if (pathname !== "/login") {
    queryClient.clear()
    void router.navigate("/login", { replace: true, state: { from: `${pathname}${search}` } })
  }
  return true
}

const queryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (error, query) => {
      if (query.queryKey[0] !== queryKeys.session[0]) redirectOnUnauthorized(error)
    },
  }),
  mutationCache: new MutationCache({
    onError: (error, _variables, _onMutateResult, mutation) => {
      if (redirectOnUnauthorized(error) || mutation.meta?.toastError === false) return
      toast.error(error.message)
    },
  }),
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      retry: (failureCount, error) =>
        !(error instanceof ApiError && error.status < 500) && failureCount < 2,
    },
  },
})

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <RouterProvider router={router} />
          <Toaster position="top-center" />
        </TooltipProvider>
      </QueryClientProvider>
    </ThemeProvider>
  </StrictMode>,
)
