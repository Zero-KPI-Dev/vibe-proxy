import { cn } from "@/lib/utils"

interface BrandMarkProps {
  className?: string
}

/** Product mark shared by the control-plane shell and authentication screens. */
export function BrandMark({ className }: BrandMarkProps) {
  return (
    <img
      src="/favicon.svg"
      alt=""
      aria-hidden="true"
      draggable={false}
      className={cn(
        "inline-block shrink-0 rounded-[28%] shadow-sm shadow-primary/20",
        className,
      )}
    />
  )
}
