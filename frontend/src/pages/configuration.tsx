import { useState, useEffect } from "react"
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { configApi } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { toast } from "sonner"
import { FileCode, RefreshCw, CheckCircle2, AlertCircle, Eye, Edit3 } from "lucide-react"
import { cn } from "@/lib/utils"
import { useTranslation } from "react-i18next"
import type { ConfigValidationIssue } from "@/lib/types"

export function ConfigurationPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [yamlContent, setYamlContent] = useState("")
  const [hasChanges, setHasChanges] = useState(false)
  const [validation, setValidation] = useState<{ valid: boolean; issues: ConfigValidationIssue[] } | null>(null)
  const [preview, setPreview] = useState(false)

  const { data, isLoading, refetch } = useQuery({
    queryKey: ["raw-config"],
    queryFn: configApi.raw,
  })

  useEffect(() => {
    if (data?.yaml) {
      setYamlContent(data.yaml)
      setHasChanges(false)
    }
  }, [data])

  const saveMutation = useMutation({
    mutationFn: (yaml: string) => configApi.saveRaw(yaml),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ["raw-config"] })
      qc.invalidateQueries({ queryKey: ["providers"] })
      setHasChanges(false)
      if (res.issues && res.issues.length > 0) {
        setValidation({
          valid: !res.issues.some((issue) => issue.level === "error"),
          issues: res.issues,
        })
        toast.warning(t("configuration.savedWithIssues"))
      } else {
        setValidation({ valid: true, issues: [] })
        toast.success(t("configuration.saved"))
      }
    },
    onError: (e: Error) => {
      toast.error(t("configuration.saveFailed", { error: e.message }))
    },
  })

  const handleSave = () => {
    saveMutation.mutate(yamlContent)
  }

  const handleValidate = async () => {
    try {
      const res = await configApi.validate()
      setValidation(res)
      if (res.valid && res.issues.length > 0) {
        toast.warning(t("configuration.validWithWarnings", { count: res.issues.length }))
      } else if (res.valid) {
        toast.success(t("configuration.valid"))
      } else {
        toast.error(t("configuration.issueCount", { count: res.issues.length }))
      }
    } catch (e) {
      toast.error(t("configuration.validationFailed", { error: (e as Error).message }))
    }
  }

  const handleReload = async () => {
    try {
      await configApi.reload()
      toast.success(t("configuration.reloaded"))
      refetch()
    } catch (e) {
      toast.error(t("configuration.reloadFailed", { error: (e as Error).message }))
    }
  }

  const validationHasIssues = (validation?.issues.length ?? 0) > 0
  const validationHasErrors = validation?.issues.some((issue) => issue.level === "error") ?? false

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">{t("configuration.title")}</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {t("configuration.description")}
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPreview(!preview)}
          >
            {preview ? (
              <><Edit3 className="h-4 w-4 mr-1" /> {t("common.edit")}</>
            ) : (
              <><Eye className="h-4 w-4 mr-1" /> {t("common.preview")}</>
            )}
          </Button>
          <Button variant="outline" size="sm" onClick={handleValidate}>
            <CheckCircle2 className="h-4 w-4 mr-1" />
            {t("common.validate")}
          </Button>
          <Button variant="outline" size="sm" onClick={handleReload}>
            <RefreshCw className="h-4 w-4 mr-1" />
            {t("common.reload")}
          </Button>
          <Button
            size="sm"
            onClick={handleSave}
            disabled={!hasChanges || saveMutation.isPending || preview}
          >
            {saveMutation.isPending ? t("common.saving") : t("configuration.saveReload")}
          </Button>
        </div>
      </div>

      {validation && (
        <Card
          className={
            validation.valid && !validationHasIssues
              ? "border-green-500/30 bg-green-500/5"
              : validationHasErrors
                ? "border-red-500/30 bg-red-500/5"
                : "border-yellow-500/30 bg-yellow-500/5"
          }
        >
          <CardContent className="p-4 flex items-start gap-3">
            {validation.valid && !validationHasIssues ? (
              <CheckCircle2 className="h-5 w-5 text-green-400 mt-0.5 shrink-0" />
            ) : (
              <AlertCircle className={cn(
                "h-5 w-5 mt-0.5 shrink-0",
                validationHasErrors ? "text-red-400" : "text-yellow-400"
              )} />
            )}
            <div className="text-sm">
              <p className={cn(
                validation.valid && !validationHasIssues && "text-green-400",
                validationHasErrors && "text-red-400",
                validation.valid && validationHasIssues && "text-yellow-400"
              )}>
                {validationHasErrors
                  ? t("configuration.validationIssues")
                  : validationHasIssues
                    ? t("configuration.validationValidWithWarnings")
                    : t("configuration.validationValid")}
              </p>
              {validationHasIssues && (
                <ul className="mt-1 space-y-1">
                  {validation.issues.map(
                    (issue, i) => (
                      <li key={i} className="text-muted-foreground">
                        {issue.level === "error" ? (
                          <Badge variant="destructive" className="mr-1">{t("configuration.error")}</Badge>
                        ) : (
                          <Badge variant="warning" className="mr-1">{t("configuration.warning")}</Badge>
                        )}
                        {issue.path && <code className="text-xs mr-1">{issue.path}:</code>}
                        {t(`configuration.issueMessages.${issue.code}`, {
                          defaultValue: issue.message,
                        })}
                      </li>
                    )
                  )}
                </ul>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardContent className="p-0">
          {isLoading ? (
            <div className="p-4">
              <Skeleton className="h-[500px] w-full" />
            </div>
          ) : preview ? (
            <div className="overflow-auto max-h-[600px] p-4 font-mono text-sm leading-relaxed">
              {yamlContent.split("\n").map((line, i) => (
                <div key={i} className="flex">
                  <span className="text-muted-foreground w-8 shrink-0 text-right select-none mr-4">
                    {i + 1}
                  </span>
                  <span
                    className={cn(
                      "whitespace-pre",
                      line.match(/^[a-zA-Z_]/) && "text-purple-300",
                      line.match(/^\s{2,}[a-zA-Z_]/) && "text-blue-300",
                      line.includes(": ") && !line.match(/^\s*#/) && "text-foreground",
                      line.match(/^\s*#/) && "text-muted-foreground italic",
                    )}
                  >
                    {line}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <Textarea
              value={yamlContent}
              onChange={(e) => {
                setYamlContent(e.target.value)
                setHasChanges(e.target.value !== (data?.yaml ?? ""))
              }}
              className="min-h-[500px] font-mono text-sm border-0 rounded-none focus-visible:ring-0 resize-y"
              placeholder={t("configuration.loading")}
            />
          )}
        </CardContent>
      </Card>
    </div>
  )
}
