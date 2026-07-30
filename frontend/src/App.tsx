import { lazy, Suspense, useEffect, useState } from "react"
import { BrowserRouter, Routes, Route } from "react-router"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { Toaster } from "sonner"
import { AppLayout } from "@/layouts/app-layout"
import { Skeleton } from "@/components/ui/skeleton"

const DashboardPage = lazy(() => import("@/pages/dashboard").then((m) => ({ default: m.DashboardPage })))
const ProvidersPage = lazy(() => import("@/pages/providers").then((m) => ({ default: m.ProvidersPage })))
const ProviderNewPage = lazy(() => import("@/pages/provider-new").then((m) => ({ default: m.ProviderNewPage })))
const ProviderEditPage = lazy(() => import("@/pages/provider-edit").then((m) => ({ default: m.ProviderEditPage })))
const PlaygroundPage = lazy(() => import("@/pages/playground").then((m) => ({ default: m.PlaygroundPage })))
const ObservabilityPage = lazy(() => import("@/pages/observability").then((m) => ({ default: m.ObservabilityPage })))
const ClientKeysPage = lazy(() => import("@/pages/client-keys").then((m) => ({ default: m.ClientKeysPage })))
const ModelRoutingPage = lazy(() => import("@/pages/model-routing").then((m) => ({ default: m.ModelRoutingPage })))
const ConfigurationPage = lazy(() => import("@/pages/configuration").then((m) => ({ default: m.ConfigurationPage })))
const SettingsPage = lazy(() => import("@/pages/settings").then((m) => ({ default: m.SettingsPage })))

function PageLoader() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-4 w-72" />
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4 mt-6">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-24" />
        ))}
      </div>
      <Skeleton className="h-[300px] w-full mt-4" />
    </div>
  )
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      retry: 1,
    },
  },
})

function App() {
  const [theme, setTheme] = useState<"light" | "dark">(
    () => localStorage.getItem("vibe_theme") === "light" ? "light" : "dark"
  )

  useEffect(() => {
    const handleThemeChange = (event: Event) => {
      const next = (event as CustomEvent<"light" | "dark">).detail
      if (next === "light" || next === "dark") setTheme(next)
    }
    window.addEventListener("vibe-theme-change", handleThemeChange)
    return () => window.removeEventListener("vibe-theme-change", handleThemeChange)
  }, [])

  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AppLayout>
          <Suspense fallback={<PageLoader />}>
            <Routes>
              <Route path="/" element={<DashboardPage />} />
              <Route path="/providers" element={<ProvidersPage />} />
              <Route path="/providers/new" element={<ProviderNewPage />} />
              <Route path="/providers/:id/edit" element={<ProviderEditPage />} />
              <Route path="/playground" element={<PlaygroundPage />} />
              <Route path="/observability" element={<ObservabilityPage />} />
              <Route path="/client-keys" element={<ClientKeysPage />} />
              <Route path="/routing" element={<ModelRoutingPage />} />
              <Route path="/configuration" element={<ConfigurationPage />} />
              <Route path="/settings" element={<SettingsPage />} />
            </Routes>
          </Suspense>
        </AppLayout>
      </BrowserRouter>
      <Toaster
        position="bottom-right"
        theme={theme}
        toastOptions={{
          style: {
            background: theme === "light" ? "#ffffff" : "#0d0f14",
            border: theme === "light" ? "1px solid #e2e8f0" : "1px solid #222631",
            color: theme === "light" ? "#0f172a" : "#f5f7fb",
          },
        }}
      />
    </QueryClientProvider>
  )
}

export default App
