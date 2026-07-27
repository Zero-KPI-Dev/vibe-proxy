import {
  Activity,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Eye,
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

function routeStyle(route: string | undefined) {
  switch (route) {
    case "ocr_fallback":
      return {
        icon: <ScanText className="h-4 w-4" />,
        iconClass: "bg-amber-500/12 text-amber-600 dark:text-amber-400",
        badge: "warning" as const,
      }
    case "vision_fallback":
      return {
        icon: <Eye className="h-4 w-4" />,
        iconClass: "bg-violet-500/12 text-violet-600 dark:text-violet-400",
        badge: "secondary" as const,
      }
    case "rejected":
      return {
        icon: <XCircle className="h-4 w-4" />,
        iconClass: "bg-destructive/10 text-destructive",
        badge: "destructive" as const,
      }
    default:
      return {
        icon: <CheckCircle2 className="h-4 w-4" />,
        iconClass: "bg-emerald-500/12 text-emerald-600 dark:text-emerald-400",
        badge: "outline" as const,
      }
  }
}

function FlowNode({
  icon,
  label,
  value,
  detail,
  iconClass,
}: {
  icon: React.ReactNode
  label: string
  value: string
  detail?: string
  iconClass?: string
}) {
  return (
    <div className="flex min-w-0 flex-1 items-center gap-2.5">
      <span
        className={cn(
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-background text-muted-foreground shadow-sm ring-1 ring-border",
          iconClass
        )}
      >
        {icon}
      </span>
      <div className="min-w-0">
        <p className="text-[11px] leading-none text-muted-foreground">{label}</p>
        <p className="mt-1 truncate text-sm font-medium" title={value}>{value}</p>
        {detail && <p className="mt-0.5 truncate text-[11px] text-muted-foreground" title={detail}>{detail}</p>}
      </div>
    </div>
  )
}

function Detail({
  label,
  value,
  mono = false,
}: {
  label: string
  value?: string | number | null
  mono?: boolean
}) {
  if (value == null || value === "") return null
  return (
    <div className="min-w-0">
      <dt className="text-[11px] text-muted-foreground">{label}</dt>
      <dd className={cn("mt-0.5 break-all text-xs", mono && "font-mono")}>{value}</dd>
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
  const originalModel = flow?.original_model || event.virtual_model
  const style = routeStyle(route)
  const usesOCR = route === "ocr_fallback" || route === "vision_fallback"
  const processingDetail = usesOCR
    ? flow?.ocr_failure_code
      ? flow.ocr_failure_code
      : [
          flow?.ocr_provider,
          flow?.ocr_latency_ms != null ? `${flow.ocr_latency_ms}ms` : "",
          flow?.ocr_min_confidence != null
            ? t("multimodalFlow.confidenceShort", { value: flow.ocr_min_confidence.toFixed(2) })
            : "",
        ].filter(Boolean).join(" · ")
    : flow?.route_reason

  return (
    <section className={cn("overflow-hidden rounded-xl border border-border bg-card shadow-sm", className)}>
      <div className="flex flex-wrap items-center gap-3 px-4 py-3">
        <span className={cn("flex h-9 w-9 items-center justify-center rounded-full", style.iconClass)}>
          {style.icon}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-sm font-semibold">{t("multimodalFlow.requestPath")}</h3>
            <Badge variant={style.badge}>{routeLabel(route, t)}</Badge>
          </div>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {t("multimodalFlow.pathSummary", {
              images: imageCount,
              model: finalModel || "-",
            })}
          </p>
        </div>
        <Badge variant={event.status_code >= 400 ? "destructive" : "outline"}>
          HTTP {event.status_code || "..."}
        </Badge>
      </div>

      <div className="overflow-x-auto border-t border-border bg-muted/25 px-4 py-3">
        <div className="flex min-w-[680px] items-center gap-2">
          <FlowNode
            icon={<ImageIcon className="h-4 w-4" />}
            label={t("multimodalFlow.input")}
            value={
              imageCount > 0
                ? t("multimodalFlow.imageCountCompact", { count: imageCount })
                : t("multimodalFlow.textOnly")
            }
            detail={event.protocol_in}
            iconClass="text-primary"
          />
          <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/60" />
          <FlowNode
            icon={
              flow?.model_image_support === "supported" ? (
                <Eye className="h-4 w-4" />
              ) : (
                <ShieldQuestion className="h-4 w-4" />
              )
            }
            label={t("multimodalFlow.capability")}
            value={supportLabel(flow?.model_image_support, t)}
            detail={originalModel}
            iconClass={
              flow?.model_image_support === "supported"
                ? "text-emerald-600 dark:text-emerald-400"
                : "text-amber-600 dark:text-amber-400"
            }
          />
          <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/60" />
          <FlowNode
            icon={style.icon}
            label={t("multimodalFlow.processing")}
            value={routeLabel(route, t)}
            detail={processingDetail}
            iconClass={style.iconClass}
          />
          <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/60" />
          <FlowNode
            icon={<Server className="h-4 w-4" />}
            label={t("multimodalFlow.finalTarget")}
            value={finalModel || "-"}
            detail={finalProvider || "-"}
            iconClass="text-emerald-600 dark:text-emerald-400"
          />
        </div>
      </div>

      <details className="group border-t border-border">
        <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-2 text-xs text-muted-foreground outline-none transition hover:bg-muted/30 hover:text-foreground focus-visible:bg-muted/30 focus-visible:text-foreground [&::-webkit-details-marker]:hidden">
          <span className="flex items-center gap-2">
            <Activity className="h-3.5 w-3.5" />
            {t("multimodalFlow.technicalDetails")}
          </span>
          <ChevronDown className="h-3.5 w-3.5 transition group-open:rotate-180" />
        </summary>
        <dl className="grid gap-x-6 gap-y-3 border-t border-border bg-muted/15 px-4 py-3 sm:grid-cols-2 lg:grid-cols-4">
          <Detail label={t("multimodalFlow.requestId")} value={event.request_id} mono />
          <Detail label={t("multimodalFlow.protocol")} value={`${event.protocol_in} → ${event.protocol_out}`} mono />
          <Detail label={t("multimodalFlow.originalTargetLabel")} value={`${flow?.original_provider || "-"} / ${originalModel}`} mono />
          <Detail label={t("multimodalFlow.effectiveTarget")} value={`${finalProvider || "-"} / ${finalModel || "-"}`} mono />
          <Detail label={t("multimodalFlow.capabilitySource")} value={flow?.capability_source} mono />
          <Detail label={t("multimodalFlow.catalogMatch")} value={flow?.catalog_match} mono />
          <Detail label={t("multimodalFlow.ocrProviderLabel")} value={flow?.ocr_provider} mono />
          <Detail label={t("multimodalFlow.ocrLatency")} value={flow?.ocr_latency_ms != null ? `${flow.ocr_latency_ms}ms` : null} />
          <Detail label={t("multimodalFlow.cacheHits")} value={flow?.ocr_cache_hits} />
          <Detail
            label={t("multimodalFlow.reportedConfidence")}
            value={flow?.ocr_min_confidence != null ? flow.ocr_min_confidence.toFixed(2) : null}
          />
          <Detail label={t("multimodalFlow.routeReason")} value={flow?.route_reason} mono />
          <Detail label={t("multimodalFlow.errorCode")} value={flow?.ocr_failure_code || event.error_code} mono />
        </dl>
      </details>
    </section>
  )
}
