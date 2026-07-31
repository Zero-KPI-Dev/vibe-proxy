import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

const layout = await readFile(new URL("../src/layouts/app-layout.tsx", import.meta.url), "utf8")
const css = await readFile(new URL("../src/index.css", import.meta.url), "utf8")
const providerForm = await readFile(new URL("../src/components/provider-form.tsx", import.meta.url), "utf8")

assert.match(layout, /h-dvh min-h-0 overflow-hidden/,
  "the application shell must own viewport scrolling")
assert.match(layout, /min-h-0 flex-1 overflow-y-auto overflow-x-hidden/,
  "the main flex child must be allowed to shrink and provide the only page scrollbar")
assert.match(css, /#root\s*\{[\s\S]*?height:\s*100%;[\s\S]*?overflow:\s*hidden;/,
  "document roots must not create a second scrollbar around the application shell")
assert.doesNotMatch(providerForm, /max-h-72 space-y-2 overflow-y-auto/,
  "provider capability rows must not create a nested page scrollbar")

console.log("desktop layout scrolling contract OK")
