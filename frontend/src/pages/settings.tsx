import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { toast } from "sonner"
import { Languages, Palette, Shield, Save } from "lucide-react"
import { setToken, getToken } from "@/lib/api"
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

  const handleSaveToken = () => {
    setToken(adminToken)
    toast.success(t("settings.tokenSaved"))
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
