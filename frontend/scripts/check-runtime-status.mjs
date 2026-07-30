import assert from "node:assert/strict"
import { formatUptime } from "../src/lib/runtime-status.ts"

const startedAt = "2026-07-28T12:00:00Z"
assert.equal(formatUptime(startedAt, Date.parse("2026-07-28T12:00:00Z")), "00:00:00")
assert.equal(formatUptime(startedAt, Date.parse("2026-07-28T13:02:03Z")), "01:02:03")
assert.equal(formatUptime(startedAt, Date.parse("2026-07-30T13:02:03Z")), "49:02:03")
assert.equal(formatUptime(startedAt, Date.parse("2026-07-28T11:59:59Z")), "00:00:00")
assert.equal(formatUptime("not-a-date"), "--:--:--")
assert.equal(formatUptime(undefined), "--:--:--")

console.log("runtime uptime formatting OK")
