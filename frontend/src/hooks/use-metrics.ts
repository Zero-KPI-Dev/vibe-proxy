import { useQuery } from "@tanstack/react-query"
import { metricsApi, providerHealthApi } from "@/lib/api"

export function useMetricsSummary() {
  return useQuery({
    queryKey: ["metrics-summary"],
    queryFn: metricsApi.summary,
    refetchInterval: 30_000,
  })
}

export function useMetricsHistory(range: string) {
  return useQuery({
    queryKey: ["metrics-history", range],
    queryFn: () => metricsApi.history(range),
    refetchInterval: 30_000,
  })
}

export function useProviderHealth() {
  return useQuery({
    queryKey: ["provider-health"],
    queryFn: providerHealthApi.list,
    refetchInterval: 30_000,
  })
}
