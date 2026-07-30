import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { providerApi } from "@/lib/api"
import type { ProviderFormData } from "@/lib/types"

export function useProviders() {
  return useQuery({
    queryKey: ["providers"],
    queryFn: providerApi.list,
  })
}

export function useCreateProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (data: ProviderFormData) => providerApi.create(data),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["providers"] }),
        qc.invalidateQueries({ queryKey: ["aliases"] }),
        qc.invalidateQueries({ queryKey: ["models"] }),
        qc.invalidateQueries({ queryKey: ["provider-health"] }),
        qc.invalidateQueries({ queryKey: ["metrics-summary"] }),
      ])
    },
  })
}

export function useUpdateProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (data: ProviderFormData) => providerApi.update(data),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["providers"] }),
        qc.invalidateQueries({ queryKey: ["aliases"] }),
        qc.invalidateQueries({ queryKey: ["models"] }),
        qc.invalidateQueries({ queryKey: ["provider-health"] }),
        qc.invalidateQueries({ queryKey: ["metrics-summary"] }),
      ])
    },
  })
}

export function useDeleteProvider() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => providerApi.remove(id),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["providers"] }),
        qc.invalidateQueries({ queryKey: ["aliases"] }),
        qc.invalidateQueries({ queryKey: ["models"] }),
        qc.invalidateQueries({ queryKey: ["provider-health"] }),
        qc.invalidateQueries({ queryKey: ["metrics-summary"] }),
      ])
    },
  })
}

export function useTestProvider() {
  return useMutation({
    mutationFn: (id: string) => providerApi.test(id),
  })
}
