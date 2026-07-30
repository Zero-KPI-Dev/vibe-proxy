import { useEffect, useMemo } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { providerApi } from "@/lib/api"
import { useTranslation } from "react-i18next"
import type { PlaygroundEndpoint } from "@/lib/playground-request"

interface ModelSelectorProps {
  value: string
  onChange: (value: string) => void
  endpoint: PlaygroundEndpoint
  onEndpointChange: (value: PlaygroundEndpoint) => void
}

type ModelOption = { id: string; vibe_type?: "alias" | "raw" }

export function ModelSelector({ value, onChange, endpoint, onEndpointChange }: ModelSelectorProps) {
  const { t } = useTranslation()
  const { data, isLoading, error } = useQuery({
    queryKey: ["models"],
    queryFn: async () => {
      // The control-plane playground is authenticated with the admin token.
      // Read its model choices from the admin snapshot instead of deliberately
      // sending that token to the data-plane /v1/models endpoint.
      const snapshot = await providerApi.list()
      const resolver = snapshot.model_resolver as unknown as {
        Aliases?: Record<string, unknown>
        aliases?: Record<string, unknown>
      }
      const seen = new Set<string>()
      const models: ModelOption[] = []
      for (const alias of Object.keys(resolver?.Aliases ?? resolver?.aliases ?? {})) {
        if (!seen.has(alias)) {
          seen.add(alias)
          models.push({ id: alias, vibe_type: "alias" })
        }
      }
      for (const provider of snapshot.providers ?? []) {
        for (const model of provider.models ?? []) {
          if (!seen.has(model)) {
            seen.add(model)
            models.push({ id: model, vibe_type: "raw" })
          }
        }
      }
      return models
    },
    refetchInterval: 60_000,
  })

  const aliases = useMemo(() => data?.filter((m) => m.vibe_type === "alias") ?? [], [data])
  const rawModels = useMemo(() => data?.filter((m) => m.vibe_type === "raw") ?? [], [data])
  const allModels = useMemo(() => [...aliases, ...rawModels], [aliases, rawModels])

  useEffect(() => {
    if (!value && allModels.length > 0) {
      onChange(allModels[0]!.id)
    }
  }, [allModels, onChange, value])

  return (
    <div className="flex gap-2 items-center">
      <Select value={endpoint} onValueChange={(value) => onEndpointChange(value as PlaygroundEndpoint)}>
        <SelectTrigger className="w-44">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="openai_chat">{t("modelSelector.openaiChat")}</SelectItem>
          <SelectItem value="openai_responses">{t("modelSelector.openaiResponses")}</SelectItem>
          <SelectItem value="anthropic">{t("modelSelector.anthropicMessages")}</SelectItem>
        </SelectContent>
      </Select>

      {isLoading ? (
        <Skeleton className="h-9 w-48" />
      ) : (
        <Select value={value} onValueChange={onChange}>
          <SelectTrigger className="w-48">
            <SelectValue placeholder={error ? t("modelSelector.loadFailed") : t("modelSelector.selectModel")} />
          </SelectTrigger>
          <SelectContent>
            {allModels.length === 0 && (
              <div className="px-2 py-3 text-sm text-muted-foreground">
                {t("modelSelector.noneConfigured")}
              </div>
            )}
            {aliases.length > 0 && (
              <>
                <div className="px-2 py-1 text-xs font-medium text-muted-foreground">{t("modelSelector.aliases")}</div>
                {aliases.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.id}
                  </SelectItem>
                ))}
              </>
            )}
            {rawModels.length > 0 && (
              <>
                <div className="px-2 py-1 text-xs font-medium text-muted-foreground">{t("modelSelector.rawModels")}</div>
                {rawModels.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.id}
                  </SelectItem>
                ))}
              </>
            )}
          </SelectContent>
        </Select>
      )}
    </div>
  )
}
