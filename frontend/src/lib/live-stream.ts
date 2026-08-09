import { AUTH_REQUIRED_EVENT } from "./auth-api"
import { getToken } from "./api"
import type { LiveRequestEvent } from "./types"

interface LiveStreamOptions {
  signal: AbortSignal
  afterId?: number
  onOpen: () => void
  onEvent: (event: LiveRequestEvent) => void
}

export async function consumeLiveRequestStream({ signal, afterId, onOpen, onEvent }: LiveStreamOptions) {
  const headers: Record<string, string> = { Accept: "text/event-stream" }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  if (afterId && afterId > 0) headers["Last-Event-ID"] = String(afterId)

  const response = await fetch("/admin/observability/live", {
    credentials: "same-origin",
    headers,
    signal,
  })
  if (response.status === 401) {
    window.dispatchEvent(new Event(AUTH_REQUIRED_EVENT))
  }
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`)
  }
  if (!response.body) {
    throw new Error("Streaming response body is unavailable")
  }
  onOpen()

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""
  try {
    while (!signal.aborted) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const frames = buffer.split(/\r?\n\r?\n/)
      buffer = frames.pop() ?? ""
      for (const frame of frames) {
        const event = parseLiveFrame(frame)
        if (event) onEvent(event)
      }
    }
  } finally {
    reader.releaseLock()
  }
}

export function parseLiveFrame(frame: string): LiveRequestEvent | null {
  let eventName = ""
  const data: string[] = []
  for (const line of frame.split(/\r?\n/)) {
    if (line.startsWith(":")) continue
    if (line.startsWith("event:")) eventName = line.slice(6).trim()
    if (line.startsWith("data:")) data.push(line.slice(5).trimStart())
  }
  if (!eventName.startsWith("request.") || data.length === 0) return null
  try {
    const parsed = JSON.parse(data.join("\n")) as LiveRequestEvent
    return parsed?.request?.request_id && Number.isFinite(parsed.id) ? parsed : null
  } catch {
    return null
  }
}
