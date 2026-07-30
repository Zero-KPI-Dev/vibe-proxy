import { useEffect, useState } from "react"
import { cn } from "@/lib/utils"
import { useHealth } from "@/hooks/use-health"
import { formatUptime } from "@/lib/runtime-status"
import { useTranslation } from "react-i18next"

export function HealthBadge({ className }: { className?: string }) {
  const { t } = useTranslation()
  const { data: health } = useHealth()
  const [now, setNow] = useState(Date.now)

  useEffect(() => {
    if (!health?.started_at) return
    setNow(Date.now())
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [health?.started_at])

  return (
    <span className={cn("inline-flex items-center gap-2 text-sm text-muted-foreground", className)}>
      <span
        className={cn(
          "h-2 w-2 rounded-full",
          health?.ok ? "bg-green-500 shadow-[0_0_8px] shadow-green-500/50" : "bg-red-500"
        )}
      />
      {health?.ok
        ? t("status.running", {
            duration: formatUptime(health.started_at, now),
          })
        : t("status.offline")}
    </span>
  )
}
