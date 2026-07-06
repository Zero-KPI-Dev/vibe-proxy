import { cn } from "@/lib/utils"
import { useHealth } from "@/hooks/use-health"

export function HealthBadge({ className }: { className?: string }) {
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
        ? `Running · ${new Date(health.loaded_at).toLocaleTimeString()}`
        : "Offline"}
    </span>
  )
}
