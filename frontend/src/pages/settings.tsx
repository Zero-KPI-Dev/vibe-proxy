import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { toast } from "sonner"
import { Palette, Shield, Save } from "lucide-react"
import { setToken, getToken } from "@/lib/api"
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
  const [adminToken, setAdminToken] = useState(getToken())
  const [theme, setTheme] = useState<Theme>(
    (localStorage.getItem("vibe_theme") as Theme | null) ?? "dark"
  )

  const handleSaveToken = () => {
    setToken(adminToken)
    toast.success("Admin token saved")
  }

  const handleThemeChange = (next: Theme) => {
    setTheme(next)
    applyTheme(next)
    toast.success(next === "light" ? "Light theme enabled" : "Dark theme enabled")
  }

  return (
    <div className="max-w-2xl space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Settings</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Global vibe-proxy settings
        </p>
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Shield className="h-5 w-5 text-primary" />
            <CardTitle className="text-base">Admin Authentication</CardTitle>
          </div>
          <CardDescription>
            The admin bearer token is used to authenticate all admin API requests. Stored in your browser's localStorage.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="adminToken">Admin Token</Label>
            <div className="flex gap-2">
              <Input
                id="adminToken"
                type="password"
                value={adminToken}
                onChange={(e) => setAdminToken(e.target.value)}
                placeholder="Enter admin token..."
                className="flex-1"
              />
              <Button onClick={handleSaveToken}>
                <Save className="h-4 w-4 mr-1" />
                Save
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Token is saved to localStorage and sent as a Bearer token with every admin API request.
            </p>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Palette className="h-5 w-5 text-primary" />
            <CardTitle className="text-base">Appearance</CardTitle>
          </div>
          <CardDescription>
            Choose the control plane theme for this browser.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <Label htmlFor="theme">Theme</Label>
          <Select value={theme} onValueChange={(v) => handleThemeChange(v as Theme)}>
            <SelectTrigger id="theme">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="dark">Dark</SelectItem>
              <SelectItem value="light">Light</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            Saved locally in your browser and applied on startup.
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
