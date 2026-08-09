import { useMemo, useState } from "react"
import { CirclePause, ListTree, Play, Radio, WifiOff } from "lucide-react"
import { useTranslation } from "react-i18next"
import { ObservabilityNav } from "@/components/observability-nav"
import { RequestFilters } from "@/components/request-filters"
import { RequestTable } from "@/components/request-table"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { EmptyState } from "@/components/empty-state"
import { Skeleton } from "@/components/ui/skeleton"
import { useObservabilityRequests } from "@/hooks/use-requests"
import { useLiveRequests } from "@/hooks/use-live-requests"
import type { LiveRequestEvent, RequestEvent, RequestQueryFilters } from "@/lib/types"

function matchesFilters(event: RequestEvent, filters: RequestQueryFilters) {
  if (filters.agent_id && event.agent_id !== filters.agent_id) return false
  if (filters.principal_name && event.principal_name !== filters.principal_name) return false
  if (filters.session_id && event.session_id !== filters.session_id) return false
  if (filters.project_id && event.project_id !== filters.project_id) return false
  if (filters.model && event.virtual_model !== filters.model && event.upstream_model !== filters.model) return false
  if (filters.provider && event.channel_id !== filters.provider) return false
  if (filters.protocol && event.protocol_in !== filters.protocol && event.protocol_out !== filters.protocol) return false
  if (filters.status_class && Math.floor((event.status_code || 0) / 100) !== Number(filters.status_class)) return false
  if (filters.capture_status && event.capture_status !== filters.capture_status) return false
  if (filters.from && new Date(event.started_at) < new Date(filters.from)) return false
  if (filters.to && new Date(event.started_at) > new Date(filters.to)) return false
  if (filters.q) {
    const haystack = [
      event.request_id, event.trace_id, event.session_id, event.agent_id, event.agent_name,
      event.principal_name, event.client_key_prefix, event.virtual_model, event.upstream_model, event.channel_id,
    ].filter(Boolean).join(" ").toLowerCase()
    if (!haystack.includes(filters.q.toLowerCase())) return false
  }
  return true
}

export function RequestsPage() {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<RequestQueryFilters>({})
  const query = useObservabilityRequests(filters)
  const live = useLiveRequests()
  const durableEvents = useMemo(() => query.data?.pages.flatMap((page) => page.items) ?? [], [query.data])
  const events = useMemo(() => {
    const merged = new Map<string, RequestEvent>()
    for (const event of durableEvents) merged.set(event.request_id, event)
    for (const liveEvent of live.events.values()) {
      if (matchesFilters(liveEvent.request, filters)) merged.set(liveEvent.request.request_id, liveEvent.request)
    }
    return [...merged.values()].sort((left, right) => {
      const leftActive = !left.completed_at
      const rightActive = !right.completed_at
      if (leftActive !== rightActive) return leftActive ? -1 : 1
      return new Date(right.started_at).getTime() - new Date(left.started_at).getTime()
    })
  }, [durableEvents, filters, live.events])
  const liveByRequest = useMemo(() => {
    const result = new Map<string, LiveRequestEvent>()
    for (const event of live.events.values()) result.set(event.request.request_id, event)
    return result
  }, [live.events])
  const activeCount = events.filter((event) => !event.completed_at).length

  return (
    <div className="space-y-6">
      <header className="space-y-4">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <h1 className="text-2xl font-semibold">{t("observability.requests.title")}</h1>
            <p className="mt-1 text-sm text-muted-foreground">{t("observability.requests.description")}</p>
          </div>
          <div className="flex items-center gap-2 self-start rounded-xl border border-border bg-card px-3 py-2 shadow-sm">
            <span className="relative flex h-2.5 w-2.5">
              {live.state === "live" && <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-60" />}
              <span className={`relative inline-flex h-2.5 w-2.5 rounded-full ${live.state === "live" ? "bg-emerald-500" : live.state === "paused" ? "bg-amber-500" : "bg-muted-foreground"}`} />
            </span>
            <div className="min-w-28">
              <p className="text-sm font-medium">{t(`observability.live.${live.state}`)}</p>
              <p className="text-xs text-muted-foreground">
                {live.pendingCount > 0 ? t("observability.live.pending", { count: live.pendingCount }) : t("observability.live.active", { count: activeCount })}
              </p>
            </div>
            {live.state === "paused" ? (
              <Button size="sm" variant="outline" onClick={live.resume}><Play className="h-3.5 w-3.5" />{t("observability.live.resume")}</Button>
            ) : (
              <Button size="sm" variant="outline" onClick={live.pause} disabled={live.state !== "live"}>
                {live.state === "error" ? <WifiOff className="h-3.5 w-3.5" /> : <CirclePause className="h-3.5 w-3.5" />}
                {t("observability.live.pause")}
              </Button>
            )}
          </div>
        </div>
        <ObservabilityNav />
      </header>

      <RequestFilters value={filters} onChange={setFilters} />

      <Card>
        <CardHeader className="flex-row items-center justify-between space-y-0">
          <CardTitle className="flex items-center gap-2 text-base">
            <Radio className="h-4 w-4 text-primary" />
            {t("observability.requests.results", { count: events.length })}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {query.isPending && events.length === 0 ? (
            <div className="space-y-2">{Array.from({ length: 6 }).map((_, index) => <Skeleton key={index} className="h-12 w-full" />)}</div>
          ) : query.isError && events.length === 0 ? (
            <EmptyState title={t("observability.loadFailed")} description={query.error.message} icon={<ListTree className="h-8 w-8" />} />
          ) : events.length === 0 ? (
            <EmptyState title={t("observability.requests.empty")} description={t("observability.requests.emptyDescription")} icon={<ListTree className="h-8 w-8" />} />
          ) : (
            <div className="space-y-4">
              <RequestTable events={events} liveByRequest={liveByRequest} openDetails />
              {query.hasNextPage && (
                <div className="flex justify-center border-t border-border pt-4">
                  <Button variant="outline" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
                    {query.isFetchingNextPage ? t("common.loading") : t("observability.loadMore")}
                  </Button>
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
