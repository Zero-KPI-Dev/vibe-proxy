import * as React from "react"
import { GitCompareArrows, Minus, Plus, RefreshCw, Shrink } from "lucide-react"
import { useTranslation } from "react-i18next"
import { CaptureStateBadge } from "@/components/capture-state-badge"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { useRequestDiff } from "@/hooks/use-requests"
import type { CaptureStatus, RequestEvent, ValueChange } from "@/lib/types"

export function RequestDiff({ requestId, candidates }: { requestId: string; candidates: RequestEvent[] }) {
  const { t } = useTranslation()
  const [base_id, setBaseId] = React.useState("")
  const query = useRequestDiff(requestId, base_id)
  const selectable = candidates.filter((candidate) => candidate.request_id !== requestId)

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <p className="text-sm font-medium">{t("observability.diff.base")}</p>
          <p className="text-xs text-muted-foreground">{t("observability.diff.baseHelp")}</p>
        </div>
        <Select value={base_id || "automatic"} onValueChange={(value) => setBaseId(value === "automatic" ? "" : value)}>
          <SelectTrigger className="sm:w-80"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="automatic">{t("observability.diff.automatic")}</SelectItem>
            {selectable.map((candidate) => (
              <SelectItem key={candidate.request_id} value={candidate.request_id}>
                {candidate.request_id} · {candidate.virtual_model || candidate.upstream_model}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {query.isPending ? <Skeleton className="h-64 w-full" /> : query.isError ? (
        <p className="rounded-xl border border-destructive/40 bg-destructive/10 p-4 text-sm text-destructive">{query.error.message}</p>
      ) : query.data.state === "no_base" ? (
        <EmptyDiff text={t("observability.diff.noBase")} />
      ) : query.data.state !== "available" ? (
        <div className="rounded-xl border border-border p-6 text-center">
          <CaptureStateBadge state={query.data.state as CaptureStatus} />
          <p className="mt-3 text-sm text-muted-foreground">{t("observability.diff.unavailable", { side: query.data.unavailable_side || "—" })}</p>
        </div>
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <Badge variant="outline">{t(`observability.diff.selection.${query.data.base_selection || "previous"}`)}</Badge>
            <span className="font-mono">{query.data.base_request_id}</span>
            {query.data.append_only && <Badge variant="success">{t("observability.diff.appendOnly")}</Badge>}
            {query.data.context_shrunk && <Badge variant="warning">{t("observability.diff.contextShrunk")}</Badge>}
          </div>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <DiffMetric icon={<Plus />} label={t("observability.diff.added")} value={query.data.messages_added} />
            <DiffMetric icon={<Minus />} label={t("observability.diff.removed")} value={query.data.messages_removed} />
            <DiffMetric icon={<RefreshCw />} label={t("observability.diff.rewritten")} value={query.data.messages_rewritten} />
            <DiffMetric icon={<Shrink />} label={t("observability.diff.commonPrefix")} value={query.data.common_prefix_messages} />
          </div>
          <div className="grid gap-3 lg:grid-cols-3">
            <ValueChangeCard label={t("observability.diff.requestedModel")} change={query.data.requested_model_change} />
            <ValueChangeCard label={t("observability.diff.effectiveModel")} change={query.data.model_change} />
            <ValueChangeCard label={t("observability.diff.provider")} change={query.data.provider_change} />
          </div>
          {query.data.tool_changes.length > 0 && (
            <Card><CardContent className="p-4"><p className="mb-3 text-sm font-medium">{t("observability.diff.tools")}</p><div className="space-y-2">{query.data.tool_changes.map((change) => <div key={`${change.kind}-${change.path}`} className="rounded-lg border border-border p-3 text-xs"><Badge variant="outline">{change.kind}</Badge><p className="mt-2 font-mono">{change.path}</p></div>)}</div></CardContent></Card>
          )}
        </>
      )}
    </div>
  )
}

function DiffMetric({ icon, label, value }: { icon: React.ReactNode; label: string; value: number }) {
  return <Card><CardContent className="flex items-center gap-3 p-4"><span className="text-primary [&_svg]:h-4 [&_svg]:w-4">{icon}</span><div><p className="text-xs text-muted-foreground">{label}</p><p className="text-xl font-semibold tabular-nums">{value}</p></div></CardContent></Card>
}

function ValueChangeCard({ label, change }: { label: string; change?: ValueChange }) {
  const { t } = useTranslation()
  return <Card><CardContent className="p-4"><p className="text-xs font-medium text-muted-foreground">{label}</p>{change ? <div className="mt-3 space-y-2 font-mono text-xs"><p className="rounded bg-red-500/10 p-2 text-red-400">− {change.before || "∅"}</p><p className="rounded bg-green-500/10 p-2 text-green-400">+ {change.after || "∅"}</p></div> : <p className="mt-3 text-sm text-muted-foreground">{t("observability.diff.unchanged")}</p>}</CardContent></Card>
}

function EmptyDiff({ text }: { text: string }) {
  return <div className="rounded-xl border border-dashed border-border p-10 text-center"><GitCompareArrows className="mx-auto h-8 w-8 text-muted-foreground" /><p className="mt-3 text-sm text-muted-foreground">{text}</p></div>
}
