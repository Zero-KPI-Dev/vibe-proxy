export const AUTH_REQUIRED_EVENT = "vibe-auth-required"

export interface AuthStatus {
  mode: "desktop" | "token"
  initialized: boolean
  authenticated: boolean
}

export class AuthAPIError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = "AuthAPIError"
    this.status = status
  }
}

async function authRequest(path: string, options: RequestInit = {}): Promise<AuthStatus> {
  const headers = new Headers(options.headers)
  if (options.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json")
  }
  const response = await fetch(path, {
    ...options,
    headers,
    credentials: "same-origin",
  })
  const text = await response.text()
  let body: unknown
  try {
    body = JSON.parse(text)
  } catch {
    body = text
  }
  if (!response.ok) {
    const message =
      typeof body === "object" &&
      body !== null &&
      "error" in body &&
      typeof (body as { error?: unknown }).error === "string"
        ? (body as { error: string }).error
        : typeof body === "string" && body.trim()
          ? body.trim()
          : `HTTP ${response.status}`
    throw new AuthAPIError(response.status, message)
  }
  return body as AuthStatus
}

export const authApi = {
  status: () => authRequest("/auth/status"),
  setup: (password: string) =>
    authRequest("/auth/setup", {
      method: "POST",
      body: JSON.stringify({ password }),
    }),
  login: (password: string) =>
    authRequest("/auth/login", {
      method: "POST",
      body: JSON.stringify({ password }),
    }),
  logout: () => authRequest("/auth/logout", { method: "POST" }),
}
