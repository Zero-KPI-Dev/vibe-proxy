import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

const layout = await readFile(new URL("../src/layouts/app-layout.tsx", import.meta.url), "utf8")
const authGate = await readFile(new URL("../src/components/auth-gate.tsx", import.meta.url), "utf8")
const brandMark = await readFile(new URL("../src/components/brand-mark.tsx", import.meta.url), "utf8")
const requestTable = await readFile(new URL("../src/components/request-table.tsx", import.meta.url), "utf8")
const badge = await readFile(new URL("../src/components/ui/badge.tsx", import.meta.url), "utf8")
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
assert.match(layout, /<BrandMark/, "the application shell must use the shared product mark")
assert.match(authGate, /<BrandMark/, "the authentication shell must use the shared product mark")
assert.match(brandMark, /5\.5 10\.5H8[\s\S]*18\.5 16h8[\s\S]*14\.5 7\.5/,
  "the shared product mark must preserve the Flow Gate geometry")
assert.match(requestTable, /min-w-\[5\.5rem\][\s\S]*flex-col items-start/,
  "request statuses must stack without forcing a wide table column")
assert.match(badge, /whitespace-nowrap/, "badges must not wrap into vertical labels")

console.log("desktop layout scrolling contract OK")
