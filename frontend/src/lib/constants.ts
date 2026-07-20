export const NAV_ITEMS = [
  { labelKey: "nav.dashboard", href: "/", icon: "LayoutDashboard" },
  { labelKey: "nav.providers", href: "/providers", icon: "Server" },
  { labelKey: "nav.playground", href: "/playground", icon: "MessagesSquare" },
  { labelKey: "nav.observability", href: "/observability", icon: "BarChart3" },
  { labelKey: "nav.clientKeys", href: "/client-keys", icon: "KeyRound" },
  { labelKey: "nav.routing", href: "/routing", icon: "Network" },
  { labelKey: "nav.configuration", href: "/configuration", icon: "FileCode" },
  { labelKey: "nav.settings", href: "/settings", icon: "Settings" },
] as const

export const TIME_RANGES = ["1h", "6h", "24h", "7d"] as const
