import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

const source = (path) => readFile(new URL(path, import.meta.url), "utf8")
const [api, settings, types] = await Promise.all([
  source("../src/lib/api.ts"),
  source("../src/pages/settings.tsx"),
  source("../src/lib/types.ts"),
])

assert.match(api, /opts\.body instanceof FormData/,
  "multipart catalog uploads must not force an application/json content type")
assert.match(api, /form\.set\("catalog", file\)/,
  "catalog upload must use the backend catalog form field")
assert.match(api, /"\/admin\/model-catalog\/import"/,
  "catalog upload must call the offline import endpoint")
assert.match(settings, /accept="\.json,application\/json"/,
  "Settings must limit the offline catalog picker to JSON files")
assert.match(settings, /file\.size > 10 \* 1024 \* 1024/,
  "Settings must reject files larger than the backend catalog limit")
assert.match(settings, /modelCatalogApi\.importFile\(file\)/,
  "Settings must submit the selected catalog file")
assert.match(settings, /href="https:\/\/models\.dev\/api\.json"/,
  "Settings must link to the official catalog download")
assert.match(types, /origin\?: "remote" \| "upload" \| "cache"/,
  "catalog state must expose its data origin")

console.log("model catalog offline import static contract OK")
