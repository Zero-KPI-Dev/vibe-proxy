import { useState } from "react"
import {
  LineChart,
  Line,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  Area,
  AreaChart,
  Legend,
} from "recharts"
import { useMetricsHistory, useMetricsSummary } from "@/hooks/use-metrics"
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
} from "@/components/ui/card"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyState } from "@/components/empty-state"
import { TIME_RANGES } from "@/lib/constants"
import { cn } from "@/lib/utils"
import { Activity, Gauge, Coins, AlertTriangle, BarChart3 } from "lucide-react"
import type { MetricPoint } from "@/lib/types"
import { useTranslation } from "react-i18next"
import { ObservabilityNav } from "@/components/observability-nav"

export function ObservabilityPage() {
  const { t, i18n } = useTranslation()
  const [range, setRange] = useState("1h")
  const { data: history, isLoading } = useMetricsHistory(range)
  const { data: summary, isLoading: loadingSummary } = useMetricsSummary()

  const points = history?.points ?? []
  const rangeLabels: Record<string, string> = {
    "1h": t("observability.lastHour"),
    "6h": t("observability.last6Hours"),
    "24h": t("observability.last24Hours"),
    "7d": t("observability.last7Days"),
  }

  return (
    <div className="space-y-6">
      <div className="space-y-4">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-2xl font-semibold">{t("observability.title")}</h1>
            <p className="text-sm text-muted-foreground mt-1">
              {t("observability.description")}
            </p>
          </div>
          <div className="flex items-center gap-1 self-start rounded-lg border border-border p-1">
            {TIME_RANGES.map((r) => (
              <button
                key={r}
                onClick={() => setRange(r)}
                className={cn(
                  "px-3 py-1 text-sm rounded-md transition-colors",
                  range === r
                    ? "bg-accent text-accent-foreground font-medium"
                    : "text-muted-foreground hover:text-foreground"
                )}
              >
                {r}
              </button>
            ))}
          </div>
        </div>
        <ObservabilityNav />
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <MetricCard
          label={t("observability.totalRequests")}
          value={summary?.total_requests ?? 0}
          loading={loadingSummary}
          icon={<Activity className="h-4 w-4" />}
        />
        <MetricCard
          label={t("observability.promptTokens")}
          value={summary?.today_tokens?.prompt ?? 0}
          loading={loadingSummary}
          icon={<Gauge className="h-4 w-4" />}
        />
        <MetricCard
          label={t("observability.completionTokens")}
          value={summary?.today_tokens?.completion ?? 0}
          loading={loadingSummary}
          icon={<Coins className="h-4 w-4" />}
        />
        <MetricCard
          label={t("observability.totalToday")}
          value={summary?.today_tokens?.total ?? 0}
          loading={loadingSummary}
          icon={<BarChart3 className="h-4 w-4" />}
        />
      </div>

      <Tabs defaultValue="requests">
        <TabsList>
          <TabsTrigger value="requests">{t("observability.requestVolume")}</TabsTrigger>
          <TabsTrigger value="latency">{t("observability.latency")}</TabsTrigger>
          <TabsTrigger value="tokens">{t("observability.tokenUsage")}</TabsTrigger>
          <TabsTrigger value="errors">{t("observability.errors")}</TabsTrigger>
        </TabsList>

        <TabsContent value="requests">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">{t("observability.requestVolume")}</CardTitle>
              <CardDescription>{rangeLabels[range]}</CardDescription>
            </CardHeader>
            <CardContent>
              {isLoading ? (
                <Skeleton className="h-[300px] w-full" />
              ) : points.length === 0 ? (
                <EmptyState title={t("observability.noData")} description={t("observability.noRequestData")} />
              ) : (
                <ResponsiveContainer width="100%" height={300}>
                  <AreaChart data={points}>
                    <defs>
                      <linearGradient id="reqGrad" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#9b8cff" stopOpacity={0.3} />
                        <stop offset="95%" stopColor="#9b8cff" stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" stroke="#222631" />
                    <XAxis
                      dataKey="timestamp"
                      tick={{ fontSize: 12, fill: "#8b93a7" }}
                      tickFormatter={(v: string) => {
                        const d = new Date(v)
                        return range === "1h"
                          ? d.toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" })
                          : d.toLocaleDateString(i18n.language, { month: "short", day: "numeric", hour: "2-digit" })
                      }}
                    />
                    <YAxis tick={{ fontSize: 12, fill: "#8b93a7" }} />
                    <Tooltip
                      contentStyle={{
                        background: "#0d0f14",
                        border: "1px solid #222631",
                        borderRadius: "8px",
                        color: "#f5f7fb",
                      }}
                    />
                    <Area
                      type="monotone"
                      dataKey="requests"
                      stroke="#9b8cff"
                      fill="url(#reqGrad)"
                      strokeWidth={2}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="latency">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">{t("observability.latencyTitle")}</CardTitle>
              <CardDescription>{t("observability.percentiles", { range: rangeLabels[range] })}</CardDescription>
            </CardHeader>
            <CardContent>
              {isLoading ? (
                <Skeleton className="h-[300px] w-full" />
              ) : points.length === 0 ? (
                <EmptyState title={t("observability.noData")} description={t("observability.noLatencyData")} />
              ) : (
                <ResponsiveContainer width="100%" height={300}>
                  <LineChart data={points}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#222631" />
                    <XAxis
                      dataKey="timestamp"
                      tick={{ fontSize: 12, fill: "#8b93a7" }}
                      tickFormatter={(v: string) => {
                        const d = new Date(v)
                        return range === "1h"
                          ? d.toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" })
                          : d.toLocaleDateString(i18n.language, { month: "short", day: "numeric" })
                      }}
                    />
                    <YAxis
                      tick={{ fontSize: 12, fill: "#8b93a7" }}
                      label={{ value: "ms", angle: -90, position: "insideLeft", fill: "#8b93a7" }}
                    />
                    <Tooltip
                      contentStyle={{
                        background: "#0d0f14",
                        border: "1px solid #222631",
                        borderRadius: "8px",
                        color: "#f5f7fb",
                      }}
                    />
                    <Legend />
                    <Line type="monotone" dataKey="ttft_p50" stroke="#22c55e" name="P50" strokeWidth={2} dot={false} />
                    <Line type="monotone" dataKey="ttft_p95" stroke="#f59e0b" name="P95" strokeWidth={2} dot={false} />
                    <Line type="monotone" dataKey="ttft_p99" stroke="#fb7185" name="P99" strokeWidth={2} dot={false} />
                  </LineChart>
                </ResponsiveContainer>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="tokens">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">{t("observability.tokenUsage")}</CardTitle>
              <CardDescription>{rangeLabels[range]}</CardDescription>
            </CardHeader>
            <CardContent>
              {isLoading ? (
                <Skeleton className="h-[300px] w-full" />
              ) : points.length === 0 ? (
                <EmptyState title={t("observability.noData")} description={t("observability.noTokenData")} />
              ) : (
                <ResponsiveContainer width="100%" height={300}>
                  <BarChart data={points}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#222631" />
                    <XAxis
                      dataKey="timestamp"
                      tick={{ fontSize: 12, fill: "#8b93a7" }}
                      tickFormatter={(v: string) => {
                        const d = new Date(v)
                        return range === "1h"
                          ? d.toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" })
                          : d.toLocaleDateString(i18n.language, { month: "short", day: "numeric" })
                      }}
                    />
                    <YAxis tick={{ fontSize: 12, fill: "#8b93a7" }} />
                    <Tooltip
                      contentStyle={{
                        background: "#0d0f14",
                        border: "1px solid #222631",
                        borderRadius: "8px",
                        color: "#f5f7fb",
                      }}
                    />
                    <Legend />
                    <Bar dataKey="tokens_prompt" stackId="a" fill="#9b8cff" name={t("observability.prompt")} />
                    <Bar dataKey="tokens_completion" stackId="a" fill="#38bdf8" name={t("observability.completion")} />
                  </BarChart>
                </ResponsiveContainer>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="errors">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">{t("observability.errorRate")}</CardTitle>
              <CardDescription>{rangeLabels[range]}</CardDescription>
            </CardHeader>
            <CardContent>
              {isLoading ? (
                <Skeleton className="h-[300px] w-full" />
              ) : points.length === 0 ? (
                <EmptyState title={t("observability.noData")} description={t("observability.noErrorData")} />
              ) : (
                <ResponsiveContainer width="100%" height={300}>
                  <AreaChart data={points}>
                    <defs>
                      <linearGradient id="errGrad" x1="0" y1="0" x2="0" y2="1">
                        <stop offset="5%" stopColor="#fb7185" stopOpacity={0.3} />
                        <stop offset="95%" stopColor="#fb7185" stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" stroke="#222631" />
                    <XAxis
                      dataKey="timestamp"
                      tick={{ fontSize: 12, fill: "#8b93a7" }}
                      tickFormatter={(v: string) => {
                        const d = new Date(v)
                        return range === "1h"
                          ? d.toLocaleTimeString(i18n.language, { hour: "2-digit", minute: "2-digit" })
                          : d.toLocaleDateString(i18n.language, { month: "short", day: "numeric" })
                      }}
                    />
                    <YAxis tick={{ fontSize: 12, fill: "#8b93a7" }} />
                    <Tooltip
                      contentStyle={{
                        background: "#0d0f14",
                        border: "1px solid #222631",
                        borderRadius: "8px",
                        color: "#f5f7fb",
                      }}
                    />
                    <Area
                      type="monotone"
                      dataKey="errors"
                      stroke="#fb7185"
                      fill="url(#errGrad)"
                      strokeWidth={2}
                    />
                  </AreaChart>
                </ResponsiveContainer>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}

function MetricCard({
  label,
  value,
  loading,
  icon,
}: {
  label: string
  value: number
  loading: boolean
  icon: React.ReactNode
}) {
  const { i18n } = useTranslation()
  return (
    <Card>
      <CardContent className="flex items-center gap-4 p-4">
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
          {icon}
        </div>
        <div className="flex-1 min-w-0">
          <p className="text-sm text-muted-foreground truncate">{label}</p>
          <p className="text-2xl font-semibold tabular-nums">
            {loading ? "..." : value.toLocaleString(i18n.language)}
          </p>
        </div>
      </CardContent>
    </Card>
  )
}
