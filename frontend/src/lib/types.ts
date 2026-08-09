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
  server: {
    listen: string
    admin_listen: string
    effective_listen: string
    effective_admin_listen: string
    restart_required: boolean
  }
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
  cache_metrics_reported?: boolean
  cache_hit_ratio?: number
}

export type CaptureMode = "off" | "metadata" | "structured" | "raw"

export type CaptureStatus =
  | "not_captured"
  | "captured"
  | "redacted"
  | "truncated"
  | "expired"
  | "dropped"
  | "missing"

export interface RequestEvent {
  request_id: string
  trace_id: string
  span_id: string
  parent_span_id?: string
  session_id?: string
  session_name?: string
  session_kind?: string
  session_path?: string
  parent_request_id?: string
  principal_type?: "client_key" | "internal" | "unknown" | string
  principal_name?: string
  client_key_prefix?: string
  client_name: string
  agent_id: string
  agent_name?: string
  agent_version?: string
  agent_source: string
  agent_confidence: string
  project_id?: string
  http_method?: string
  http_path?: string
  virtual_model: string
  initial_provider?: string
  initial_model?: string
  upstream_model: string
  channel_id: string
  protocol_in: string
  protocol_out: string
  started_at: string
  first_token_at?: string
  completed_at?: string
  duration_ms?: number
  ttft_ms?: number
  tpot_ms?: number
  tps?: number
  status_code: number
  error_code?: string
  finish_reason?: string
  upstream_request_id?: string
  retry_count?: number
  usage: Usage
  request_shape: RequestShapeSummary
  capture_mode: CaptureMode
  capture_status: CaptureStatus
  capture_truncated: boolean
  redaction_count: number
  input_labels_json?: string
  output_labels_json?: string
  transformation?: TransformationSummary
}

export interface RequestShapeSummary {
  input_message_count: number
  input_block_count: number
  input_tool_count: number
  input_image_count: number
  input_text_chars: number
  output_message_count: number
  output_block_count: number
  output_tool_call_count: number
  output_reasoning_chars: number
  output_text_chars: number
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
  vision_strategy?: "assist" | "takeover" | "reject"
  vision_provider?: string
  vision_model?: string
  vision_cache_hit?: boolean
  vision_latency_ms?: number
  vision_evidence_chars?: number
}

export interface RecentRequestsResponse {
  active: RequestEvent[]
  recent: RequestEvent[]
}

export interface RequestQueryFilters {
  agent_id?: string
  principal_name?: string
  session_id?: string
  project_id?: string
  model?: string
  provider?: string
  protocol?: string
  status_class?: string
  capture_status?: CaptureStatus | ""
  q?: string
  from?: string
  to?: string
}

export interface SessionQueryFilters {
  session_id?: string
  agent_id?: string
  principal_name?: string
  project_id?: string
  q?: string
}

export interface RequestPage {
  items: RequestEvent[]
  next_cursor?: string
}

export interface SessionSummary {
  session_id: string
  session_name?: string
  session_kind?: string
  session_path?: string
  agent_id?: string
  agent_name?: string
  principal_name?: string
  project_id?: string
  started_at: string
  updated_at: string
  first_request_id: string
  last_request_id: string
  request_count: number
  error_count: number
  total_tokens: number
}

export interface SessionPage {
  items: SessionSummary[]
  next_cursor?: string
}

export interface TraceObservation {
  observation_id: string
  request_id: string
  trace_id: string
  span_id: string
  parent_span_id?: string
  type: string
  name: string
  started_at: string
  completed_at?: string
  duration_ms: number
  status: string
  error_code?: string
  attributes?: Record<string, unknown>
}

export type PayloadStage =
  | "client_request"
  | "canonical_request"
  | "ocr_request"
  | "ocr_response"
  | "vision_request"
  | "vision_response"
  | "effective_canonical_request"
  | "upstream_request"
  | "canonical_response"

export interface PayloadContent {
  stage: PayloadStage
  state: CaptureStatus
  capture_mode?: CaptureMode
  media_type?: string
  content_encoding?: string
  headers?: Record<string, string[]>
  body?: unknown
  original_bytes?: number
  stored_bytes?: number
  truncated?: boolean
  truncation_reason?: string
  redaction_count?: number
  sha256?: string
  error?: string
  created_at?: string
  expires_at?: string
}

export interface RequestDetails {
  request: RequestEvent
  observations: TraceObservation[]
  payloads: PayloadContent[]
}

export interface SessionDetailResponse {
  session: SessionSummary
  requests: RequestEvent[]
}

export interface ValueChange {
  before: string
  after: string
}

export interface DiffChange {
  kind: string
  path: string
  before?: string
  after?: string
}

export interface RequestDiff {
  state: "available" | "no_base" | CaptureStatus
  unavailable_side?: string
  request_id: string
  base_request_id?: string
  base_selection?: "parent" | "previous" | "explicit"
  append_only: boolean
  context_shrunk: boolean
  common_prefix_messages: number
  messages_added: number
  messages_removed: number
  messages_rewritten: number
  requested_model_change?: ValueChange
  model_change?: ValueChange
  provider_change?: ValueChange
  tool_changes: DiffChange[]
}

export interface DeleteRequestContentResponse {
  request_id: string
  deleted: boolean
  state: "expired"
}

// ---- Metrics ----
export interface MetricsSummary {
  total_requests: number
  today_requests: number
  active_providers: number
  total_models: number
  today_tokens: {
    prompt: number
    completion: number
    total: number
    cache_read: number
    cache_write: number
  }
  prompt_cache: {
    reported_requests: number
    eligible_prompt_tokens: number
    weighted_hit_ratio: number
    reporting_coverage: number
  }
}

export interface MetricPoint {
  timestamp: string
  requests: number
  errors: number
  ttft_avg: number
  ttft_p50: number
  ttft_p95: number
  ttft_p99: number
  tpot_avg: number
  tpot_p50: number
  tpot_p95: number
  tps_avg: number
  tps_p50: number
  tps_p95: number
  tokens_prompt: number
  tokens_completion: number
  tokens_cache_read: number
  tokens_cache_write: number
  cache_hit_ratio: number
  cache_coverage: number
}

export type LiveRequestKind = "started" | "updated" | "first_token" | "progress" | "finished" | "failed"

export type RequestPhase =
  | "received"
  | "authenticated"
  | "parsed"
  | "routed"
  | "preprocessing"
  | "upstream_started"
  | "streaming"
  | "completed"

export interface LiveRequestEvent {
  id: number
  kind: LiveRequestKind
  phase: RequestPhase
  emitted_at: string
  request: RequestEvent
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
  reachable?: boolean
  model_listing?: "supported" | "unsupported"
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
  origin?: "remote" | "upload" | "cache"
  stale: boolean
  providers: number
  models: number
  error?: string
}

export interface ModelCatalogStatusResponse {
  catalog: ModelCatalogState
  proxy: ModelCatalogProxyState
}

export interface ModelCatalogProxyState {
  configured: boolean
  display_url?: string
}

export interface ModelCatalogSettingsResponse {
  ok?: boolean
  proxy: ModelCatalogProxyState
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

// ---- Desktop application ----
export type CloseBehavior = "ask" | "tray" | "quit"

export interface DesktopSnapshot {
  available: boolean
  platform?: "windows" | "darwin"
  close_behavior?: CloseBehavior
  listen_address?: string
  data_address?: string
  admin_address?: string
  data_dir?: string
  log_dir?: string
  owns_gateway?: boolean
}

// ---- Multimodal fallback ----
interface MultimodalAdminFields {
  enabled: boolean
  strategy: "ocr_then_vision"
  provider: "builtin" | "http"
  endpoint: string
  auth_type: "none" | "bearer" | "api_key_header"
  api_key_source: "" | "env" | "literal"
  api_key_env: string
  header: string
  vision_fallback_model: string
  vision_fallback_strategy: "assist" | "takeover" | "reject"
  min_confidence: number
  min_text_chars: number
  max_images: number
}

export interface VisionFallbackModelOption {
  target: string
  provider_id: string
  model: string
  source: "model_override" | "provider_default" | "models_dev"
}

export interface MultimodalAdminConfig extends MultimodalAdminFields {
  vision_fallback_models: VisionFallbackModelOption[]
}

export interface MultimodalAdminInput extends MultimodalAdminFields {
  api_key?: string
}

export interface OCRTestResponse {
  ok: boolean
  enabled: boolean
  active: boolean
  warning?: "multimodal_disabled" | "multimodal_not_active"
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
