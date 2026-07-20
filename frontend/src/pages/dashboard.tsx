import { useState } from "react"
import { Activity, Server, Brain, Database, Copy, Check, KeyRound } from "lucide-react"
import { StatsCard } from "@/components/stats-card"
import { HealthBadge } from "@/components/health-badge"
import { RequestTable } from "@/components/request-table"
import { EmptyState } from "@/components/empty-state"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useProviders } from "@/hooks/use-providers"
import { useRecentRequests } from "@/hooks/use-requests"
import { useMetricsSummary } from "@/hooks/use-metrics"
import { useProviderHealth } from "@/hooks/use-metrics"
import { useClientKeys } from "@/hooks/use-client-keys"
import { cn } from "@/lib/utils"
import { useTranslation } from "react-i18next"

function CopyBox({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = () => {
    navigator.clipboard.writeText(value)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <div className="space-y-1">
      <p className="text-xs text-muted-foreground">{label}</p>
      <div className="flex items-center gap-2 rounded-lg bg-surface-2 border border-border px-3 py-2 group">
        <code className="flex-1 text-sm font-mono truncate">{value}</code>
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity"
          onClick={handleCopy}
        >
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
        </Button>
      </div>
    </div>
  )
}

export function DashboardPage() {
  const { t } = useTranslation()
  const { data: snapshot, isLoading: loadingProviders } = useProviders()
  const { data: requests, isLoading: loadingRequests } = useRecentRequests()
  const { data: metrics, isLoading: loadingMetrics } = useMetricsSummary()
  const { data: providerHealth } = useProviderHealth()
  const { data: clientKeys } = useClientKeys()

  const providers = snapshot?.providers ?? []
  const modelCount = providers.reduce((acc, p) => acc + (p.models?.length ?? 0), 0)
  const recentRequests = requests?.recent ?? []
  const healthList = providerHealth?.providers ?? []
  const firstKeyPrefix = clientKeys?.keys?.[0]?.key_prefix

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">{t("dashboard.title")}</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {t("dashboard.description")}
          </p>
        </div>
        <HealthBadge />
      </div>

      <Card className="border-primary/20 bg-primary/5">
        <CardContent className="p-4 space-y-3">
          <div className="flex items-center gap-2">
            <KeyRound className="h-4 w-4 text-primary" />
            <span className="text-sm font-medium">{t("dashboard.quickConnect")}</span>
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            <CopyBox
              label={t("dashboard.openaiEndpoint")}
              value="http://127.0.0.1:8080/v1"
            />
            <CopyBox
              label={t("dashboard.anthropicEndpoint")}
              value="http://127.0.0.1:8080/anthropic"
            />
            <CopyBox
              label={firstKeyPrefix ? t("dashboard.clientKeyPrefix") : t("dashboard.noClientKey")}
              value={firstKeyPrefix ? `${firstKeyPrefix}...` : t("dashboard.createClientKey")}
            />
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatsCard
          label={t("dashboard.totalRequests")}
          value={loadingMetrics ? "..." : (metrics?.total_requests ?? 0)}
          icon={<Activity className="h-4 w-4" />}
        />
        <StatsCard
          label={t("dashboard.activeProviders")}
          value={loadingProviders ? "..." : providers.length}
          icon={<Server className="h-4 w-4" />}
        />
        <StatsCard
          label={t("dashboard.availableModels")}
          value={loadingProviders ? "..." : modelCount}
          icon={<Brain className="h-4 w-4" />}
        />
        <StatsCard
          label={t("dashboard.todayTokens")}
          value={loadingMetrics ? "..." : (metrics?.today_tokens?.total ?? 0).toLocaleString()}
            icon={<Database className="h-4 w-4" />}
        />
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="text-base">{t("dashboard.recentRequests")}</CardTitle>
          </CardHeader>
          <CardContent>
            {loadingRequests ? (
              <div className="space-y-2">
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} className="h-8 w-full" />
                ))}
              </div>
            ) : recentRequests.length === 0 ? (
              <EmptyState
                title={t("dashboard.noRequests")}
                description={t("dashboard.noRequestsDescription")}
                icon={<Activity className="h-8 w-8" />}
              />
            ) : (
              <RequestTable events={recentRequests.slice(0, 10)} />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">{t("dashboard.providerHealth")}</CardTitle>
          </CardHeader>
          <CardContent>
            {healthList.length === 0 ? (
              <div className="text-sm text-muted-foreground py-4 text-center">
                {loadingProviders ? t("common.loading") : t("dashboard.noProviders")}
              </div>
            ) : (
              <div className="space-y-3">
                {healthList.map((p) => (
                  <div key={p.id} className="flex items-center justify-between">
                    <div className="flex items-center gap-2 min-w-0">
                      <span
                        className={cn(
                          "h-2 w-2 shrink-0 rounded-full",
                          p.healthy ? "bg-green-500" : "bg-red-500"
                        )}
                      />
                      <span className="text-sm truncate font-mono">{p.id}</span>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      {p.latency_ms != null && (
                        <span className="text-xs text-muted-foreground">{p.latency_ms}ms</span>
                      )}
                      <Badge variant={p.healthy ? "success" : "destructive"}>
                        {p.healthy ? t("status.healthy") : t("status.down")}
                      </Badge>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
