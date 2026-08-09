import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { consumeLiveRequestStream } from "@/lib/live-stream"
import type { LiveRequestEvent } from "@/lib/types"

export type LiveConnectionState = "connecting" | "live" | "reconnecting" | "paused" | "error"

const MAX_LIVE_REQUESTS = 200

function mergeEvent(current: Map<string, LiveRequestEvent>, event: LiveRequestEvent) {
  const next = new Map(current)
  const existing = next.get(event.request.request_id)
  if (!existing || event.id > existing.id) next.set(event.request.request_id, event)
  if (next.size > MAX_LIVE_REQUESTS) {
    const oldest = [...next.values()].sort((a, b) => a.id - b.id).slice(0, next.size - MAX_LIVE_REQUESTS)
    for (const item of oldest) next.delete(item.request.request_id)
  }
  return next
}

function abortError(error: unknown) {
  return error instanceof DOMException && error.name === "AbortError"
}

export function useLiveRequests() {
  const queryClient = useQueryClient()
  const [events, setEvents] = useState<Map<string, LiveRequestEvent>>(() => new Map())
  const [transportState, setTransportState] = useState<Exclude<LiveConnectionState, "paused">>("connecting")
  const [paused, setPaused] = useState(false)
  const [pendingCount, setPendingCount] = useState(0)
  const lastEventId = useRef(0)
  const pausedRef = useRef(false)
  const pending = useRef<Map<string, LiveRequestEvent>>(new Map())

  useEffect(() => {
    const controller = new AbortController()
    let reconnectDelay = 500
    const run = async () => {
      while (!controller.signal.aborted) {
        setTransportState(lastEventId.current > 0 ? "reconnecting" : "connecting")
        try {
          await consumeLiveRequestStream({
            signal: controller.signal,
            afterId: lastEventId.current,
            onOpen: () => {
              reconnectDelay = 500
              setTransportState("live")
            },
            onEvent: (event) => {
              if (event.id <= lastEventId.current) return
              lastEventId.current = event.id
              if (pausedRef.current) {
                pending.current.set(event.request.request_id, event)
                setPendingCount(pending.current.size)
                return
              }
              setEvents((current) => mergeEvent(current, event))
              if (event.kind === "finished" || event.kind === "failed") {
                window.setTimeout(() => {
                  void queryClient.invalidateQueries({ queryKey: ["observability", "requests"] })
                  void queryClient.invalidateQueries({ queryKey: ["metrics-summary"] })
                }, 350)
              }
            },
          })
          if (!controller.signal.aborted) setTransportState("reconnecting")
        } catch (error) {
          if (controller.signal.aborted || abortError(error)) return
          setTransportState("error")
        }
        await new Promise((resolve) => window.setTimeout(resolve, reconnectDelay))
        reconnectDelay = Math.min(reconnectDelay * 2, 5_000)
      }
    }
    void run()
    return () => controller.abort()
  }, [queryClient])

  const pause = useCallback(() => {
    pausedRef.current = true
    setPaused(true)
  }, [])

  const resume = useCallback(() => {
    pausedRef.current = false
    setPaused(false)
    const buffered = [...pending.current.values()].sort((a, b) => a.id - b.id)
    pending.current.clear()
    setPendingCount(0)
    if (buffered.length > 0) {
      setEvents((current) => buffered.reduce(mergeEvent, current))
    }
  }, [])

  return useMemo(() => ({
    events,
    state: paused ? "paused" as const : transportState,
    pendingCount,
    pause,
    resume,
  }), [events, pause, paused, pendingCount, resume, transportState])
}
