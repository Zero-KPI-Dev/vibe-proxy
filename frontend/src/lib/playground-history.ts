import type { PlaygroundMessage } from "@/lib/playground-request"

export type PlaygroundConversations = Record<string, PlaygroundMessage[]>

export function conversationForStorage(messages: PlaygroundMessage[]): PlaygroundMessage[] {
  return messages.map((message) => ({
    ...message,
    images: message.images?.map(({ dataUrl: _dataUrl, ...image }) => image),
  }))
}

export function withConversation(
  conversations: PlaygroundConversations,
  id: string,
  messages: PlaygroundMessage[]
): PlaygroundConversations {
  return {
    ...conversations,
    [id]: conversationForStorage(messages),
  }
}

export function withoutConversation(
  conversations: PlaygroundConversations,
  id: string
): PlaygroundConversations {
  const next = { ...conversations }
  delete next[id]
  return next
}
