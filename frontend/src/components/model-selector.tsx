import { useQuery } from "@tanstack/react-query"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"

interface ModelSelectorProps {
  value: string
  onChange: (value: string) => void
  endpoint: string
  onEndpointChange: (value: string) => void
}

export function ModelSelector({ value, onChange, endpoint, onEndpointChange }: ModelSelectorProps) {
  const { data, isLoading } = useQuery({
    queryKey: ["models"],
    queryFn: async () => {
      const token = localStorage.getItem("vibe_admin_token") ?? ""
      const resp = await fetch("/v1/models", {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      })
      if (!resp.ok) throw new Error("Failed to fetch models")
      const json = await resp.json()
      return (json.data ?? []) as Array<{ id: string; vibe_type?: string }>
    },
    refetchInterval: 60_000,
  })

  const aliases = data?.filter((m) => m.vibe_type === "alias") ?? []
  const rawModels = data?.filter((m) => m.vibe_type === "raw") ?? []

  return (
    <div className="flex gap-2 items-center">
      <Select value={endpoint} onValueChange={onEndpointChange}>
        <SelectTrigger className="w-44">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="openai_chat">OpenAI Chat</SelectItem>
          <SelectItem value="openai_responses">OpenAI Responses</SelectItem>
          <SelectItem value="anthropic">Anthropic Messages</SelectItem>
        </SelectContent>
      </Select>

      {isLoading ? (
        <Skeleton className="h-9 w-48" />
      ) : (
        <Select value={value} onValueChange={onChange}>
          <SelectTrigger className="w-48">
            <SelectValue placeholder="Select model..." />
          </SelectTrigger>
          <SelectContent>
            {aliases.length > 0 && (
              <>
                <div className="px-2 py-1 text-xs font-medium text-muted-foreground">Aliases</div>
                {aliases.map((m) => (
                  <SelectItem key={m.id} value={m.id}>
                    {m.id}
                  </SelectItem>
                ))}
              </>
            )}
            {rawModels.length > 0 && (
              <>
                <div className="px-2 py-1 text-xs font-medium text-muted-foreground">Raw Models</div>
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
