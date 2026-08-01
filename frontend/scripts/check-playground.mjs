import assert from "node:assert/strict"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import {
  buildPlaygroundBody,
  clipboardImageFiles,
  streamDelta,
  streamReasoningDelta,
  unaryText,
  unaryReasoning,
} from "../src/lib/playground-request.ts"
import {
  conversationForStorage,
  withConversation,
  withoutConversation,
} from "../src/lib/playground-history.ts"
import { conversationImageIds } from "../src/lib/playground-images.ts"

const image = {
  id: "image-1",
  name: "receipt.png",
  mediaType: "image/png",
  size: 3,
  dataUrl: "data:image/png;base64,AAEC",
}
const messages = [
  {
    id: "message-1",
    role: "user",
    content: "read this",
    images: [image],
  },
]
const parameters = {
  stream: false,
  temperature: 0.7,
  topP: 1,
  maxTokens: 128,
  stop: ["END"],
}

const clipboardImage = { name: "clipboard.png", type: "image/png" }
assert.deepEqual(
  clipboardImageFiles({
    items: [
      { kind: "string", type: "text/plain", getAsFile: () => null },
      { kind: "file", type: "image/png", getAsFile: () => clipboardImage },
    ],
    files: [],
  }),
  [clipboardImage]
)
assert.deepEqual(
  clipboardImageFiles({
    items: [],
    files: [clipboardImage, { name: "notes.txt", type: "text/plain" }],
  }),
  [clipboardImage]
)

const conversationWithImage = [{
  id: "message-1",
  role: "user",
  content: "describe this",
  images: [{
    id: "image-1",
    name: "cat.png",
    mediaType: "image/png",
    size: 42,
    dataUrl: "data:image/png;base64,secret-image-bytes",
  }],
}]
const storedConversation = conversationForStorage(conversationWithImage)
assert.equal(storedConversation[0].images[0].dataUrl, undefined)
assert.equal(conversationWithImage[0].images[0].dataUrl, "data:image/png;base64,secret-image-bytes")
assert.deepEqual(conversationImageIds(conversationWithImage), ["image-1"])

const conversationWithReasoning = conversationForStorage([{
  id: "assistant-1",
  role: "assistant",
  content: "final answer",
  reasoning: "private reasoning",
}])
assert.equal(conversationWithReasoning[0].reasoning, "private reasoning")

const conversationStore = withConversation({}, "conversation-1", conversationWithImage)
const prunedConversationStore = withoutConversation(conversationStore, "conversation-1")
assert.deepEqual(prunedConversationStore, {})
assert.equal(Object.keys(conversationStore).length, 1)

const markdownHtml = renderToStaticMarkup(
  React.createElement(ReactMarkdown, {
    remarkPlugins: [remarkGfm],
    children: [
      "## Summary",
      "",
      "1. First",
      "2. Second",
      "",
      "| Model | Vision |",
      "| --- | --- |",
      "| demo | yes |",
    ].join("\n"),
  })
)
assert.match(markdownHtml, /<h2>Summary<\/h2>/)
assert.match(markdownHtml, /<ol>/)
assert.match(markdownHtml, /<table>/)

const chat = buildPlaygroundBody("openai_chat", "vision", messages, parameters)
assert.deepEqual(chat.messages[0].content, [
  { type: "text", text: "read this" },
  { type: "image_url", image_url: { url: image.dataUrl } },
])
assert.deepEqual(chat.stop, ["END"])

const responses = buildPlaygroundBody("openai_responses", "vision", messages, parameters)
assert.deepEqual(responses.input[0].content, [
  { type: "input_text", text: "read this" },
  { type: "input_image", image_url: image.dataUrl },
])
assert.equal(responses.max_output_tokens, 128)

const anthropic = buildPlaygroundBody("anthropic", "vision", messages, parameters)
assert.deepEqual(anthropic.messages[0].content, [
  { type: "text", text: "read this" },
  {
    type: "image",
    source: { type: "base64", media_type: "image/png", data: "AAEC" },
  },
])
assert.deepEqual(anthropic.stop_sequences, ["END"])

assert.equal(
  streamDelta("openai_responses", { type: "response.output_text.delta", delta: "ok" }),
  "ok"
)
assert.equal(
  streamReasoningDelta("openai_chat", {
    choices: [{ delta: { reasoning_content: "thinking" } }],
  }),
  "thinking"
)
assert.equal(
  streamReasoningDelta("anthropic", {
    delta: { type: "thinking_delta", thinking: "thinking" },
  }),
  "thinking"
)
assert.equal(
  streamReasoningDelta("openai_responses", {
    type: "response.reasoning_summary_text.delta",
    delta: "thinking",
  }),
  "thinking"
)
assert.equal(
  unaryText("openai_responses", {
    output: [{ content: [{ type: "output_text", text: "done" }] }],
  }),
  "done"
)
assert.equal(
  unaryReasoning("openai_chat", {
    choices: [{ message: { reasoning_content: "thinking" } }],
  }),
  "thinking"
)
assert.equal(
  unaryReasoning("anthropic", {
    content: [{ type: "thinking", thinking: "thinking" }],
  }),
  "thinking"
)
assert.equal(
  unaryReasoning("openai_responses", {
    output: [{
      type: "reasoning",
      summary: [{ type: "summary_text", text: "thinking" }],
    }],
  }),
  "thinking"
)

console.log("playground protocol encoding OK")
