import type {
  CreatedGatewayKey,
  EvalRun,
  EvalRunDetail,
  EvalRunInput,
  EvalTaskList,
  GatewayKey,
  Model,
  ModelCreate,
  ModelUpdate,
  Provider,
  ProviderCreate,
  ProviderUpdate,
  RequestFilter,
  RequestPage,
  RequestRecord,
  Route,
  RouteInput,
  Settings,
  Stats,
  StatsRange,
  SubscriptionLimits,
} from "@/lib/types"

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
  }
}

type Method = "GET" | "POST" | "PATCH" | "PUT" | "DELETE"

async function errorMessage(res: Response): Promise<string> {
  const fallback = `${res.status} ${res.statusText}`.trim()
  try {
    const data: unknown = await res.json()
    if (data && typeof data === "object" && "error" in data && typeof data.error === "string") {
      return data.error
    }
    return fallback
  } catch {
    return fallback
  }
}

async function call<T>(method: Method, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, credentials: "same-origin" }
  if (method === "POST" || method === "PATCH" || method === "PUT") {
    init.headers = { "Content-Type": "application/json" }
    init.body = JSON.stringify(body ?? {})
  }
  const res = await fetch(`/api/admin${path}`, init)
  if (!res.ok) {
    throw new ApiError(res.status, await errorMessage(res))
  }
  if (res.status === 204) {
    return undefined as T
  }
  return (await res.json()) as T
}

function queryString(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value))
    }
  }
  const text = search.toString()
  return text ? `?${text}` : ""
}

export const api = {
  login: (token: string) => call<void>("POST", "/login", { token }),
  logout: () => call<void>("POST", "/logout"),
  getSession: async () => {
    await call<void>("GET", "/session")
    return true
  },

  listProviders: () => call<Provider[]>("GET", "/providers"),
  createProvider: (input: ProviderCreate) => call<Provider>("POST", "/providers", input),
  updateProvider: (id: number, input: ProviderUpdate) =>
    call<Provider>("PATCH", `/providers/${id}`, input),
  deleteProvider: (id: number) => call<void>("DELETE", `/providers/${id}`),
  syncModels: (id: number) => call<Model[]>("POST", `/providers/${id}/sync-models`),
  createModel: (providerId: number, input: ModelCreate) =>
    call<Model>("POST", `/providers/${providerId}/models`, input),

  listModels: (providerId?: number) =>
    call<Model[]>("GET", `/models${queryString({ provider_id: providerId })}`),
  updateModel: (id: number, input: ModelUpdate) => call<Model>("PATCH", `/models/${id}`, input),

  listRoutes: () => call<Route[]>("GET", "/routes"),
  createRoute: (input: RouteInput) => call<Route>("POST", "/routes", input),
  updateRoute: (id: number, input: Partial<RouteInput>) =>
    call<Route>("PATCH", `/routes/${id}`, input),
  deleteRoute: (id: number) => call<void>("DELETE", `/routes/${id}`),

  listKeys: () => call<GatewayKey[]>("GET", "/keys"),
  createKey: (name: string) => call<CreatedGatewayKey>("POST", "/keys", { name }),
  revokeKey: (id: number) => call<void>("DELETE", `/keys/${id}`),

  listRequests: (filter: RequestFilter) =>
    call<RequestPage>("GET", `/requests${queryString(filter)}`),
  getRequest: (id: number) => call<RequestRecord>("GET", `/requests/${id}`),

  getStats: (range: StatsRange) => call<Stats>("GET", `/stats${queryString({ range })}`),
  getSubscriptionLimits: () => call<SubscriptionLimits>("GET", "/subscription/limits"),

  getSettings: () => call<Settings>("GET", "/settings"),
  updateSettings: (input: Partial<Settings>) => call<Settings>("PUT", "/settings", input),

  listEvalTasks: () => call<EvalTaskList>("GET", "/eval/tasks"),
  listEvalRuns: () => call<EvalRun[]>("GET", "/eval/runs"),
  createEvalRun: (input: EvalRunInput) => call<EvalRun>("POST", "/eval/runs", input),
  getEvalRun: (id: number) => call<EvalRunDetail>("GET", `/eval/runs/${id}`),
  cancelEvalRun: (id: number) => call<void>("POST", `/eval/runs/${id}/cancel`),
  deleteEvalRun: (id: number) => call<void>("DELETE", `/eval/runs/${id}`),
}
