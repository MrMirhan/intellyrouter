export type ProviderType =
  | "anthropic"
  | "anthropic-compatible"
  | "anthropic-subscription"
  | "openrouter"
  | "openai"
  | "openai-compatible"

export interface Provider {
  id: number
  // "combo" is the built-in provider that holds combos.
  type: ProviderType | "combo"
  name: string
  // Models of the provider can be used without a route as "<slug>/<model_id>".
  slug: string
  base_url: string
  has_key: boolean
  enabled: boolean
  created_at: number
}

export type UpstreamProvider = Provider & { type: ProviderType }

export interface ProviderCreate {
  type: ProviderType
  name: string
  slug?: string
  base_url?: string
  api_key?: string
  enabled?: boolean
}

export type ProviderUpdate = Partial<ProviderCreate>

export type ComboStrategy = "fallback" | "round-robin" | "least-used"

export interface Combo {
  id: number
  name: string
  strategy: ComboStrategy
  enabled: boolean
  // Model row IDs in order.
  members: number[]
}

export interface ComboInput {
  name: string
  strategy: ComboStrategy
  enabled?: boolean
  members: number[]
}

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
  // Vision is true when the model accepts image content blocks.
  vision: boolean
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
  vision?: boolean
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
    claude_code: boolean
  }
  checkpoints: {
    turn_start: boolean
    failed_results: number
    steps: number
    repeats: number
    unsure: boolean
    review_on_success: boolean
  }
  escalate_after: number
  consult: boolean
}

export interface RouteAdvisor {
  model_id?: number
  off?: boolean
  effort?: DirectorEffort
  max_calls_per_turn?: number
}

export interface Route {
  id: number
  name: string
  strategy: Strategy
  tiers: Tier[]
  settings: Partial<EscalateSettings & GuidedSettings> & { advisor?: RouteAdvisor }
  created_at: number
}

export interface RouteInput {
  name: string
  strategy: Strategy
  tiers: Tier[]
  settings: (EscalateSettings | GuidedSettings | object) & { advisor?: RouteAdvisor }
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

export type LegRole = "direct" | "executor" | "escalation" | "classifier" | "director" | "advisor"

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

export interface LegModel {
  role: LegRole
  model: string
  billing: Billing
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
  models: LegModel[]
  answer_model: string
  captured: boolean
  legs?: Leg[]
}

export interface ContentBlock {
  type: string
  text?: string
  thinking?: string
  signature?: string
  id?: string
  name?: string
  input?: unknown
  tool_use_id?: string
  content?: string | ContentBlock[]
  is_error?: boolean
  source?: { type?: string; media_type?: string }
}

export interface AnthropicMessage {
  id: string
  type: "message"
  role: string
  model: string
  content: ContentBlock[]
  stop_reason: string | null
  usage: Record<string, unknown>
}

export interface MessageParam {
  role: string
  content: string | ContentBlock[]
}

export interface ToolDefinition {
  name: string
  description?: string
  input_schema?: unknown
}

export interface CapturedRequest {
  model?: string
  system?: string | ContentBlock[]
  tools?: ToolDefinition[]
  messages?: MessageParam[]
  [key: string]: unknown
}

export interface ContentLeg {
  seq: number
  role: LegRole
  model: string
  billing: Billing
  input: string
  output: AnthropicMessage | null
}

export interface RequestContent {
  message_count: number
  request: CapturedRequest
  response: AnthropicMessage | null
  legs: ContentLeg[]
}

export interface Comparison {
  actual_usd: number
  api_usd: number
  subscription_value_usd: number
  reference_model: string
  work_tokens: {
    input: number
    output: number
    cache_read: number
    cache_write: number
  }
  single_model: { model: string; cost_usd: number }[]
}

export interface SessionSummary {
  session_id: string
  first_ts: number
  last_ts: number
  requests: number
  errors: number
  agents: number
  routes: string[]
  models: { model: string; billing: Billing; calls: number }[]
  cost_usd: number
  subscription_value_usd: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  director_calls: number
  advisor_calls: number
  captured_requests: number
}

export interface SessionPage {
  items: SessionSummary[]
  total: number
}

export type SessionFilter = {
  route?: string
  limit?: number
  offset?: number
}

export interface SessionModelUsage {
  role: LegRole
  model: string
  billing: Billing
  calls: number
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  cost_usd: number
  latency_ms: number
}

export interface SessionCheckpoint {
  request_id: number
  ts: number
  role: LegRole
  model: string
  billing: Billing
  status: string
  note: string
  latency_ms: number
}

export interface SessionDetail {
  summary: SessionSummary
  by_model: SessionModelUsage[]
  checkpoints: SessionCheckpoint[]
  comparison: Comparison
  requests: RequestRecord[]
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
  advisor_cost_usd: number
  comparison: Comparison
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
  capture_content: boolean
  capture_retention_days: number
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
