import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

const source = (path) => readFile(new URL(path, import.meta.url), "utf8")
const [api, types, hook, page, en, zhCN] = await Promise.all([
  source("../src/lib/api.ts"),
  source("../src/lib/types.ts"),
  source("../src/hooks/use-client-keys.ts"),
  source("../src/pages/client-keys.tsx"),
  source("../src/locales/en.json"),
  source("../src/locales/zh-CN.json"),
])

assert.match(types, /recoverable: boolean/,
  "client-key list metadata must identify whether a full key can be viewed")
assert.match(api, /reveal: \(name: string\)[\s\S]*?\/admin\/client-keys\/\$\{encodeURIComponent\(name\)\}/,
  "client-key API must expose the authenticated reveal endpoint")
assert.match(hook, /useRevealClientKey[\s\S]*?clientKeyApi\.reveal/,
  "revealing a client key must be an explicit, on-demand user action")
assert.match(api, /rotate: \(name: string\)[\s\S]*?\/admin\/client-keys\/\$\{encodeURIComponent\(name\)\}\/rotate/,
  "legacy keys must have an explicit rotation endpoint")
assert.match(hook, /useRotateClientKey[\s\S]*?clientKeyApi\.rotate/,
  "client-key rotation must refresh list metadata")
assert.match(page, /handleRevealKey[\s\S]*?revealedKey\.rawKey/,
  "client-key page must render the value returned by an explicit reveal")
assert.match(page, /k\.recoverable \? \([\s\S]*?handleRevealKey\(k\)[\s\S]*?: \([\s\S]*?handleRotateKey\(k\)/,
  "legacy hash-only keys must offer rotation instead of a misleading reveal")

const english = JSON.parse(en).clientKeys
const chinese = JSON.parse(zhCN).clientKeys
assert.deepEqual(Object.keys(chinese).sort(), Object.keys(english).sort(),
  "client-key locale keys must match")
assert.doesNotMatch(chinese.createDescription, /只会显示一次|无法再次查看/,
  "new client keys must not be described as one-time secrets")

console.log("recoverable client Keys static contract OK")
