import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

const source = (path) => readFile(new URL(path, import.meta.url), "utf8")
const [api, settings, desktopSettings, en, zhCN] = await Promise.all([
  source("../src/lib/api.ts"),
  source("../src/pages/settings.tsx"),
  source("../src/components/desktop-settings.tsx"),
  source("../src/locales/en.json"),
  source("../src/locales/zh-CN.json"),
])

assert.match(api, /fetch\(path, \{ \.\.\.opts, credentials: "same-origin", headers \}\)/,
  "admin requests must explicitly send same-origin cookies")
assert.doesNotMatch(settings, /if \(!getToken\(\)\) return/,
  "model catalog loading must not require a browser bearer token")
assert.match(api, /export const desktopApi = \{[\s\S]*?snapshot: \(\) => request<DesktopSnapshot>\("\/admin\/desktop"\),/,
  "desktop snapshot API must use DesktopSnapshot")
assert.match(api, /setCloseBehavior:[\s\S]*?request<\{ close_behavior: CloseBehavior \}>\("\/admin\/desktop\/preferences"/,
  "desktop preferences must use the controller's exact response schema")
assert.match(api, /openDataDir:[\s\S]*?request<\{ opened: boolean \}>\("\/admin\/desktop\/open-data-dir"/,
  "open-data action must use the controller's exact response schema")
assert.match(api, /importConfig:[\s\S]*?request<\{ imported: boolean; path\?: string; loaded_at\?: string \}>\(/,
  "config import must expose the controller response schema")
assert.match(settings, /desktop\?\.available && <DesktopSettings/,
  "desktop controls must render only when the desktop snapshot is available")

for (const value of ["ask", "tray", "quit"]) {
  assert.match(desktopSettings, new RegExp(`value="${value}"`),
    `desktop settings must offer the ${value} close behavior`)
}
assert.match(desktopSettings, /desktopApi\.openDataDir\(\)/,
  "open-directory action must call desktopApi")
assert.match(desktopSettings, /desktopApi\.importConfig\(\)/,
  "config import action must call desktopApi")
assert.doesNotMatch(`${api}\n${settings}\n${desktopSettings}`, /\bwails\b/i,
  "desktop settings must not call Wails directly")

const englishDesktop = JSON.parse(en).settings?.desktop
const chineseDesktop = JSON.parse(zhCN).settings?.desktop
assert.ok(englishDesktop && chineseDesktop, "both locales must define settings.desktop")
assert.deepEqual(Object.keys(chineseDesktop).sort(), Object.keys(englishDesktop).sort(),
  "settings.desktop locale keys must match")
for (const [key, value] of Object.entries(chineseDesktop)) {
  assert.notEqual(value, englishDesktop[key], `Chinese desktop string ${key} must be translated`)
}

console.log("desktop Settings static contract OK")
