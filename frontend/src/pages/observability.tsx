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

const rangeLabels: Record<string, string> = {
  "1h": "Last hour",
  "6h": "Last 6 hours",
  "24h": "Last 24 hours",
  "7d": "Last 7 days",
}

export function ObservabilityPage() {
  const [range, setRange] = useState("1h")
  const { data: history, isLoading } = useMetricsHistory(range)
  const { data: summary, isLoading: loadingSummary } = useMetricsSummary()

  const points = history?.points ?? []

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Observability</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Monitor proxy performance and usage
          </p>
        </div>
        <div className="flex items-center gap-1 rounded-lg border border-border p-1">
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

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <MetricCard
          label="Total Requests"
          value={summary?.total_requests ?? 0}
          loading={loadingSummary}
          icon={<Activity className="h-4 w-4" />}
        />
        <MetricCard
          label="Prompt Tokens"
          value={summary?.today_tokens?.prompt ?? 0}
          loading={loadingSummary}
          icon={<Gauge className="h-4 w-4" />}
        />
        <MetricCard
          label="Completion Tokens"
          value={summary?.today_tokens?.completion ?? 0}
          loading={loadingSummary}
          icon={<Coins className="h-4 w-4" />}
        />
        <MetricCard
          label="Total Today"
          value={summary?.today_tokens?.total ?? 0}
          loading={loadingSummary}
          icon={<BarChart3 className="h-4 w-4" />}
        />
      </div>

      <Tabs defaultValue="requests">
        <TabsList>
          <TabsTrigger value="requests">Request Volume</TabsTrigger>
          <TabsTrigger value="latency">Latency</TabsTrigger>
          <TabsTrigger value="tokens">Token Usage</TabsTrigger>
          <TabsTrigger value="errors">Errors</TabsTrigger>
        </TabsList>

        <TabsContent value="requests">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Request Volume</CardTitle>
              <CardDescription>{rangeLabels[range]}</CardDescription>
            </CardHeader>
            <CardContent>
              {isLoading ? (
                <Skeleton className="h-[300px] w-full" />
              ) : points.length === 0 ? (
                <EmptyState title="No data" description="No request data available for this period." />
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
                          ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
                          : d.toLocaleDateString([], { month: "short", day: "numeric", hour: "2-digit" })
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
              <CardTitle className="text-base">Latency (TTFT)</CardTitle>
              <CardDescription>{rangeLabels[range]} — Percentile distribution</CardDescription>
            </CardHeader>
            <CardContent>
              {isLoading ? (
                <Skeleton className="h-[300px] w-full" />
              ) : points.length === 0 ? (
                <EmptyState title="No data" description="No latency data available for this period." />
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
                          ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
                          : d.toLocaleDateString([], { month: "short", day: "numeric" })
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
              <CardTitle className="text-base">Token Usage</CardTitle>
              <CardDescription>{rangeLabels[range]}</CardDescription>
            </CardHeader>
            <CardContent>
              {isLoading ? (
                <Skeleton className="h-[300px] w-full" />
              ) : points.length === 0 ? (
                <EmptyState title="No data" description="No token usage data available for this period." />
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
                          ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
                          : d.toLocaleDateString([], { month: "short", day: "numeric" })
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
                    <Bar dataKey="tokens_prompt" stackId="a" fill="#9b8cff" name="Prompt" />
                    <Bar dataKey="tokens_completion" stackId="a" fill="#38bdf8" name="Completion" />
                  </BarChart>
                </ResponsiveContainer>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="errors">
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Error Rate</CardTitle>
              <CardDescription>{rangeLabels[range]}</CardDescription>
            </CardHeader>
            <CardContent>
              {isLoading ? (
                <Skeleton className="h-[300px] w-full" />
              ) : points.length === 0 ? (
                <EmptyState title="No data" description="No error data available for this period." />
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
                          ? d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
                          : d.toLocaleDateString([], { month: "short", day: "numeric" })
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
  return (
    <Card>
      <CardContent className="flex items-center gap-4 p-4">
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
          {icon}
        </div>
        <div className="flex-1 min-w-0">
          <p className="text-sm text-muted-foreground truncate">{label}</p>
          <p className="text-2xl font-semibold tabular-nums">
            {loading ? "..." : value.toLocaleString()}
          </p>
        </div>
      </CardContent>
    </Card>
  )
}
