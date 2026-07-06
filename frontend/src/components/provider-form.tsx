import { useForm } from "react-hook-form"
import { zodResolver } from "@hookform/resolvers/zod"
import { z } from "zod"
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

  const handleFormSubmit = async (values: ProviderFormValues) => {
    // react-hook-form receives the resolver output here, so transformed fields
    // such as models may already be arrays. Avoid parsing the transformed
    // object a second time, because the Zod input schema expects models to be a
    // comma-separated string.
    await onSubmit(values as unknown as ProviderFormData)
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
          <Label htmlFor="models">Models (comma-separated)</Label>
          <Input
            id="models"
            placeholder="gpt-4, gpt-3.5-turbo"
            {...register("models")}
          />
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
