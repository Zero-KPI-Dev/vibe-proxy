# Local Observability Tracing Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add local request tracing, Agent/Session classification, bounded request snapshots, searchable Session replay, and request-to-request diffs without requiring an external observability service.

**Architecture:** Extend the existing `telemetry.Event` and SQLite `request_logs` compatibility surface, add numbered SQLite migrations plus observation/payload tables, and capture a trace envelope before request parsing. Keep metadata capture as the default, expose richer cursor-based Admin APIs, then build Requests and Sessions views over those APIs. Payload capture stays independently bounded, redacted, retained, and optional.

**Tech Stack:** Go 1.25+, `net/http`, pure-Go `modernc.org/sqlite`, React 19, TypeScript, TanStack Query, Tailwind, existing Node contract checks.

---

### Task 1: Introduce numbered SQLite migrations and trace-ready schema

**Files:**
- Create: `internal/store/migrations.go`
- Modify: `internal/store/sqlite.go`
- Modify: `internal/store/sqlite_test.go`

**Step 1: Write failing migration tests**

Add tests that open a current/legacy database and assert:

- a `schema_migrations` table exists with ordered versions;
- new trace/session/Agent columns exist on `request_logs`;
- `trace_observations`, `payload_snapshots`, and `session_annotations` exist;
- reopening is idempotent;
- the committed legacy fixture remains readable.

**Step 2: Run the focused tests and confirm failure**

Run: `go test ./internal/store -run 'TestSQLite(Migrations|OpensAndExtendsLegacy)' -count=1`

Expected: FAIL because migrations/tables are missing.

**Step 3: Implement transactional numbered migrations**

Create a migration list such as:

```go
type migration struct {
    Version int
    SQL     string
}
```

Bootstrap the existing request table as migration 1 and add observability fields/tables/indexes as migration 2. Record each version only after its SQL completes inside one transaction. Preserve duplicate-column compatibility for databases created by the previous ad-hoc migration.

**Step 4: Run store tests**

Run: `go test ./internal/store -count=1`

Expected: PASS.

**Step 5: Commit**

```text
git add internal/store/migrations.go internal/store/sqlite.go internal/store/sqlite_test.go
git commit -m "feat: add observability database migrations"
```

### Task 2: Compile Agent profiles and extract trace/session identity

**Files:**
- Create: `internal/telemetry/identity.go`
- Create: `internal/telemetry/identity_test.go`
- Modify: `internal/config/simple.go`
- Modify: `internal/config/simple_test.go`
- Modify: `internal/config/validate.go`
- Modify: `internal/config/validate_test.go`
- Modify: `internal/ir/ir.go`

**Step 1: Write failing identity and config tests**

Cover:

- `agent_profiles` survive `CompileSimple` in deterministic priority order;
- explicit `X-Vibe-*` headers beat baggage, protocol metadata, profiles, and User-Agent heuristics;
- valid W3C `traceparent` is accepted and invalid context generates a new trace;
- values are trimmed, length-bounded, and reject control characters;
- Agent labels do not change the authenticated principal;
- no Session is fabricated from an API key or User-Agent.

**Step 2: Run tests and confirm failure**

Run: `go test ./internal/telemetry ./internal/config -run 'Identity|AgentProfile' -count=1`

Expected: FAIL because identity extraction and compiled profiles do not exist.

**Step 3: Implement identity types and precedence**

Add typed identity fields:

```go
type RequestIdentity struct {
    TraceID, SpanID, ParentSpanID string
    SessionID, SessionName, SessionKind, SessionPath string
    ParentRequestID string
    AgentID, AgentName, AgentVersion, AgentSource, AgentConfidence string
    ProjectID string
}
```

Parse only allowlisted baggage keys. Add built-in Agent signatures as a small data table. Compile configured profiles into `RuntimeConfig` and validate patterns/limits.

**Step 4: Run focused and package tests**

Run: `go test ./internal/telemetry ./internal/config -count=1`

Expected: PASS.

**Step 5: Commit**

```text
git add internal/telemetry/identity.go internal/telemetry/identity_test.go internal/config internal/ir/ir.go
git commit -m "feat: classify request trace and agent identity"
```

### Task 3: Enrich request summaries and persist/query identity fields

**Files:**
- Modify: `internal/telemetry/telemetry.go`
- Modify: `internal/telemetry/recent_test.go`
- Modify: `internal/store/sqlite.go`
- Modify: `internal/store/sqlite_test.go`
- Modify: `internal/runtime/server.go`
- Modify: `internal/runtime/server_test.go`
- Modify: `frontend/src/lib/types.ts`

**Step 1: Write failing persistence/runtime tests**

Assert a successful request records principal, Agent, Session, trace IDs, total duration, request shape, and both initial/effective route. Assert authentication, parse, authorization, and resolution failures produce finished telemetry without exposing credentials or invalid body content.

**Step 2: Run focused tests and confirm failure**

Run: `go test ./internal/store ./internal/runtime -run 'Identity|EarlyFailure|RequestShape' -count=1`

Expected: FAIL because the fields and early trace envelope are missing.

**Step 3: Extend events and move tracking earlier**

Create the request ID/trace envelope after adapter detection, classify identity from the HTTP request, and finish the tracker on every later error path. Populate request shape after Canonical IR parsing. Keep `client_name` as the serialized compatibility alias while exposing `principal_name`.

**Step 4: Persist and read the new typed columns**

Update SQLite insert/select scanning and TypeScript contracts. Keep legacy null/empty values valid.

**Step 5: Run tests**

Run: `go test ./internal/telemetry ./internal/store ./internal/runtime -count=1`

Expected: PASS.

**Step 6: Commit**

```text
git add internal/telemetry internal/store internal/runtime frontend/src/lib/types.ts
git commit -m "feat: persist trace session and agent summaries"
```

### Task 4: Add bounded content-capture configuration and sanitizer

**Files:**
- Create: `internal/telemetry/capture.go`
- Create: `internal/telemetry/capture_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/simple.go`
- Modify: `internal/config/validate.go`
- Modify: `internal/config/simple_test.go`
- Modify: `internal/config/validate_test.go`
- Modify: `configs/simple.yaml`

**Step 1: Write failing capture tests**

Cover defaults, modes (`metadata`, `structured`, `raw`), request-level reduction,
secret-key redaction, Authorization/cookie exclusion, image/data-URL replacement,
size truncation, and explicit capture status.

**Step 2: Run tests and confirm failure**

Run: `go test ./internal/telemetry ./internal/config -run 'Capture|Observability' -count=1`

Expected: FAIL.

**Step 3: Implement defaults, validation, and sanitizer**

Use metadata mode by default, a 256 KiB snapshot cap, three-day content retention,
and a 512 MiB content quota. The sanitizer must operate on decoded JSON values,
never regex-rewrite serialized JSON.

**Step 4: Run tests and commit**

Run: `go test ./internal/telemetry ./internal/config -count=1`

```text
git add internal/telemetry internal/config configs/simple.yaml
git commit -m "feat: add bounded observability content capture"
```

### Task 5: Persist payload snapshots and gateway observations

**Files:**
- Modify: `internal/telemetry/telemetry.go`
- Create: `internal/telemetry/observation.go`
- Modify: `internal/store/sqlite.go`
- Modify: `internal/store/sqlite_test.go`
- Modify: `internal/runtime/server.go`
- Modify: `internal/runtime/server_test.go`
- Modify: `internal/gatewayapp/app.go`
- Modify: `internal/gatewayapp/app_test.go`

**Step 1: Write failing tests**

Test request/canonical/upstream/response stages, streaming accumulation, payload
expiry, quota eviction, payload-first dropping, and graceful close.

**Step 2: Run tests and confirm failure**

Run: `go test ./internal/store ./internal/runtime ./internal/gatewayapp -run 'Payload|Observation|Recorder' -count=1`

Expected: FAIL.

**Step 3: Implement snapshots and observations**

Extend the sink through optional interfaces so existing sinks remain source
compatible. Serialize snapshots under a shared per-request byte budget. Store
canonical streaming output, not raw SSE frames. Telemetry write failure must not
change the model response.

**Step 4: Run tests and commit**

Run: `go test ./internal/telemetry ./internal/store ./internal/runtime ./internal/gatewayapp -count=1`

```text
git add internal/telemetry internal/store internal/runtime internal/gatewayapp
git commit -m "feat: record request payloads and trace observations"
```

### Task 6: Add cursor-based Requests, Sessions, details, and diff APIs

**Files:**
- Modify: `internal/telemetry/observability.go`
- Modify: `internal/store/sqlite.go`
- Modify: `internal/store/sqlite_test.go`
- Create: `internal/telemetry/diff.go`
- Create: `internal/telemetry/diff_test.go`
- Create: `internal/runtime/observability_handlers.go`
- Modify: `internal/runtime/server.go`
- Modify: `internal/runtime/server_test.go`
- Modify: `docs/admin-api.md`

**Step 1: Write failing query and diff tests**

Cover stable cursors, every supported filter, session aggregates, parent/previous
base selection, append-only turns, rewrites, context shrink, tool/model/provider
changes, missing/expired payloads, and concurrent chronological comparisons.

**Step 2: Run tests and confirm failure**

Run: `go test ./internal/telemetry ./internal/store ./internal/runtime -run 'Query|Session|Diff|Observability' -count=1`

Expected: FAIL.

**Step 3: Implement store queries and Admin handlers**

Use `(started_at, request_id)` opaque cursors. Return capture states rather than
empty bodies. Add exact request-content deletion and `Cache-Control: no-store`.

**Step 4: Run tests and commit**

Run: `go test ./internal/telemetry ./internal/store ./internal/runtime -count=1`

```text
git add internal/telemetry internal/store internal/runtime docs/admin-api.md
git commit -m "feat: expose observability sessions and diffs"
```

### Task 7: Build Requests, Session replay, and detail UI

**Files:**
- Create: `frontend/src/pages/requests.tsx`
- Create: `frontend/src/pages/sessions.tsx`
- Create: `frontend/src/pages/request-detail.tsx`
- Create: `frontend/src/components/request-filters.tsx`
- Create: `frontend/src/components/request-timeline.tsx`
- Create: `frontend/src/components/request-diff.tsx`
- Create: `frontend/scripts/check-observability.mjs`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/pages/observability.tsx`
- Modify: `frontend/src/components/request-table.tsx`
- Modify: `frontend/src/hooks/use-requests.ts`
- Modify: `frontend/src/lib/api.ts`
- Modify: `frontend/src/lib/types.ts`
- Modify: `frontend/src/lib/constants.ts`
- Modify: `frontend/src/locales/en.json`
- Modify: `frontend/src/locales/zh-CN.json`
- Modify: `frontend/package.json`

**Step 1: Add a failing frontend contract check**

Require routes, filter parameters, Session list/timeline, Agent/principal separation,
capture-state UI, escaped JSON display, manual diff-base selection, and both locale
keys.

**Step 2: Run and confirm failure**

Run: `npm --prefix frontend run test:observability`

Expected: FAIL.

**Step 3: Implement the UI**

Keep aggregate charts on Observability. Add Requests and Sessions tabs/routes,
cursor pagination, a detail page with Summary/Timeline/Request/Response/Changes/
Metadata tabs, and clear empty/redacted/truncated/expired states.

**Step 4: Verify frontend and embedded assets**

Run: `npm --prefix frontend test`

Run: `npm --prefix frontend run build`

Run: `go test ./internal/runtime -run 'SPA|Embedded' -count=1`

Expected: PASS.

**Step 5: Commit**

```text
git add frontend internal/runtime/web/dist
git commit -m "feat: add observability request and session views"
```

### Task 8: Update current-state documentation, accept ADR, and verify

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/config-schema.md`
- Modify: `docs/admin-api.md`
- Modify: `docs/contributing-architecture.md`
- Modify: `docs/adr/0007-local-first-observability-trace-model.md`
- Modify: `docs/adr/README.md`
- Modify: `README.md`

**Step 1: Update observable contracts**

Document identities, capture defaults and warnings, retention/quota, endpoints,
Session semantics, limitations, and optional future OTLP mapping. Change ADR-0007
to Accepted only after the implementation and tests match its invariants.

**Step 2: Run complete verification**

Run: `gofmt -w <changed-go-files>`

Run: `$env:CGO_ENABLED='0'; go test ./... -count=1`

Run: `$env:CGO_ENABLED='0'; go vet ./...`

Run: `npm --prefix frontend test`

Run: `npm --prefix frontend run build`

Run: `git diff --check`

Expected: all commands pass and the embedded frontend matches the production build.

**Step 3: Commit**

```text
git add README.md docs frontend internal/runtime/web/dist
git commit -m "docs: document local observability tracing"
```

