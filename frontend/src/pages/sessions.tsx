import { FormEvent, useId, useMemo, useState } from "react"
import { ArrowLeft, ChevronRight, MessagesSquare, Search } from "lucide-react"
import { Link, useParams } from "react-router"
import { useTranslation } from "react-i18next"
import { ObservabilityNav } from "@/components/observability-nav"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { EmptyState } from "@/components/empty-state"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useObservabilitySessions, useSessionDetails } from "@/hooks/use-requests"
import type { SessionQueryFilters } from "@/lib/types"

export function SessionsPage() {
  const { sessionId } = useParams()
  return sessionId ? <SessionReplay sessionId={sessionId} /> : <SessionList />
}

function SessionList() {
  const { t, i18n } = useTranslation()
  const [filters, setFilters] = useState<SessionQueryFilters>({})
  const [draft, setDraft] = useState<SessionQueryFilters>({})
  const query = useObservabilitySessions(filters)
  const sessions = useMemo(() => query.data?.pages.flatMap((page) => page.items) ?? [], [query.data])

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setFilters(draft)
  }

  return (
    <div className="space-y-6">
      <header className="space-y-4">
        <div>
          <h1 className="text-2xl font-semibold">{t("observability.sessions.title")}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t("observability.sessions.description")}</p>
        </div>
        <ObservabilityNav />
      </header>

      <Card>
        <CardContent className="p-4">
          <form onSubmit={submit} className="grid gap-3 md:grid-cols-2 xl:grid-cols-6 xl:items-end">
            <SessionFilter label={t("observability.filters.search")} value={draft.q} onChange={(q) => setDraft((value) => ({ ...value, q }))} icon />
            <SessionFilter label={t("observability.filters.session")} value={draft.session_id} onChange={(session_id) => setDraft((value) => ({ ...value, session_id }))} />
            <SessionFilter label={t("observability.filters.agent")} value={draft.agent_id} onChange={(agent_id) => setDraft((value) => ({ ...value, agent_id }))} />
            <SessionFilter label={t("observability.filters.principal")} value={draft.principal_name} onChange={(principal_name) => setDraft((value) => ({ ...value, principal_name }))} />
            <SessionFilter label={t("observability.filters.project")} value={draft.project_id} onChange={(project_id) => setDraft((value) => ({ ...value, project_id }))} />
            <div className="flex gap-2">
              <Button type="submit" className="flex-1">{t("observability.filters.apply")}</Button>
              <Button type="button" variant="outline" onClick={() => { setDraft({}); setFilters({}) }}>{t("observability.filters.reset")}</Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle className="text-base">{t("observability.sessions.results", { count: sessions.length })}</CardTitle></CardHeader>
        <CardContent>
          {query.isPending ? (
            <div className="space-y-2">{Array.from({ length: 5 }).map((_, index) => <Skeleton key={index} className="h-12 w-full" />)}</div>
          ) : query.isError ? (
            <EmptyState title={t("observability.loadFailed")} description={query.error.message} />
          ) : sessions.length === 0 ? (
            <EmptyState title={t("observability.sessions.empty")} description={t("observability.sessions.emptyDescription")} icon={<MessagesSquare className="h-8 w-8" />} />
          ) : (
            <div className="space-y-4">
              <Table>
                <TableHeader><TableRow>
                  <TableHead>{t("observability.columns.session")}</TableHead>
                  <TableHead>{t("observability.columns.agent")}</TableHead>
                  <TableHead>{t("observability.columns.principal")}</TableHead>
                  <TableHead className="text-right">{t("observability.columns.requests")}</TableHead>
                  <TableHead className="text-right">{t("observability.columns.errors")}</TableHead>
                  <TableHead className="text-right">{t("observability.columns.tokens")}</TableHead>
                  <TableHead>{t("observability.columns.updated")}</TableHead>
                  <TableHead className="w-8" />
                </TableRow></TableHeader>
                <TableBody>
                  {sessions.map((session) => (
                    <TableRow key={session.session_id}>
                      <TableCell>
                        <Link className="block min-w-48" to={`/observability/sessions/${encodeURIComponent(session.session_id)}`}>
                          <p className="font-medium hover:text-primary">{session.session_name || session.session_id}</p>
                          {session.session_name && <p className="font-mono text-xs text-muted-foreground">{session.session_id}</p>}
                        </Link>
                      </TableCell>
                      <TableCell>{session.agent_name || session.agent_id || t("observability.unclassified")}</TableCell>
                      <TableCell>{session.principal_name || "—"}</TableCell>
                      <TableCell className="text-right tabular-nums">{session.request_count}</TableCell>
                      <TableCell className="text-right tabular-nums">{session.error_count || "—"}</TableCell>
                      <TableCell className="text-right tabular-nums">{session.total_tokens}</TableCell>
                      <TableCell className="whitespace-nowrap text-xs text-muted-foreground">{new Date(session.updated_at).toLocaleString(i18n.language)}</TableCell>
                      <TableCell><Link aria-label={t("observability.sessions.open")} to={`/observability/sessions/${encodeURIComponent(session.session_id)}`}><ChevronRight className="h-4 w-4" /></Link></TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {query.hasNextPage && <div className="flex justify-center border-t border-border pt-4"><Button variant="outline" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>{query.isFetchingNextPage ? t("common.loading") : t("observability.loadMore")}</Button></div>}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}

function SessionReplay({ sessionId }: { sessionId: string }) {
  const { t, i18n } = useTranslation()
  const query = useSessionDetails(sessionId)

  if (query.isPending) return <div className="space-y-4"><Skeleton className="h-9 w-72" /><Skeleton className="h-[420px] w-full" /></div>
  if (query.isError) return <EmptyState title={t("observability.loadFailed")} description={query.error.message} />

  const { session, requests } = query.data
  return (
    <div className="space-y-6">
      <header className="space-y-4">
        <Button asChild variant="ghost" className="w-fit px-2"><Link to="/observability/sessions"><ArrowLeft className="h-4 w-4" />{t("observability.sessions.back")}</Link></Button>
        <div>
          <h1 className="text-2xl font-semibold">{session.session_name || session.session_id}</h1>
          <p className="mt-1 font-mono text-xs text-muted-foreground">{session.session_id}</p>
        </div>
        <ObservabilityNav />
      </header>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <SessionStat label={t("observability.columns.agent")} value={session.agent_name || session.agent_id || t("observability.unclassified")} />
        <SessionStat label={t("observability.columns.principal")} value={session.principal_name || "—"} />
        <SessionStat label={t("observability.columns.requests")} value={String(session.request_count)} />
        <SessionStat label={t("observability.columns.tokens")} value={session.total_tokens.toLocaleString(i18n.language)} />
      </div>

      <Card>
        <CardHeader><CardTitle className="text-base">{t("observability.sessions.replay")}</CardTitle></CardHeader>
        <CardContent>
          <ol className="relative ml-3 border-l border-border">
            {requests.map((request, index) => (
              <li key={request.request_id} className="relative pb-6 pl-7 last:pb-0">
                <span className="absolute -left-3 flex h-6 w-6 items-center justify-center rounded-full border border-border bg-background text-xs font-semibold text-muted-foreground">{index + 1}</span>
                <Link to={`/observability/requests/${encodeURIComponent(request.request_id)}`} className="block rounded-xl border border-border p-4 transition-colors hover:border-primary/50 hover:bg-accent/40">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div>
                      <p className="font-mono text-sm">{request.virtual_model || request.upstream_model || "—"}</p>
                      <p className="mt-1 text-xs text-muted-foreground">{new Date(request.started_at).toLocaleString(i18n.language)} · {request.channel_id || "—"}</p>
                    </div>
                    <div className="flex items-center gap-2"><Badge variant={request.status_code >= 400 ? "destructive" : "success"}>HTTP {request.status_code}</Badge><span className="text-xs tabular-nums text-muted-foreground">{request.duration_ms ?? 0} ms</span></div>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
                    <span>{t("observability.columns.agent")}: {request.agent_name || request.agent_id || t("observability.unclassified")}</span>
                    <span>{t("observability.columns.principal")}: {request.principal_name || request.client_name || "—"}</span>
                    {request.session_path && <span>{t("observability.columns.step")}: {request.session_path}</span>}
                  </div>
                </Link>
              </li>
            ))}
          </ol>
        </CardContent>
      </Card>
    </div>
  )
}

function SessionFilter({ label, value, onChange, icon = false }: { label: string; value?: string; onChange: (value: string) => void; icon?: boolean }) {
  const id = useId()
  return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label><div className="relative">{icon && <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />}<Input id={id} className={icon ? "pl-9" : undefined} value={value ?? ""} onChange={(event) => onChange(event.target.value)} /></div></div>
}

function SessionStat({ label, value }: { label: string; value: string }) {
  return <Card><CardContent className="p-4"><p className="text-xs text-muted-foreground">{label}</p><p className="mt-1 truncate font-medium">{value}</p></CardContent></Card>
}
