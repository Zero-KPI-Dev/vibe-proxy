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
  MetricsSummary,
  MetricsHistoryResponse,
  ProviderHealthResponse,
  RawConfigResponse,
  ConfigValidationIssue,
  ModelCatalogRefreshResponse,
  ModelCatalogStatusResponse,
} from "./types"

export function getToken(): string {
  return localStorage.getItem("vibe_admin_token") ?? ""
}

export function setToken(t: string) {
  localStorage.setItem("vibe_admin_token", t)
}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(opts.headers as Record<string, string>),
  }
  const token = getToken()
  if (token) headers["Authorization"] = `Bearer ${token}`

  const resp = await fetch(path, { ...opts, headers })
  const text = await resp.text()
  let body: unknown
  try {
    body = JSON.parse(text)
  } catch {
    body = text
  }
  if (!resp.ok) {
    const msg = typeof body === "object" && body && "error" in body
      ? (body as Record<string, unknown>).error as string
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
  refresh: () =>
    request<ModelCatalogRefreshResponse>("/admin/model-catalog/refresh", {
      method: "POST",
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
