export const NAV_ITEMS = [
  { label: "Dashboard", href: "/", icon: "LayoutDashboard" },
  { label: "Providers", href: "/providers", icon: "Server" },
  { label: "Playground", href: "/playground", icon: "MessagesSquare" },
  { label: "Observability", href: "/observability", icon: "BarChart3" },
  { label: "Client Keys", href: "/client-keys", icon: "KeyRound" },
  { label: "Model Routing", href: "/routing", icon: "Network" },
  { label: "Configuration", href: "/configuration", icon: "FileCode" },
  { label: "Settings", href: "/settings", icon: "Settings" },
] as const

export const TIME_RANGES = ["1h", "6h", "24h", "7d"] as const
