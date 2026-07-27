import assert from "node:assert/strict"
import {
  buildPlaygroundBody,
  streamDelta,
  unaryText,
} from "../src/lib/playground-request.ts"

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
  unaryText("openai_responses", {
    output: [{ content: [{ type: "output_text", text: "done" }] }],
  }),
  "done"
)

console.log("playground protocol encoding OK")
