import { useState } from "react"
import {
  useClientKeys,
  useCreateClientKey,
  useRevealClientKey,
  useRotateClientKey,
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
import { KeyRound, Plus, Trash2, Copy, Check, Pencil, Eye, RotateCw } from "lucide-react"
import type { ClientKey } from "@/lib/types"
import { useTranslation } from "react-i18next"

export function ClientKeysPage() {
  const { t } = useTranslation()
  const { data, isLoading } = useClientKeys()
  const createKey = useCreateClientKey()
  const revealKey = useRevealClientKey()
  const rotateKey = useRotateClientKey()
  const updateKey = useUpdateClientKey()
  const deleteKey = useDeleteClientKey()

  const [showCreate, setShowCreate] = useState(false)
  const [newName, setNewName] = useState("")
  const [newRpm, setNewRpm] = useState("60")
  const [newModels, setNewModels] = useState("")
  const [createdRawKey, setCreatedRawKey] = useState<string | null>(null)
  const [revealedKey, setRevealedKey] = useState<{ name: string; rawKey: string } | null>(null)
  const [copiedValue, setCopiedValue] = useState<string | null>(null)
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

  const handleCopyKey = (value: string) => {
    navigator.clipboard.writeText(value)
    setCopiedValue(value)
    setTimeout(() => setCopiedValue((current) => current === value ? null : current), 2000)
  }

  const handleRevealKey = async (key: ClientKey) => {
    if (!key.recoverable) {
      toast.error(t("clientKeys.legacyUnavailable"))
      return
    }
    try {
      const result = await revealKey.mutateAsync(key.name)
      setRevealedKey({ name: result.name, rawKey: result.raw_key })
    } catch (e) {
      toast.error(t("clientKeys.revealFailed", {
        error: e instanceof Error ? e.message : t("common.unknownError"),
      }))
    }
  }

  const handleRotateKey = async (key: ClientKey) => {
    if (!window.confirm(t("clientKeys.rotateConfirm", { name: key.name }))) return
    try {
      const result = await rotateKey.mutateAsync(key.name)
      setRevealedKey({ name: result.key.name, rawKey: result.raw_key })
      toast.success(t("clientKeys.rotated", { name: key.name }))
    } catch (e) {
      toast.error(t("clientKeys.rotateFailed", {
        error: e instanceof Error ? e.message : t("common.unknownError"),
      }))
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
          : ["*"],
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
        <Card className="border-primary/25 bg-primary/[0.04]">
          <CardContent className="p-4">
            <p className="text-sm font-medium mb-1">{t("clientKeys.createdKeyTitle")}</p>
            <p className="text-xs text-muted-foreground mb-3">{t("clientKeys.savedNotice")}</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 rounded bg-background px-3 py-2 text-sm font-mono border border-border break-all">
                {createdRawKey}
              </code>
              <Button variant="outline" size="sm" onClick={() => handleCopyKey(createdRawKey)} aria-label={t("clientKeys.copyKey")}>
                {copiedValue === createdRawKey ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
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
                  <TableHead className="w-32">{t("common.actions")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((k) => (
                  <TableRow key={k.name}>
                    <TableCell className="font-medium">{k.name}</TableCell>
                    <TableCell className="font-mono text-xs text-muted-foreground">
                      {k.key_prefix ? `${k.key_prefix}...` : t("clientKeys.prefixUnavailable")}
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
                        {k.recoverable ? (
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={revealKey.isPending || rotateKey.isPending}
                            onClick={() => handleRevealKey(k)}
                            title={t("clientKeys.reveal")}
                            aria-label={t("clientKeys.revealNamed", { name: k.name })}
                          >
                            <Eye className="h-3.5 w-3.5" />
                          </Button>
                        ) : (
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={rotateKey.isPending}
                            onClick={() => handleRotateKey(k)}
                            title={t("clientKeys.rotate")}
                            aria-label={t("clientKeys.rotateNamed", { name: k.name })}
                          >
                            <RotateCw className="h-3.5 w-3.5" />
                          </Button>
                        )}
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

      <Dialog open={!!revealedKey} onOpenChange={(open) => { if (!open) setRevealedKey(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("clientKeys.revealTitle", { name: revealedKey?.name })}</DialogTitle>
            <DialogDescription>{t("clientKeys.revealDescription")}</DialogDescription>
          </DialogHeader>
          {revealedKey && (
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 break-all rounded-md border border-border bg-muted/40 px-3 py-2.5 font-mono text-sm">
                {revealedKey.rawKey}
              </code>
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleCopyKey(revealedKey.rawKey)}
                aria-label={t("clientKeys.copyKey")}
              >
                {copiedValue === revealedKey.rawKey ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
              </Button>
            </div>
          )}
          <DialogFooter>
            <Button onClick={() => setRevealedKey(null)}>{t("common.done")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
