import { AUTH_REQUIRED_EVENT } from "./auth-api"
import { getToken } from "./api"
import type { LiveRequestEvent, LiveStreamMeta } from "./types"

interface LiveStreamOptions {
  signal: AbortSignal
  afterId?: number
  epoch?: string
  onOpen: () => void
  onMeta: (meta: LiveStreamMeta) => void
  onEvent: (event: LiveRequestEvent) => void
}

type ParsedLiveFrame =
  | { type: "meta"; value: LiveStreamMeta }
  | { type: "request"; value: LiveRequestEvent }

export async function consumeLiveRequestStream({ signal, afterId, epoch, onOpen, onMeta, onEvent }: LiveStreamOptions) {
  const headers: Record<string, string> = { Accept: "text/event-stream" }
  const token = getToken()
  if (token) headers.Authorization = `Bearer ${token}`
  if (afterId && afterId > 0) headers["Last-Event-ID"] = String(afterId)
  if (epoch) headers["X-Vibe-Stream-Epoch"] = epoch

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
        if (event?.type === "meta") onMeta(event.value)
        if (event?.type === "request") onEvent(event.value)
      }
    }
  } finally {
    reader.releaseLock()
  }
}

export function parseLiveFrame(frame: string): ParsedLiveFrame | null {
  let eventName = ""
  const data: string[] = []
  for (const line of frame.split(/\r?\n/)) {
    if (line.startsWith(":")) continue
    if (line.startsWith("event:")) eventName = line.slice(6).trim()
    if (line.startsWith("data:")) data.push(line.slice(5).trimStart())
  }
  if (data.length === 0) return null
  try {
    if (eventName === "stream.meta") {
      const parsed = JSON.parse(data.join("\n")) as LiveStreamMeta
      return parsed?.epoch && Number.isFinite(parsed.latest_id) ? { type: "meta", value: parsed } : null
    }
    if (!eventName.startsWith("request.")) return null
    const parsed = JSON.parse(data.join("\n")) as LiveRequestEvent
    return parsed?.request?.request_id && Number.isFinite(parsed.id) ? { type: "request", value: parsed } : null
  } catch {
    return null
  }
}
