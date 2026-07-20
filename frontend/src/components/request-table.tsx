import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import type { RequestEvent } from "@/lib/types"
import { useTranslation } from "react-i18next"

interface RequestTableProps {
  events: RequestEvent[]
}

function statusBadge(status: number) {
  if (status >= 200 && status < 300) return <Badge variant="success">{status}</Badge>
  if (status >= 400 && status < 500) return <Badge variant="warning">{status}</Badge>
  if (status >= 500) return <Badge variant="destructive">{status}</Badge>
  return <Badge variant="outline">{status}</Badge>
}

export function RequestTable({ events }: RequestTableProps) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("requestTable.client")}</TableHead>
          <TableHead>{t("requestTable.model")}</TableHead>
          <TableHead>{t("requestTable.provider")}</TableHead>
          <TableHead>{t("requestTable.status")}</TableHead>
          <TableHead className="text-right">{t("requestTable.tokens")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {events.map((e) => (
          <TableRow key={e.request_id}>
            <TableCell className="font-medium">{e.client_name}</TableCell>
            <TableCell className="font-mono text-xs">{e.virtual_model}</TableCell>
            <TableCell className="font-mono text-xs">{e.channel_id}</TableCell>
            <TableCell>
              <div className="flex flex-wrap items-center gap-1.5">
                {statusBadge(e.status_code)}
                {e.transformation?.multimodal_route === "ocr_fallback" && (
                  <Badge
                    variant="warning"
                    title={t("requestTable.ocrDetails", {
                      images: e.transformation.ocr_processed ?? 0,
                      hits: e.transformation.ocr_cache_hits ?? 0,
                      latency: e.transformation.ocr_latency_ms ?? 0,
                    })}
                  >
                    {t("requestTable.ocrFallback")}
                  </Badge>
                )}
                {e.transformation?.multimodal_route === "vision_fallback" && (
                  <Badge variant="secondary">{t("requestTable.visionFallback")}</Badge>
                )}
                {e.transformation?.multimodal_route === "rejected" && e.transformation.ocr_failure_code && (
                  <Badge variant="destructive">{t("requestTable.imageRejected")}</Badge>
                )}
              </div>
            </TableCell>
            <TableCell className="text-right tabular-nums">
              {e.usage?.total_tokens ?? 0}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
