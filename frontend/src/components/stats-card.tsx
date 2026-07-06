import { Card, CardContent } from "@/components/ui/card"
import { cn } from "@/lib/utils"

interface StatsCardProps {
  label: string
  value: string | number
  icon: React.ReactNode
  trend?: string
  className?: string
}

export function StatsCard({ label, value, icon, trend, className }: StatsCardProps) {
  return (
    <Card className={cn("", className)}>
      <CardContent className="flex items-center gap-4 p-4">
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
          {icon}
        </div>
        <div className="flex-1 min-w-0">
          <p className="text-sm text-muted-foreground truncate">{label}</p>
          <p className="text-2xl font-semibold tabular-nums">{value}</p>
          {trend && <p className="text-xs text-muted-foreground">{trend}</p>}
        </div>
      </CardContent>
    </Card>
  )
}
