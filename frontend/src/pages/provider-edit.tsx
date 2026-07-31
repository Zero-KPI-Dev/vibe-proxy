import { useParams, useNavigate } from "react-router"
import { useProviders, useUpdateProvider } from "@/hooks/use-providers"
import { ProviderForm } from "@/components/provider-form"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { toast } from "sonner"
import type { ProviderFormData } from "@/lib/types"
import { useTranslation } from "react-i18next"

export function ProviderEditPage() {
  const { t } = useTranslation()
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const { data, isLoading } = useProviders()
  const updateProvider = useUpdateProvider()

  const provider = (data?.providers ?? []).find((p) => p.id === id)

  const handleSubmit = async (formData: ProviderFormData) => {
    try {
      await updateProvider.mutateAsync(formData)
      toast.success(t("providers.updated", { id }))
      navigate("/providers")
    } catch (e) {
      toast.error(
        t("providers.updateFailed", {
          error: e instanceof Error ? e.message : t("common.unknownError"),
        })
      )
    }
  }

  if (isLoading) {
    return (
      <div className="max-w-4xl space-y-6">
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
        <h1 className="text-2xl font-semibold">{t("providers.notFound")}</h1>
        <p className="text-muted-foreground">
          {t("providers.notFoundDescription", { id })}
        </p>
      </div>
    )
  }

  return (
    <div className="max-w-4xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">{t("providers.editTitle")}</h1>
        <p className="text-sm text-muted-foreground mt-1">
          {t("providers.editing", { id })}
        </p>
      </div>
      <Card>
        <CardContent className="pt-6">
          <ProviderForm
            defaultValues={{
              id: provider.id,
              type: provider.type,
              base_url: provider.base_url,
              catalog_provider: provider.catalog_provider ?? "",
              auth_type: provider.auth_type ?? "bearer",
              api_key_source: provider.api_key_source ?? "env",
              api_key_env: provider.api_key_env ?? "",
              api_key: "",
              models: (provider.models ?? []).join(", "),
              max_concurrency: provider.max_concurrency ?? 32,
              default_image_input: provider.default_capabilities?.image_input ?? "",
              model_image_capabilities: Object.fromEntries(
                Object.entries(provider.model_capabilities ?? {})
                  .filter(([, capabilities]) => Boolean(capabilities.image_input))
                  .map(([model, capabilities]) => [model, capabilities.image_input!])
              ),
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
