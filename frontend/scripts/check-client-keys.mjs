import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

const source = (path) => readFile(new URL(path, import.meta.url), "utf8")
const [api, types, hook, page, clipboard, en, zhCN] = await Promise.all([
  source("../src/lib/api.ts"),
  source("../src/lib/types.ts"),
  source("../src/hooks/use-client-keys.ts"),
  source("../src/pages/client-keys.tsx"),
  source("../src/lib/clipboard.ts"),
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
assert.match(page, /t\("clientKeys\.key"\)/,
  "the client-key column must use the concise key label")
assert.match(page, /handleToggleKey\(k\)[\s\S]*?revealedKey\.rawKey/,
  "clicking a masked key must reveal it inline")
assert.match(page, /handleCopyKey\(k\)/,
  "each recoverable key must have an adjacent copy action")
const copyHandler = page.match(/const handleCopyKey = async[\s\S]*?\n  }\n\n  const handleRotateKey/)?.[0] ?? ""
assert.doesNotMatch(copyHandler, /setRevealedKey/,
  "copying a key must not change its masked or revealed display state")
assert.match(page, /<Table className="table-fixed">[\s\S]*?<colgroup>/,
  "revealing a long key must not cause the browser to recalculate table columns")
assert.doesNotMatch(page, /max-w-\[34rem\]/,
  "the key value must not be constrained to a width that wraps on wide tables")
assert.match(page, /flex-1 truncate whitespace-nowrap/,
  "revealing a long key must keep the table row on a single line")
assert.doesNotMatch(page, /<Dialog open=\{!!revealedKey\}/,
  "revealing a client key must not open a separate dialog")
assert.match(page, /!k\.recoverable[\s\S]*?handleRotateKey\(k\)/,
  "legacy hash-only keys must offer rotation instead of a misleading reveal")
assert.match(page, /copyText\(value\)/,
  "client-key copy must use the WebView-compatible clipboard helper")
assert.match(clipboard, /navigator\.clipboard\?\.writeText/,
  "clipboard helper should prefer the modern Clipboard API")
assert.match(clipboard, /desktopApi\.copyText\(value\)/,
  "desktop WebViews must fall back to the authenticated native clipboard bridge")
assert.match(api, /copyText: \(text: string\)[\s\S]*?\/admin\/desktop\/clipboard/,
  "frontend API must expose the authenticated desktop clipboard endpoint")
assert.match(clipboard, /document\.execCommand\("copy"\)/,
  "clipboard helper must retain a local WebView fallback")

const english = JSON.parse(en).clientKeys
const chinese = JSON.parse(zhCN).clientKeys
assert.deepEqual(Object.keys(chinese).sort(), Object.keys(english).sort(),
  "client-key locale keys must match")
assert.equal(chinese.key, "密钥",
  "the Chinese table header should say key, not key prefix")
assert.equal(chinese.keyPrefix, undefined,
  "the obsolete key-prefix label must be removed")
assert.doesNotMatch(chinese.createDescription, /只会显示一次|无法再次查看/,
  "new client keys must not be described as one-time secrets")

console.log("recoverable client Keys static contract OK")
