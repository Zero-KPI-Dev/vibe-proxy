import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { aliasApi } from "@/lib/api"

export function useAliases() {
  return useQuery({
    queryKey: ["aliases"],
    queryFn: aliasApi.list,
  })
}

export function useCreateAlias() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ alias, target }: { alias: string; target: string }) =>
      aliasApi.create(alias, target),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["aliases"] }),
        qc.invalidateQueries({ queryKey: ["providers"] }),
        qc.invalidateQueries({ queryKey: ["models"] }),
      ])
    },
  })
}

export function useDeleteAlias() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (alias: string) => aliasApi.remove(alias),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["aliases"] }),
        qc.invalidateQueries({ queryKey: ["providers"] }),
        qc.invalidateQueries({ queryKey: ["models"] }),
      ])
    },
  })
}

export function useUpdateAliasDefaults() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ default_model, allow_raw }: { default_model: string; allow_raw: boolean }) =>
      aliasApi.updateDefault(default_model, allow_raw),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: ["aliases"] }),
        qc.invalidateQueries({ queryKey: ["providers"] }),
        qc.invalidateQueries({ queryKey: ["models"] }),
      ])
    },
  })
}
