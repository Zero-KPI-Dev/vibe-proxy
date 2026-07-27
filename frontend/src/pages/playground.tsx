import { useState, useRef, useEffect, useCallback } from "react"
import {
  Send,
  Trash2,
  Settings2,
  StopCircle,
  History,
  FileJson,
  KeyRound,
  Eye,
  EyeOff,
  ImagePlus,
  X,
  LoaderCircle,
  ClipboardPaste,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Separator } from "@/components/ui/separator"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Slider } from "@/components/ui/slider"
import { Badge } from "@/components/ui/badge"
import { ChatMessage } from "@/components/chat-message"
import { ModelSelector } from "@/components/model-selector"
import { EmptyState } from "@/components/empty-state"
import { MultimodalFlow } from "@/components/multimodal-flow"
import { requestApi } from "@/lib/api"
import type { RequestEvent } from "@/lib/types"
import {
  buildPlaygroundBody,
  clipboardImageFiles,
  streamDelta,
  unaryText,
  type PlaygroundEndpoint,
  type PlaygroundImage,
  type PlaygroundMessage,
} from "@/lib/playground-request"
import {
  withConversation,
  withoutConversation,
  type PlaygroundConversations,
} from "@/lib/playground-history"
import {
  deleteConversationImages,
  hydrateConversationImages,
  persistConversationImages,
} from "@/lib/playground-images"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"

type ChatEntry = PlaygroundMessage

const STORAGE_KEY = "vibe_playground_conversations"
const MAX_IMAGES = 4
const MAX_IMAGE_BYTES = 5 * 1024 * 1024
const SUPPORTED_IMAGE_TYPES = new Set(["image/png", "image/jpeg", "image/webp", "image/gif"])

function loadConversations(): PlaygroundConversations {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? JSON.parse(raw) : {}
  } catch {
    return {}
  }
}

function persistConversations(conversations: PlaygroundConversations) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(conversations))
  } catch {
    // storage full — silently ignore
  }
}

export function PlaygroundPage() {
  const { t } = useTranslation()
  const [messages, setMessages] = useState<ChatEntry[]>([])
  const [input, setInput] = useState("")
  const [model, setModel] = useState("")
  const [endpoint, setEndpoint] = useState<PlaygroundEndpoint>("openai_chat")
  const [streaming, setStreaming] = useState(true)
  const [isStreaming, setIsStreaming] = useState(false)
  const [temperature, setTemperature] = useState(0.7)
  const [topP, setTopP] = useState(1)
  const [maxTokens, setMaxTokens] = useState(2048)
  const [stopSequences, setStopSequences] = useState("")
  const [convId, setConvId] = useState<string | null>(null)
  const [rawJson, setRawJson] = useState<string | null>(null)
  const [useClientKey, setUseClientKey] = useState(false)
  const [clientKeyValue, setClientKeyValue] = useState("")
  const [showKey, setShowKey] = useState(false)
  const [pendingImages, setPendingImages] = useState<PlaygroundImage[]>([])
  const [flowEvent, setFlowEvent] = useState<RequestEvent | null>(null)
  const [flowLoading, setFlowLoading] = useState(false)
  const [savedConvs, setSavedConvs] = useState<PlaygroundConversations>(() => loadConversations())
  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const convEntries = Object.entries(savedConvs)

  const apiPath =
    endpoint === "openai_responses"
      ? "/v1/responses"
      : endpoint === "anthropic"
        ? "/anthropic/v1/messages"
        : "/v1/chat/completions"

  const readImage = (file: File) =>
    new Promise<PlaygroundImage>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () =>
        resolve({
          id: crypto.randomUUID(),
          name: file.name,
          mediaType: file.type,
          size: file.size,
          dataUrl: String(reader.result),
        })
      reader.onerror = () => reject(reader.error ?? new Error("image_read_failed"))
      reader.readAsDataURL(file)
    })

  const addImages = async (files: File[]): Promise<number> => {
    const availableSlots = MAX_IMAGES - pendingImages.length
    if (availableSlots <= 0) {
      toast.error(t("playground.tooManyImages", { count: MAX_IMAGES }))
      return 0
    }
    const accepted: File[] = []
    for (const file of files) {
      if (!SUPPORTED_IMAGE_TYPES.has(file.type)) {
        toast.error(t("playground.unsupportedImage", { name: file.name }))
        continue
      }
      if (file.size > MAX_IMAGE_BYTES) {
        toast.error(t("playground.imageTooLarge", { name: file.name, size: 5 }))
        continue
      }
      accepted.push(file)
    }
    if (accepted.length > availableSlots) {
      toast.error(t("playground.tooManyImages", { count: MAX_IMAGES }))
    }
    const selected = accepted.slice(0, availableSlots)
    try {
      const next = await Promise.all(selected.map(readImage))
      setPendingImages((current) => [...current, ...next])
      return next.length
    } catch {
      toast.error(t("playground.imageReadFailed"))
      return 0
    }
  }

  const handlePasteCapture = async (event: React.ClipboardEvent<HTMLDivElement>) => {
    const imageFiles = clipboardImageFiles(event.clipboardData)
    if (imageFiles.length === 0) return
    event.preventDefault()
    if (isStreaming) return
    const attached = await addImages(imageFiles)
    if (attached > 0) {
      toast.success(t("playground.pastedImages", { count: attached }))
    }
  }

  const pollRequestFlow = async (requestId: string) => {
    setFlowLoading(true)
    for (let attempt = 0; attempt < 8; attempt += 1) {
      try {
        const result = await requestApi.recent()
        const event = [...(result.recent ?? []), ...(result.active ?? [])].find(
          (item) => item.request_id === requestId
        )
        if (event?.completed_at || (event && attempt === 7)) {
          setFlowEvent(event)
          setFlowLoading(false)
          return
        }
      } catch {
        setFlowLoading(false)
        return
      }
      await new Promise((resolve) => setTimeout(resolve, 150))
    }
    setFlowLoading(false)
  }

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [])

  useEffect(() => {
    scrollToBottom()
  }, [messages, scrollToBottom])

  useEffect(() => {
    if (!convId) return
    // Keep local history small and avoid retaining image bytes in localStorage.
    // Metadata remains visible after reload, but images must be attached again
    // if IndexedDB is unavailable.
    void persistConversationImages(messages)
    setSavedConvs((current) => withConversation(current, convId, messages))
  }, [messages, convId])

  useEffect(() => {
    persistConversations(savedConvs)
  }, [savedConvs])

  const handleLoad = async (id: string) => {
    const conv = savedConvs[id]
    if (conv) {
      let restored = conv
      try {
        restored = await hydrateConversationImages(conv)
      } catch {
        // Keep the conversation usable even if browser storage is unavailable.
      }
      setMessages(restored)
      setConvId(id)
      toast.success(t("playground.restored"))
    }
  }

  const handleDeleteConv = (id: string) => {
    const conversation = savedConvs[id]
    if (conversation) {
      void deleteConversationImages(conversation)
    }
    setSavedConvs((current) => withoutConversation(current, id))
    if (convId === id) {
      setConvId(null)
      setMessages([])
    }
    toast.success(t("playground.deleted"))
  }

  const handleSend = async () => {
    const trimmed = input.trim()
    if ((!trimmed && pendingImages.length === 0) || !model) return
    if (useClientKey && !clientKeyValue.trim()) {
      toast.error(t("playground.clientKeyRequired"))
      return
    }

    const userMsg: ChatEntry = {
      id: crypto.randomUUID(),
      role: "user",
      content: trimmed,
      images: pendingImages,
    }
    const assistantMsg: ChatEntry = { id: crypto.randomUUID(), role: "assistant", content: "" }
    const requestMessages = [...messages, userMsg]

    setMessages((prev) => [...prev, userMsg, assistantMsg])
    if (!convId) setConvId(crypto.randomUUID())
    setInput("")
    setPendingImages([])
    setFlowEvent(null)
    setFlowLoading(true)
    setIsStreaming(true)

    const controller = new AbortController()
    abortRef.current = controller
    let requestId = ""

    try {
      const adminToken = localStorage.getItem("vibe_admin_token") ?? ""
      const token = useClientKey ? clientKeyValue.trim() : adminToken
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
      }
      if (token) headers["Authorization"] = `Bearer ${token}`

      const stop = stopSequences.split(",").map((item) => item.trim()).filter(Boolean)
      const body = buildPlaygroundBody(endpoint, model, requestMessages, {
        stream: streaming,
        temperature,
        topP,
        maxTokens,
        stop,
      })
      const requestPath = useClientKey ? apiPath : `/admin/playground${apiPath}`

      const resp = await fetch(requestPath, {
        method: "POST",
        headers,
        body: JSON.stringify(body),
        signal: controller.signal,
      })
      requestId = resp.headers.get("X-Vibe-Proxy-Request-ID") ?? ""

      if (!resp.ok) {
        const errText = await resp.text()
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantMsg.id
              ? { ...m, content: t("playground.requestError", { error: `${resp.status} - ${errText}` }) }
              : m
          )
        )
        return
      }

      if (streaming && resp.body) {
        const reader = resp.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ""

        while (true) {
          const { done, value } = await reader.read()
          if (done) break

          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split("\n")
          buffer = lines.pop() ?? ""

          for (const line of lines) {
            if (line.startsWith("data: ")) {
              const data = line.slice(6).trim()
              if (data === "[DONE]") continue
              try {
                const parsed = JSON.parse(data) as Record<string, any>
                const delta = streamDelta(endpoint, parsed)
                if (delta) {
                  setMessages((prev) =>
                    prev.map((m) =>
                      m.id === assistantMsg.id
                        ? { ...m, content: m.content + delta }
                        : m
                    )
                  )
                }
              } catch {
                // skip parse errors for partial lines
              }
            }
          }
        }
      } else {
        const json = await resp.json()
        setRawJson(JSON.stringify(json, null, 2))
        const content = unaryText(endpoint, json)
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantMsg.id ? { ...m, content } : m
          )
        )
      }
    } catch (e) {
      if ((e as Error).name === "AbortError") {
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantMsg.id && !m.content
              ? { ...m, content: t("playground.stopped") }
              : m
          )
        )
      } else {
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantMsg.id
              ? { ...m, content: t("playground.requestError", { error: (e as Error).message }) }
              : m
          )
        )
      }
    } finally {
      if (requestId) {
        await pollRequestFlow(requestId)
      } else {
        setFlowLoading(false)
      }
      setIsStreaming(false)
      abortRef.current = null
    }
  }

  const handleStop = () => {
    abortRef.current?.abort()
  }

  const handleClear = () => {
    setMessages([])
    setInput("")
    setConvId(null)
    setRawJson(null)
    setPendingImages([])
    setFlowEvent(null)
    setFlowLoading(false)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  return (
    <div className="flex h-full flex-col" onPasteCapture={(event) => void handlePasteCapture(event)}>
      <div className="flex items-center justify-between mb-4 shrink-0">
        <div>
          <h1 className="text-2xl font-semibold">{t("playground.title")}</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {t("playground.description")}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm">
                <History className="h-4 w-4 mr-1" />
                {t("playground.history")}
                {convEntries.length > 0 && (
                  <Badge variant="secondary" className="ml-1 text-xs">{convEntries.length}</Badge>
                )}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuLabel>{t("playground.savedConversations")}</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {convEntries.length === 0 ? (
                <DropdownMenuItem disabled>{t("playground.noSavedConversations")}</DropdownMenuItem>
              ) : (
                convEntries.map(([id, msgs]) => (
                  <div key={id} className="flex items-center gap-1 px-1">
                    <DropdownMenuItem
                      className="flex-1 truncate"
                      onClick={() => void handleLoad(id)}
                    >
                      {msgs[0]?.content.slice(0, 40) || t("playground.emptyConversation")}...
                    </DropdownMenuItem>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-6 w-6 shrink-0"
                      onClick={(event) => {
                        event.preventDefault()
                        event.stopPropagation()
                        handleDeleteConv(id)
                      }}
                      aria-label={t("common.delete")}
                      title={t("common.delete")}
                    >
                      <Trash2 className="h-3 w-3 text-destructive" />
                    </Button>
                  </div>
                ))
              )}
            </DropdownMenuContent>
          </DropdownMenu>

          {rawJson && (
            <Dialog>
              <DialogTrigger asChild>
                <Button variant="outline" size="sm">
                  <FileJson className="h-4 w-4 mr-1" />
                  {t("playground.rawResponse")}
                </Button>
              </DialogTrigger>
              <DialogContent className="max-w-2xl max-h-[80vh]">
                <DialogHeader>
                  <DialogTitle>{t("playground.rawResponse")}</DialogTitle>
                </DialogHeader>
                <pre className="overflow-auto rounded-lg bg-background border border-border p-4 text-xs font-mono max-h-[60vh]">
                  {rawJson}
                </pre>
              </DialogContent>
            </Dialog>
          )}

          <Button variant="ghost" size="sm" onClick={handleClear}>
            <Trash2 className="h-4 w-4 mr-1" />
            {t("common.clear")}
          </Button>

          <Sheet>
            <SheetTrigger asChild>
              <Button variant="outline" size="sm">
                <Settings2 className="h-4 w-4 mr-1" />
                {t("playground.parameters")}
              </Button>
            </SheetTrigger>
            <SheetContent>
              <SheetHeader>
                <SheetTitle>{t("playground.parameters")}</SheetTitle>
                <SheetDescription>
                  {t("playground.parametersDescription")}
                </SheetDescription>
              </SheetHeader>
              <div className="space-y-6 py-6">
                <div className="space-y-2">
                  <div className="flex justify-between">
                    <Label>{t("playground.temperature")}</Label>
                    <span className="text-sm text-muted-foreground">{temperature}</span>
                  </div>
                  <Slider
                    value={[temperature]}
                    onValueChange={([v]) => setTemperature(v ?? 0.7)}
                    min={0}
                    max={2}
                    step={0.05}
                  />
                </div>
                <div className="space-y-2">
                  <div className="flex justify-between">
                    <Label>{t("playground.topP")}</Label>
                    <span className="text-sm text-muted-foreground">{topP}</span>
                  </div>
                  <Slider
                    value={[topP]}
                    onValueChange={([v]) => setTopP(v ?? 1)}
                    min={0}
                    max={1}
                    step={0.05}
                  />
                </div>
                <div className="space-y-2">
                  <div className="flex justify-between">
                    <Label>{t("playground.maxTokens")}</Label>
                    <span className="text-sm text-muted-foreground">{maxTokens}</span>
                  </div>
                  <Slider
                    value={[maxTokens]}
                    onValueChange={([v]) => setMaxTokens(v ?? 2048)}
                    min={1}
                    max={16384}
                    step={1}
                  />
                </div>
                <div className="space-y-2">
                  <Label>{t("playground.stopSequences")}</Label>
                  <Input
                    value={stopSequences}
                    onChange={(e) => setStopSequences(e.target.value)}
                    placeholder={t("playground.stopPlaceholder")}
                  />
                  <p className="text-xs text-muted-foreground">
                    {t("playground.stopHelp")}
                  </p>
                </div>
                <Separator />
                <div className="flex items-center justify-between">
                  <Label>{t("playground.streaming")}</Label>
                  <Switch checked={streaming} onCheckedChange={setStreaming} />
                </div>
                <Separator />
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <KeyRound className="h-4 w-4 text-muted-foreground" />
                      <Label>{t("playground.useClientKey")}</Label>
                    </div>
                    <Switch checked={useClientKey} onCheckedChange={setUseClientKey} />
                  </div>
                  {useClientKey && (
                    <div className="relative">
                      <Input
                        type={showKey ? "text" : "password"}
                        value={clientKeyValue}
                        onChange={(e) => setClientKeyValue(e.target.value)}
                        placeholder="sk-vibe-..."
                        className="pr-8 text-sm font-mono"
                      />
                      <Button
                        variant="ghost"
                        size="icon"
                        className="absolute right-1 top-1/2 -translate-y-1/2 h-6 w-6"
                        onClick={() => setShowKey(!showKey)}
                      >
                        {showKey ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                      </Button>
                    </div>
                  )}
                  <p className="text-xs text-muted-foreground">
                    {t("playground.clientKeyHelp")}
                  </p>
                </div>
              </div>
            </SheetContent>
          </Sheet>
        </div>
      </div>

      <div className="flex items-center gap-2 mb-4 shrink-0 flex-wrap">
        <ModelSelector
          value={model}
          onChange={setModel}
          endpoint={endpoint}
          onEndpointChange={setEndpoint}
        />
        {model && (
          <Badge variant="outline" className="text-xs font-mono">
            {model}
          </Badge>
        )}
      </div>

      {(flowEvent || flowLoading) && (
        <div className="mb-4 shrink-0">
          {flowEvent ? (
            <MultimodalFlow event={flowEvent} />
          ) : (
            <div className="flex items-center gap-3 rounded-xl border border-border bg-card px-4 py-3 shadow-sm">
              <span className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10 text-primary">
                <LoaderCircle className="h-4 w-4 animate-spin" />
              </span>
              <div>
                <p className="text-sm font-medium">{t("playground.requestFlow")}</p>
                <p className="text-xs text-muted-foreground">{t("playground.loadingFlow")}</p>
              </div>
            </div>
          )}
        </div>
      )}

      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto rounded-xl border border-border bg-card mb-4"
      >
        {messages.length === 0 ? (
          <EmptyState
            title={t("playground.startConversation")}
            description={t("playground.startDescription")}
            className="h-full"
          />
        ) : (
          <div className="px-4">
            {messages.map((msg) => (
              <ChatMessage
                key={msg.id}
                role={msg.role}
                content={msg.content}
                images={msg.images}
              />
            ))}
          </div>
        )}
      </div>

      <div className="shrink-0">
        <div className="flex items-end gap-3">
          <input
            ref={fileInputRef}
            type="file"
            accept="image/png,image/jpeg,image/webp,image/gif"
            multiple
            className="hidden"
            onChange={(event) => {
              void addImages(Array.from(event.target.files ?? []))
              event.target.value = ""
            }}
          />
          <div className="min-w-0 flex-1 overflow-hidden rounded-2xl border border-border bg-card shadow-sm transition focus-within:border-primary/50 focus-within:ring-2 focus-within:ring-primary/10">
            {pendingImages.length > 0 && (
              <div className="flex flex-wrap gap-2 px-3 pt-3">
                {pendingImages.map((image) => (
                  <div key={image.id} className="group relative h-16 w-16" title={image.name}>
                    <img
                      src={image.dataUrl}
                      alt={image.name}
                      className="h-full w-full rounded-lg border border-border object-cover"
                    />
                    <Button
                      type="button"
                      variant="destructive"
                      size="icon"
                      className="absolute -right-1.5 -top-1.5 h-5 w-5 rounded-full opacity-95 shadow-sm"
                      onClick={() =>
                        setPendingImages((current) => current.filter((item) => item.id !== image.id))
                      }
                      aria-label={t("playground.removeImage", { name: image.name })}
                    >
                      <X className="h-3 w-3" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
            <Textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={model ? t("playground.messagePlaceholder") : t("playground.selectModelFirst")}
              disabled={!model || isStreaming}
              className="min-h-[68px] resize-none border-0 bg-transparent px-4 pb-2 pt-3 shadow-none focus-visible:ring-0"
              rows={2}
            />
            <div className="flex items-center justify-between gap-3 border-t border-border/60 px-2 py-1.5">
              <div className="flex min-w-0 items-center gap-1">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-8 rounded-lg px-2 text-muted-foreground hover:text-foreground"
                  onClick={() => fileInputRef.current?.click()}
                  disabled={!model || isStreaming || pendingImages.length >= MAX_IMAGES}
                >
                  <ImagePlus className="mr-1.5 h-4 w-4" />
                  {t("playground.image")}
                </Button>
                <span className="hidden items-center gap-1.5 text-xs text-muted-foreground sm:flex">
                  <ClipboardPaste className="h-3.5 w-3.5" />
                  {t("playground.pasteImageHint")}
                </span>
              </div>
              <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">
                {t("playground.imageCounter", {
                  current: pendingImages.length,
                  max: MAX_IMAGES,
                })}
              </span>
            </div>
          </div>
          {isStreaming ? (
            <Button variant="destructive" size="icon" className="h-11 w-11 rounded-xl" onClick={handleStop}>
              <StopCircle className="h-4 w-4" />
            </Button>
          ) : (
            <Button
              onClick={handleSend}
              size="icon"
              className="h-11 w-11 shrink-0 rounded-xl shadow-sm"
              disabled={!model || (!input.trim() && pendingImages.length === 0)}
              aria-label={t("common.send")}
            >
              <Send className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
