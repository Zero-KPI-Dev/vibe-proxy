import { useState } from "react"
import { useTranslation } from "react-i18next"
import { FolderOpen, Monitor, Upload } from "lucide-react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { desktopApi } from "@/lib/api"
import type { CloseBehavior, DesktopSnapshot } from "@/lib/types"

interface DesktopSettingsProps {
  desktop: DesktopSnapshot
  onDesktopChange: (desktop: DesktopSnapshot) => void
  onImported: () => Promise<void>
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Unknown error"
}

export function DesktopSettings({ desktop, onDesktopChange, onImported }: DesktopSettingsProps) {
  const { t } = useTranslation()
  const [savingCloseBehavior, setSavingCloseBehavior] = useState(false)
  const [openingDataDirectory, setOpeningDataDirectory] = useState(false)
  const [importing, setImporting] = useState(false)

  const handleCloseBehaviorChange = async (closeBehavior: CloseBehavior) => {
    setSavingCloseBehavior(true)
    try {
      const result = await desktopApi.setCloseBehavior(closeBehavior)
      onDesktopChange({ ...desktop, close_behavior: result.close_behavior })
    } catch (error) {
      const message = errorMessage(error)
      toast.error(t(
        /invalid|validation/i.test(message)
          ? "settings.desktop.validationFailed"
          : "settings.desktop.operationFailed",
        { error: message },
      ))
    } finally {
      setSavingCloseBehavior(false)
    }
  }

  const handleOpenDataDirectory = async () => {
    setOpeningDataDirectory(true)
    try {
      const result = await desktopApi.openDataDir()
      if (!result.opened) throw new Error("open data directory was not confirmed")
    } catch (error) {
      toast.error(t("settings.desktop.operationFailed", { error: errorMessage(error) }))
    } finally {
      setOpeningDataDirectory(false)
    }
  }

  const handleImportConfig = async () => {
    setImporting(true)
    try {
      const result = await desktopApi.importConfig()
      if (!result.imported) {
        toast.message(t("settings.desktop.importCancelled"))
        return
      }
      await onImported()
      toast.success(t("settings.desktop.imported"))
    } catch (error) {
      toast.error(t("settings.desktop.operationFailed", { error: errorMessage(error) }))
    } finally {
      setImporting(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <Monitor className="h-5 w-5 text-primary" />
          <CardTitle className="text-base">{t("settings.desktop.title")}</CardTitle>
        </div>
        <CardDescription>{t("settings.desktop.description")}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="rounded-md border border-border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
          {t("settings.desktop.authenticated")}
        </div>

        <div className="space-y-2">
          <Label htmlFor="desktopCloseBehavior">{t("settings.desktop.closeBehavior")}</Label>
          <Select
            value={desktop.close_behavior ?? "ask"}
            onValueChange={(value) => void handleCloseBehaviorChange(value as CloseBehavior)}
            disabled={savingCloseBehavior}
          >
            <SelectTrigger id="desktopCloseBehavior"><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="ask">{t("settings.desktop.ask")}</SelectItem>
              <SelectItem value="tray">{t("settings.desktop.tray")}</SelectItem>
              <SelectItem value="quit">{t("settings.desktop.quit")}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="rounded-md border border-border bg-muted/20 p-3">
            <p className="text-sm font-medium">{t("settings.desktop.apiEndpoint")}</p>
            <p className="mt-1 select-all break-all font-mono text-xs text-muted-foreground">
              {desktop.data_address ?? desktop.listen_address
                ? `http://${desktop.data_address ?? desktop.listen_address}`
                : "—"}
            </p>
            <p className="mt-2 text-xs text-muted-foreground">{t("settings.desktop.apiEndpointHelp")}</p>
          </div>
          <div className="rounded-md border border-border bg-muted/20 p-3">
            <p className="text-sm font-medium">{t("settings.desktop.adminEndpoint")}</p>
            <p className="mt-1 select-all break-all font-mono text-xs text-muted-foreground">
              {desktop.admin_address ? `http://${desktop.admin_address}` : "—"}
            </p>
            <p className="mt-2 text-xs text-muted-foreground">{t("settings.desktop.adminEndpointHelp")}</p>
          </div>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1">
            <p className="text-sm font-medium">{t("settings.desktop.dataDirectory")}</p>
            <p className="select-all break-all font-mono text-xs text-muted-foreground">{desktop.data_dir ?? "—"}</p>
          </div>
          <div className="space-y-1">
            <p className="text-sm font-medium">{t("settings.desktop.logsDirectory")}</p>
            <p className="select-all break-all font-mono text-xs text-muted-foreground">{desktop.log_dir ?? "—"}</p>
          </div>
        </div>

        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" onClick={() => void handleOpenDataDirectory()} disabled={openingDataDirectory}>
            <FolderOpen className="mr-1 h-4 w-4" />
            {t("settings.desktop.openData")}
          </Button>
          <Button type="button" variant="outline" onClick={() => void handleImportConfig()} disabled={importing}>
            <Upload className="mr-1 h-4 w-4" />
            {t("settings.desktop.importConfig")}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
