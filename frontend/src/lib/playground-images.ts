import type { PlaygroundImage, PlaygroundMessage } from "@/lib/playground-request"

const DATABASE_NAME = "vibe-proxy-playground"
const DATABASE_VERSION = 1
const IMAGE_STORE = "conversation-images"

interface StoredConversationImage {
  id: string
  dataUrl: string
  updatedAt: number
}

let databasePromise: Promise<IDBDatabase> | null = null

function openDatabase(): Promise<IDBDatabase> {
  if (databasePromise) return databasePromise
  databasePromise = new Promise((resolve, reject) => {
    if (typeof indexedDB === "undefined") {
      reject(new Error("indexeddb_unavailable"))
      return
    }
    const request = indexedDB.open(DATABASE_NAME, DATABASE_VERSION)
    request.onupgradeneeded = () => {
      const database = request.result
      if (!database.objectStoreNames.contains(IMAGE_STORE)) {
        database.createObjectStore(IMAGE_STORE, { keyPath: "id" })
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error("indexeddb_open_failed"))
  })
  return databasePromise
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve()
    transaction.onerror = () => reject(transaction.error ?? new Error("indexeddb_transaction_failed"))
    transaction.onabort = () => reject(transaction.error ?? new Error("indexeddb_transaction_aborted"))
  })
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error ?? new Error("indexeddb_request_failed"))
  })
}

export function conversationImageIds(messages: PlaygroundMessage[]): string[] {
  return [...new Set(messages.flatMap((message) => (message.images ?? []).map((image) => image.id)))]
}

export async function persistConversationImages(messages: PlaygroundMessage[]): Promise<void> {
  const images = messages
    .flatMap((message) => message.images ?? [])
    .filter((image): image is PlaygroundImage & { dataUrl: string } => Boolean(image.dataUrl))
  if (images.length === 0) return

  const database = await openDatabase()
  const transaction = database.transaction(IMAGE_STORE, "readwrite")
  const store = transaction.objectStore(IMAGE_STORE)
  const updatedAt = Date.now()
  for (const image of images) {
    const record: StoredConversationImage = {
      id: image.id,
      dataUrl: image.dataUrl,
      updatedAt,
    }
    store.put(record)
  }
  await transactionDone(transaction)
}

export async function hydrateConversationImages(
  messages: PlaygroundMessage[]
): Promise<PlaygroundMessage[]> {
  const ids = conversationImageIds(messages)
  if (ids.length === 0) return messages

  const database = await openDatabase()
  const transaction = database.transaction(IMAGE_STORE, "readonly")
  const store = transaction.objectStore(IMAGE_STORE)
  const records = await Promise.all(
    ids.map((id) => requestResult(store.get(id) as IDBRequest<StoredConversationImage | undefined>))
  )
  const dataUrls = new Map(
    records
      .filter((record): record is StoredConversationImage => Boolean(record?.dataUrl))
      .map((record) => [record.id, record.dataUrl])
  )
  return messages.map((message) => ({
    ...message,
    images: message.images?.map((image) => ({
      ...image,
      dataUrl: image.dataUrl || dataUrls.get(image.id),
    })),
  }))
}

export async function deleteConversationImages(messages: PlaygroundMessage[]): Promise<void> {
  const ids = conversationImageIds(messages)
  if (ids.length === 0) return

  const database = await openDatabase()
  const transaction = database.transaction(IMAGE_STORE, "readwrite")
  const store = transaction.objectStore(IMAGE_STORE)
  for (const id of ids) {
    store.delete(id)
  }
  await transactionDone(transaction)
}
