import { useParams, useNavigate } from "react-router-dom"
import { useProviders, useUpdateProvider } from "@/hooks/use-providers"
import { ProviderForm } from "@/components/provider-form"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { toast } from "sonner"
import type { ProviderFormData } from "@/lib/types"

export function ProviderEditPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { data, isLoading } = useProviders()
  const updateProvider = useUpdateProvider()

  const provider = (data?.providers ?? []).find((p) => p.id === id)

  const handleSubmit = async (formData: ProviderFormData) => {
    try {
      await updateProvider.mutateAsync(formData)
      toast.success(`Provider "${id}" updated`)
      navigate("/providers")
    } catch (e) {
      toast.error(
        `Failed to update provider: ${e instanceof Error ? e.message : "Unknown error"}`
      )
    }
  }

  if (isLoading) {
    return (
      <div className="max-w-2xl space-y-6">
        <Skeleton className="h-8 w-48" />
        <Card>
          <CardContent className="pt-6 space-y-4">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-9 w-full" />
            ))}
          </CardContent>
        </Card>
      </div>
    )
  }

  if (!provider) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-semibold">Provider not found</h1>
        <p className="text-muted-foreground">
          Provider "{id}" does not exist.
        </p>
      </div>
    )
  }

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Edit Provider</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Editing provider: {id}
        </p>
      </div>
      <Card>
        <CardContent className="pt-6">
          <ProviderForm
            defaultValues={{
              id: provider.id,
              type: provider.type,
              base_url: provider.base_url,
              api_key_env: provider.api_key_env ?? "",
              auth_type: "bearer",
              models: (provider.models ?? []).join(", "),
              max_concurrency: provider.max_concurrency ?? 32,
            }}
            onSubmit={handleSubmit}
            isPending={updateProvider.isPending}
            mode="edit"
          />
        </CardContent>
      </Card>
    </div>
  )
}
