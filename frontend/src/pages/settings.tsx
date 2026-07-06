import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import { toast } from "sonner"
import { Shield, Save } from "lucide-react"
import { setToken, getToken } from "@/lib/api"

export function SettingsPage() {
  const [adminToken, setAdminToken] = useState(getToken())

  const handleSaveToken = () => {
    setToken(adminToken)
    toast.success("Admin token saved")
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
    </div>
  )
}
