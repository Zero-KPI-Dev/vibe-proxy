import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

const source = (path) => readFile(new URL(path, import.meta.url), "utf8")
const [component, types, english, chinese] = await Promise.all([
  source("../src/components/ocr-fallback-settings.tsx"),
  source("../src/lib/types.ts"),
  source("../src/locales/en.json"),
  source("../src/locales/zh-CN.json"),
])

assert.match(types, /interface VisionFallbackModelOption/,
  "multimodal responses must expose typed Vision fallback candidates")
assert.match(types, /vision_fallback_models: VisionFallbackModelOption\[\]/,
  "multimodal snapshots must carry the effective Vision fallback candidates")
assert.match(component, /response\.vision_fallback_models/,
  "OCR settings must consume the backend-resolved candidate list")
assert.doesNotMatch(component, /providerApi\.list/,
  "OCR settings must not fetch raw providers to infer capabilities")
assert.doesNotMatch(component, /provider\.model_capabilities/,
  "OCR settings must not duplicate capability precedence in the browser")
assert.match(component, /visionFallbackUnavailable/,
  "a configured candidate that is no longer valid must be labelled unavailable")
assert.match(types, /vision_fallback_strategy: "assist" \| "takeover" \| "reject"/,
  "multimodal configuration must type all Vision fallback strategies")
assert.match(component, /visionFallbackAssist[\s\S]*visionFallbackTakeover[\s\S]*visionFallbackReject/,
  "OCR settings must expose assist, takeover, and reject choices")
assert.match(english, /"visionFallbackSourceModelsDev"/,
  "English translations must explain catalog-derived capabilities")
assert.match(chinese, /"visionFallbackSourceModelsDev"/,
  "Chinese translations must explain catalog-derived capabilities")

console.log("OCR Vision fallback candidate contract OK")
