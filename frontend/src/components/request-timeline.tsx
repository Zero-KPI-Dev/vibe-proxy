import { Braces, CheckCircle2, CircleDot, XCircle } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Badge } from "@/components/ui/badge"
import type { RequestEvent, TraceObservation } from "@/lib/types"

export function RequestTimeline({ request, observations }: { request: RequestEvent; observations: TraceObservation[] }) {
  const { t, i18n } = useTranslation()
  const ordered = [...observations].sort((left, right) => new Date(left.started_at).getTime() - new Date(right.started_at).getTime())

  if (ordered.length === 0) {
    return <div className="rounded-xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">{t("observability.timeline.empty")}</div>
  }

  return (
    <ol className="relative ml-3 border-l border-border">
      <TimelineItem
        name={t("observability.timeline.request")}
        type={request.protocol_in}
        status={request.status_code >= 400 ? "error" : "ok"}
        startedAt={request.started_at}
        duration={request.duration_ms ?? 0}
      />
      {ordered.map((observation) => (
        <TimelineItem
          key={observation.observation_id}
          name={observation.name}
          type={observation.type}
          status={observation.status}
          startedAt={observation.started_at}
          duration={observation.duration_ms}
          error={observation.error_code}
          attributes={observation.attributes}
        />
      ))}
      <li className="relative pl-8">
        <span className="absolute -left-2 top-0 h-4 w-4 rounded-full border-2 border-background bg-primary" />
        <p className="text-sm font-medium">{t("observability.timeline.completed")}</p>
        <p className="text-xs text-muted-foreground">{request.completed_at ? new Date(request.completed_at).toLocaleString(i18n.language) : t("observability.timeline.inProgress")}</p>
      </li>
    </ol>
  )
}

function TimelineItem({
  name,
  type,
  status,
  startedAt,
  duration,
  error,
  attributes,
}: {
  name: string
  type: string
  status: string
  startedAt: string
  duration: number
  error?: string
  attributes?: Record<string, unknown>
}) {
  const { i18n } = useTranslation()
  const failed = status === "error" || status === "failed"
  const running = status === "in_progress"
  const Icon = failed ? XCircle : running ? CircleDot : CheckCircle2

  return (
    <li className="relative pb-6 pl-8">
      <span className="absolute -left-3 top-0 flex h-6 w-6 items-center justify-center rounded-full border border-border bg-background">
        <Icon className={failed ? "h-4 w-4 text-destructive" : running ? "h-4 w-4 text-yellow-400" : "h-4 w-4 text-green-400"} />
      </span>
      <div className="rounded-xl border border-border p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div><p className="font-medium">{name}</p><Badge variant="outline" className="mt-1">{type}</Badge></div>
          <div className="text-right text-xs text-muted-foreground"><p>{duration} ms</p><p>{new Date(startedAt).toLocaleString(i18n.language)}</p></div>
        </div>
        {error && <p className="mt-3 font-mono text-xs text-destructive">{error}</p>}
        {attributes && Object.keys(attributes).length > 0 && (
          <details className="mt-3">
            <summary className="flex cursor-pointer items-center gap-2 text-xs text-muted-foreground"><Braces className="h-3.5 w-3.5" />attributes</summary>
            <pre className="mt-2 max-h-56 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-muted p-3 text-xs">{JSON.stringify(attributes, null, 2)}</pre>
          </details>
        )}
      </div>
    </li>
  )
}
