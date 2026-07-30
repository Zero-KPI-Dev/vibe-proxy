export function formatUptime(startedAt: string | undefined, now = Date.now()): string {
  if (!startedAt) return "--:--:--"
  const startedAtMs = Date.parse(startedAt)
  if (!Number.isFinite(startedAtMs)) return "--:--:--"

  const totalSeconds = Math.max(0, Math.floor((now - startedAtMs) / 1000))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  return [hours, minutes, seconds].map((value) => String(value).padStart(2, "0")).join(":")
}
