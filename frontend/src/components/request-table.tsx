import { useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { useTranslation } from "react-i18next"
import { Badge } from "@/components/ui/badge"
import { CaptureStateBadge } from "@/components/capture-state-badge"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { MultimodalFlow } from "@/components/multimodal-flow"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import type { LiveRequestEvent, RequestEvent } from "@/lib/types"

interface RequestTableProps {
  events: RequestEvent[]
  openDetails?: boolean
  liveByRequest?: Map<string, LiveRequestEvent>
}

function statusBadge(status: number) {
  if (status >= 200 && status < 300) return <Badge variant="success">{status}</Badge>
  if (status >= 400 && status < 500) return <Badge variant="warning">{status}</Badge>
  if (status >= 500) return <Badge variant="destructive">{status}</Badge>
  return <Badge variant="outline">{status || "—"}</Badge>
}

function formatDuration(value: number) {
  if (value < 1_000) return `${Math.max(0, value)} ms`
  return `${(value / 1_000).toFixed(value < 10_000 ? 2 : 1)} s`
}

export function RequestTable({ events, openDetails = false, liveByRequest = new Map() }: RequestTableProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const [selected, setSelected] = useState<RequestEvent | null>(null)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!events.some((event) => !event.completed_at)) return
    const timer = window.setInterval(() => setNow(Date.now()), 500)
    return () => window.clearInterval(timer)
  }, [events])

  const open = (event: RequestEvent) => {
    if (openDetails && event.completed_at) {
      void navigate(`/observability/requests/${encodeURIComponent(event.request_id)}`)
    } else {
      setSelected(event)
    }
  }

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("observability.columns.time")}</TableHead>
            <TableHead>{t("observability.columns.agent")}</TableHead>
            <TableHead>{t("observability.columns.principal")}</TableHead>
            <TableHead>{t("observability.columns.route")}</TableHead>
            <TableHead>{t("observability.columns.status")}</TableHead>
            <TableHead>{t("observability.columns.performance")}</TableHead>
            <TableHead>{t("observability.columns.tokens")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {events.map((event) => {
            const live = liveByRequest.get(event.request_id)
            const active = !event.completed_at && live?.kind !== "finished" && live?.kind !== "failed"
            const duration = active ? now - new Date(event.started_at).getTime() : event.duration_ms ?? 0
            const agentName = event.agent_id === "vibe-proxy-playground"
              ? t("observability.identity.playgroundAgent")
              : event.agent_name || event.agent_id || t("observability.unclassified")
            const internalPlayground = event.principal_type === "internal" && event.principal_name === "admin-playground"
            const sourceName = internalPlayground
              ? t("observability.identity.playgroundSource")
              : event.principal_name || event.client_name || t("observability.identity.unknownSource")
            return (
            <TableRow
              key={event.request_id}
              className="cursor-pointer align-top"
              tabIndex={0}
              onClick={() => open(event)}
              onKeyDown={(keyboardEvent) => {
                if (keyboardEvent.key === "Enter" || keyboardEvent.key === " ") open(event)
              }}
            >
              <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                {new Date(event.started_at).toLocaleString(i18n.language, {
                  month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit",
                })}
                {active && <p className="mt-1 font-medium text-emerald-500">{formatDuration(duration)}</p>}
              </TableCell>
              <TableCell>
                <div className="min-w-28">
                  <p className="font-medium">{agentName}</p>
                  {event.agent_id && event.agent_id !== "unknown" && event.agent_id !== "vibe-proxy-playground" && <p className="font-mono text-xs text-muted-foreground">{event.agent_id}</p>}
                </div>
              </TableCell>
              <TableCell>
                <div className="min-w-32 text-sm">
                  <p className="font-medium">{sourceName}</p>
                  {event.client_key_prefix && <p className="font-mono text-xs text-muted-foreground">{event.client_key_prefix}...</p>}
                </div>
              </TableCell>
              <TableCell>
                <div className="min-w-40">
                  <p className="font-mono text-xs">{event.upstream_model || event.virtual_model || "—"}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {event.channel_id || "—"}
                    {event.virtual_model && event.virtual_model !== event.upstream_model ? ` · ${event.virtual_model}` : ""}
                  </p>
                </div>
              </TableCell>
              <TableCell>
                <div className="flex flex-wrap items-center gap-1.5">
                  {active ? <Badge variant="secondary" className="gap-1.5"><span className="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-500" />{t(`observability.live.phases.${live?.phase ?? "received"}`)}</Badge> : statusBadge(event.status_code)}
                  {event.transformation?.multimodal_route === "ocr_fallback" && (
                    <Badge variant="warning">{t("requestTable.ocrFallback")}</Badge>
                  )}
                  {event.transformation?.multimodal_route === "vision_fallback" && (
                    <Badge variant="secondary">{t("requestTable.visionFallback")}</Badge>
                  )}
                  {!active && <CaptureStateBadge state={event.capture_status || "not_captured"} />}
                </div>
              </TableCell>
              <TableCell>
                <div className="min-w-32 text-xs tabular-nums">
                  <p>{t("observability.performance.total")} {formatDuration(duration)}</p>
                  <p className="mt-1 text-muted-foreground">
                    TTFT {event.ttft_ms && event.ttft_ms > 0 ? formatDuration(event.ttft_ms) : "—"}
                    {event.tpot_ms && event.tpot_ms > 0 ? ` · TPOT ${event.tpot_ms.toFixed(1)} ms` : ""}
                  </p>
                </div>
              </TableCell>
              <TableCell>
                <div className="min-w-32 text-xs tabular-nums">
                  <p>{(event.usage?.total_tokens ?? 0).toLocaleString(i18n.language)} Token</p>
                  {event.usage?.cache_metrics_reported ? (
                    <p className="mt-1 font-medium text-emerald-600 dark:text-emerald-400">
                      {t("observability.cache.hit", { ratio: Math.round((event.usage.cache_hit_ratio ?? 0) * 100) })}
                    </p>
                  ) : (
                    <p className="mt-1 text-muted-foreground">{t("observability.cache.notReported")}</p>
                  )}
                </div>
              </TableCell>
            </TableRow>
            )
          })}
        </TableBody>
      </Table>

      <Dialog open={selected != null} onOpenChange={(isOpen) => !isOpen && setSelected(null)}>
        <DialogContent className="max-w-6xl">
          <DialogHeader><DialogTitle>{t("requestTable.flowDetails")}</DialogTitle></DialogHeader>
          {selected && <MultimodalFlow event={selected} />}
        </DialogContent>
      </Dialog>
    </>
  )
}
