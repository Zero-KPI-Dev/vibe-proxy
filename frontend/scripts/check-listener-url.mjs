import assert from "node:assert/strict"
import { dataPlaneOrigin } from "../src/lib/listener-url.ts"

const cases = [
  [undefined, "127.0.0.1", "http://127.0.0.1:8080"],
  ["0.0.0.0:9000", "127.0.0.1", "http://127.0.0.1:9000"],
  ["0.0.0.0:9000", "[::1]", "http://127.0.0.1:9000"],
  ["[::]:9000", "[::1]", "http://[::1]:9000"],
  ["[::]:9000", "127.0.0.1", "http://127.0.0.1:9000"],
  ["[::1]:9000", "127.0.0.1", "http://[::1]:9000"],
  ["192.168.1.20:9000", "[::1]", "http://192.168.1.20:9000"],
  ["malformed", "127.0.0.1", "http://127.0.0.1:8080"],
]

for (const [listen, browserHostname, expected] of cases) {
  assert.equal(dataPlaneOrigin(listen, browserHostname), expected)
}

console.log("listener URL formatting OK")
