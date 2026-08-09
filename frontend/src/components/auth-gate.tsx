import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import {
  ArrowRight,
  Check,
  Eye,
  EyeOff,
  KeyRound,
  Laptop,
  LoaderCircle,
  LockKeyhole,
  RefreshCw,
  ShieldCheck,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { BrandMark } from "@/components/brand-mark"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  AUTH_REQUIRED_EVENT,
  AuthAPIError,
  authApi,
  type AuthStatus,
} from "@/lib/auth-api"

type AuthScreen = "loading" | "setup" | "login" | "native-required" | "ready" | "error"

function screenForStatus(status: AuthStatus): AuthScreen {
  if (status.mode === "token") return "ready"
  if (status.authenticated && status.initialized) return "ready"
  if (status.authenticated) return "setup"
  if (status.initialized) return "login"
  return "native-required"
}

function Brand() {
  const { t } = useTranslation()
  return (
    <div className="flex items-center justify-center gap-3">
      <BrandMark className="h-10 w-10 shadow-lg" />
      <div className="text-left">
        <div className="text-lg font-semibold tracking-tight">vibe-proxy</div>
        <div className="text-[11px] uppercase tracking-[0.22em] text-muted-foreground">{t("auth.tagline")}</div>
      </div>
    </div>
  )
}

function AuthShell({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-background px-4 py-10">
      <div className="pointer-events-none absolute inset-0">
        <div className="absolute left-[-12rem] top-[-10rem] h-[30rem] w-[30rem] rounded-full bg-primary/10 blur-3xl" />
        <div className="absolute bottom-[-14rem] right-[-10rem] h-[34rem] w-[34rem] rounded-full bg-cyan-400/10 blur-3xl" />
        <div className="absolute inset-0 opacity-[0.035] [background-image:linear-gradient(to_right,currentColor_1px,transparent_1px),linear-gradient(to_bottom,currentColor_1px,transparent_1px)] [background-size:32px_32px]" />
      </div>

      <div className="relative w-full max-w-[460px] space-y-6">
        <Brand />
        {children}
        <div className="flex flex-wrap items-center justify-center gap-x-5 gap-y-2 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1.5">
            <Check className="h-3.5 w-3.5 text-emerald-500" />
            {t("auth.localOnly")}
          </span>
          <span className="inline-flex items-center gap-1.5">
            <Check className="h-3.5 w-3.5 text-emerald-500" />
            {t("auth.httpOnlySession")}
          </span>
          <span className="inline-flex items-center gap-1.5">
            <Check className="h-3.5 w-3.5 text-emerald-500" />
            {t("auth.separateProviderKeys")}
          </span>
        </div>
      </div>
    </main>
  )
}

function PasswordInput({
  id,
  value,
  onChange,
  autoComplete,
  placeholder,
  disabled,
}: {
  id: string
  value: string
  onChange: (value: string) => void
  autoComplete: "current-password" | "new-password"
  placeholder: string
  disabled: boolean
}) {
  const { t } = useTranslation()
  const [visible, setVisible] = useState(false)
  return (
    <div className="relative">
      <Input
        id={id}
        type={visible ? "text" : "password"}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        autoComplete={autoComplete}
        placeholder={placeholder}
        disabled={disabled}
        maxLength={1024}
        className="h-11 pr-11"
      />
      <button
        type="button"
        onClick={() => setVisible((current) => !current)}
        className="absolute inset-y-0 right-0 inline-flex w-11 items-center justify-center text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
        aria-label={visible ? t("auth.hidePassword") : t("auth.showPassword")}
      >
        {visible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
      </button>
    </div>
  )
}

function AuthCard({
  icon,
  eyebrow,
  title,
  description,
  children,
}: {
  icon: ReactNode
  eyebrow: string
  title: string
  description: string
  children: ReactNode
}) {
  return (
    <Card className="border-border/80 bg-card/95 shadow-2xl shadow-black/10 backdrop-blur">
      <CardHeader className="space-y-4 pb-5">
        <div className="flex items-center justify-between">
          <div className="flex h-11 w-11 items-center justify-center rounded-xl bg-primary/10 text-primary">
            {icon}
          </div>
          <span className="rounded-full border border-border bg-muted/40 px-3 py-1 text-[11px] font-medium text-muted-foreground">
            {eyebrow}
          </span>
        </div>
        <div className="space-y-2">
          <CardTitle className="text-2xl">{title}</CardTitle>
          <CardDescription className="leading-6">{description}</CardDescription>
        </div>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  )
}

export function AuthGate({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const [screen, setScreen] = useState<AuthScreen>("loading")
  const [password, setPassword] = useState("")
  const [confirmation, setConfirmation] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState("")

  const refreshStatus = useCallback(async () => {
    setScreen("loading")
    setError("")
    try {
      const status = await authApi.status()
      setScreen(screenForStatus(status))
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : String(requestError))
      setScreen("error")
    }
  }, [])

  useEffect(() => {
    void refreshStatus()
  }, [refreshStatus])

  useEffect(() => {
    const handleAuthRequired = () => void refreshStatus()
    window.addEventListener(AUTH_REQUIRED_EVENT, handleAuthRequired)
    return () => window.removeEventListener(AUTH_REQUIRED_EVENT, handleAuthRequired)
  }, [refreshStatus])

  const handleSetup = async (event: FormEvent) => {
    event.preventDefault()
    setError("")
    if ([...password].length < 8) {
      setError(t("auth.passwordTooShort"))
      return
    }
    if (password !== confirmation) {
      setError(t("auth.passwordMismatch"))
      return
    }
    setSubmitting(true)
    try {
      const status = await authApi.setup(password)
      setPassword("")
      setConfirmation("")
      setScreen(screenForStatus(status))
    } catch (requestError) {
      if (requestError instanceof AuthAPIError && requestError.status === 409) {
        setError(t("auth.alreadyConfigured"))
      } else if (requestError instanceof AuthAPIError && requestError.status === 400) {
        setError(t("auth.passwordInvalid"))
      } else {
        setError(requestError instanceof Error ? requestError.message : t("common.unknownError"))
      }
    } finally {
      setSubmitting(false)
    }
  }

  const handleLogin = async (event: FormEvent) => {
    event.preventDefault()
    setError("")
    if (!password) {
      setError(t("auth.passwordRequired"))
      return
    }
    setSubmitting(true)
    try {
      const status = await authApi.login(password)
      setPassword("")
      setScreen(screenForStatus(status))
    } catch (requestError) {
      if (requestError instanceof AuthAPIError && requestError.status === 401) {
        setError(t("auth.invalidPassword"))
      } else {
        setError(requestError instanceof Error ? requestError.message : t("common.unknownError"))
      }
    } finally {
      setSubmitting(false)
    }
  }

  if (screen === "ready") return children

  if (screen === "loading") {
    return (
      <AuthShell>
        <Card className="border-border/80 bg-card/95 p-10 shadow-2xl shadow-black/10 backdrop-blur">
          <div className="flex flex-col items-center gap-4 text-center">
            <LoaderCircle className="h-7 w-7 animate-spin text-primary" />
            <div>
              <p className="font-medium">{t("auth.checking")}</p>
              <p className="mt-1 text-sm text-muted-foreground">{t("auth.checkingDescription")}</p>
            </div>
          </div>
        </Card>
      </AuthShell>
    )
  }

  if (screen === "native-required") {
    return (
      <AuthShell>
        <AuthCard
          icon={<Laptop className="h-5 w-5" />}
          eyebrow={t("auth.firstRun")}
          title={t("auth.openNativeTitle")}
          description={t("auth.openNativeDescription")}
        >
          <div className="rounded-lg border border-primary/20 bg-primary/5 p-4 text-sm leading-6 text-muted-foreground">
            {t("auth.openNativeHelp")}
          </div>
          <Button className="mt-5 w-full" variant="outline" onClick={() => void refreshStatus()}>
            <RefreshCw className="h-4 w-4" />
            {t("auth.checkAgain")}
          </Button>
        </AuthCard>
      </AuthShell>
    )
  }

  if (screen === "error") {
    return (
      <AuthShell>
        <AuthCard
          icon={<KeyRound className="h-5 w-5" />}
          eyebrow={t("auth.connectionIssue")}
          title={t("auth.unavailableTitle")}
          description={t("auth.unavailableDescription")}
        >
          {error && (
            <div role="alert" className="mb-4 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}
          <Button className="w-full" onClick={() => void refreshStatus()}>
            <RefreshCw className="h-4 w-4" />
            {t("common.retry")}
          </Button>
        </AuthCard>
      </AuthShell>
    )
  }

  if (screen === "setup") {
    return (
      <AuthShell>
        <AuthCard
          icon={<ShieldCheck className="h-5 w-5" />}
          eyebrow={t("auth.firstRun")}
          title={t("auth.setupTitle")}
          description={t("auth.setupDescription")}
        >
          <form className="space-y-4" onSubmit={(event) => void handleSetup(event)}>
            <div className="space-y-2">
              <Label htmlFor="management-password">{t("auth.managementPassword")}</Label>
              <PasswordInput
                id="management-password"
                value={password}
                onChange={setPassword}
                autoComplete="new-password"
                placeholder={t("auth.setupPasswordPlaceholder")}
                disabled={submitting}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="management-password-confirmation">{t("auth.confirmPassword")}</Label>
              <PasswordInput
                id="management-password-confirmation"
                value={confirmation}
                onChange={setConfirmation}
                autoComplete="new-password"
                placeholder={t("auth.confirmPasswordPlaceholder")}
                disabled={submitting}
              />
            </div>
            <p className="text-xs leading-5 text-muted-foreground">{t("auth.passwordHelp")}</p>
            {error && (
              <div role="alert" className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
                {error}
              </div>
            )}
            <Button type="submit" className="h-11 w-full" disabled={submitting}>
              {submitting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
              {submitting ? t("auth.savingPassword") : t("auth.createPassword")}
            </Button>
          </form>
        </AuthCard>
      </AuthShell>
    )
  }

  return (
    <AuthShell>
      <AuthCard
        icon={<LockKeyhole className="h-5 w-5" />}
        eyebrow={t("auth.localControlPlane")}
        title={t("auth.loginTitle")}
        description={t("auth.loginDescription")}
      >
        <form className="space-y-4" onSubmit={(event) => void handleLogin(event)}>
          <div className="space-y-2">
            <Label htmlFor="login-password">{t("auth.managementPassword")}</Label>
            <PasswordInput
              id="login-password"
              value={password}
              onChange={setPassword}
              autoComplete="current-password"
              placeholder={t("auth.loginPasswordPlaceholder")}
              disabled={submitting}
            />
          </div>
          {error && (
            <div role="alert" className="rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}
          <Button type="submit" className="h-11 w-full" disabled={submitting}>
            {submitting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <LockKeyhole className="h-4 w-4" />}
            {submitting ? t("auth.signingIn") : t("auth.signIn")}
            {!submitting && <ArrowRight className="ml-auto h-4 w-4" />}
          </Button>
          <p className="text-center text-xs leading-5 text-muted-foreground">{t("auth.nativeAutoLogin")}</p>
          <p className="text-center text-[11px] leading-5 text-muted-foreground/80">{t("auth.resetPasswordHelp")}</p>
        </form>
      </AuthCard>
    </AuthShell>
  )
}
