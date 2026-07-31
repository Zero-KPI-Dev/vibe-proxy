import { User, Bot, Copy, Check, FileImage, BrainCircuit, ChevronDown } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import type { PlaygroundImage } from "@/lib/playground-request"

interface ChatMessageProps {
  role: "user" | "assistant"
  content: string
  reasoning?: string
  images?: PlaygroundImage[]
}

export function ChatMessage({ role, content, reasoning = "", images = [] }: ChatMessageProps) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(content)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div
      className={cn(
        "flex gap-3 py-4",
        role === "user" ? "justify-end" : "justify-start"
      )}
    >
      {role === "assistant" && (
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary">
          <Bot className="h-4 w-4" />
        </div>
      )}

      <div
        className={cn(
          "group relative min-w-0 rounded-xl px-4 py-3",
          role === "user"
            ? "max-w-[76%] bg-primary text-primary-foreground"
            : "w-full max-w-[min(88%,56rem)] border border-border/70 bg-muted/55"
        )}
      >
        {images.length > 0 && (
          <div className="mb-2 grid max-w-md grid-cols-2 gap-2">
            {images.map((image) =>
              image.dataUrl ? (
                <img
                  key={image.id}
                  src={image.dataUrl}
                  alt={image.name}
                  className="max-h-40 w-full rounded-lg border border-white/20 object-cover"
                />
              ) : (
                <div
                  key={image.id}
                  className="flex items-center gap-2 rounded-lg border border-current/20 px-2 py-2 text-xs"
                >
                  <FileImage className="h-4 w-4 shrink-0" />
                  <span className="truncate">{image.name}</span>
                </div>
              )
            )}
          </div>
        )}
        {role === "assistant" && reasoning.trim() && (
          <details className="group/reasoning mb-3 overflow-hidden rounded-lg border border-border/70 bg-background/60">
            <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground [&::-webkit-details-marker]:hidden">
              <span className="flex h-6 w-6 items-center justify-center rounded-md bg-primary/10 text-primary">
                <BrainCircuit className="h-3.5 w-3.5" />
              </span>
              <span className="flex-1">{t("playground.reasoning")}</span>
              <ChevronDown className="h-3.5 w-3.5 transition-transform duration-200 group-open/reasoning:rotate-180" />
            </summary>
            <div className="border-t border-border/60 px-3 py-3 text-xs leading-6 text-muted-foreground">
              <div className="markdown-response reasoning-response">
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{reasoning}</ReactMarkdown>
              </div>
            </div>
          </details>
        )}
        <div className={cn(role === "assistant" ? "markdown-response" : "text-sm leading-6")}>
          {role === "assistant" ? (
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
          ) : content ? (
            <p className="whitespace-pre-wrap">{content}</p>
          ) : null}
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="absolute -top-2 -right-2 h-6 w-6 opacity-0 group-hover:opacity-100 transition-opacity"
          onClick={handleCopy}
        >
          {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
        </Button>
      </div>

      {role === "user" && (
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground">
          <User className="h-4 w-4" />
        </div>
      )}
    </div>
  )
}
