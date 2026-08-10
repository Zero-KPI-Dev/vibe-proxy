import type {
  HealthzResponse,
  SnapshotResponse,
  ProviderFormData,
  ProviderTestResponse,
  ProviderModelsResponse,
  ConfigureResponse,
  ValidateConfigResponse,
  RecentRequestsResponse,
  AliasEntry,
  ClientKey,
  ClientKeyCreateResponse,
  ClientKeyRevealResponse,
  MetricsSummary,
  MetricsHistoryResponse,
  ProviderHealthResponse,
  RawConfigResponse,
  ConfigValidationIssue,
  ModelCatalogRefreshResponse,
  ModelCatalogSettingsResponse,
  ModelCatalogStatusResponse,
  ModelCatalogLookupResponse,
  MultimodalAdminConfig,
  MultimodalAdminInput,
  OCRTestResponse,
  CloseBehavior,
  DesktopSnapshot,
  RequestQueryFilters,
  SessionQueryFilters,
  RequestPage,
  SessionPage,
  RequestDetails,
  SessionDetailResponse,
  RequestDiff,
  DeleteRequestContentResponse,
} from "./types"
import { AUTH_REQUIRED_EVENT } from "./auth-api"

export function getToken(): string {
  return localStorage.getItem("vibe_admin_token") ?? ""
}

export function setToken(t: string) {
  localStorage.setItem("vibe_admin_token", t)
}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    ...(opts.headers as Record<string, string>),
  }
  const isFormData = typeof FormData !== "undefined" && opts.body instanceof FormData
  if (!isFormData && !headers["Content-Type"]) headers["Content-Type"] = "application/json"
  const token = getToken()
  if (token) headers["Authorization"] = `Bearer ${token}`

  const resp = await fetch(path, { ...opts, credentials: "same-origin", headers })
  if (resp.status === 401) {
    window.dispatchEvent(new Event(AUTH_REQUIRED_EVENT))
  }
  const text = await resp.text()
  let body: unknown
  try {
    body = JSON.parse(text)
  } catch {
    body = text
  }
  if (!resp.ok) {
    const msg = typeof body === "object" && body && "message" in body && typeof (body as Record<string, unknown>).message === "string"
      ? (body as Record<string, unknown>).message as string
      : typeof body === "object" && body && "error" in body
      ? String((body as Record<string, unknown>).error)
      : typeof body === "string"
        ? body
        : `HTTP ${resp.status}`
    throw new Error(msg)
  }
  return body as T
}

// ---- Health ----
export const healthApi = {
  check: () => request<HealthzResponse>("/healthz"),
}

// ---- Providers ----
export const providerApi = {
  list: () => request<SnapshotResponse>("/admin/config/snapshot"),

  create: (data: ProviderFormData) =>
    request<ConfigureResponse>("/admin/local/configure", {
      method: "POST",
      body: JSON.stringify(data),
    }),

  update: (data: ProviderFormData) =>
    request<ConfigureResponse>("/admin/providers", {
      method: "PUT",
      body: JSON.stringify(data),
    }),

  remove: (id: string) =>
    request<{ ok: boolean }>("/admin/providers", {
      method: "DELETE",
      body: JSON.stringify({ id }),
    }),

  test: (id: string) =>
    request<ProviderTestResponse>(
      `/admin/providers/test?id=${encodeURIComponent(id)}`,
      { method: "POST" }
    ),

  models: (data: ProviderFormData) =>
    request<ProviderModelsResponse>("/admin/providers/models", {
      method: "POST",
      body: JSON.stringify(data),
    }),
}

export const modelCatalogApi = {
  status: () => request<ModelCatalogStatusResponse>("/admin/model-catalog/status"),
  updateProxy: (proxy_url: string) =>
    request<ModelCatalogSettingsResponse>("/admin/model-catalog/settings", {
      method: "PUT",
      body: JSON.stringify({ proxy_url }),
    }),
  refresh: () =>
    request<ModelCatalogRefreshResponse>("/admin/model-catalog/refresh", {
      method: "POST",
    }),
  importFile: (file: File) => {
    const form = new FormData()
    form.set("catalog", file)
    return request<ModelCatalogRefreshResponse>("/admin/model-catalog/import", {
      method: "POST",
      body: form,
    })
  },
  lookup: (data: { provider_id: string; catalog_provider?: string; base_url: string; models: string[] }) =>
    request<ModelCatalogLookupResponse>("/admin/model-catalog/lookup", {
      method: "POST",
      body: JSON.stringify(data),
    }),
}

// ---- Desktop application ----
export const desktopApi = {
  snapshot: () => request<DesktopSnapshot>("/admin/desktop"),
  setCloseBehavior: (close_behavior: CloseBehavior) =>
    request<{ close_behavior: CloseBehavior }>("/admin/desktop/preferences", {
      method: "PUT",
      body: JSON.stringify({ close_behavior }),
    }),
  copyText: (text: string) =>
    request<{ copied: boolean }>("/admin/desktop/clipboard", {
      method: "POST",
      body: JSON.stringify({ text }),
    }),
  openDataDir: () =>
    request<{ opened: boolean }>("/admin/desktop/open-data-dir", { method: "POST" }),
  importConfig: () =>
    request<{ imported: boolean; path?: string; loaded_at?: string }>(
      "/admin/desktop/import-config",
      { method: "POST" },
    ),
}

export const multimodalApi = {
  get: () => request<MultimodalAdminConfig>("/admin/multimodal"),
  save: (data: MultimodalAdminInput) =>
    request<{ ok: boolean; multimodal: MultimodalAdminConfig; loaded_at: string }>("/admin/multimodal", {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  testOCR: (data: MultimodalAdminInput) =>
    request<OCRTestResponse>("/admin/multimodal/ocr/test", {
      method: "POST",
      body: JSON.stringify(data),
    }),
}

// ---- Aliases ----
export const aliasApi = {
  list: () => request<{ default_model: string; allow_raw: boolean; aliases: Record<string, string> }>("/admin/aliases"),

  create: (alias: string, target: string) =>
    request<{ ok: boolean }>("/admin/aliases", {
      method: "POST",
      body: JSON.stringify({ alias, target }),
    }),

  remove: (alias: string) =>
    request<{ ok: boolean }>(`/admin/aliases/${encodeURIComponent(alias)}`, {
      method: "DELETE",
    }),

  updateDefault: (default_model: string, allow_raw: boolean) =>
    request<{ ok: boolean }>("/admin/aliases/default", {
      method: "PUT",
      body: JSON.stringify({ default_model, allow_raw }),
    }),
}

// ---- Client Keys ----
export const clientKeyApi = {
  list: () => request<{ keys: ClientKey[] }>("/admin/client-keys"),

  reveal: (name: string) =>
    request<ClientKeyRevealResponse>(`/admin/client-keys/${encodeURIComponent(name)}`),

  rotate: (name: string) =>
    request<ClientKeyCreateResponse>(`/admin/client-keys/${encodeURIComponent(name)}/rotate`, {
      method: "POST",
    }),

  create: (data: { name: string; allowed_models?: string[]; rpm?: number }) =>
    request<ClientKeyCreateResponse>("/admin/client-keys", {
      method: "POST",
      body: JSON.stringify(data),
    }),

  update: (name: string, data: { enabled?: boolean; allowed_models?: string[]; rpm?: number }) =>
    request<{ ok: boolean }>(`/admin/client-keys/${encodeURIComponent(name)}`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),

  remove: (name: string) =>
    request<{ ok: boolean }>(`/admin/client-keys/${encodeURIComponent(name)}`, {
      method: "DELETE",
    }),
}

// ---- Metrics ----
export const metricsApi = {
  summary: () => request<MetricsSummary>("/admin/metrics/summary"),
  history: (range: string) =>
    request<MetricsHistoryResponse>(`/admin/metrics/history?range=${range}`),
}

// ---- Provider Health ----
export const providerHealthApi = {
  list: () => request<ProviderHealthResponse>("/admin/providers/health"),
}

// ---- Config ----
export const configApi = {
  validate: () => request<ValidateConfigResponse>("/admin/config/validate"),
  reload: () =>
    request<{ reloaded: boolean; loaded_at: string }>("/admin/config/reload", {
      method: "POST",
    }),
  raw: () => request<RawConfigResponse>("/admin/config/raw"),
  saveRaw: (yaml: string) =>
    request<{ ok: boolean; issues: ConfigValidationIssue[]; loaded_at: string }>("/admin/config/raw", {
      method: "PUT",
      body: JSON.stringify({ yaml }),
    }),
}

// ---- Requests ----
export const requestApi = {
  recent: () => request<RecentRequestsResponse>("/admin/requests/recent"),
}

const REQUEST_FILTER_KEYS: Array<keyof RequestQueryFilters> = [
  "agent_id",
  "principal_name",
  "session_id",
  "project_id",
  "model",
  "provider",
  "protocol",
  "status_class",
  "capture_status",
  "q",
  "from",
  "to",
]

const SESSION_FILTER_KEYS: Array<keyof SessionQueryFilters> = [
  "session_id",
  "agent_id",
  "principal_name",
  "project_id",
  "q",
]

function observabilityQuery<T extends object>(
  filters: T,
  keys: Array<keyof T>,
  cursor?: string,
  limit = 50,
): string {
  const params = new URLSearchParams({ limit: String(limit) })
  for (const key of keys) {
    const value = filters[key]
    if (typeof value === "string" && value.trim()) params.set(String(key), value.trim())
  }
  if (cursor) params.set("cursor", cursor)
  return params.toString()
}

// ---- Local observability ----
export const observabilityApi = {
  requests: (filters: RequestQueryFilters = {}, cursor?: string, limit = 50) =>
    request<RequestPage>(
      `/admin/observability/requests?${observabilityQuery(filters, REQUEST_FILTER_KEYS, cursor, limit)}`,
    ),

  request: (requestId: string) =>
    request<RequestDetails>(`/admin/observability/requests/${encodeURIComponent(requestId)}`),

  diff: (requestId: string, base_id?: string) => {
    const query = base_id ? `?base_id=${encodeURIComponent(base_id)}` : ""
    return request<RequestDiff>(
      `/admin/observability/requests/${encodeURIComponent(requestId)}/diff${query}`,
    )
  },

  deleteContent: (requestId: string) =>
    request<DeleteRequestContentResponse>(
      `/admin/observability/requests/${encodeURIComponent(requestId)}/content`,
      { method: "DELETE" },
    ),

  sessions: (filters: SessionQueryFilters = {}, cursor?: string, limit = 50) =>
    request<SessionPage>(
      `/admin/observability/sessions?${observabilityQuery(filters, SESSION_FILTER_KEYS, cursor, limit)}`,
    ),

  session: (sessionId: string) =>
    request<SessionDetailResponse>(
      `/admin/observability/sessions/${encodeURIComponent(sessionId)}`,
    ),
}
