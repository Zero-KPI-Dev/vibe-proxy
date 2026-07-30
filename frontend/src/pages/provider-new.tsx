import { useNavigate } from "react-router"
import { useCreateProvider } from "@/hooks/use-providers"
import { ProviderForm } from "@/components/provider-form"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { toast } from "sonner"
import type { ProviderFormData } from "@/lib/types"
import { useTranslation } from "react-i18next"

export function ProviderNewPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const createProvider = useCreateProvider()

  const handleSubmit = async (data: ProviderFormData) => {
    try {
      await createProvider.mutateAsync(data)
      toast.success(t("providers.created", { id: data.id }))
      navigate("/providers")
    } catch (e) {
      toast.error(
        t("providers.createFailed", {
          error: e instanceof Error ? e.message : t("common.unknownError"),
        })
      )
    }
  }

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">{t("providers.newTitle")}</h1>
        <p className="text-sm text-muted-foreground mt-1">
          {t("providers.newDescription")}
        </p>
      </div>
      <Card>
        <CardContent className="pt-6">
          <ProviderForm
            onSubmit={handleSubmit}
            isPending={createProvider.isPending}
            mode="create"
          />
        </CardContent>
      </Card>
    </div>
  )
}
