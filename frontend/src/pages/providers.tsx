import { useState } from "react"
import { useNavigate } from "react-router-dom"
import { useProviders, useDeleteProvider, useTestProvider } from "@/hooks/use-providers"
import { EmptyState } from "@/components/empty-state"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { toast } from "sonner"
import { Server, Plus, Trash2, RefreshCw, ExternalLink } from "lucide-react"
import type { ProviderConfig } from "@/lib/types"
import { useTranslation } from "react-i18next"

export function ProvidersPage() {
  const { t } = useTranslation()
  const { data, isLoading, refetch } = useProviders()
  const deleteProvider = useDeleteProvider()
  const testProvider = useTestProvider()
  const navigate = useNavigate()
  const [deleting, setDeleting] = useState<string | null>(null)

  const providers = data?.providers ?? []

  const handleDelete = async (id: string) => {
    try {
      await deleteProvider.mutateAsync(id)
      toast.success(t("providers.deleted", { id }))
      setDeleting(null)
    } catch (e) {
      toast.error(t("providers.deleteFailed", { error: e instanceof Error ? e.message : t("common.unknownError") }))
    }
  }

  const handleTest = async (id: string) => {
    toast.promise(testProvider.mutateAsync(id), {
      loading: t("providers.testing", { id }),
      success: (res) => t("providers.testResult", {
        result: res.ok ? t("providers.testSucceeded") : t("providers.testFailedResult"),
        detail: res.status ?? res.error,
        latency: res.latency_ms,
      }),
      error: (e) => t("providers.testFailed", {
        error: e instanceof Error ? e.message : t("common.unknownError"),
      }),
    })
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">{t("providers.title")}</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {t("providers.description")}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            <RefreshCw className="h-4 w-4 mr-1" />
            {t("common.refresh")}
          </Button>
          <Button size="sm" onClick={() => navigate("/providers/new")}>
            <Plus className="h-4 w-4 mr-1" />
            {t("providers.add")}
          </Button>
        </div>
      </div>

      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="p-4 space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : providers.length === 0 ? (
            <EmptyState
              title={t("providers.empty")}
              description={t("providers.emptyDescription")}
              icon={<Server className="h-8 w-8" />}
              action={
                <Button onClick={() => navigate("/providers/new")}>
                  <Plus className="h-4 w-4 mr-1" />
                  {t("providers.add")}
                </Button>
              }
            />
          ) : (
            <ProviderTable
              providers={providers}
              onEdit={(id) => navigate(`/providers/${encodeURIComponent(id)}/edit`)}
              onDelete={(id) => setDeleting(id)}
              onTest={handleTest}
            />
          )}
        </CardContent>
      </Card>

      <Dialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("providers.deleteTitle")}</DialogTitle>
            <DialogDescription>
              {t("providers.deleteDescription", { id: deleting })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleting(null)}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={() => deleting && handleDelete(deleting)}
              disabled={deleteProvider.isPending}
            >
              {deleteProvider.isPending ? t("common.deleting") : t("common.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function ProviderTable({
  providers,
  onEdit,
  onDelete,
  onTest,
}: {
  providers: ProviderConfig[]
  onEdit: (id: string) => void
  onDelete: (id: string) => void
  onTest: (id: string) => void
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>ID</TableHead>
          <TableHead>{t("providers.type")}</TableHead>
          <TableHead>{t("providers.baseUrl")}</TableHead>
          <TableHead>{t("providers.models")}</TableHead>
          <TableHead className="text-right">{t("common.actions")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {providers.map((p) => (
          <TableRow key={p.id}>
            <TableCell className="font-mono text-sm font-medium">{p.id}</TableCell>
            <TableCell>
              <Badge variant="outline">{p.type}</Badge>
            </TableCell>
            <TableCell className="font-mono text-xs text-muted-foreground max-w-[240px] truncate">
              {p.base_url}
            </TableCell>
            <TableCell>
              <div className="flex flex-wrap gap-1">
                {(p.models ?? []).slice(0, 3).map((m) => (
                  <Badge key={m} variant="secondary" className="text-xs">
                    {m}
                  </Badge>
                ))}
                {(p.models?.length ?? 0) > 3 && (
                  <Badge variant="outline" className="text-xs">
                    +{p.models!.length - 3}
                  </Badge>
                )}
              </div>
            </TableCell>
            <TableCell>
              <div className="flex items-center justify-end gap-1">
                <Button variant="ghost" size="sm" onClick={() => onTest(p.id)}>
                  <ExternalLink className="h-3.5 w-3.5" />
                </Button>
                <Button variant="ghost" size="sm" onClick={() => onEdit(p.id)}>
                  {t("common.edit")}
                </Button>
                <Button variant="ghost" size="sm" onClick={() => onDelete(p.id)}>
                  <Trash2 className="h-3.5 w-3.5 text-destructive" />
                </Button>
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
