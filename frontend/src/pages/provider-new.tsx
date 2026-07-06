import { useNavigate } from "react-router-dom"
import { useCreateProvider } from "@/hooks/use-providers"
import { ProviderForm } from "@/components/provider-form"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { toast } from "sonner"
import type { ProviderFormData } from "@/lib/types"

export function ProviderNewPage() {
  const navigate = useNavigate()
  const createProvider = useCreateProvider()

  const handleSubmit = async (data: ProviderFormData) => {
    try {
      await createProvider.mutateAsync(data)
      toast.success(`Provider "${data.id}" created`)
      navigate("/providers")
    } catch (e) {
      toast.error(
        `Failed to create provider: ${e instanceof Error ? e.message : "Unknown error"}`
      )
    }
  }

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Add Provider</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Configure a new upstream LLM provider
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
