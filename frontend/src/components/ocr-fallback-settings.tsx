import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Image, Loader2, Save, TestTube2 } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { multimodalApi } from "@/lib/api"
import type { MultimodalAdminConfig, MultimodalAdminInput, VisionFallbackModelOption } from "@/lib/types"

const defaultConfig: MultimodalAdminInput = {
  enabled: false,
  strategy: "ocr_then_vision",
  provider: "builtin",
  endpoint: "",
  auth_type: "none",
  api_key_source: "",
  api_key_env: "",
  api_key: "",
  header: "",
  vision_fallback_model: "",
  vision_fallback_strategy: "assist",
  min_confidence: 0.55,
  min_text_chars: 4,
  max_images: 4,
}

const visionSourceTranslationKeys: Record<VisionFallbackModelOption["source"], string> = {
  model_override: "settings.visionFallbackSourceModelOverride",
  provider_default: "settings.visionFallbackSourceProviderDefault",
  models_dev: "settings.visionFallbackSourceModelsDev",
}

function snapshotInput(response: MultimodalAdminConfig): MultimodalAdminInput {
  return {
    enabled: response.enabled,
    strategy: response.strategy,
    provider: response.provider,
    endpoint: response.endpoint,
    auth_type: response.auth_type,
    api_key_source: response.api_key_source,
    api_key_env: response.api_key_env,
    api_key: "",
    header: response.header,
    vision_fallback_model: response.vision_fallback_model,
    vision_fallback_strategy: response.vision_fallback_strategy || "assist",
    min_confidence: response.min_confidence,
    min_text_chars: response.min_text_chars,
    max_images: response.max_images,
  }
}

interface OCRFallbackSettingsProps {
  authVersion: number
}

export function OCRFallbackSettings({ authVersion }: OCRFallbackSettingsProps) {
  const { t } = useTranslation()
  const [config, setConfig] = useState<MultimodalAdminInput>(defaultConfig)
  const [visionModels, setVisionModels] = useState<VisionFallbackModelOption[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)

  useEffect(() => {
    let active = true
    setLoading(true)
    void Promise.allSettled([multimodalApi.get()]).then(([multimodal]) => {
      if (!active) return
      if (multimodal.status === "fulfilled") {
        const response = multimodal.value
        setConfig(snapshotInput(response))
        setVisionModels(response.vision_fallback_models ?? [])
      }
      setLoading(false)
    })
    return () => { active = false }
  }, [authVersion])

  const unavailableSelection = config.vision_fallback_model
    && !visionModels.some((option) => option.target === config.vision_fallback_model)
    ? config.vision_fallback_model
    : ""

  const update = <K extends keyof MultimodalAdminInput>(key: K, value: MultimodalAdminInput[K]) => {
    setConfig((current) => ({ ...current, [key]: value }))
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      const result = await multimodalApi.save(config)
      const response = result.multimodal
      setConfig(snapshotInput(response))
      setVisionModels(response.vision_fallback_models ?? [])
      toast.success(t("settings.ocrSaved"))
    } catch (error) {
      toast.error(t("settings.ocrSaveFailed", {
        error: error instanceof Error ? error.message : t("common.unknownError"),
      }))
    } finally {
      setSaving(false)
    }
  }

  const handleTest = async () => {
    if (config.provider === "http" && !config.endpoint) {
      toast.error(t("settings.ocrEndpointRequired"))
      return
    }
    setTesting(true)
    try {
      const result = await multimodalApi.testOCR(config)
      const message = t(
        result.provider === "builtin" ? "settings.builtinOCRTestSucceeded" : "settings.ocrTestSucceeded",
        { latency: result.latency_ms, confidence: result.confidence != null ? Math.round(result.confidence * 100) : "-" },
      )
      if (result.warning) {
        toast.warning(t(
          result.warning === "multimodal_disabled"
            ? "settings.ocrTestDisabledWarning"
            : "settings.ocrTestNotActiveWarning",
          { result: message },
        ))
      } else {
        toast.success(message)
      }
    } catch (error) {
      toast.error(t("settings.ocrTestFailed", {
        error: error instanceof Error ? error.message : t("common.unknownError"),
      }))
    } finally {
      setTesting(false)
    }
  }

  return (
    <Card>
      <CardHeader>
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1.5">
            <div className="flex items-center gap-2">
              <Image className="h-5 w-5 text-primary" />
              <CardTitle className="text-base">{t("settings.ocrFallback")}</CardTitle>
              <Badge variant={config.enabled ? "success" : "secondary"}>
                {config.enabled ? t("common.enabled") : t("common.disabled")}
              </Badge>
            </div>
            <CardDescription>{t("settings.ocrFallbackDescription")}</CardDescription>
          </div>
          <Switch
            aria-label={t("settings.ocrEnable")}
            checked={config.enabled}
            onCheckedChange={(checked) => update("enabled", checked)}
            disabled={loading}
          />
        </div>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="rounded-md border border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
          {t("settings.ocrRouteHelp")}
        </div>

        <div className="space-y-2">
          <Label>{t("settings.ocrProvider")}</Label>
          <Select
            value={config.provider}
            onValueChange={(value) => update("provider", value as MultimodalAdminInput["provider"])}
            disabled={loading}
          >
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="builtin">{t("settings.ocrProviderBuiltin")}</SelectItem>
              <SelectItem value="http">{t("settings.ocrProviderHTTP")}</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">{t("settings.ocrProviderHelp")}</p>
        </div>

        {config.provider === "builtin" ? (
          <div className="rounded-lg border border-primary/20 bg-primary/5 p-4">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="success">{t("settings.builtinOCRReady")}</Badge>
              <Badge variant="secondary">Tesseract WASM</Badge>
              <Badge variant="secondary">{t("settings.builtinOCRLanguages")}</Badge>
            </div>
            <p className="mt-2 text-sm text-muted-foreground">{t("settings.builtinOCRDescription")}</p>
          </div>
        ) : (
          <>
            <div className="space-y-2">
              <Label htmlFor="ocrEndpoint">{t("settings.ocrEndpoint")}</Label>
              <Input
                id="ocrEndpoint"
                value={config.endpoint}
                onChange={(event) => update("endpoint", event.target.value)}
                placeholder="http://127.0.0.1:32180/v1/ocr"
                disabled={loading}
              />
              <p className="text-xs text-muted-foreground">{t("settings.ocrContractHelp")}</p>
            </div>

            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-2">
                <Label>{t("settings.ocrAuthType")}</Label>
                <Select
                  value={config.auth_type}
                  onValueChange={(value) => update("auth_type", value as MultimodalAdminInput["auth_type"])}
                  disabled={loading}
                >
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="none">{t("providerForm.none")}</SelectItem>
                    <SelectItem value="bearer">{t("providerForm.bearerToken")}</SelectItem>
                    <SelectItem value="api_key_header">{t("providerForm.apiKeyHeader")}</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              {config.auth_type !== "none" && (
                <div className="space-y-2">
                  <Label>{t("providerForm.keySource")}</Label>
                  <Select
                    value={config.api_key_source || "env"}
                    onValueChange={(value) => update("api_key_source", value as "env" | "literal")}
                    disabled={loading}
                  >
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="env">{t("providerForm.environmentVariable")}</SelectItem>
                      <SelectItem value="literal">{t("providerForm.pasteKey")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              )}

              {config.auth_type !== "none" && (config.api_key_source || "env") === "env" && (
                <div className="space-y-2">
                  <Label htmlFor="ocrKeyEnv">{t("providerForm.apiKeyEnv")}</Label>
                  <Input
                    id="ocrKeyEnv"
                    value={config.api_key_env}
                    onChange={(event) => update("api_key_env", event.target.value)}
                    placeholder="OCR_API_KEY"
                    disabled={loading}
                  />
                </div>
              )}

              {config.auth_type !== "none" && config.api_key_source === "literal" && (
                <div className="space-y-2">
                  <Label htmlFor="ocrKey">{t("providerForm.apiKey")}</Label>
                  <Input
                    id="ocrKey"
                    type="password"
                    autoComplete="off"
                    value={config.api_key ?? ""}
                    onChange={(event) => update("api_key", event.target.value)}
                    placeholder={t("providerForm.keepCurrentKey")}
                    disabled={loading}
                  />
                </div>
              )}

              {config.auth_type === "api_key_header" && (
                <div className="space-y-2">
                  <Label htmlFor="ocrHeader">{t("providerForm.headerName")}</Label>
                  <Input
                    id="ocrHeader"
                    value={config.header}
                    onChange={(event) => update("header", event.target.value)}
                    placeholder="x-api-key"
                    disabled={loading}
                  />
                </div>
              )}
            </div>
          </>
        )}

        <div className="space-y-2">
          <Label>{t("settings.visionFallbackStrategy")}</Label>
          <Select
            value={config.vision_fallback_strategy}
            onValueChange={(value) => update("vision_fallback_strategy", value as MultimodalAdminInput["vision_fallback_strategy"])}
            disabled={loading}
          >
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="assist">{t("settings.visionFallbackAssist")}</SelectItem>
              <SelectItem value="takeover">{t("settings.visionFallbackTakeover")}</SelectItem>
              <SelectItem value="reject">{t("settings.visionFallbackReject")}</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            {t(`settings.visionFallbackStrategyHelp.${config.vision_fallback_strategy}`)}
          </p>
        </div>

        <div className="space-y-2">
          <Label>{t("settings.visionFallbackModel")}</Label>
          <Select
            value={config.vision_fallback_model || "none"}
            onValueChange={(value) => update("vision_fallback_model", value === "none" ? "" : value)}
            disabled={loading}
          >
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="none">{t("settings.visionFallbackNone")}</SelectItem>
              {unavailableSelection && (
                <SelectItem value={unavailableSelection}>
                  {unavailableSelection} ({t("settings.visionFallbackUnavailable")})
                </SelectItem>
              )}
              {visionModels.map((option) => (
                <SelectItem key={option.target} value={option.target}>
                  <span className="flex w-full items-center justify-between gap-3">
                    <span>{option.target}</span>
                    <span className="text-xs text-muted-foreground">
                      {t(visionSourceTranslationKeys[option.source])}
                    </span>
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            {visionModels.length > 0
              ? t("settings.visionFallbackHelp")
              : t("settings.visionFallbackEmpty")}
          </p>
        </div>

        <details className="rounded-md border border-border px-3 py-2">
          <summary className="cursor-pointer text-sm font-medium">{t("settings.ocrAdvanced")}</summary>
          <div className="mt-4 grid gap-4 sm:grid-cols-3">
            <div className="space-y-2">
              <Label htmlFor="ocrConfidence">{t("settings.ocrMinConfidence")}</Label>
              <Input id="ocrConfidence" type="number" min="0" max="1" step="0.05" value={config.min_confidence} onChange={(event) => update("min_confidence", Number(event.target.value))} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ocrMinText">{t("settings.ocrMinText")}</Label>
              <Input id="ocrMinText" type="number" min="1" max="1000" value={config.min_text_chars} onChange={(event) => update("min_text_chars", Number(event.target.value))} />
            </div>
            <div className="space-y-2">
              <Label htmlFor="ocrMaxImages">{t("settings.ocrMaxImages")}</Label>
              <Input id="ocrMaxImages" type="number" min="1" max="16" value={config.max_images} onChange={(event) => update("max_images", Number(event.target.value))} />
            </div>
          </div>
        </details>

        <div className="flex flex-wrap gap-2">
          <Button type="button" onClick={() => void handleSave()} disabled={loading || saving || testing}>
            {saving ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <Save className="mr-1 h-4 w-4" />}
            {saving ? t("common.saving") : t("settings.saveOCR")}
          </Button>
          <Button type="button" variant="outline" onClick={() => void handleTest()} disabled={loading || saving || testing || (config.provider === "http" && !config.endpoint)}>
            {testing ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <TestTube2 className="mr-1 h-4 w-4" />}
            {testing ? t("settings.ocrTesting") : t("settings.testOCR")}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
