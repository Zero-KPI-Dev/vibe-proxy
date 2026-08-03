import { useState } from "react"
import { useNavigate } from "react-router"
import { useTranslation } from "react-i18next"
import { Badge } from "@/components/ui/badge"
import { CaptureStateBadge } from "@/components/capture-state-badge"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { MultimodalFlow } from "@/components/multimodal-flow"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import type { RequestEvent } from "@/lib/types"

interface RequestTableProps {
  events: RequestEvent[]
  openDetails?: boolean
}

function statusBadge(status: number) {
  if (status >= 200 && status < 300) return <Badge variant="success">{status}</Badge>
  if (status >= 400 && status < 500) return <Badge variant="warning">{status}</Badge>
  if (status >= 500) return <Badge variant="destructive">{status}</Badge>
  return <Badge variant="outline">{status || "—"}</Badge>
}

export function RequestTable({ events, openDetails = false }: RequestTableProps) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const [selected, setSelected] = useState<RequestEvent | null>(null)

  const open = (event: RequestEvent) => {
    if (openDetails) {
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
            <TableHead>{t("observability.columns.model")}</TableHead>
            <TableHead>{t("observability.columns.provider")}</TableHead>
            <TableHead>{t("observability.columns.status")}</TableHead>
            <TableHead>{t("observability.columns.capture")}</TableHead>
            <TableHead className="text-right">{t("observability.columns.duration")}</TableHead>
            <TableHead className="text-right">{t("observability.columns.tokens")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {events.map((event) => (
            <TableRow
              key={event.request_id}
              className="cursor-pointer"
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
              </TableCell>
              <TableCell>
                <div className="min-w-28">
                  <p className="font-medium">{event.agent_name || event.agent_id || t("observability.unclassified")}</p>
                  {event.agent_name && event.agent_id && <p className="font-mono text-xs text-muted-foreground">{event.agent_id}</p>}
                </div>
              </TableCell>
              <TableCell className="text-sm">{event.principal_name || event.client_name || "—"}</TableCell>
              <TableCell>
                <div className="min-w-32">
                  <p className="font-mono text-xs">{event.upstream_model || event.virtual_model || "—"}</p>
                  {event.virtual_model && event.virtual_model !== event.upstream_model && (
                    <p className="text-xs text-muted-foreground">{event.virtual_model}</p>
                  )}
                </div>
              </TableCell>
              <TableCell className="font-mono text-xs">{event.channel_id || "—"}</TableCell>
              <TableCell>
                <div className="flex flex-wrap items-center gap-1.5">
                  {statusBadge(event.status_code)}
                  {event.transformation?.multimodal_route === "ocr_fallback" && (
                    <Badge variant="warning">{t("requestTable.ocrFallback")}</Badge>
                  )}
                  {event.transformation?.multimodal_route === "vision_fallback" && (
                    <Badge variant="secondary">{t("requestTable.visionFallback")}</Badge>
                  )}
                </div>
              </TableCell>
              <TableCell><CaptureStateBadge state={event.capture_status || "not_captured"} /></TableCell>
              <TableCell className="whitespace-nowrap text-right tabular-nums">{event.duration_ms ?? 0} ms</TableCell>
              <TableCell className="text-right tabular-nums">{event.usage?.total_tokens ?? 0}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      {!openDetails && (
        <Dialog open={selected != null} onOpenChange={(isOpen) => !isOpen && setSelected(null)}>
          <DialogContent className="max-w-6xl">
            <DialogHeader><DialogTitle>{t("requestTable.flowDetails")}</DialogTitle></DialogHeader>
            {selected && <MultimodalFlow event={selected} />}
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}
