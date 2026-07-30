// ---- Health ----
export interface HealthzResponse {
  ok: boolean
  started_at: string
  loaded_at: string
}

// ---- Providers ----
export interface ProviderConfig {
  id: string
  type: "openai-compatible" | "anthropic"
  base_url: string
  catalog_provider?: string
  default_capabilities?: ModelCapabilities
  model_capabilities?: Record<string, ModelCapabilities>
  models: string[]
  max_concurrency?: number
  api_key_env?: string
  api_key_source?: "env" | "literal"
  auth_type?: "bearer" | "api_key_header" | "none"
}

export interface ModelCapabilities {
  image_input?: ModelCapabilitySupport
}

export interface ProviderFormData {
  id: string
  type: "openai-compatible" | "anthropic"
  base_url: string
  catalog_provider?: string
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
  default_image_input?: "" | ModelCapabilitySupport
  model_image_capabilities?: Record<string, ModelCapabilitySupport>
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
  transformation?: TransformationSummary
}

export interface TransformationSummary {
  multimodal_route?: string
  route_reason?: string
  model_image_support?: ModelCapabilitySupport
  capability_source?: string
  catalog_match?: string
  input_images?: number
  original_provider?: string
  original_model?: string
  effective_provider?: string
  effective_model?: string
  ocr_provider?: string
  ocr_processed?: number
  ocr_cache_hits?: number
  ocr_latency_ms?: number
  ocr_min_confidence?: number
  ocr_failure_code?: string
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
  level: "error" | "warning"
  path: string
  code: string
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

export interface ProviderModelsResponse {
  ok: boolean
  models: string[]
  model_details?: Record<string, ModelCatalogMatch>
  catalog?: ModelCatalogState
  catalog_error?: string
  status?: number
  latency_ms?: number
  target?: string
  error?: string
}

export type ModelCapabilitySupport = "unknown" | "supported" | "unsupported"

export type ModelCatalogMatchStatus =
  | "not_found"
  | "ambiguous"
  | "exact_provider"
  | "exact_prefixed"
  | "exact_unique"
  | "consensus"

export interface ModelCatalogInfo {
  provider_id?: string
  id: string
  name?: string
  family?: string
  input_modalities?: string[]
  output_modalities?: string[]
  attachment?: boolean
  reasoning?: boolean
  tool_call?: boolean
  structured_output?: boolean
  temperature?: boolean
  context_limit?: number
  input_limit?: number
  output_limit?: number
  status?: string
  release_date?: string
  last_updated?: string
}

export interface ModelCatalogCandidate {
  provider_id: string
  model_id: string
  name?: string
  image_input: ModelCapabilitySupport
}

export interface ModelCatalogMatch {
  requested_model: string
  status: ModelCatalogMatchStatus
  source: "models_dev"
  image_input: ModelCapabilitySupport
  model?: ModelCatalogInfo
  candidates?: ModelCatalogCandidate[]
}

export interface ModelCatalogState {
  source_url: string
  etag?: string
  fetched_at?: string
  stale: boolean
  providers: number
  models: number
  error?: string
}

export interface ModelCatalogStatusResponse {
  catalog: ModelCatalogState
}

export interface ModelCatalogRefreshResponse {
  ok: boolean
  catalog: ModelCatalogState
}

export interface ModelCatalogLookupResponse {
  matches: Record<string, ModelCatalogMatch>
  catalog: ModelCatalogState
  catalog_error?: string
}

// ---- Multimodal fallback ----
export interface MultimodalAdminConfig {
  enabled: boolean
  strategy: "ocr_then_vision"
  provider: "builtin" | "http"
  endpoint: string
  auth_type: "none" | "bearer" | "api_key_header"
  api_key_source: "" | "env" | "literal"
  api_key_env: string
  header: string
  vision_fallback_model: string
  min_confidence: number
  min_text_chars: number
  max_images: number
}

export interface MultimodalAdminInput extends MultimodalAdminConfig {
  api_key?: string
}

export interface OCRTestResponse {
  ok: boolean
  provider?: string
  latency_ms: number
  result_count?: number
  has_text?: boolean
  confidence?: number
  engine?: string
  language?: string
  error?: string
}

export interface ConfigureResponse {
  ok: boolean
  loaded_at: string
  issues?: ConfigValidationIssue[]
}
