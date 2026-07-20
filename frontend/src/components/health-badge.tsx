import { cn } from "@/lib/utils"
import { useHealth } from "@/hooks/use-health"
import { useTranslation } from "react-i18next"

export function HealthBadge({ className }: { className?: string }) {
  const { t, i18n } = useTranslation()
  const { data: health } = useHealth()
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
            time: new Date(health.loaded_at).toLocaleTimeString(i18n.language),
          })
        : t("status.offline")}
    </span>
  )
}
