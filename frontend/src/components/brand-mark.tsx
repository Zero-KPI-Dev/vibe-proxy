import { cn } from "@/lib/utils"

interface BrandMarkProps {
  className?: string
}

/**
 * Product mark shared by the control-plane shell and authentication screens.
 * Keep the geometry aligned with desktop/assets/appicon.svg so the web and
 * native surfaces present one identity without embedding another image asset.
 */
export function BrandMark({ className }: BrandMarkProps) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "relative inline-flex shrink-0 overflow-hidden rounded-[28%] bg-[linear-gradient(135deg,#7657FF_0%,#5B32DB_48%,#26105F_100%)] shadow-sm shadow-primary/20",
        className,
      )}
    >
      <svg viewBox="0 0 32 32" className="h-full w-full" fill="none">
        <path
          d="M5.5 10.5H8c2.8 0 4 1.8 5.5 3.3l1.5 1.5M5.5 21.5H8c2.8 0 4-1.8 5.5-3.3l1.5-1.5"
          stroke="#EDE9FF"
          strokeWidth="2.7"
          strokeLinecap="round"
        />
        <path d="M18.5 16h8" stroke="#66F0C6" strokeWidth="3" strokeLinecap="round" />
        <path
          d="m14.5 7.5 4.2 8.5-4.2 8.5"
          stroke="#FFFFFF"
          strokeWidth="2.8"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </span>
  )
}
