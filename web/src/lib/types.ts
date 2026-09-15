export type ProviderType =
  | "anthropic"
  | "anthropic-compatible"
  | "anthropic-subscription"
  | "openrouter"
  | "openai"
  | "openai-compatible"

export interface Provider {
  id: number
  type: ProviderType
  name: string
  base_url: string
  has_key: boolean
  enabled: boolean
  created_at: number
}

export interface ProviderCreate {
  type: ProviderType
  name: string
  base_url?: string
  api_key?: string
  enabled?: boolean
}

export type ProviderUpdate = Partial<ProviderCreate>

export interface Model {
  id: number
  provider_id: number
  model_id: string
  display_name: string
  price_in: number
  price_out: number
  price_cache_read: number
  price_cache_write: number
  context: number
  enabled: boolean
}

export interface ModelCreate {
  model_id: string
  display_name?: string
  price_in?: number
  price_out?: number
  price_cache_read?: number
  price_cache_write?: number
  context?: number
  enabled?: boolean
}

export type ModelUpdate = Partial<Omit<ModelCreate, "model_id">>

export type Strategy = "direct" | "escalate" | "guided"

export type EscalationTarget = "next" | "top"

export interface Tier {
  model_id: number
  label: string
}

export interface EscalateSettings {
  classifier: {
    enabled: boolean
    model_id: number
    target: EscalationTarget
  }
  failure_streak: {
    enabled: boolean
    threshold: number
    target: EscalationTarget
  }
}

export type DirectorEffort = "" | "low" | "medium" | "high" | "xhigh" | "max"

export interface GuidedSettings {
  director: {
    model_id: number
    effort: DirectorEffort
    max_calls_per_turn: number
  }
  checkpoints: {
    turn_start: boolean
    failed_results: number
    steps: number
    unsure: boolean
    review_on_success: boolean
  }
  escalate_after: number
}

export interface Route {
  id: number
  name: string
  strategy: Strategy
  tiers: Tier[]
  settings: Partial<EscalateSettings & GuidedSettings>
  created_at: number
}

export interface RouteInput {
  name: string
  strategy: Strategy
  tiers: Tier[]
  settings: EscalateSettings | GuidedSettings | Record<string, never>
}

export interface GatewayKey {
  id: number
  name: string
  prefix: string
  created_at: number
  last_used_at: number
  revoked_at: number
}

export interface CreatedGatewayKey extends GatewayKey {
  key: string
}

export type RequestStatus = "ok" | "upstream_error" | "error" | "canceled"

export type LegRole = "direct" | "executor" | "escalation" | "classifier" | "director"

export type Billing = "api" | "subscription"

export interface Leg {
  seq: number
  role: LegRole
  provider: string
  model: string
  billing: Billing
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost_usd: number
  latency_ms: number
  status: string
  stop_reason: string
  note: string
}

export interface RequestRecord {
  id: number
  ts: number
  session_id: string
  agent_id: string
  route: string
  strategy: string
  client_model: string
  stream: boolean
  status: RequestStatus
  http_status: number
  error: string
  cost_usd: number
  subscription_value_usd: number
  reference_cost_usd: number
  latency_ms: number
  legs?: Leg[]
}

export interface RequestPage {
  items: RequestRecord[]
  total: number
}

export type RequestFilter = {
  route?: string
  status?: RequestStatus
  session_id?: string
  limit?: number
  offset?: number
}

export type StatsRange = "24h" | "7d" | "30d"

export interface StatsTotals {
  requests: number
  errors: number
  escalate_requests: number
  escalated_requests: number
  cost_usd: number
  subscription_value_usd: number
  reference_cost_usd: number
  savings_usd: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  api_tokens: number
  subscription_tokens: number
  classifier_cost_usd: number
  director_cost_usd: number
}

export interface StatsPoint {
  ts: number
  requests: number
  cost_usd: number
  subscription_value_usd: number
  reference_cost_usd: number
  api_tokens: number
  subscription_tokens: number
}

export interface RouteStats {
  route: string
  requests: number
  cost_usd: number
  subscription_value_usd: number
  reference_cost_usd: number
}

export interface ModelStats {
  provider: string
  model: string
  billing: Billing
  calls: number
  cost_usd: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
}

export interface Stats {
  range: StatsRange
  since: number
  bucket_ms: number
  totals: StatsTotals
  series: StatsPoint[]
  by_route: RouteStats[]
  by_model: ModelStats[]
}

export interface SubscriptionLimits {
  captured_at?: number
  headers?: Record<string, string>
}

export interface Settings {
  reference_model: string
  fallback_route: string
}

export type EvalLanguage = "go" | "python"

export type EvalDifficulty = "easy" | "medium" | "hard"

export interface EvalTask {
  id: string
  language: EvalLanguage
  difficulty: EvalDifficulty
  prompt: string
}

export interface EvalTaskList {
  tasks_dir: string
  claude_path: string
  error: string
  tasks: EvalTask[]
}

export type EvalMode = "key" | "subscription"

export type EvalRunStatus = "running" | "done" | "canceled" | "failed"

export interface EvalRun {
  id: number
  created_at: number
  finished_at: number
  status: EvalRunStatus
  mode: EvalMode
  routes: string[]
  tasks: string[]
  parallel: number
  total: number
  done: number
  error: string
}

export interface EvalRunInput {
  routes: string[]
  task_ids: string[]
  mode: EvalMode
  parallel: number
  confirm: true
}

export interface EvalSummary {
  route: string
  tasks: number
  passed: number
  pass_rate: number
  cost_usd: number
  subscription_value_usd: number
  cost_per_solved_usd: number
  api_tokens: number
  subscription_tokens: number
  escalated_requests: number
  avg_duration_ms: number
}

export interface EvalResult {
  task: string
  route: string
  passed: boolean
  duration_ms: number
  requests: number
  escalated_requests: number
  cost_usd: number
  subscription_value_usd: number
  api_tokens: number
  subscription_tokens: number
  claude_output: string
  test_output: string
  error?: string
}

export interface EvalRunDetail extends EvalRun {
  summaries: EvalSummary[]
  results: EvalResult[]
}
