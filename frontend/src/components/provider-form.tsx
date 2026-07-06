import { useMemo, useState } from "react"
import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
import { Check, ChevronDown } from "lucide-react"
import { Button } from "@/components/ui/button"
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
import { providerApi } from "@/lib/api"
import type { ProviderFormData } from "@/lib/types"

const providerSchema = z
  .object({
    id: z.string().min(1, "Provider ID is required"),
    type: z.enum(["openai-compatible", "anthropic"]),
    base_url: z.string().min(1, "Base URL is required").url("Must be a valid URL"),
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
  })
  .superRefine((v, ctx) => {
    if (v.auth_type === "none") return
    const source = v.api_key_source ?? "env"
    if (source === "env" && !v.api_key_env) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["api_key_env"],
        message: "API key env var is required",
      })
    }
    if (source === "literal" && !v.api_key && !isEditMode()) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["api_key"],
        message: "API key is required",
      })
    }
    if (v.auth_type === "api_key_header" && !v.header) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["header"],
        message: "Header name is required",
      })
    }
  })

let currentFormMode: "create" | "edit" = "create"
function isEditMode() {
  return currentFormMode === "edit"
}

type ProviderFormValues = z.input<typeof providerSchema>

interface ProviderFormProps {
  defaultValues?: Partial<ProviderFormValues>
  onSubmit: (data: ProviderFormData) => Promise<void>
  isPending: boolean
  mode: "create" | "edit"
}

export function ProviderForm({ defaultValues, onSubmit, isPending, mode }: ProviderFormProps) {
  currentFormMode = mode
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
      api_key_env: "",
      api_key: "",
      api_key_source: "env",
      auth_type: "bearer",
      models: "",
      alias: "",
      alias_model: "",
      default_model: "",
      max_concurrency: 32,
      ...defaultValues,
    },
  })

  const selectedType = watch("type")
  const selectedAuth = watch("auth_type")
  const selectedKeySource = watch("api_key_source") ?? "env"
  const [isFetchingModels, setIsFetchingModels] = useState(false)
  const [modelFetchMessage, setModelFetchMessage] = useState("")
  const [availableModels, setAvailableModels] = useState<string[]>([])
  const [modelFilter, setModelFilter] = useState("")
  const selectedModelsText = watch("models")

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

  const writeSelectedModels = (models: Iterable<string>) => {
    setValue("models", Array.from(models).join(", "))
  }

  const handleFormSubmit = async (values: ProviderFormValues) => {
    // react-hook-form receives the resolver output here, so transformed fields
    // such as models may already be arrays. Avoid parsing the transformed
    // object a second time, because the Zod input schema expects models to be a
    // comma-separated string.
    await onSubmit(values as unknown as ProviderFormData)
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
        setModelFetchMessage(`Could not fetch models${result.status ? ` (HTTP ${result.status})` : ""}`)
        return
      }
      setAvailableModels(result.models)
      const nextSelected = new Set(selectedModels)
      if (nextSelected.size === 0 && result.models.length === 1) {
        nextSelected.add(result.models[0]!)
        writeSelectedModels(nextSelected)
      }
      setModelFetchMessage(`Fetched ${result.models.length} model${result.models.length === 1 ? "" : "s"}. Select the ones you want to enable.`)
    } catch (e) {
      setModelFetchMessage(e instanceof Error ? e.message : "Could not fetch models")
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

  return (
    <form onSubmit={handleSubmit(handleFormSubmit)} className="space-y-6">
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="id">Provider ID</Label>
          <Input
            id="id"
            placeholder="my-provider"
            {...register("id")}
            disabled={mode === "edit"}
          />
          {errors.id && <p className="text-xs text-destructive">{errors.id.message}</p>}
        </div>

        <div className="space-y-2">
          <Label htmlFor="type">Protocol</Label>
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
          <Label htmlFor="base_url">Base URL</Label>
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
          <Label htmlFor="auth_type">Auth Type</Label>
          <Select
            defaultValue={defaultValues?.auth_type ?? "bearer"}
            onValueChange={(v) => setValue("auth_type", v as "bearer" | "api_key_header" | "none")}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="bearer">Bearer Token</SelectItem>
              <SelectItem value="api_key_header">API Key Header</SelectItem>
              <SelectItem value="none">None</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {selectedAuth !== "none" && (
          <div className="space-y-2">
            <Label htmlFor="api_key_source">Key Source</Label>
            <Select
              defaultValue={defaultValues?.api_key_source ?? "env"}
              onValueChange={(v) => setValue("api_key_source", v as "env" | "literal")}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="env">Environment variable</SelectItem>
                <SelectItem value="literal">Paste API key locally</SelectItem>
              </SelectContent>
            </Select>
          </div>
        )}

        {selectedAuth !== "none" && selectedKeySource === "env" && (
          <div className="space-y-2">
            <Label htmlFor="api_key_env">API Key Env Var</Label>
            <Input id="api_key_env" placeholder="OPENAI_API_KEY" {...register("api_key_env")} />
            {errors.api_key_env && (
              <p className="text-xs text-destructive">{errors.api_key_env.message}</p>
            )}
          </div>
        )}

        {selectedAuth !== "none" && selectedKeySource === "literal" && (
          <div className="space-y-2">
            <Label htmlFor="api_key">API Key</Label>
            <Input
              id="api_key"
              type="password"
              placeholder={mode === "edit" ? "Leave blank to keep current key" : "sk-..."}
              autoComplete="off"
              {...register("api_key")}
            />
            {errors.api_key && (
              <p className="text-xs text-destructive">{errors.api_key.message}</p>
            )}
            <p className="text-xs text-muted-foreground">
              Saved only in your local vibe-proxy YAML as a literal secret; it is never shown again.
            </p>
          </div>
        )}

        {selectedAuth === "api_key_header" && (
          <div className="space-y-2">
            <Label htmlFor="header">Header Name</Label>
            <Input id="header" placeholder="x-api-key" {...register("header")} />
            {errors.header && (
              <p className="text-xs text-destructive">{errors.header.message}</p>
            )}
            <p className="text-xs text-muted-foreground">
              Custom header name for API key authentication
            </p>
          </div>
        )}

        <div className="space-y-2 sm:col-span-2">
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="models">Models</Label>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              onClick={handleFetchModels}
              disabled={isFetchingModels || !watch("base_url") || (selectedAuth !== "none" && selectedKeySource === "env" && !watch("api_key_env")) || (selectedAuth !== "none" && selectedKeySource === "literal" && !watch("api_key") && mode !== "edit")}
            >
              {isFetchingModels ? "Fetching..." : "Fetch Models"}
            </Button>
          </div>
          <Input
            id="models"
            placeholder="Click Fetch Models, or enter: gpt-4, gpt-3.5-turbo"
            {...register("models")}
          />
          <p className="text-xs text-muted-foreground">
            Fill Base URL and auth first, then fetch available models from the provider.
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
                      Choose models · {selectedModels.size}/{availableModels.length}
                    </span>
                    <ChevronDown className="h-4 w-4 opacity-70" />
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-[min(34rem,calc(100vw-2rem))] space-y-3">
                  <div className="space-y-1">
                    <div className="text-sm font-medium">Select provider models</div>
                    <div className="text-xs text-muted-foreground">
                      Fetched models are candidates only. Check the models you want to expose through vibe-proxy.
                    </div>
                  </div>
                  <Input
                    value={modelFilter}
                    onChange={(e) => setModelFilter(e.target.value)}
                    placeholder="Filter models..."
                  />
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-xs text-muted-foreground">
                      {selectedModels.size} selected · {filteredModels.length} shown
                    </div>
                    <div className="flex gap-2">
                      <Button type="button" variant="outline" size="sm" onClick={selectFilteredModels}>
                        Select shown
                      </Button>
                      <Button type="button" variant="ghost" size="sm" onClick={clearFilteredModels}>
                        Clear shown
                      </Button>
                    </div>
                  </div>
                  <div className="max-h-72 overflow-y-auto rounded-md border border-border">
                    {filteredModels.length === 0 ? (
                      <div className="px-3 py-6 text-center text-sm text-muted-foreground">
                        No models match this filter.
                      </div>
                    ) : (
                      filteredModels.map((model) => {
                        const checked = selectedModels.has(model)
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
                            <span className="font-mono text-xs">{model}</span>
                          </button>
                        )
                      })
                    )}
                  </div>
                </PopoverContent>
              </Popover>
              <p className="text-xs text-muted-foreground">
                Only checked models are saved. You can still edit the text field manually.
              </p>
            </div>
          )}
        </div>

        <div className="space-y-2">
          <Label htmlFor="max_concurrency">Max Concurrency</Label>
          <Input
            id="max_concurrency"
            type="number"
            {...register("max_concurrency")}
          />
        </div>
      </div>

      <div className="border-t border-border pt-4">
        <h3 className="text-sm font-medium mb-4">Alias Configuration (optional)</h3>
        <div className="grid gap-4 sm:grid-cols-3">
          <div className="space-y-2">
            <Label htmlFor="alias">Alias</Label>
            <Input id="alias" placeholder="vibe-chat" {...register("alias")} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="alias_model">Alias Target</Label>
            <Input id="alias_model" placeholder="gpt-4" {...register("alias_model")} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="default_model">Default Model</Label>
            <Input id="default_model" placeholder="vibe-chat" {...register("default_model")} />
          </div>
        </div>
      </div>

      <div className="flex gap-2">
        <Button type="submit" disabled={isPending}>
          {isPending ? "Saving..." : mode === "create" ? "Create Provider" : "Save Changes"}
        </Button>
      </div>
    </form>
  )
}
