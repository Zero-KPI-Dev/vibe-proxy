import { useMemo, useState } from "react"
import { ListTree } from "lucide-react"
import { useTranslation } from "react-i18next"
import { ObservabilityNav } from "@/components/observability-nav"
import { RequestFilters } from "@/components/request-filters"
import { RequestTable } from "@/components/request-table"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { EmptyState } from "@/components/empty-state"
import { Skeleton } from "@/components/ui/skeleton"
import { useObservabilityRequests } from "@/hooks/use-requests"
import type { RequestQueryFilters } from "@/lib/types"

export function RequestsPage() {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<RequestQueryFilters>({})
  const query = useObservabilityRequests(filters)
  const events = useMemo(() => query.data?.pages.flatMap((page) => page.items) ?? [], [query.data])

  return (
    <div className="space-y-6">
      <header className="space-y-4">
        <div>
          <h1 className="text-2xl font-semibold">{t("observability.requests.title")}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{t("observability.requests.description")}</p>
        </div>
        <ObservabilityNav />
      </header>

      <RequestFilters value={filters} onChange={setFilters} />

      <Card>
        <CardHeader className="flex-row items-center justify-between space-y-0">
          <CardTitle className="text-base">{t("observability.requests.results", { count: events.length })}</CardTitle>
        </CardHeader>
        <CardContent>
          {query.isPending ? (
            <div className="space-y-2">{Array.from({ length: 6 }).map((_, index) => <Skeleton key={index} className="h-12 w-full" />)}</div>
          ) : query.isError ? (
            <EmptyState title={t("observability.loadFailed")} description={query.error.message} icon={<ListTree className="h-8 w-8" />} />
          ) : events.length === 0 ? (
            <EmptyState title={t("observability.requests.empty")} description={t("observability.requests.emptyDescription")} icon={<ListTree className="h-8 w-8" />} />
          ) : (
            <div className="space-y-4">
              <RequestTable events={events} openDetails />
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
