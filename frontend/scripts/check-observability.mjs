import assert from "node:assert/strict"
import { readFile } from "node:fs/promises"

const source = (path) => readFile(new URL(path, import.meta.url), "utf8")
const [app, api, hooks, requests, sessions, details, filters, timeline, diff, table, en, zhCN] = await Promise.all([
  source("../src/App.tsx"),
  source("../src/lib/api.ts"),
  source("../src/hooks/use-requests.ts"),
  source("../src/pages/requests.tsx"),
  source("../src/pages/sessions.tsx"),
  source("../src/pages/request-detail.tsx"),
  source("../src/components/request-filters.tsx"),
  source("../src/components/request-timeline.tsx"),
  source("../src/components/request-diff.tsx"),
  source("../src/components/request-table.tsx"),
  source("../src/locales/en.json"),
  source("../src/locales/zh-CN.json"),
])

for (const route of [
  "/observability/requests",
  "/observability/requests/:requestId",
  "/observability/sessions",
  "/observability/sessions/:sessionId",
]) {
  assert.ok(app.includes(`path="${route}"`), `missing observability route ${route}`)
}

for (const endpoint of [
  "/admin/observability/requests",
  "/admin/observability/sessions",
  "/diff",
  "/content",
]) {
  assert.ok(api.includes(endpoint), `missing observability API contract ${endpoint}`)
}

for (const parameter of [
  "agent_id", "principal_name", "session_id", "project_id", "model", "provider",
  "protocol", "status_class", "capture_status", "q", "from", "to", "cursor",
]) {
  assert.ok(api.includes(parameter), `request query must support ${parameter}`)
}

assert.match(hooks, /useInfiniteQuery/, "cursor pagination must use an infinite query")
assert.match(requests, /RequestFilters/, "requests page must render all request filters")
assert.match(sessions, /session_id/, "sessions page must show explicit Session identity")
assert.match(sessions, /principal_name/, "sessions page must keep principal separate from Agent")
assert.match(details, /summary[\s\S]*timeline[\s\S]*request[\s\S]*response[\s\S]*changes[\s\S]*metadata/i,
  "request details must expose all required tabs")
assert.match(filters, /capture_status/, "filters must expose capture state")
assert.match(timeline, /observations/, "timeline must render gateway observations")
assert.match(diff, /base_id/, "diff UI must support a manual base request")
assert.match(table, /agent_id[\s\S]*principal_name/, "request rows must keep Agent and principal separate")

const captureStates = ["not_captured", "captured", "redacted", "truncated", "expired", "dropped", "missing"]
for (const state of captureStates) {
  assert.ok(`${details}\n${en}\n${zhCN}`.includes(state), `missing visible capture state ${state}`)
}

for (const locale of [JSON.parse(en), JSON.parse(zhCN)]) {
  assert.ok(locale.observability?.navigation?.requests, "locale is missing Requests navigation")
  assert.ok(locale.observability?.navigation?.sessions, "locale is missing Sessions navigation")
  assert.ok(locale.observability?.detail?.tabs?.metadata, "locale is missing request detail tabs")
  for (const state of captureStates) {
    assert.ok(locale.observability?.captureStates?.[state], `locale is missing capture state ${state}`)
  }
}

for (const unsafeSource of [requests, sessions, details, timeline, diff, table]) {
  assert.doesNotMatch(unsafeSource, /dangerouslySetInnerHTML/, "captured content must never be injected as HTML")
}
assert.match(details, /JSON\.stringify/, "captured JSON must be rendered as escaped text")

console.log("observability frontend contract OK")
