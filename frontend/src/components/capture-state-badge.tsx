import { Badge } from "@/components/ui/badge"
import type { CaptureStatus } from "@/lib/types"
import { useTranslation } from "react-i18next"

const variants: Record<CaptureStatus, "success" | "warning" | "destructive" | "secondary" | "outline"> = {
  not_captured: "outline",
  captured: "success",
  redacted: "warning",
  truncated: "warning",
  expired: "secondary",
  dropped: "destructive",
  missing: "outline",
}

export function CaptureStateBadge({ state }: { state: CaptureStatus }) {
  const { t } = useTranslation()
  return <Badge variant={variants[state]}>{t(`observability.captureStates.${state}`)}</Badge>
}
