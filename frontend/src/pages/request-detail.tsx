import type { ReactNode } from "react"
import { ArrowLeft, Clock3, Database, Route, ShieldCheck, Trash2 } from "lucide-react"
import { Link, useParams } from "react-router"
import { useTranslation } from "react-i18next"
import { CaptureStateBadge } from "@/components/capture-state-badge"
import { ObservabilityNav } from "@/components/observability-nav"
import { RequestDiff } from "@/components/request-diff"
import { RequestTimeline } from "@/components/request-timeline"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { EmptyState } from "@/components/empty-state"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useDeleteRequestContent, useRequestDetails, useSessionDetails } from "@/hooks/use-requests"
import type { CaptureStatus, PayloadContent, RequestEvent } from "@/lib/types"

const requestStages = ["client_request", "canonical_request"] as const
const responseStages = ["canonical_response"] as const

function processingStagesFor(request: RequestEvent): PayloadContent["stage"][] {
  const stages: PayloadContent["stage"][] = []
  if (request.transformation?.ocr_provider) stages.push("ocr_request", "ocr_response")
  if (request.transformation?.vision_strategy === "assist") stages.push("vision_request", "vision_response")
  stages.push("effective_canonical_request", "upstream_request")
  return stages
}

export function RequestDetailPage() {
  const { requestId = "" } = useParams()
  const { t, i18n } = useTranslation()
  const details = useRequestDetails(requestId)
  const sessionId = details.data?.request.session_id ?? ""
  const session = useSessionDetails(sessionId)
  const deletion = useDeleteRequestContent(requestId)

  if (details.isPending) return <div className="space-y-4"><Skeleton className="h-10 w-80" /><Skeleton className="h-[520px] w-full" /></div>
  if (details.isError) return <EmptyState title={t("observability.loadFailed")} description={details.error.message} />

  const { request, observations, payloads } = details.data
  const candidates = session.data?.requests ?? []
  const deleteContent = () => {
    if (window.confirm(t("observability.detail.deleteConfirm"))) deletion.mutate()
  }

  return (
    <div className="space-y-6">
      <header className="space-y-4">
        <Button asChild variant="ghost" className="w-fit px-2"><Link to="/observability/requests"><ArrowLeft className="h-4 w-4" />{t("observability.detail.back")}</Link></Button>
        <div className="flex flex-col justify-between gap-3 lg:flex-row lg:items-start">
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-2xl font-semibold">{t("observability.detail.title")}</h1>
              <Badge variant={request.status_code >= 400 ? "destructive" : "success"}>HTTP {request.status_code}</Badge>
              <CaptureStateBadge state={request.capture_status || "not_captured"} />
            </div>
            <p className="mt-2 font-mono text-xs text-muted-foreground">{request.request_id}</p>
          </div>
          <Button variant="destructive" disabled={deletion.isPending || request.capture_status === "expired"} onClick={deleteContent}>
            <Trash2 className="h-4 w-4" />{deletion.isPending ? t("common.loading") : t("observability.detail.deleteContent")}
          </Button>
        </div>
        <ObservabilityNav />
      </header>

      <Tabs defaultValue="summary" className="space-y-4">
        <TabsList className="flex h-auto w-full justify-start overflow-x-auto">
          <TabsTrigger value="summary">{t("observability.detail.tabs.summary")}</TabsTrigger>
          <TabsTrigger value="timeline">{t("observability.detail.tabs.timeline")}</TabsTrigger>
          <TabsTrigger value="request">{t("observability.detail.tabs.request")}</TabsTrigger>
          <TabsTrigger value="processing">{t("observability.detail.tabs.processing")}</TabsTrigger>
          <TabsTrigger value="response">{t("observability.detail.tabs.response")}</TabsTrigger>
          <TabsTrigger value="changes">{t("observability.detail.tabs.changes")}</TabsTrigger>
          <TabsTrigger value="metadata">{t("observability.detail.tabs.metadata")}</TabsTrigger>
        </TabsList>

        <TabsContent value="summary"><RequestSummary request={request} /></TabsContent>
        <TabsContent value="timeline"><Card><CardContent className="p-6"><RequestTimeline request={request} observations={observations} /></CardContent></Card></TabsContent>
        <TabsContent value="request"><PayloadStages payloads={payloads} stages={requestStages} /></TabsContent>
        <TabsContent value="processing"><PayloadStages payloads={payloads} stages={processingStagesFor(request)} /></TabsContent>
        <TabsContent value="response"><PayloadStages payloads={payloads} stages={responseStages} /></TabsContent>
        <TabsContent value="changes"><Card><CardContent className="p-6"><RequestDiff requestId={request.request_id} candidates={candidates} /></CardContent></Card></TabsContent>
        <TabsContent value="metadata"><Metadata request={request} locale={i18n.language} /></TabsContent>
      </Tabs>
    </div>
  )
}

function RequestSummary({ request }: { request: RequestEvent }) {
  const { t, i18n } = useTranslation()
  const shape = request.request_shape ?? {
    input_message_count: 0, input_block_count: 0, input_tool_count: 0, input_image_count: 0, input_text_chars: 0,
    output_message_count: 0, output_block_count: 0, output_tool_call_count: 0, output_reasoning_chars: 0, output_text_chars: 0,
  }
  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <SummaryCard icon={<Clock3 />} label={t("observability.detail.duration")} value={`${request.duration_ms ?? 0} ms`} detail={`TTFT ${request.ttft_ms ?? 0} ms`} />
        <SummaryCard icon={<Route />} label={t("observability.detail.route")} value={`${request.channel_id || "—"} / ${request.upstream_model || "—"}`} detail={request.virtual_model || "—"} />
        <SummaryCard icon={<ShieldCheck />} label={t("observability.detail.identity")} value={request.agent_name || request.agent_id || t("observability.unclassified")} detail={`${t("observability.columns.principal")}: ${request.principal_name || request.client_name || "—"}`} />
        <SummaryCard icon={<Database />} label={t("observability.columns.tokens")} value={(request.usage?.total_tokens ?? 0).toLocaleString(i18n.language)} detail={`${request.usage?.prompt_tokens ?? 0} → ${request.usage?.completion_tokens ?? 0}`} />
      </div>
      <Card>
        <CardHeader><CardTitle className="text-base">{t("observability.detail.shape")}</CardTitle></CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Shape label={t("observability.detail.inputMessages")} value={shape.input_message_count} />
          <Shape label={t("observability.detail.inputTools")} value={shape.input_tool_count} />
          <Shape label={t("observability.detail.inputImages")} value={shape.input_image_count} />
          <Shape label={t("observability.detail.inputChars")} value={shape.input_text_chars} />
          <Shape label={t("observability.detail.outputMessages")} value={shape.output_message_count} />
          <Shape label={t("observability.detail.outputTools")} value={shape.output_tool_call_count} />
          <Shape label={t("observability.detail.outputReasoning")} value={shape.output_reasoning_chars} />
          <Shape label={t("observability.detail.outputChars")} value={shape.output_text_chars} />
        </CardContent>
      </Card>
    </div>
  )
}

function PayloadStages({ payloads, stages }: { payloads: PayloadContent[]; stages: readonly PayloadContent["stage"][] }) {
  const byStage = new Map(payloads.map((payload) => [payload.stage, payload]))
  return <div className="space-y-4">{stages.map((stage) => <PayloadPanel key={stage} payload={byStage.get(stage) ?? { stage, state: "missing" }} />)}</div>
}

function PayloadPanel({ payload }: { payload: PayloadContent }) {
  const { t } = useTranslation()
  const available = ["captured", "redacted", "truncated"].includes(payload.state) && payload.body !== undefined
  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between space-y-0 gap-3">
        <div><CardTitle className="text-base">{t(`observability.payloadStages.${payload.stage}`)}</CardTitle><p className="mt-1 text-xs text-muted-foreground">{payload.capture_mode || "—"} · {payload.media_type || "application/json"}</p></div>
        <CaptureStateBadge state={payload.state as CaptureStatus} />
      </CardHeader>
      <CardContent>
        {available ? (
          <pre className="max-h-[34rem] overflow-auto whitespace-pre-wrap break-words rounded-xl border border-border bg-muted/40 p-4 font-mono text-xs leading-5">{JSON.stringify(payload.body, null, 2)}</pre>
        ) : (
          <div className="rounded-xl border border-dashed border-border p-8 text-center"><p className="text-sm font-medium">{t(`observability.captureStates.${payload.state}`)}</p><p className="mt-1 text-xs text-muted-foreground">{payload.error || t(`observability.captureDescriptions.${payload.state}`)}</p></div>
        )}
        <div className="mt-3 flex flex-wrap gap-x-5 gap-y-1 text-xs text-muted-foreground">
          {payload.original_bytes != null && <span>{t("observability.detail.originalBytes")}: {payload.original_bytes}</span>}
          {payload.stored_bytes != null && <span>{t("observability.detail.storedBytes")}: {payload.stored_bytes}</span>}
          {payload.redaction_count != null && <span>{t("observability.detail.redactions")}: {payload.redaction_count}</span>}
          {payload.truncation_reason && <span>{t("observability.detail.truncationReason")}: {payload.truncation_reason}</span>}
        </div>
      </CardContent>
    </Card>
  )
}

function Metadata({ request, locale }: { request: RequestEvent; locale: string }) {
  const { t } = useTranslation()
  const entries = [
    ["request_id", request.request_id], ["trace_id", request.trace_id], ["span_id", request.span_id],
    ["parent_span_id", request.parent_span_id], ["parent_request_id", request.parent_request_id],
    ["session_id", request.session_id], ["session_name", request.session_name], ["session_kind", request.session_kind],
    ["session_path", request.session_path], ["project_id", request.project_id], ["agent_id", request.agent_id],
    ["agent_name", request.agent_name], ["agent_version", request.agent_version], ["agent_source", request.agent_source],
    ["agent_confidence", request.agent_confidence], ["principal_name", request.principal_name || request.client_name],
    ["http", `${request.http_method || ""} ${request.http_path || ""}`.trim()], ["protocol", `${request.protocol_in || "—"} → ${request.protocol_out || "—"}`],
    ["started_at", new Date(request.started_at).toLocaleString(locale)], ["completed_at", request.completed_at ? new Date(request.completed_at).toLocaleString(locale) : ""],
    ["finish_reason", request.finish_reason], ["upstream_request_id", request.upstream_request_id], ["error_code", request.error_code],
  ].filter((entry) => entry[1])
  return <Card><CardHeader><CardTitle className="text-base">{t("observability.detail.metadata")}</CardTitle></CardHeader><CardContent><dl className="divide-y divide-border">{entries.map(([label, value]) => <div key={label} className="grid gap-1 py-3 sm:grid-cols-[12rem_1fr]"><dt className="font-mono text-xs text-muted-foreground">{label}</dt><dd className="break-all text-sm">{value}</dd></div>)}</dl></CardContent></Card>
}

function SummaryCard({ icon, label, value, detail }: { icon: ReactNode; label: string; value: string; detail: string }) {
  return <Card><CardContent className="flex items-start gap-3 p-4"><span className="rounded-lg bg-primary/10 p-2 text-primary [&_svg]:h-4 [&_svg]:w-4">{icon}</span><div className="min-w-0"><p className="text-xs text-muted-foreground">{label}</p><p className="mt-1 truncate font-medium">{value}</p><p className="mt-1 truncate text-xs text-muted-foreground">{detail}</p></div></CardContent></Card>
}

function Shape({ label, value }: { label: string; value: number }) {
  return <div className="rounded-lg border border-border p-3"><p className="text-xs text-muted-foreground">{label}</p><p className="mt-1 text-lg font-semibold tabular-nums">{value}</p></div>
}
