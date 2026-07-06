import { useQuery } from "@tanstack/react-query"
import { healthApi } from "@/lib/api"

export function useHealth() {
  return useQuery({
    queryKey: ["health"],
    queryFn: healthApi.check,
    refetchInterval: 10_000,
    retry: 1,
  })
}
