import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { observabilityApi, requestApi } from "@/lib/api"
import type { RequestQueryFilters, SessionQueryFilters } from "@/lib/types"

export function useRecentRequests() {
  return useQuery({
    queryKey: ["recent-requests"],
    queryFn: requestApi.recent,
    refetchInterval: 5_000,
  })
}

export function useObservabilityRequests(filters: RequestQueryFilters) {
  return useInfiniteQuery({
    queryKey: ["observability", "requests", filters],
    queryFn: ({ pageParam }) => observabilityApi.requests(filters, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  })
}

export function useRequestDetails(requestId: string) {
  return useQuery({
    queryKey: ["observability", "request", requestId],
    queryFn: () => observabilityApi.request(requestId),
    enabled: Boolean(requestId),
  })
}

export function useRequestDiff(requestId: string, baseId: string) {
  return useQuery({
    queryKey: ["observability", "request", requestId, "diff", baseId],
    queryFn: () => observabilityApi.diff(requestId, baseId || undefined),
    enabled: Boolean(requestId),
  })
}

export function useDeleteRequestContent(requestId: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => observabilityApi.deleteContent(requestId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["observability", "request", requestId] })
      void queryClient.invalidateQueries({ queryKey: ["observability", "request", requestId, "diff"] })
      void queryClient.invalidateQueries({ queryKey: ["observability", "requests"] })
    },
  })
}

export function useObservabilitySessions(filters: SessionQueryFilters) {
  return useInfiniteQuery({
    queryKey: ["observability", "sessions", filters],
    queryFn: ({ pageParam }) => observabilityApi.sessions(filters, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor || undefined,
  })
}

export function useSessionDetails(sessionId: string) {
  return useQuery({
    queryKey: ["observability", "session", sessionId],
    queryFn: () => observabilityApi.session(sessionId),
    enabled: Boolean(sessionId),
  })
}
