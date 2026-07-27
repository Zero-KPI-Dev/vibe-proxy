export type PlaygroundEndpoint = "openai_chat" | "openai_responses" | "anthropic"

export interface PlaygroundImage {
  id: string
  name: string
  mediaType: string
  size: number
  dataUrl?: string
}

export interface PlaygroundMessage {
  id: string
  role: "user" | "assistant"
  content: string
  images?: PlaygroundImage[]
}

export interface PlaygroundParameters {
  temperature: number
  topP: number
  maxTokens: number
  stop: string[]
  stream: boolean
}

function usableImages(message: PlaygroundMessage) {
  return (message.images ?? []).filter(
    (image): image is PlaygroundImage & { dataUrl: string } => Boolean(image.dataUrl)
  )
}

function openAIChatContent(message: PlaygroundMessage): string | Record<string, unknown>[] {
  const images = usableImages(message)
  if (images.length === 0) return message.content
  const blocks: Record<string, unknown>[] = []
  if (message.content) blocks.push({ type: "text", text: message.content })
  for (const image of images) {
    blocks.push({ type: "image_url", image_url: { url: image.dataUrl } })
  }
  return blocks
}

function responsesContent(message: PlaygroundMessage): Record<string, unknown>[] {
  const blocks: Record<string, unknown>[] = []
  if (message.content) blocks.push({ type: "input_text", text: message.content })
  for (const image of usableImages(message)) {
    blocks.push({ type: "input_image", image_url: image.dataUrl })
  }
  return blocks
}

function anthropicContent(message: PlaygroundMessage): Record<string, unknown>[] {
  const blocks: Record<string, unknown>[] = []
  if (message.content) blocks.push({ type: "text", text: message.content })
  for (const image of usableImages(message)) {
    const comma = image.dataUrl.indexOf(",")
    const data = comma >= 0 ? image.dataUrl.slice(comma + 1) : image.dataUrl
    blocks.push({
      type: "image",
      source: {
        type: "base64",
        media_type: image.mediaType,
        data,
      },
    })
  }
  return blocks
}

export function buildPlaygroundBody(
  endpoint: PlaygroundEndpoint,
  model: string,
  messages: PlaygroundMessage[],
  parameters: PlaygroundParameters
): Record<string, unknown> {
  if (endpoint === "openai_responses") {
    return {
      model,
      input: messages.map((message) => ({
        type: "message",
        role: message.role,
        content: responsesContent(message),
      })),
      stream: parameters.stream,
      temperature: parameters.temperature,
      top_p: parameters.topP,
      max_output_tokens: parameters.maxTokens,
    }
  }

  if (endpoint === "anthropic") {
    const body: Record<string, unknown> = {
      model,
      messages: messages.map((message) => ({
        role: message.role,
        content: anthropicContent(message),
      })),
      stream: parameters.stream,
      temperature: parameters.temperature,
      top_p: parameters.topP,
      max_tokens: parameters.maxTokens,
    }
    if (parameters.stop.length > 0) body.stop_sequences = parameters.stop
    return body
  }

  const body: Record<string, unknown> = {
    model,
    messages: messages.map((message) => ({
      role: message.role,
      content: openAIChatContent(message),
    })),
    stream: parameters.stream,
    temperature: parameters.temperature,
    top_p: parameters.topP,
    max_tokens: parameters.maxTokens,
  }
  if (parameters.stop.length > 0) body.stop = parameters.stop
  return body
}

export function streamDelta(endpoint: PlaygroundEndpoint, payload: Record<string, any>): string {
  if (endpoint === "anthropic") return payload.delta?.text ?? ""
  if (endpoint === "openai_responses") {
    return payload.type === "response.output_text.delta" ? payload.delta ?? "" : ""
  }
  return payload.choices?.[0]?.delta?.content ?? payload.choices?.[0]?.text ?? ""
}

export function unaryText(endpoint: PlaygroundEndpoint, payload: Record<string, any>): string {
  if (endpoint === "anthropic") {
    const text = (payload.content ?? [])
      .filter((block: Record<string, unknown>) => block.type === "text")
      .map((block: Record<string, unknown>) => String(block.text ?? ""))
      .join("")
    return text || JSON.stringify(payload)
  }
  if (endpoint === "openai_responses") {
    const text = (payload.output ?? [])
      .flatMap((item: Record<string, any>) => item.content ?? [])
      .filter((block: Record<string, unknown>) => block.type === "output_text")
      .map((block: Record<string, unknown>) => String(block.text ?? ""))
      .join("")
    return text || JSON.stringify(payload)
  }
  return payload.choices?.[0]?.message?.content ?? payload.choices?.[0]?.text ?? JSON.stringify(payload)
}
