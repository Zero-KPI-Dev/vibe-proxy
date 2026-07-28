import { useState } from "react"
import { Link, useLocation } from "react-router"
import { useTranslation } from "react-i18next"
import {
  LayoutDashboard,
  Server,
  MessagesSquare,
  BarChart3,
  KeyRound,
  Network,
  FileCode,
  Settings,
  Menu,
  X,
} from "lucide-react"
import { cn } from "@/lib/utils"
import { useHealth } from "@/hooks/use-health"
import { NAV_ITEMS } from "@/lib/constants"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"

const iconMap: Record<string, React.ReactNode> = {
  LayoutDashboard: <LayoutDashboard className="h-4 w-4" />,
  Server: <Server className="h-4 w-4" />,
  MessagesSquare: <MessagesSquare className="h-4 w-4" />,
  BarChart3: <BarChart3 className="h-4 w-4" />,
  KeyRound: <KeyRound className="h-4 w-4" />,
  Network: <Network className="h-4 w-4" />,
  FileCode: <FileCode className="h-4 w-4" />,
  Settings: <Settings className="h-4 w-4" />,
}

export function AppLayout({ children }: { children: React.ReactNode }) {
  const { t, i18n } = useTranslation()
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const location = useLocation()
  const { data: health } = useHealth()

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      <aside
        className={cn(
          "fixed inset-y-0 left-0 z-40 w-60 border-r border-border bg-card transform transition-transform duration-200 lg:relative lg:translate-x-0",
          sidebarOpen ? "translate-x-0" : "-translate-x-full"
        )}
      >
        <div className="flex h-full flex-col">
          <div className="flex h-14 items-center gap-3 px-4 border-b border-border">
            <div className="h-7 w-7 rounded-lg bg-gradient-to-br from-[#9b8cff] to-[#38bdf8] shrink-0" />
            <div className="font-semibold">vibe-proxy</div>
          </div>

          <nav className="flex-1 space-y-1 p-3">
            {NAV_ITEMS.map((item) => {
              const active = location.pathname === item.href
              return (
                <Link
                  key={item.href}
                  to={item.href}
                  onClick={() => setSidebarOpen(false)}
                  className={cn(
                    "flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
                    active
                      ? "bg-accent text-accent-foreground font-medium"
                      : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                  )}
                >
                  {iconMap[item.icon]}
                  {t(item.labelKey)}
                </Link>
              )
            })}
          </nav>

          <div className="p-3 border-t border-border">
            <div className="flex items-center gap-2 rounded-lg px-3 py-2 text-xs text-muted-foreground">
              <span
                className={cn(
                  "h-2 w-2 rounded-full",
                  health?.ok ? "bg-green-500 shadow-[0_0_8px] shadow-green-500" : "bg-red-500"
                )}
              />
              {health?.ok
                ? t("status.running", {
                    time: new Date(health.loaded_at).toLocaleTimeString(i18n.language),
                  })
                : t("status.offline")}
            </div>
          </div>
        </div>
      </aside>

      <div className="flex flex-1 flex-col min-w-0">
        <header className="flex h-14 items-center gap-4 border-b border-border px-4 lg:px-6">
          <Button
            variant="ghost"
            size="icon"
            className="lg:hidden"
            onClick={() => setSidebarOpen(!sidebarOpen)}
          >
            {sidebarOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
          </Button>
          <Separator orientation="vertical" className="h-6 lg:hidden" />
        </header>

        <main className="flex-1 overflow-auto p-4 lg:p-6">
          {children}
        </main>
      </div>
    </div>
  )
}
