import { FormEvent, useEffect, useId, useState } from "react"
import { Filter, RotateCcw, Search, SlidersHorizontal } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import type { CaptureStatus, RequestQueryFilters } from "@/lib/types"

interface RequestFiltersProps {
  value: RequestQueryFilters
  onChange: (filters: RequestQueryFilters) => void
}

const captureStates: CaptureStatus[] = [
  "not_captured",
  "captured",
  "redacted",
  "truncated",
  "expired",
  "dropped",
]

function localDateTime(value?: string): string {
  if (!value) return ""
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ""
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
  return local.toISOString().slice(0, 16)
}

function rfc3339(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString()
}

export function RequestFilters({ value, onChange }: RequestFiltersProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<RequestQueryFilters>(value)
  const advancedCount = Object.entries(draft).filter(
    ([key, filterValue]) => !["q", "status_class"].includes(key) && Boolean(filterValue),
  ).length
  const [advancedOpen, setAdvancedOpen] = useState(advancedCount > 0)

  useEffect(() => setDraft(value), [value])
  useEffect(() => {
    if (advancedCount > 0) setAdvancedOpen(true)
  }, [advancedCount])

  const update = (key: keyof RequestQueryFilters, next: string) => {
    setDraft((current) => ({ ...current, [key]: next }))
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    onChange({
      ...draft,
      from: rfc3339(draft.from ?? ""),
      to: rfc3339(draft.to ?? ""),
    })
  }

  const reset = () => {
    setDraft({})
    onChange({})
  }
  return (
    <Card>
      <CardContent className="p-4">
        <form onSubmit={submit} className="space-y-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-end">
            <div className="flex-1 space-y-1.5">
              <Label htmlFor="request-q">{t("observability.filters.search")}</Label>
              <div className="relative">
                <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  id="request-q"
                  value={draft.q ?? ""}
                  onChange={(event) => update("q", event.target.value)}
                  className="pl-9"
                  placeholder={t("observability.filters.searchPlaceholder")}
                />
              </div>
            </div>
            <div className="w-full space-y-1.5 lg:w-40">
              <FilterSelect
                label={t("observability.filters.status")}
                value={draft.status_class ?? "all"}
                onChange={(next) => update("status_class", next === "all" ? "" : next)}
                options={[
                  ["all", t("observability.filters.all")],
                  ["2", "2xx"],
                  ["3", "3xx"],
                  ["4", "4xx"],
                  ["5", "5xx"],
                ]}
              />
            </div>
            <Button type="submit">
              <Filter className="h-4 w-4" />
              {t("observability.filters.apply")}
            </Button>
            <Button type="button" variant="outline" onClick={reset}>
              <RotateCcw className="h-4 w-4" />
              {t("observability.filters.reset")}
            </Button>
          </div>

          <details
            className="group rounded-lg border border-border bg-muted/20 px-3 py-2"
            open={advancedOpen}
            onToggle={(event) => setAdvancedOpen(event.currentTarget.open)}
          >
            <summary className="flex cursor-pointer list-none items-center gap-2 text-sm font-medium text-muted-foreground hover:text-foreground">
              <SlidersHorizontal className="h-4 w-4" />
              {t("observability.filters.advanced")}
              {advancedCount > 0 && <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs text-primary">{advancedCount}</span>}
            </summary>
            <div className="mt-3 grid gap-3 border-t border-border pt-3 sm:grid-cols-2 xl:grid-cols-4">
              <FilterInput label={t("observability.filters.agent")} value={draft.agent_id} onChange={(next) => update("agent_id", next)} />
              <FilterInput label={t("observability.filters.principal")} value={draft.principal_name} onChange={(next) => update("principal_name", next)} />
              <FilterInput label={t("observability.filters.session")} value={draft.session_id} onChange={(next) => update("session_id", next)} />
              <FilterInput label={t("observability.filters.project")} value={draft.project_id} onChange={(next) => update("project_id", next)} />
              <FilterInput label={t("observability.filters.model")} value={draft.model} onChange={(next) => update("model", next)} />
              <FilterInput label={t("observability.filters.provider")} value={draft.provider} onChange={(next) => update("provider", next)} />

              <FilterSelect
                label={t("observability.filters.protocol")}
                value={draft.protocol ?? "all"}
                onChange={(next) => update("protocol", next === "all" ? "" : next)}
                options={[
                  ["all", t("observability.filters.all")],
                  ["openai_chat", "OpenAI Chat"],
                  ["openai_responses", "OpenAI Responses"],
                  ["anthropic_messages", "Anthropic Messages"],
                ]}
              />
              <FilterSelect
                label={t("observability.filters.capture")}
                value={draft.capture_status || "all"}
                onChange={(next) => update("capture_status", next === "all" ? "" : next)}
                options={[
                  ["all", t("observability.filters.all")],
                  ...captureStates.map((state) => [state, t(`observability.captureStates.${state}`)] as [string, string]),
                ]}
              />

              <div className="space-y-1.5">
                <Label htmlFor="request-from">{t("observability.filters.from")}</Label>
                <Input id="request-from" type="datetime-local" value={localDateTime(draft.from)} onChange={(event) => update("from", event.target.value)} />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="request-to">{t("observability.filters.to")}</Label>
                <Input id="request-to" type="datetime-local" value={localDateTime(draft.to)} onChange={(event) => update("to", event.target.value)} />
              </div>
            </div>
          </details>
        </form>
      </CardContent>
    </Card>
  )
}

function FilterInput({ label, value, onChange }: { label: string; value?: string; onChange: (value: string) => void }) {
  const id = useId()
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} value={value ?? ""} onChange={(event) => onChange(event.target.value)} />
    </div>
  )
}

function FilterSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: string
  options: Array<[string, string]>
  onChange: (value: string) => void
}) {
  const id = useId()
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger id={id}><SelectValue /></SelectTrigger>
        <SelectContent>
          {options.map(([optionValue, label]) => <SelectItem key={optionValue} value={optionValue}>{label}</SelectItem>)}
        </SelectContent>
      </Select>
    </div>
  )
}
