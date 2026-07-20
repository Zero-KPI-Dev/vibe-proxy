import { useEffect, useMemo, useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import type { TFunction } from "i18next"
import { useTranslation } from "react-i18next"
import { Check, ChevronDown } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover"
import { cn } from "@/lib/utils"
import { modelCatalogApi, providerApi } from "@/lib/api"
import type { ModelCatalogMatch, ProviderFormData } from "@/lib/types"

const createProviderSchema = (t: TFunction, mode: "create" | "edit") => z
  .object({
    id: z.string().min(1, t("providerForm.validation.providerIdRequired")),
    type: z.enum(["openai-compatible", "anthropic"]),
    base_url: z.string()
      .min(1, t("providerForm.validation.baseUrlRequired"))
      .url(t("providerForm.validation.validUrl")),
    catalog_provider: z.string().optional(),
    auth_type: z.enum(["bearer", "api_key_header", "none"]),
    api_key_source: z.enum(["env", "literal"]).optional(),
    api_key_env: z.string().optional(),
    api_key: z.string().optional(),
    header: z.string().optional(),
    models: z.string().transform((s) => s.split(",").map((m) => m.trim()).filter(Boolean)),
    alias: z.string().optional(),
    alias_model: z.string().optional(),
    default_model: z.string().optional(),
    max_concurrency: z.coerce.number().int().positive().optional(),
    default_image_input: z.enum(["", "unknown", "supported", "unsupported"]).optional(),
    model_image_capabilities: z.record(z.enum(["unknown", "supported", "unsupported"])).optional(),
  })
  .superRefine((v, ctx) => {
    if (v.auth_type === "none") return
    const source = v.api_key_source ?? "env"
    if (source === "env" && !v.api_key_env) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["api_key_env"],
        message: t("providerForm.validation.apiKeyEnvRequired"),
      })
    }
    if (source === "literal" && !v.api_key && mode !== "edit") {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["api_key"],
        message: t("providerForm.validation.apiKeyRequired"),
      })
    }
    if (v.auth_type === "api_key_header" && !v.header) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["header"],
        message: t("providerForm.validation.headerRequired"),
      })
    }
  })

type ProviderFormValues = z.input<ReturnType<typeof createProviderSchema>>

interface ProviderFormProps {
  defaultValues?: Partial<ProviderFormValues>
  onSubmit: (data: ProviderFormData) => Promise<void>
  isPending: boolean
  mode: "create" | "edit"
}

export function ProviderForm({ defaultValues, onSubmit, isPending, mode }: ProviderFormProps) {
  const { t } = useTranslation()
  const providerSchema = useMemo(() => createProviderSchema(t, mode), [mode, t])
  const {
    register,
    handleSubmit,
    setValue,
    getValues,
    watch,
    formState: { errors },
  } = useForm<ProviderFormValues>({
    resolver: zodResolver(providerSchema),
    defaultValues: {
      id: "",
      type: "openai-compatible",
      base_url: "http://host.docker.internal:3000/v1",
      catalog_provider: "",
      api_key_env: "",
      api_key: "",
      api_key_source: "env",
      auth_type: "bearer",
      models: "",
      alias: "",
      alias_model: "",
      default_model: "",
      max_concurrency: 32,
      default_image_input: "",
      model_image_capabilities: {},
      ...defaultValues,
    },
  })

  const selectedType = watch("type")
  const selectedAuth = watch("auth_type")
  const selectedKeySource = watch("api_key_source") ?? "env"
  const [isFetchingModels, setIsFetchingModels] = useState(false)
  const [modelFetchMessage, setModelFetchMessage] = useState("")
  const initialModels = useMemo(() => {
    const value = defaultValues?.models
    return (typeof value === "string" ? value.split(",") : value ?? [])
      .map((model) => model.trim())
      .filter(Boolean)
  }, [defaultValues?.models])
  const [availableModels, setAvailableModels] = useState<string[]>(initialModels)
  const [modelDetails, setModelDetails] = useState<Record<string, ModelCatalogMatch>>({})
  const [modelFilter, setModelFilter] = useState("")
  const selectedModelsText = watch("models")
  const defaultImageInput = watch("default_image_input") ?? ""
  const modelImageCapabilities = watch("model_image_capabilities") ?? {}

  const selectedModels = new Set(
    (typeof selectedModelsText === "string" ? selectedModelsText.split(",") : selectedModelsText)
      .map((m) => m.trim())
      .filter(Boolean)
  )
  const filteredModels = useMemo(
    () => availableModels.filter((model) =>
      model.toLowerCase().includes(modelFilter.toLowerCase())
    ),
    [availableModels, modelFilter]
  )

  useEffect(() => {
    if (initialModels.length === 0) return
    let active = true
    void modelCatalogApi.lookup({
      provider_id: defaultValues?.id ?? "",
      catalog_provider: defaultValues?.catalog_provider ?? "",
      base_url: defaultValues?.base_url ?? "",
      models: initialModels,
    }).then((result) => {
      if (active) setModelDetails(result.matches ?? {})
    }).catch(() => {
      // A missing/stale catalog should not block editing an existing provider.
    })
    return () => { active = false }
  }, [defaultValues?.base_url, defaultValues?.catalog_provider, defaultValues?.id, initialModels])

  const writeSelectedModels = (models: Iterable<string>) => {
    setValue("models", Array.from(models).join(", "))
  }

  const handleFormSubmit = async (values: ProviderFormValues) => {
    // react-hook-form receives the resolver output here, so transformed fields
    // such as models may already be arrays. Avoid parsing the transformed
    // object a second time, because the Zod input schema expects models to be a
    // comma-separated string.
    const parsed = values as unknown as ProviderFormData
    const enabledModels = new Set(parsed.models ?? [])
    const modelImageCapabilities = Object.fromEntries(
      Object.entries(parsed.model_image_capabilities ?? {})
        .filter(([model]) => enabledModels.has(model))
    )
    await onSubmit({ ...parsed, model_image_capabilities: modelImageCapabilities })
  }

  const handleFetchModels = async () => {
    setModelFetchMessage("")
    setIsFetchingModels(true)
    try {
      const values = getValues()
      const payload = {
        ...values,
        models: typeof values.models === "string"
          ? values.models.split(",").map((m) => m.trim()).filter(Boolean)
          : values.models,
      } as unknown as ProviderFormData
      const result = await providerApi.models(payload)
      if (!result.ok) {
        setModelFetchMessage(t("providerForm.fetchFailed", {
          status: result.status ? ` (HTTP ${result.status})` : "",
        }))
        return
      }
      setAvailableModels(result.models)
      setModelDetails(result.model_details ?? {})
      const nextSelected = new Set(selectedModels)
      if (nextSelected.size === 0 && result.models.length === 1) {
        nextSelected.add(result.models[0]!)
        writeSelectedModels(nextSelected)
      }
      const matched = Object.values(result.model_details ?? {}).filter(
        (detail) => detail.status !== "not_found" && detail.status !== "ambiguous"
      ).length
      setModelFetchMessage(
        result.catalog_error
          ? t("providerForm.fetchedCatalogUnavailable", { count: result.models.length })
          : t("providerForm.fetchedWithCapabilities", { count: result.models.length, matched })
      )
    } catch (e) {
      setModelFetchMessage(e instanceof Error ? e.message : t("providerForm.fetchFailed", { status: "" }))
    } finally {
      setIsFetchingModels(false)
    }
  }

  const toggleModel = (model: string, checked: boolean) => {
    const next = new Set(selectedModels)
    if (checked) {
      next.add(model)
    } else {
      next.delete(model)
    }
    writeSelectedModels(next)
  }

  const selectFilteredModels = () => {
    const next = new Set(selectedModels)
    for (const model of filteredModels) next.add(model)
    writeSelectedModels(next)
  }

  const clearFilteredModels = () => {
    if (filteredModels.length === 0) {
      writeSelectedModels([])
      return
    }
    const next = new Set(selectedModels)
    for (const model of filteredModels) next.delete(model)
    writeSelectedModels(next)
  }

  const setModelImageCapability = (model: string, value: string) => {
    const next = { ...modelImageCapabilities }
    if (value === "auto") {
      delete next[model]
    } else {
      next[model] = value as "unknown" | "supported" | "unsupported"
    }
    setValue("model_image_capabilities", next, { shouldDirty: true })
  }

  return (
    <form onSubmit={handleSubmit(handleFormSubmit)} className="space-y-6">
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="id">{t("providerForm.providerId")}</Label>
          <Input
            id="id"
            placeholder="my-provider"
            {...register("id")}
            disabled={mode === "edit"}
          />
          {errors.id && <p className="text-xs text-destructive">{errors.id.message}</p>}
        </div>

        <div className="space-y-2">
          <Label htmlFor="type">{t("providerForm.protocol")}</Label>
          <Select
            defaultValue={defaultValues?.type ?? "openai-compatible"}
            onValueChange={(v) => setValue("type", v as "openai-compatible" | "anthropic")}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="openai-compatible">OpenAI-compatible</SelectItem>
              <SelectItem value="anthropic">Anthropic</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="catalog_provider">{t("providerForm.catalogProvider")}</Label>
          <Input
            id="catalog_provider"
            placeholder={t("providerForm.catalogProviderPlaceholder")}
            {...register("catalog_provider")}
          />
          <p className="text-xs text-muted-foreground">
            {t("providerForm.catalogProviderHelp")}
          </p>
        </div>

        <div className="space-y-2 sm:col-span-2">
          <Label htmlFor="base_url">{t("providerForm.baseUrl")}</Label>
          <Input
            id="base_url"
            placeholder={
              selectedType === "anthropic"
                ? "https://api.anthropic.com"
                : "https://api.openai.com/v1"
            }
            {...register("base_url")}
          />
          {errors.base_url && <p className="text-xs text-destructive">{errors.base_url.message}</p>}
        </div>

        <div className="space-y-2">
          <Label htmlFor="auth_type">{t("providerForm.authType")}</Label>
          <Select
            defaultValue={defaultValues?.auth_type ?? "bearer"}
            onValueChange={(v) => setValue("auth_type", v as "bearer" | "api_key_header" | "none")}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="bearer">{t("providerForm.bearerToken")}</SelectItem>
              <SelectItem value="api_key_header">{t("providerForm.apiKeyHeader")}</SelectItem>
              <SelectItem value="none">{t("providerForm.none")}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {selectedAuth !== "none" && (
          <div className="space-y-2">
            <Label htmlFor="api_key_source">{t("providerForm.keySource")}</Label>
            <Select
              defaultValue={defaultValues?.api_key_source ?? "env"}
              onValueChange={(v) => setValue("api_key_source", v as "env" | "literal")}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="env">{t("providerForm.environmentVariable")}</SelectItem>
                <SelectItem value="literal">{t("providerForm.pasteKey")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        )}

        {selectedAuth !== "none" && selectedKeySource === "env" && (
          <div className="space-y-2">
            <Label htmlFor="api_key_env">{t("providerForm.apiKeyEnv")}</Label>
            <Input id="api_key_env" placeholder="OPENAI_API_KEY" {...register("api_key_env")} />
            {errors.api_key_env && (
              <p className="text-xs text-destructive">{errors.api_key_env.message}</p>
            )}
          </div>
        )}

        {selectedAuth !== "none" && selectedKeySource === "literal" && (
          <div className="space-y-2">
            <Label htmlFor="api_key">{t("providerForm.apiKey")}</Label>
            <Input
              id="api_key"
              type="password"
              placeholder={mode === "edit" ? t("providerForm.keepCurrentKey") : "sk-..."}
              autoComplete="off"
              {...register("api_key")}
            />
            {errors.api_key && (
              <p className="text-xs text-destructive">{errors.api_key.message}</p>
            )}
            <p className="text-xs text-muted-foreground">
              {t("providerForm.literalKeyHelp")}
            </p>
          </div>
        )}

        {selectedAuth === "api_key_header" && (
          <div className="space-y-2">
            <Label htmlFor="header">{t("providerForm.headerName")}</Label>
            <Input id="header" placeholder="x-api-key" {...register("header")} />
            {errors.header && (
              <p className="text-xs text-destructive">{errors.header.message}</p>
            )}
            <p className="text-xs text-muted-foreground">
              {t("providerForm.headerHelp")}
            </p>
          </div>
        )}

        <div className="space-y-2 sm:col-span-2">
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="models">{t("providerForm.models")}</Label>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={handleFetchModels}
              disabled={isFetchingModels || !watch("base_url") || (selectedAuth !== "none" && selectedKeySource === "env" && !watch("api_key_env")) || (selectedAuth !== "none" && selectedKeySource === "literal" && !watch("api_key") && mode !== "edit")}
            >
              {isFetchingModels ? t("providerForm.fetchingModels") : t("providerForm.fetchModels")}
            </Button>
          </div>
          <Input
            id="models"
            placeholder={t("providerForm.modelsPlaceholder")}
            {...register("models")}
          />
          <p className="text-xs text-muted-foreground">
            {t("providerForm.modelsHelp")}
          </p>
          {modelFetchMessage && (
            <p className="text-xs text-muted-foreground">{modelFetchMessage}</p>
          )}
          {availableModels.length > 0 && (
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              <Popover>
                <PopoverTrigger asChild>
                  <Button type="button" variant="outline" className="justify-between sm:w-72">
                    <span>
                      {t("providerForm.chooseModels", {
                        selected: selectedModels.size,
                        total: availableModels.length,
                      })}
                    </span>
                    <ChevronDown className="h-4 w-4 opacity-70" />
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-[min(34rem,calc(100vw-2rem))] space-y-3">
                  <div className="space-y-1">
                    <div className="text-sm font-medium">{t("providerForm.selectProviderModels")}</div>
                    <div className="text-xs text-muted-foreground">
                      {t("providerForm.candidateHelp")}
                    </div>
                  </div>
                  <Input
                    value={modelFilter}
                    onChange={(e) => setModelFilter(e.target.value)}
                    placeholder={t("providerForm.filterModels")}
                  />
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-xs text-muted-foreground">
                      {t("providerForm.selectionSummary", {
                        selected: selectedModels.size,
                        shown: filteredModels.length,
                      })}
                    </div>
                    <div className="flex gap-2">
                      <Button type="button" variant="outline" size="sm" onClick={selectFilteredModels}>
                        {t("providerForm.selectShown")}
                      </Button>
                      <Button type="button" variant="ghost" size="sm" onClick={clearFilteredModels}>
                        {t("providerForm.clearShown")}
                      </Button>
                    </div>
                  </div>
                  <div className="max-h-72 overflow-y-auto rounded-md border border-border">
                    {filteredModels.length === 0 ? (
                      <div className="px-3 py-6 text-center text-sm text-muted-foreground">
                        {t("providerForm.noMatches")}
                      </div>
                    ) : (
                      filteredModels.map((model) => {
                        const checked = selectedModels.has(model)
                        const detail = modelDetails[model]
                        return (
                          <button
                            key={model}
                            type="button"
                            onClick={() => toggleModel(model, !checked)}
                            className={cn(
                              "flex w-full cursor-pointer items-center gap-2 border-b border-border px-3 py-2 text-left text-sm last:border-b-0 hover:bg-accent",
                              checked && "bg-accent/60"
                            )}
                          >
                            <span className={cn(
                              "flex h-4 w-4 shrink-0 items-center justify-center rounded border border-border",
                              checked && "border-primary bg-primary text-primary-foreground"
                            )}>
                              {checked && <Check className="h-3 w-3" />}
                            </span>
                            <span className="min-w-0 flex-1">
                              <span className="block truncate font-mono text-xs">{model}</span>
                              <span className="mt-1 flex flex-wrap gap-1">
                                {detail?.image_input === "supported" && (
                                  <Badge variant="success" className="px-1.5 py-0 text-[10px]">
                                    {t("providerForm.capabilityVision")}
                                  </Badge>
                                )}
                                {detail?.image_input === "unsupported" && (
                                  <Badge variant="outline" className="px-1.5 py-0 text-[10px]">
                                    {t("providerForm.capabilityText")}
                                  </Badge>
                                )}
                                {(!detail || detail.image_input === "unknown") && (
                                  <Badge variant="warning" className="px-1.5 py-0 text-[10px]">
                                    {detail?.status === "ambiguous"
                                      ? t("providerForm.capabilityAmbiguous")
                                      : t("providerForm.capabilityUnknown")}
                                  </Badge>
                                )}
                                {detail?.model?.tool_call && (
                                  <Badge variant="secondary" className="px-1.5 py-0 text-[10px]">
                                    {t("providerForm.capabilityTools")}
                                  </Badge>
                                )}
                                {detail?.model?.reasoning && (
                                  <Badge variant="secondary" className="px-1.5 py-0 text-[10px]">
                                    {t("providerForm.capabilityReasoning")}
                                  </Badge>
                                )}
                                {(detail?.model?.context_limit ?? 0) > 0 && (
                                  <Badge variant="secondary" className="px-1.5 py-0 text-[10px]">
                                    {t("providerForm.capabilityContext", {
                                      value: formatTokenLimit(detail?.model?.context_limit ?? 0),
                                    })}
                                  </Badge>
                                )}
                                {detail && detail.status !== "not_found" && detail.status !== "ambiguous" && (
                                  <Badge variant="outline" className="px-1.5 py-0 text-[10px] text-muted-foreground">
                                    models.dev
                                  </Badge>
                                )}
                              </span>
                            </span>
                          </button>
                        )
                      })
                    )}
                  </div>
                </PopoverContent>
              </Popover>
              <p className="text-xs text-muted-foreground">
                {t("providerForm.selectedHelp")}
              </p>
            </div>
          )}
        </div>

        <div className="space-y-2 sm:col-span-2">
          <Label>{t("providerForm.defaultImageCapability")}</Label>
          <Select
            value={defaultImageInput || "auto"}
            onValueChange={(value) => setValue(
              "default_image_input",
              value === "auto" ? "" : value as "unknown" | "supported" | "unsupported",
              { shouldDirty: true }
            )}
          >
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value="auto">{t("providerForm.capabilityAuto")}</SelectItem>
              <SelectItem value="supported">{t("providerForm.capabilitySupported")}</SelectItem>
              <SelectItem value="unsupported">{t("providerForm.capabilityUnsupported")}</SelectItem>
              <SelectItem value="unknown">{t("providerForm.capabilityUnknownExplicit")}</SelectItem>
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">{t("providerForm.defaultImageCapabilityHelp")}</p>
        </div>

        {selectedModels.size > 0 && (
          <details className="sm:col-span-2 rounded-md border border-border px-3 py-2">
            <summary className="cursor-pointer text-sm font-medium">
              {t("providerForm.modelCapabilityOverrides", { count: selectedModels.size })}
            </summary>
            <p className="mt-2 text-xs text-muted-foreground">
              {t("providerForm.modelCapabilityOverridesHelp")}
            </p>
            <div className="mt-3 max-h-72 space-y-2 overflow-y-auto pr-1">
              {Array.from(selectedModels).sort().map((model) => (
                <div key={model} className="grid items-center gap-2 rounded-md border border-border px-3 py-2 sm:grid-cols-[minmax(0,1fr)_12rem]">
                  <div className="min-w-0">
                    <div className="truncate font-mono text-xs">{model}</div>
                    <div className="mt-1 text-[11px] text-muted-foreground">
                      {modelDetails[model]?.image_input === "supported"
                        ? t("providerForm.catalogSaysVision")
                        : modelDetails[model]?.image_input === "unsupported"
                          ? t("providerForm.catalogSaysText")
                          : t("providerForm.catalogSaysUnknown")}
                    </div>
                  </div>
                  <Select
                    value={modelImageCapabilities[model] || "auto"}
                    onValueChange={(value) => setModelImageCapability(model, value)}
                  >
                    <SelectTrigger aria-label={t("providerForm.modelCapabilityFor", { model })}>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="auto">{t("providerForm.capabilityInherit")}</SelectItem>
                      <SelectItem value="supported">{t("providerForm.capabilitySupported")}</SelectItem>
                      <SelectItem value="unsupported">{t("providerForm.capabilityUnsupported")}</SelectItem>
                      <SelectItem value="unknown">{t("providerForm.capabilityUnknownExplicit")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              ))}
            </div>
          </details>
        )}

        <div className="space-y-2">
          <Label htmlFor="max_concurrency">{t("providerForm.maxConcurrency")}</Label>
          <Input
            id="max_concurrency"
            type="number"
            {...register("max_concurrency")}
          />
        </div>
      </div>

      <div className="border-t border-border pt-4">
        <h3 className="text-sm font-medium mb-4">{t("providerForm.aliasConfiguration")}</h3>
        <div className="grid gap-4 sm:grid-cols-3">
          <div className="space-y-2">
            <Label htmlFor="alias">{t("providerForm.alias")}</Label>
            <Input id="alias" placeholder="vibe-chat" {...register("alias")} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="alias_model">{t("providerForm.aliasTarget")}</Label>
            <Input id="alias_model" placeholder="gpt-4" {...register("alias_model")} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="default_model">{t("providerForm.defaultModel")}</Label>
            <Input id="default_model" placeholder="vibe-chat" {...register("default_model")} />
          </div>
        </div>
      </div>

      <div className="flex gap-2">
        <Button type="submit" disabled={isPending}>
          {isPending
            ? t("common.saving")
            : mode === "create"
              ? t("providerForm.createProvider")
              : t("providerForm.saveChanges")}
        </Button>
      </div>
    </form>
  )
}

function formatTokenLimit(value: number) {
  if (value >= 1_000_000) return `${Number((value / 1_000_000).toFixed(1))}M`
  if (value >= 1_000) return `${Number((value / 1_000).toFixed(1))}K`
  return String(value)
}
