import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { toast } from "sonner"
import { Database, Languages, Palette, RefreshCw, Shield, Save } from "lucide-react"
import { setToken, getToken, modelCatalogApi } from "@/lib/api"
import type { ModelCatalogState } from "@/lib/types"
import { normalizeLanguage, setAppLanguage, type AppLanguage } from "@/i18n"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"

type Theme = "dark" | "light"

function applyTheme(theme: Theme) {
  localStorage.setItem("vibe_theme", theme)
  document.documentElement.classList.toggle("light", theme === "light")
  document.documentElement.classList.toggle("dark", theme === "dark")
}

export function SettingsPage() {
  const { t, i18n } = useTranslation()
  const [adminToken, setAdminToken] = useState(getToken())
  const [theme, setTheme] = useState<Theme>(
    (localStorage.getItem("vibe_theme") as Theme | null) ?? "dark"
  )
  const [catalog, setCatalog] = useState<ModelCatalogState | null>(null)
  const [catalogLoading, setCatalogLoading] = useState(false)

  const loadCatalogStatus = async () => {
    if (!getToken()) return
    try {
      const result = await modelCatalogApi.status()
      setCatalog(result.catalog)
    } catch {
      // The admin token may not be configured yet. Keep Settings usable.
    }
  }

  useEffect(() => {
    void loadCatalogStatus()
  }, [])

  const handleSaveToken = () => {
    setToken(adminToken)
    toast.success(t("settings.tokenSaved"))
    void loadCatalogStatus()
  }

  const handleCatalogRefresh = async () => {
    setCatalogLoading(true)
    try {
      const result = await modelCatalogApi.refresh()
      setCatalog(result.catalog)
      toast.success(t("settings.catalogRefreshed", { count: result.catalog.models }))
    } catch (error) {
      toast.error(t("settings.catalogRefreshFailed", {
        error: error instanceof Error ? error.message : t("common.unknownError"),
      }))
      await loadCatalogStatus()
    } finally {
      setCatalogLoading(false)
    }
  }

  const handleThemeChange = (next: Theme) => {
    setTheme(next)
    applyTheme(next)
    toast.success(next === "light" ? t("settings.lightEnabled") : t("settings.darkEnabled"))
  }

  const handleLanguageChange = async (next: AppLanguage) => {
    await setAppLanguage(next)
    toast.success(i18n.t("settings.languageChanged"))
  }

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">{t("settings.title")}</h1>
        <p className="text-sm text-muted-foreground mt-1">
          {t("settings.description")}
        </p>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Shield className="h-5 w-5 text-primary" />
            <CardTitle className="text-base">{t("settings.adminAuth")}</CardTitle>
          </div>
          <CardDescription>
            {t("settings.adminDescription")}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="adminToken">{t("settings.adminToken")}</Label>
            <div className="flex gap-2">
              <Input
                id="adminToken"
                type="password"
                value={adminToken}
                onChange={(e) => setAdminToken(e.target.value)}
                placeholder={t("settings.adminPlaceholder")}
                className="flex-1"
              />
              <Button onClick={handleSaveToken}>
                <Save className="h-4 w-4 mr-1" />
                {t("common.save")}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("settings.tokenHelp")}
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Database className="h-5 w-5 text-primary" />
            <CardTitle className="text-base">{t("settings.modelCatalog")}</CardTitle>
          </div>
          <CardDescription>{t("settings.modelCatalogDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-3 text-sm">
            <div className="rounded-md border border-border p-3">
              <div className="text-xs text-muted-foreground">{t("settings.catalogModels")}</div>
              <div className="mt-1 text-lg font-semibold">{catalog?.models ?? 0}</div>
            </div>
            <div className="rounded-md border border-border p-3">
              <div className="text-xs text-muted-foreground">{t("settings.catalogUpdated")}</div>
              <div className="mt-1 text-sm font-medium">
                {catalog?.fetched_at
                  ? new Date(catalog.fetched_at).toLocaleString(i18n.language)
                  : t("settings.catalogNever")}
              </div>
            </div>
          </div>
          <div className="flex items-center justify-between gap-3">
            <p className="text-xs text-muted-foreground">
              {catalog?.error
                ? t("settings.catalogError", { error: catalog.error })
                : catalog?.stale
                  ? t("settings.catalogStale")
                  : t("settings.catalogReady")}
            </p>
            <Button
              type="button"
              variant="outline"
              onClick={() => void handleCatalogRefresh()}
              disabled={catalogLoading}
            >
              <RefreshCw className={`mr-1 h-4 w-4 ${catalogLoading ? "animate-spin" : ""}`} />
              {catalogLoading ? t("settings.catalogRefreshing") : t("settings.catalogRefresh")}
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Palette className="h-5 w-5 text-primary" />
            <CardTitle className="text-base">{t("settings.appearance")}</CardTitle>
          </div>
          <CardDescription>
            {t("settings.appearanceDescription")}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <Label htmlFor="theme">{t("settings.theme")}</Label>
          <Select value={theme} onValueChange={(v) => handleThemeChange(v as Theme)}>
            <SelectTrigger id="theme">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="dark">{t("settings.dark")}</SelectItem>
              <SelectItem value="light">{t("settings.light")}</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            {t("settings.themeHelp")}
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Languages className="h-5 w-5 text-primary" />
            <CardTitle className="text-base">{t("settings.language")}</CardTitle>
          </div>
          <CardDescription>{t("settings.languageDescription")}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <Label htmlFor="language">{t("settings.languageLabel")}</Label>
          <Select
            value={normalizeLanguage(i18n.resolvedLanguage)}
            onValueChange={(value) => void handleLanguageChange(value as AppLanguage)}
          >
            <SelectTrigger id="language">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="zh-CN">{t("settings.chinese")}</SelectItem>
              <SelectItem value="en">{t("settings.english")}</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">{t("settings.languageHelp")}</p>
        </CardContent>
      </Card>
    </div>
  )
}
