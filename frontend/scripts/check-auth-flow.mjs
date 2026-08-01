import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

const source = (path) => readFile(new URL(path, import.meta.url), "utf8")
const [main, api, authApi, authGate, en, zhCN] = await Promise.all([
  source("../src/main.tsx"),
  source("../src/lib/api.ts"),
  source("../src/lib/auth-api.ts"),
  source("../src/components/auth-gate.tsx"),
  source("../src/locales/en.json"),
  source("../src/locales/zh-CN.json"),
])

assert.match(main, /<AuthGate>[\s\S]*?<App \/>[\s\S]*?<\/AuthGate>/,
  "the application must be protected by AuthGate")
for (const endpoint of ["/auth/status", "/auth/setup", "/auth/login", "/auth/logout"]) {
  assert.ok(authApi.includes(`"${endpoint}"`), `auth API must expose ${endpoint}`)
}
assert.match(authApi, /credentials: "same-origin"/,
  "auth requests must send the HttpOnly same-origin session cookie")
assert.doesNotMatch(authApi, /localStorage|Authorization|Bearer/,
  "management-password auth must not expose or depend on the hidden admin token")
assert.match(authGate, /status\.mode === "token"/,
  "CLI bearer-token mode must remain compatible")
assert.match(authGate, /status\.authenticated && status\.initialized/,
  "desktop mode must require an authenticated initialized session")
assert.match(authGate, /authApi\.setup\(password\)/,
  "first-run setup must use the password setup endpoint")
assert.match(authGate, /authApi\.login\(password\)/,
  "browser login must use the password login endpoint")
assert.match(authGate, /autoComplete="new-password"/,
  "first-run password fields must support password managers")
assert.match(authGate, /autoComplete="current-password"/,
  "login must identify the current-password field")
assert.match(api, /resp\.status === 401[\s\S]*?AUTH_REQUIRED_EVENT/,
  "expired admin sessions must return the UI to the auth gate")

const englishAuth = JSON.parse(en).auth
const chineseAuth = JSON.parse(zhCN).auth
assert.ok(englishAuth && chineseAuth, "both locales must define auth strings")
assert.deepEqual(Object.keys(chineseAuth).sort(), Object.keys(englishAuth).sort(),
  "auth locale keys must match")
for (const [key, value] of Object.entries(chineseAuth)) {
  assert.notEqual(value, englishAuth[key], `Chinese auth string ${key} must be translated`)
}

console.log("management password auth flow static contract OK")
