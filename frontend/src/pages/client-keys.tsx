import { useRef, useState } from "react"
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
import { Card, CardContent } from "@/components/ui/card"
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
import { KeyRound, Plus, Trash2, Copy, Check, Pencil, RotateCw } from "lucide-react"
import type { ClientKey } from "@/lib/types"
import { copyText } from "@/lib/clipboard"
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
  const [revealedKey, setRevealedKey] = useState<{ name: string; rawKey: string } | null>(null)
  const [copiedKeyName, setCopiedKeyName] = useState<string | null>(null)
  const [editingKey, setEditingKey] = useState<ClientKey | null>(null)
  const [editModels, setEditModels] = useState("")
  const [editRpm, setEditRpm] = useState("")
  const rawKeyCache = useRef(new Map<string, string>())

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
      rawKeyCache.current.set(res.key.name, res.raw_key)
      setRevealedKey({ name: res.key.name, rawKey: res.raw_key })
      setShowCreate(false)
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
      rawKeyCache.current.delete(name)
      setRevealedKey((current) => current?.name === name ? null : current)
      setCopiedKeyName((current) => current === name ? null : current)
      toast.success(t("clientKeys.deleted", { name }))
    } catch (e) {
      toast.error(t("clientKeys.deleteFailed"))
    }
  }

  const loadClientKey = async (key: ClientKey) => {
    const cached = rawKeyCache.current.get(key.name)
    if (cached) return cached
    if (!key.recoverable) {
      toast.error(t("clientKeys.legacyUnavailable"))
      return null
    }
    try {
      const result = await revealKey.mutateAsync(key.name)
      rawKeyCache.current.set(result.name, result.raw_key)
      return result.raw_key
    } catch (e) {
      toast.error(t("clientKeys.revealFailed", {
        error: e instanceof Error ? e.message : t("common.unknownError"),
      }))
      return null
    }
  }

  const handleToggleKey = async (key: ClientKey) => {
    if (revealedKey?.name === key.name) {
      setRevealedKey(null)
      return
    }
    const value = await loadClientKey(key)
    if (value) setRevealedKey({ name: key.name, rawKey: value })
  }

  const handleCopyKey = async (key: ClientKey) => {
    const value = await loadClientKey(key)
    if (!value) return
    if (!await copyText(value)) {
      toast.error(t("clientKeys.copyFailed"))
      return
    }
    setCopiedKeyName(key.name)
    toast.success(t("clientKeys.copied"))
    setTimeout(() => setCopiedKeyName((current) => current === key.name ? null : current), 2000)
  }

  const handleRotateKey = async (key: ClientKey) => {
    if (!window.confirm(t("clientKeys.rotateConfirm", { name: key.name }))) return
    try {
      const result = await rotateKey.mutateAsync(key.name)
      rawKeyCache.current.set(result.key.name, result.raw_key)
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
        <Dialog open={showCreate} onOpenChange={setShowCreate}>
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
            <Table className="table-fixed">
              <colgroup>
                <col className="w-[12%]" />
                <col className="w-[56%]" />
                <col className="w-[7%]" />
                <col className="w-[7%]" />
                <col className="w-[7%]" />
                <col className="w-[11%]" />
              </colgroup>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("clientKeys.name")}</TableHead>
                  <TableHead>{t("clientKeys.key")}</TableHead>
                  <TableHead>{t("clientKeys.models")}</TableHead>
                  <TableHead>{t("clientKeys.status")}</TableHead>
                  <TableHead>RPM</TableHead>
                  <TableHead>{t("common.actions")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((k) => (
                  <TableRow key={k.name}>
                    <TableCell className="font-medium">{k.name}</TableCell>
                    <TableCell>
                      <div className="flex w-full min-w-0 items-center gap-1.5">
                        <button
                          type="button"
                          className="min-w-0 flex-1 truncate whitespace-nowrap rounded-md px-1 py-1.5 text-left font-mono text-[11px] text-foreground transition-colors hover:bg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                          disabled={revealKey.isPending || rotateKey.isPending}
                          onClick={() => handleToggleKey(k)}
                          title={revealedKey?.name === k.name ? revealedKey.rawKey : t("clientKeys.reveal")}
                          aria-label={revealedKey?.name === k.name
                            ? t("clientKeys.hideNamed", { name: k.name })
                            : t("clientKeys.revealNamed", { name: k.name })}
                        >
                          <span className="break-all">
                            {revealedKey?.name === k.name
                              ? revealedKey.rawKey
                              : k.key_prefix
                                ? `${k.key_prefix.slice(0, 7)}**********`
                                : t("clientKeys.prefixUnavailable")}
                          </span>
                        </button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7 shrink-0"
                          disabled={!k.recoverable || revealKey.isPending || rotateKey.isPending}
                          onClick={() => handleCopyKey(k)}
                          title={t("clientKeys.copyKey")}
                          aria-label={t("clientKeys.copyNamed", { name: k.name })}
                        >
                          {copiedKeyName === k.name
                            ? <Check className="h-3.5 w-3.5 text-emerald-500" />
                            : <Copy className="h-3.5 w-3.5" />}
                        </Button>
                      </div>
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
                        {!k.recoverable && (
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7"
                            disabled={rotateKey.isPending}
                            onClick={() => handleRotateKey(k)}
                            title={t("clientKeys.rotate")}
                            aria-label={t("clientKeys.rotateNamed", { name: k.name })}
                          >
                            <RotateCw className="h-3.5 w-3.5" />
                          </Button>
                        )}
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7"
                          onClick={() => openEdit(k)}
                          title={t("common.edit")}
                          aria-label={`${t("common.edit")} ${k.name}`}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7"
                          onClick={() => handleDelete(k.name)}
                          title={t("common.delete")}
                          aria-label={`${t("common.delete")} ${k.name}`}
                        >
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
