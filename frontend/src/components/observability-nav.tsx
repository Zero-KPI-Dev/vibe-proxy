import { Activity, ListTree, MessagesSquare } from "lucide-react"
import { Link, useLocation } from "react-router"
import { useTranslation } from "react-i18next"
import { cn } from "@/lib/utils"

const items = [
  { href: "/observability", key: "overview", icon: Activity, exact: true },
  { href: "/observability/requests", key: "requests", icon: ListTree, exact: false },
  { href: "/observability/sessions", key: "sessions", icon: MessagesSquare, exact: false },
] as const

export function ObservabilityNav() {
  const { t } = useTranslation()
  const location = useLocation()

  return (
    <nav className="flex w-full gap-1 overflow-x-auto rounded-xl border border-border bg-card p-1 sm:w-fit">
      {items.map(({ href, key, icon: Icon, exact }) => {
        const active = exact ? location.pathname === href : location.pathname.startsWith(href)
        return (
          <Link
            key={href}
            to={href}
            className={cn(
              "flex items-center gap-2 whitespace-nowrap rounded-lg px-3 py-2 text-sm font-medium transition-colors",
              active
                ? "bg-primary text-primary-foreground shadow-sm"
                : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
            )}
          >
            <Icon className="h-4 w-4" />
            {t(`observability.navigation.${key}`)}
          </Link>
        )
      })}
    </nav>
  )
}
