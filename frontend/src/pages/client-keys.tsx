import { useState } from "react"
import {
  useClientKeys,
  useCreateClientKey,
  useUpdateClientKey,
  useDeleteClientKey,
} from "@/hooks/use-client-keys"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { EmptyState } from "@/components/empty-state"
import { toast } from "sonner"
import { KeyRound, Plus, Trash2, Copy, Check, Pencil } from "lucide-react"
import type { ClientKey } from "@/lib/types"
import { useTranslation } from "react-i18next"

export function ClientKeysPage() {
  const { t } = useTranslation()
  const { data, isLoading, refetch } = useClientKeys()
  const createKey = useCreateClientKey()
  const updateKey = useUpdateClientKey()
  const deleteKey = useDeleteClientKey()

  const [showCreate, setShowCreate] = useState(false)
  const [newName, setNewName] = useState("")
  const [newRpm, setNewRpm] = useState("60")
  const [newModels, setNewModels] = useState("")
  const [createdRawKey, setCreatedRawKey] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [editingKey, setEditingKey] = useState<ClientKey | null>(null)
  const [editModels, setEditModels] = useState("")
  const [editRpm, setEditRpm] = useState("")

  const keys = data?.keys ?? []

  const handleCreate = async () => {
    if (!newName.trim()) return
    try {
      const res = await createKey.mutateAsync({
        name: newName.trim(),
        rpm: parseInt(newRpm) || 60,
        allowed_models: newModels.trim()
          ? newModels.split(",").map((s) => s.trim()).filter(Boolean)
          : undefined,
      })
      setCreatedRawKey(res.raw_key)
      setNewName("")
      setNewRpm("60")
      setNewModels("")
      toast.success(t("clientKeys.created"))
    } catch (e) {
      toast.error(t("clientKeys.createFailed", {
        error: e instanceof Error ? e.message : t("common.unknownError"),
      }))
    }
  }

  const handleToggleEnabled = async (key: ClientKey) => {
    try {
      await updateKey.mutateAsync({ name: key.name, enabled: !key.enabled })
      toast.success(t("clientKeys.toggled", {
        name: key.name,
        state: key.enabled ? t("clientKeys.disabled") : t("clientKeys.enabled"),
      }))
    } catch (e) {
      toast.error(t("clientKeys.updateFailed"))
    }
  }

  const handleDelete = async (name: string) => {
    try {
      await deleteKey.mutateAsync(name)
      toast.success(t("clientKeys.deleted", { name }))
    } catch (e) {
      toast.error(t("clientKeys.deleteFailed"))
    }
  }

  const handleCopyKey = () => {
    if (createdRawKey) {
      navigator.clipboard.writeText(createdRawKey)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  const openEdit = (key: ClientKey) => {
    setEditingKey(key)
    setEditModels((key.allowed_models ?? []).join(", "))
    setEditRpm(String(key.rpm))
  }

  const handleSaveEdit = async () => {
    if (!editingKey) return
    try {
      await updateKey.mutateAsync({
        name: editingKey.name,
        rpm: parseInt(editRpm) || editingKey.rpm,
        allowed_models: editModels.trim()
          ? editModels.split(",").map((s) => s.trim()).filter(Boolean)
          : [],
      })
      setEditingKey(null)
      toast.success(t("clientKeys.updated", { name: editingKey.name }))
    } catch (e) {
      toast.error(t("clientKeys.updateFailed"))
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">{t("clientKeys.title")}</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {t("clientKeys.description")}
          </p>
        </div>
        <Dialog open={showCreate && !createdRawKey} onOpenChange={(o) => { setShowCreate(o); if (!o) setCreatedRawKey(null) }}>
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus className="h-4 w-4 mr-1" />
              {t("clientKeys.create")}
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("clientKeys.createTitle")}</DialogTitle>
              <DialogDescription>
                {t("clientKeys.createDescription")}
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="keyName">{t("clientKeys.keyName")}</Label>
                <Input
                  id="keyName"
                  placeholder="my-agent"
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="keyRpm">{t("clientKeys.rateLimit")}</Label>
                <Input
                  id="keyRpm"
                  type="number"
                  placeholder="60"
                  value={newRpm}
                  onChange={(e) => setNewRpm(e.target.value)}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="keyModels">{t("clientKeys.allowedModelsOptional")}</Label>
                <Input
                  id="keyModels"
                  placeholder="gpt-4, claude-3"
                  value={newModels}
                  onChange={(e) => setNewModels(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  {t("clientKeys.modelsHelp")}
                </p>
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => setShowCreate(false)}>
                {t("common.cancel")}
              </Button>
              <Button onClick={handleCreate} disabled={createKey.isPending || !newName.trim()}>
                {createKey.isPending ? t("clientKeys.creating") : t("common.create")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </div>

      {createdRawKey && (
        <Card className="border-yellow-500/30 bg-yellow-500/5">
          <CardContent className="p-4">
            <p className="text-sm font-medium mb-2 text-yellow-400">{t("clientKeys.saveWarning")}</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 rounded bg-background px-3 py-2 text-sm font-mono border border-border break-all">
                {createdRawKey}
              </code>
              <Button variant="outline" size="sm" onClick={handleCopyKey}>
                {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
              </Button>
            </div>
            <Button
              variant="default"
              size="sm"
              className="mt-3"
              onClick={() => { setCreatedRawKey(null); setShowCreate(false) }}
            >
              {t("common.done")}
            </Button>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="p-4 space-y-2">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-12 w-full" />
              ))}
            </div>
          ) : keys.length === 0 ? (
            <EmptyState
              title={t("clientKeys.empty")}
              description={t("clientKeys.emptyDescription")}
              icon={<KeyRound className="h-8 w-8" />}
              action={
                <Button onClick={() => setShowCreate(true)}>
                  <Plus className="h-4 w-4 mr-1" />
                  {t("clientKeys.create")}
                </Button>
              }
            />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("clientKeys.name")}</TableHead>
                  <TableHead>{t("clientKeys.keyPrefix")}</TableHead>
                  <TableHead>{t("clientKeys.models")}</TableHead>
                  <TableHead>{t("clientKeys.status")}</TableHead>
                  <TableHead>RPM</TableHead>
                  <TableHead className="w-24">{t("common.actions")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((k) => (
                  <TableRow key={k.name}>
                    <TableCell className="font-medium">{k.name}</TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">
                      {k.key_prefix}...
                    </TableCell>
                    <TableCell>
                      {k.allowed_models && k.allowed_models.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                          {k.allowed_models.map((m) => (
                            <Badge key={m} variant="outline" className="text-xs font-mono">{m}</Badge>
                          ))}
                        </div>
                      ) : (
                        <span className="text-xs text-muted-foreground">{t("clientKeys.allModels")}</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={k.enabled}
                        onCheckedChange={() => handleToggleEnabled(k)}
                      />
                    </TableCell>
                    <TableCell className="tabular-nums">{k.rpm}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <Button variant="ghost" size="sm" onClick={() => openEdit(k)}>
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => handleDelete(k.name)}>
                          <Trash2 className="h-3.5 w-3.5 text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog open={!!editingKey} onOpenChange={(o) => { if (!o) setEditingKey(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("clientKeys.editTitle", { name: editingKey?.name })}</DialogTitle>
            <DialogDescription>{t("clientKeys.editDescription")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>{t("clientKeys.allowedModels")}</Label>
              <Input
                value={editModels}
                onChange={(e) => setEditModels(e.target.value)}
                placeholder="gpt-4, claude-3"
              />
              <p className="text-xs text-muted-foreground">
                {t("clientKeys.editModelsHelp")}
              </p>
            </div>
            <div className="space-y-2">
              <Label>{t("clientKeys.rateLimit")}</Label>
              <Input
                type="number"
                value={editRpm}
                onChange={(e) => setEditRpm(e.target.value)}
                placeholder="60"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditingKey(null)}>{t("common.cancel")}</Button>
            <Button onClick={handleSaveEdit} disabled={updateKey.isPending}>
              {updateKey.isPending ? t("common.saving") : t("common.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
