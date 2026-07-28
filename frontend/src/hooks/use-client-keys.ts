import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { clientKeyApi } from "@/lib/api"

export function useClientKeys() {
  return useQuery({
    queryKey: ["client-keys"],
    queryFn: clientKeyApi.list,
  })
}

export function useCreateClientKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (data: { name: string; allowed_models?: string[]; rpm?: number }) =>
      clientKeyApi.create(data),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["client-keys"] })
    },
  })
}

export function useUpdateClientKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ name, ...data }: { name: string; enabled?: boolean; allowed_models?: string[]; rpm?: number }) =>
      clientKeyApi.update(name, data),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["client-keys"] })
    },
  })
}

export function useDeleteClientKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (name: string) => clientKeyApi.remove(name),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["client-keys"] })
    },
  })
}
