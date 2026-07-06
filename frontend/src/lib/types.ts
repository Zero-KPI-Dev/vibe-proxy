// ---- Health ----
export interface HealthzResponse {
  ok: boolean
  loaded_at: string
}

// ---- Providers ----
export interface ProviderConfig {
  id: string
  type: "openai-compatible" | "anthropic"
  base_url: string
  models: string[]
  max_concurrency?: number
  api_key_env?: string
  api_key_source?: "env" | "literal"
  auth_type?: "bearer" | "api_key_header" | "none"
}

export interface ProviderFormData {
  id: string
  type: "openai-compatible" | "anthropic"
  base_url: string
  api_key_env?: string
  api_key?: string
  api_key_source?: "env" | "literal"
  auth_type: string
  header?: string
  models: string[]
  alias?: string
  alias_model?: string
  default_model?: string
  max_concurrency?: number
}

export interface SnapshotResponse {
  loaded_at: string
  providers: ProviderConfig[]
  model_resolver: ModelResolverConfig
}

// ---- Model Resolver / Aliases ----
export interface ModelResolverConfig {
  default_model: string
  allow_raw: boolean
  aliases: Record<string, string>
}

export interface AliasEntry {
  alias: string
  target: string
}

// ---- Client Keys ----
export interface ClientKey {
  name: string
  key_prefix: string
  enabled: boolean
  allowed_models: string[]
  rpm: number
  created_at?: string
}

export interface ClientKeyCreateResponse {
  key: ClientKey
  raw_key: string
}

// ---- Telemetry / Requests ----
export interface Usage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cache_read_tokens?: number
  cache_write_tokens?: number
  cache_hit_ratio?: number
}

export interface RequestEvent {
  request_id: string
  client_name: string
  virtual_model: string
  upstream_model: string
  channel_id: string
  protocol_in: string
  protocol_out: string
  started_at: string
  first_token_at?: string
  completed_at?: string
  ttft_ms?: number
  tpot_ms?: number
  tps?: number
  status_code: number
  error_code?: string
  usage: Usage
}

export interface RecentRequestsResponse {
  active: RequestEvent[]
  recent: RequestEvent[]
}

// ---- Metrics ----
export interface MetricsSummary {
  total_requests: number
  active_providers: number
  total_models: number
  today_tokens: {
    prompt: number
    completion: number
    total: number
  }
}

export interface MetricPoint {
  timestamp: string
  requests: number
  errors: number
  ttft_p50: number
  ttft_p95: number
  ttft_p99: number
  tpot_p50: number
  tpot_p95: number
  tokens_prompt: number
  tokens_completion: number
}

export interface MetricsHistoryResponse {
  range: string
  points: MetricPoint[]
}

export interface ProviderHealth {
  id: string
  type: string
  base_url: string
  last_tested?: string
  healthy: boolean
  latency_ms?: number
}

export interface ProviderHealthResponse {
  providers: ProviderHealth[]
}

// ---- Config ----
export interface ConfigValidationIssue {
  severity: "error" | "warning"
  field: string
  message: string
}

export interface ValidateConfigResponse {
  valid: boolean
  issues: ConfigValidationIssue[]
}

export interface RawConfigResponse {
  yaml: string
}

export interface ProviderTestResponse {
  ok: boolean
  provider: string
  status?: number
  latency_ms?: number
  target?: string
  error?: string
}

export interface ConfigureResponse {
  ok: boolean
  loaded_at: string
  issues?: ConfigValidationIssue[]
}
