import { useQuery } from "@tanstack/react-query"
import { requestApi } from "@/lib/api"

export function useRecentRequests() {
  return useQuery({
    queryKey: ["recent-requests"],
    queryFn: requestApi.recent,
    refetchInterval: 5_000,
  })
}
