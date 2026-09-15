import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type {
  ModelCreate,
  ModelUpdate,
  ProviderUpdate,
  RequestFilter,
  RouteInput,
  SessionFilter,
  StatsRange,
} from "@/lib/types"

declare module "@tanstack/react-query" {
  interface Register {
    mutationMeta: { toastError?: boolean }
  }
}

export const queryKeys = {
  session: ["session"],
  providers: ["providers"],
  models: ["models"],
  allModels: ["models", "all"],
  providerModels: (providerId: number) => ["models", "provider", providerId],
  routes: ["routes"],
  keys: ["keys"],
  requestList: (filter: RequestFilter) => ["requests", "list", filter],
  request: (id: number) => ["requests", "detail", id],
  requestContent: (id: number, tail: number) => ["requests", "content", id, tail],
  sessionList: (filter: SessionFilter) => ["sessions", "list", filter],
  sessionDetail: (id: string) => ["sessions", "detail", id],
  stats: (range: StatsRange) => ["stats", range],
  subscriptionLimits: ["subscription-limits"],
  settings: ["settings"],
  evals: ["eval"],
  evalTasks: ["eval", "tasks"],
  evalRuns: ["eval", "runs"],
  evalRun: (id: number) => ["eval", "run", id],
} as const

export function useSession() {
  return useQuery({
    queryKey: queryKeys.session,
    queryFn: api.getSession,
    retry: false,
    staleTime: 60_000,
  })
}

export function useLogin() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.login,
    meta: { toastError: false },
    onSuccess: () => {
      queryClient.clear()
      queryClient.setQueryData(queryKeys.session, true)
    },
  })
}

export function useLogout() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.logout,
    onSettled: () => queryClient.clear(),
  })
}

export function useProviders() {
  return useQuery({ queryKey: queryKeys.providers, queryFn: api.listProviders })
}

export function useCreateProvider() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.createProvider,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.providers }),
  })
}

export function useUpdateProvider() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: ProviderUpdate }) =>
      api.updateProvider(id, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.providers }),
  })
}

export function useDeleteProvider() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.deleteProvider,
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: queryKeys.providers }),
        queryClient.invalidateQueries({ queryKey: queryKeys.models }),
      ]),
  })
}

export function useSyncModels() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.syncModels,
    meta: { toastError: false },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.models }),
  })
}

export function useModels(providerId?: number) {
  return useQuery({
    queryKey: providerId === undefined ? queryKeys.allModels : queryKeys.providerModels(providerId),
    queryFn: () => api.listModels(providerId),
  })
}

export function useCreateModel() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ providerId, input }: { providerId: number; input: ModelCreate }) =>
      api.createModel(providerId, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.models }),
  })
}

export function useUpdateModel() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: ModelUpdate }) => api.updateModel(id, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.models }),
  })
}

export function useRoutes() {
  return useQuery({ queryKey: queryKeys.routes, queryFn: api.listRoutes })
}

export function useCreateRoute() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.createRoute,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.routes }),
  })
}

export function useUpdateRoute() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: Partial<RouteInput> }) =>
      api.updateRoute(id, input),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.routes }),
  })
}

export function useDeleteRoute() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.deleteRoute,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.routes }),
  })
}

export function useKeys() {
  return useQuery({ queryKey: queryKeys.keys, queryFn: api.listKeys })
}

export function useCreateKey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.createKey,
    gcTime: 0,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.keys }),
  })
}

export function useRevokeKey() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.revokeKey,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.keys }),
  })
}

export function useRequests(filter: RequestFilter) {
  return useQuery({
    queryKey: queryKeys.requestList(filter),
    queryFn: () => api.listRequests(filter),
    placeholderData: keepPreviousData,
    refetchInterval: 15_000,
  })
}

export function useRequest(id: number) {
  return useQuery({
    queryKey: queryKeys.request(id),
    queryFn: () => api.getRequest(id),
    enabled: Number.isInteger(id) && id > 0,
  })
}

export function useRequestContent(id: number, tail: number, enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.requestContent(id, tail),
    queryFn: () => api.getRequestContent(id, tail),
    enabled,
    placeholderData: keepPreviousData,
  })
}

export function useSessions(filter: SessionFilter) {
  return useQuery({
    queryKey: queryKeys.sessionList(filter),
    queryFn: () => api.listSessions(filter),
    placeholderData: keepPreviousData,
    refetchInterval: 15_000,
  })
}

export function useSessionDetail(id: string) {
  return useQuery({
    queryKey: queryKeys.sessionDetail(id),
    queryFn: () => api.getSessionDetail(id),
    enabled: id !== "",
    refetchInterval: 30_000,
  })
}

export function useStats(range: StatsRange) {
  return useQuery({
    queryKey: queryKeys.stats(range),
    queryFn: () => api.getStats(range),
    placeholderData: keepPreviousData,
    refetchInterval: 30_000,
  })
}

export function useSubscriptionLimits() {
  return useQuery({
    queryKey: queryKeys.subscriptionLimits,
    queryFn: api.getSubscriptionLimits,
    refetchInterval: 60_000,
  })
}

export function useSettings() {
  return useQuery({ queryKey: queryKeys.settings, queryFn: api.getSettings })
}

export function useUpdateSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.updateSettings,
    onSuccess: (settings) => queryClient.setQueryData(queryKeys.settings, settings),
  })
}

export function useEvalTasks() {
  return useQuery({ queryKey: queryKeys.evalTasks, queryFn: api.listEvalTasks })
}

export function useEvalRuns() {
  return useQuery({
    queryKey: queryKeys.evalRuns,
    queryFn: api.listEvalRuns,
    refetchInterval: (query) =>
      query.state.data?.some((run) => run.status === "running") ? 3000 : false,
  })
}

export function useEvalRun(id: number) {
  return useQuery({
    queryKey: queryKeys.evalRun(id),
    queryFn: () => api.getEvalRun(id),
    enabled: Number.isInteger(id) && id > 0,
    refetchInterval: (query) => (query.state.data?.status === "running" ? 3000 : false),
  })
}

export function useCreateEvalRun() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.createEvalRun,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.evalRuns }),
  })
}

export function useCancelEvalRun() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.cancelEvalRun,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: queryKeys.evals }),
  })
}

export function useDeleteEvalRun() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: api.deleteEvalRun,
    onSuccess: (_, id) => {
      queryClient.removeQueries({ queryKey: queryKeys.evalRun(id) })
      return queryClient.invalidateQueries({ queryKey: queryKeys.evalRuns })
    },
  })
}
