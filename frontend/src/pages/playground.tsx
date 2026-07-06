import { useState, useRef, useEffect, useCallback } from "react"
import { Send, Trash2, Settings2, StopCircle, History, FileJson, KeyRound, Eye, EyeOff } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Separator } from "@/components/ui/separator"
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
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
import { toast } from "sonner"

type ChatEntry = {
  id: string
  role: "user" | "assistant"
  content: string
}

const STORAGE_KEY = "vibe_playground_conversations"

function loadConversations(): Record<string, ChatEntry[]> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? JSON.parse(raw) : {}
  } catch {
    return {}
  }
}

function saveConversation(id: string, messages: ChatEntry[]) {
  const all = loadConversations()
  all[id] = messages
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(all))
  } catch {
    // storage full — silently ignore
  }
}

function deleteConversation(id: string) {
  const all = loadConversations()
  delete all[id]
  localStorage.setItem(STORAGE_KEY, JSON.stringify(all))
}

export function PlaygroundPage() {
  const [messages, setMessages] = useState<ChatEntry[]>([])
  const [input, setInput] = useState("")
  const [model, setModel] = useState("")
  const [endpoint, setEndpoint] = useState("openai_chat")
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
  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  const savedConvs = loadConversations()
  const convEntries = Object.entries(savedConvs)

  const apiPath =
    endpoint === "openai_responses"
      ? "/v1/responses"
      : endpoint === "anthropic"
        ? "/anthropic/v1/messages"
        : "/v1/chat/completions"

  const scrollToBottom = useCallback(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [])

  useEffect(() => {
    scrollToBottom()
  }, [messages, scrollToBottom])

  useEffect(() => {
    if (convId) saveConversation(convId, messages)
  }, [messages, convId])

  const handleLoad = (id: string) => {
    const conv = savedConvs[id]
    if (conv) {
      setMessages(conv)
      setConvId(id)
      toast.success("Conversation restored")
    }
  }

  const handleDeleteConv = (id: string) => {
    deleteConversation(id)
    if (convId === id) {
      setConvId(null)
      setMessages([])
    }
    toast.success("Conversation deleted")
  }

  const handleSend = async () => {
    const trimmed = input.trim()
    if (!trimmed || !model) return

    const userMsg: ChatEntry = { id: crypto.randomUUID(), role: "user", content: trimmed }
    const assistantMsg: ChatEntry = { id: crypto.randomUUID(), role: "assistant", content: "" }

    setMessages((prev) => [...prev, userMsg, assistantMsg])
    if (!convId) setConvId(crypto.randomUUID())
    setInput("")
    setIsStreaming(true)

    const controller = new AbortController()
    abortRef.current = controller

    try {
      const adminToken = localStorage.getItem("vibe_admin_token") ?? ""
      const token = useClientKey && clientKeyValue ? clientKeyValue : adminToken
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
      }
      if (token) headers["Authorization"] = `Bearer ${token}`

      const body: Record<string, unknown> = {
        model,
        messages: [...messages, userMsg].map((m) => ({
          role: m.role,
          content: m.content,
        })),
        stream: streaming,
        temperature,
        top_p: topP,
        max_tokens: maxTokens,
      }
      if (stopSequences.trim()) {
        body.stop = stopSequences.split(",").map((s) => s.trim()).filter(Boolean)
      }

      const resp = await fetch(apiPath, {
        method: "POST",
        headers,
        body: JSON.stringify(body),
        signal: controller.signal,
      })

      if (!resp.ok) {
        const errText = await resp.text()
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantMsg.id
              ? { ...m, content: `Error: ${resp.status} - ${errText}` }
              : m
          )
        )
        setIsStreaming(false)
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
                const parsed = JSON.parse(data)
                let delta = ""
                if (endpoint === "anthropic") {
                  delta = parsed.delta?.text ?? ""
                } else {
                  delta = parsed.choices?.[0]?.delta?.content ?? parsed.choices?.[0]?.text ?? ""
                }
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
        let content = ""
        if (endpoint === "anthropic") {
          content = json.content?.[0]?.text ?? JSON.stringify(json)
        } else {
          content = json.choices?.[0]?.message?.content ?? json.choices?.[0]?.text ?? JSON.stringify(json)
        }
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
              ? { ...m, content: "[Stopped]" }
              : m
          )
        )
      } else {
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantMsg.id
              ? { ...m, content: `Error: ${(e as Error).message}` }
              : m
          )
        )
      }
    } finally {
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
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between mb-4 shrink-0">
        <div>
          <h1 className="text-2xl font-semibold">Playground</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Test models through vibe-proxy
          </p>
        </div>
        <div className="flex items-center gap-2">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm">
                <History className="h-4 w-4 mr-1" />
                History
                {convEntries.length > 0 && (
                  <Badge variant="secondary" className="ml-1 text-xs">{convEntries.length}</Badge>
                )}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuLabel>Saved Conversations</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {convEntries.length === 0 ? (
                <DropdownMenuItem disabled>No saved conversations</DropdownMenuItem>
              ) : (
                convEntries.map(([id, msgs]) => (
                  <div key={id} className="flex items-center gap-1 px-1">
                    <DropdownMenuItem
                      className="flex-1 truncate"
                      onClick={() => handleLoad(id)}
                    >
                      {msgs[0]?.content.slice(0, 40) || "Empty"}...
                    </DropdownMenuItem>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-6 w-6 shrink-0"
                      onClick={() => handleDeleteConv(id)}
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
                  Raw JSON
                </Button>
              </DialogTrigger>
              <DialogContent className="max-w-2xl max-h-[80vh]">
                <DialogHeader>
                  <DialogTitle>Raw Response JSON</DialogTitle>
                </DialogHeader>
                <pre className="overflow-auto rounded-lg bg-background border border-border p-4 text-xs font-mono max-h-[60vh]">
                  {rawJson}
                </pre>
              </DialogContent>
            </Dialog>
          )}

          <Button variant="ghost" size="sm" onClick={handleClear}>
            <Trash2 className="h-4 w-4 mr-1" />
            Clear
          </Button>

          <Sheet>
            <SheetTrigger asChild>
              <Button variant="outline" size="sm">
                <Settings2 className="h-4 w-4 mr-1" />
                Params
              </Button>
            </SheetTrigger>
            <SheetContent>
              <SheetHeader>
                <SheetTitle>Parameters</SheetTitle>
                <SheetDescription>
                  Adjust request parameters for the playground
                </SheetDescription>
              </SheetHeader>
              <div className="space-y-6 py-6">
                <div className="space-y-2">
                  <div className="flex justify-between">
                    <Label>Temperature</Label>
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
                    <Label>Top-P</Label>
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
                    <Label>Max Tokens</Label>
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
                  <Label>Stop Sequences</Label>
                  <Input
                    value={stopSequences}
                    onChange={(e) => setStopSequences(e.target.value)}
                    placeholder="comma-separated, e.g. \n\n,---"
                  />
                  <p className="text-xs text-muted-foreground">
                    Comma-separated stop sequences
                  </p>
                </div>
                <Separator />
                <div className="flex items-center justify-between">
                  <Label>Streaming</Label>
                  <Switch checked={streaming} onCheckedChange={setStreaming} />
                </div>
                <Separator />
                <div className="space-y-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <KeyRound className="h-4 w-4 text-muted-foreground" />
                      <Label>Use Client Key</Label>
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
                    Use a vibe-proxy client key instead of the admin token for data-plane requests
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

      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto rounded-xl border border-border bg-card mb-4"
      >
        {messages.length === 0 ? (
          <EmptyState
            title="Start a conversation"
            description="Select a model and send a message to test the proxy."
            className="h-full"
          />
        ) : (
          <div className="px-4">
            {messages.map((msg) => (
              <ChatMessage key={msg.id} role={msg.role} content={msg.content} />
            ))}
          </div>
        )}
      </div>

      <div className="flex items-end gap-2 shrink-0">
        <div className="flex-1 relative">
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={model ? "Type a message..." : "Select a model first..."}
            disabled={!model || isStreaming}
            className="pr-10 py-3 h-auto"
          />
        </div>
        {isStreaming ? (
          <Button variant="destructive" onClick={handleStop}>
            <StopCircle className="h-4 w-4 mr-1" />
            Stop
          </Button>
        ) : (
          <Button onClick={handleSend} disabled={!model || !input.trim()}>
            <Send className="h-4 w-4 mr-1" />
            Send
          </Button>
        )}
      </div>
    </div>
  )
}
