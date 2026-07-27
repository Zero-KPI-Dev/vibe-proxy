import {
  ArrowRight,
  Eye,
  FileSearch,
  Image as ImageIcon,
  ScanText,
  Server,
  ShieldQuestion,
  XCircle,
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import type { RequestEvent } from "@/lib/types"
import { useTranslation } from "react-i18next"

interface MultimodalFlowProps {
  event: RequestEvent
  className?: string
}

function supportLabel(
  support: string | undefined,
  t: (key: string, options?: Record<string, unknown>) => string
) {
  if (support === "supported") return t("multimodalFlow.supported")
  if (support === "unsupported") return t("multimodalFlow.unsupported")
  return t("multimodalFlow.unknown")
}

function routeLabel(
  route: string | undefined,
  t: (key: string, options?: Record<string, unknown>) => string
) {
  switch (route) {
    case "direct_vision":
      return t("multimodalFlow.directVision")
    case "ocr_fallback":
      return t("multimodalFlow.ocrFallback")
    case "vision_fallback":
      return t("multimodalFlow.visionFallback")
    case "rejected":
      return t("multimodalFlow.rejected")
    case "legacy_passthrough":
      return t("multimodalFlow.legacyPassthrough")
    case "direct_text":
      return t("multimodalFlow.directText")
    default:
      return t("multimodalFlow.notRecorded")
  }
}

function FlowStep({
  icon,
  title,
  children,
  tone,
}: {
  icon: React.ReactNode
  title: string
  children: React.ReactNode
  tone?: "normal" | "warning" | "danger" | "success"
}) {
  return (
    <div
      className={cn(
        "min-w-0 flex-1 rounded-lg border border-border bg-background p-3",
        tone === "warning" && "border-amber-500/40 bg-amber-500/5",
        tone === "danger" && "border-destructive/40 bg-destructive/5",
        tone === "success" && "border-emerald-500/40 bg-emerald-500/5"
      )}
    >
      <div className="mb-2 flex items-center gap-2 text-sm font-medium">
        {icon}
        <span>{title}</span>
      </div>
      <div className="space-y-1 text-xs text-muted-foreground">{children}</div>
    </div>
  )
}

export function MultimodalFlow({ event, className }: MultimodalFlowProps) {
  const { t } = useTranslation()
  const flow = event.transformation
  const route = flow?.multimodal_route
  const imageCount = flow?.input_images ?? 0
  const finalProvider = flow?.effective_provider || event.channel_id
  const finalModel = flow?.effective_model || event.upstream_model
  const usesOCR = route === "ocr_fallback" || route === "vision_fallback"

  return (
    <div className={cn("space-y-3", className)}>
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={event.status_code >= 400 ? "destructive" : "outline"}>
          HTTP {event.status_code || "..."}
        </Badge>
        <Badge
          variant={
            route === "ocr_fallback"
              ? "warning"
              : route === "vision_fallback"
                ? "secondary"
                : route === "rejected"
                  ? "destructive"
                  : "outline"
          }
        >
          {routeLabel(route, t)}
        </Badge>
        <code className="break-all text-[11px] text-muted-foreground">{event.request_id}</code>
      </div>

      <div className="flex flex-col items-stretch gap-2 lg:flex-row lg:items-center">
        <FlowStep
          icon={<ImageIcon className="h-4 w-4 text-primary" />}
          title={t("multimodalFlow.input")}
        >
          <p>{t("multimodalFlow.imageCount", { count: imageCount })}</p>
          <p className="font-mono">{event.protocol_in}</p>
        </FlowStep>

        <ArrowRight className="hidden h-4 w-4 shrink-0 text-muted-foreground lg:block" />

        <FlowStep
          icon={
            flow?.model_image_support === "supported" ? (
              <Eye className="h-4 w-4 text-emerald-500" />
            ) : (
              <ShieldQuestion className="h-4 w-4 text-amber-500" />
            )
          }
          title={t("multimodalFlow.capability")}
        >
          <p>{supportLabel(flow?.model_image_support, t)}</p>
          <p>{t("multimodalFlow.source", { source: flow?.capability_source || "-" })}</p>
          {flow?.catalog_match && <p>models.dev: {flow.catalog_match}</p>}
        </FlowStep>

        <ArrowRight className="hidden h-4 w-4 shrink-0 text-muted-foreground lg:block" />

        <FlowStep
          icon={
            route === "rejected" ? (
              <XCircle className="h-4 w-4 text-destructive" />
            ) : usesOCR ? (
              <ScanText className="h-4 w-4 text-amber-500" />
            ) : (
              <FileSearch className="h-4 w-4 text-primary" />
            )
          }
          title={t("multimodalFlow.processing")}
          tone={
            route === "rejected"
              ? "danger"
              : route === "ocr_fallback" || route === "vision_fallback"
                ? "warning"
                : "normal"
          }
        >
          <p>{routeLabel(route, t)}</p>
          {usesOCR && (
            <>
              <p>{t("multimodalFlow.ocrProvider", { provider: flow?.ocr_provider || "-" })}</p>
              <p>
                {t("multimodalFlow.ocrStats", {
                  processed: flow?.ocr_processed ?? 0,
                  hits: flow?.ocr_cache_hits ?? 0,
                  latency: flow?.ocr_latency_ms ?? 0,
                })}
              </p>
              {flow?.ocr_min_confidence != null && (
                <p>{t("multimodalFlow.confidence", { value: flow.ocr_min_confidence.toFixed(2) })}</p>
              )}
              {flow?.ocr_failure_code && (
                <p className="font-mono text-destructive">{flow.ocr_failure_code}</p>
              )}
            </>
          )}
          {flow?.route_reason && <p className="font-mono">{flow.route_reason}</p>}
        </FlowStep>

        <ArrowRight className="hidden h-4 w-4 shrink-0 text-muted-foreground lg:block" />

        <FlowStep
          icon={<Server className="h-4 w-4 text-emerald-500" />}
          title={t("multimodalFlow.finalTarget")}
          tone={event.status_code >= 200 && event.status_code < 300 ? "success" : "normal"}
        >
          <p className="break-all font-mono">{finalProvider || "-"}</p>
          <p className="break-all font-mono">{finalModel || "-"}</p>
          {flow?.original_provider &&
            (flow.original_provider !== finalProvider || flow.original_model !== finalModel) && (
              <p>
                {t("multimodalFlow.originalTarget", {
                  provider: flow.original_provider,
                  model: flow.original_model,
                })}
              </p>
            )}
        </FlowStep>
      </div>

      {event.error_code && (
        <p className="text-xs text-destructive">
          {t("multimodalFlow.errorCode")}: <code>{event.error_code}</code>
        </p>
      )}
    </div>
  )
}
