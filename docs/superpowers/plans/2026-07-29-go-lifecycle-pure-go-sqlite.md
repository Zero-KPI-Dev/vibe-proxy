# Go Lifecycle and Pure-Go SQLite Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Upgrade vibe-proxy to the currently supported Go lifecycle and replace its CGO SQLite driver with a database-compatible pure-Go driver.

**Architecture:** Keep the existing `internal/store.SQLite` API and schema, but construct a platform-safe SQLite file URI and open it with `modernc.org/sqlite`. Pin Go 1.25 as the minimum, test Go 1.25 and 1.26, and build all artifacts with CGO disabled.

**Tech Stack:** Go 1.25/1.26, `database/sql`, `modernc.org/sqlite` v1.55.0, Docker, GitHub Actions

## Global Constraints

- `go.mod` minimum version is exactly `go 1.25.0`.
- Docker and release builds use exactly Go 1.26.5 for this 2026-07-29 change.
- CI covers the latest Go 1.25.x and Go 1.26.x patch releases.
- The existing `Open(path string) (*SQLite, error)` API and `request_logs` schema remain unchanged.
- Existing SQLite database files must open without export, import, or schema rebuilding.
- Production and test builds pass with `CGO_ENABLED=0`.
- Do not modify the dirty main worktree at `/Users/twn/Workspaces/vibe-proxy`.

---

## File Structure

- `internal/store/sqlite.go`: pure-Go driver import, URI construction, connection lifecycle.
- `internal/store/sqlite_test.go`: PRAGMA, special-path, legacy database, and existing observability tests.
- `internal/store/testdata/legacy-mattn.db`: pre-migration database compatibility fixture.
- `go.mod`, `go.sum`: Go floor and driver dependency lock.
- `Dockerfile`: current supported Go builder and CGO-free link.
- `.github/workflows/test.yml`: supported Go matrix, vet, and no-CGO build checks.
- `scripts/dev-docker.sh`: current development container toolchain.
- `README.md`, `docs/development.md`: supported local and Docker development versions.

### Task 1: Add migration regression tests and legacy fixture

**Files:**
- Create: `internal/store/testdata/legacy-mattn.db`
- Modify: `internal/store/sqlite_test.go`

**Interfaces:**
- Consumes: existing `Open(path string) (*SQLite, error)` and `SQLite.RequestFinished`.
- Produces: regression coverage for platform-safe paths, PRAGMAs, and old database files.

- [ ] **Step 1: Generate the legacy fixture with the current driver**

Create a temporary Go program outside the repository that imports the current
store package indirectly through the existing schema or uses
`github.com/mattn/go-sqlite3` directly. It must create
`internal/store/testdata/legacy-mattn.db` with one `request_logs` row:

```text
request_id: legacy-request
virtual_model: legacy-model
upstream_model: legacy-upstream
status_code: 200
transformation_json: {"multimodal_route":"ocr_fallback","ocr_processed":1}
```

Close the database cleanly so no `-wal` or `-shm` sidecar is committed.

- [ ] **Step 2: Write failing behavior tests**

Add these tests to `internal/store/sqlite_test.go`:

```go
func TestSQLiteSupportsSpecialCharactersInPath(t *testing.T)
func TestSQLiteConfiguresWALAndBusyTimeout(t *testing.T)
func TestSQLiteOpensAndExtendsLegacyMattnDatabase(t *testing.T)
```

`TestSQLiteSupportsSpecialCharactersInPath` uses a database filename containing
spaces, `?`, and `#`, writes one event, closes it, reopens the same path, and
asserts the event remains.

`TestSQLiteConfiguresWALAndBusyTimeout` queries:

```sql
PRAGMA journal_mode
PRAGMA busy_timeout
```

and asserts `wal` and `5000`.

`TestSQLiteOpensAndExtendsLegacyMattnDatabase` copies the fixture to `t.TempDir`,
opens the copy, checks the legacy transformation, writes `modernc-request`,
reopens it, and checks both rows.

- [ ] **Step 3: Run the special-path test and verify RED**

Run:

```bash
docker run --rm -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  golang:1.26.5 \
  sh -lc 'go test ./internal/store -run TestSQLiteSupportsSpecialCharactersInPath -count=1'
```

Expected: FAIL because the existing driver appends query parameters to the raw
path and interprets `?`/`#` as URI syntax instead of filename characters.

- [ ] **Step 4: Run Store tests with CGO disabled and verify RED**

Run:

```bash
docker run --rm -v "$PWD":/src -w /src \
  -v /tmp/vibe-proxy-gomodcache:/go/pkg/mod \
  -v /tmp/vibe-proxy-gocache:/root/.cache/go-build \
  -e CGO_ENABLED=0 golang:1.26.5 \
  sh -lc 'go test ./internal/store -count=1'
```

Expected: FAIL with the current `go-sqlite3` CGO-disabled stub.

### Task 2: Migrate the Store to modernc SQLite

**Files:**
- Modify: `internal/store/sqlite.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: regression tests from Task 1.
- Produces: `sqliteDSN(path string) (string, error)` and a CGO-free implementation of the unchanged `Open` API.

- [ ] **Step 1: Upgrade the module and replace the dependency**

Run under Go 1.26.5:

```bash
go mod edit -go=1.25.0
go get modernc.org/sqlite@v1.55.0
go mod edit -droprequire=github.com/mattn/go-sqlite3
go mod tidy
```

Verify that `go.mod` contains `go 1.25.0`, directly requires
`modernc.org/sqlite v1.55.0`, and no longer references
`github.com/mattn/go-sqlite3`.

- [ ] **Step 2: Implement the minimal DSN and driver migration**

In `internal/store/sqlite.go`:

```go
import (
    "database/sql"
    "net/url"
    "path/filepath"

    _ "modernc.org/sqlite"
)

func sqliteDSN(path string) (string, error) {
    absolute, err := filepath.Abs(path)
    if err != nil {
        return "", err
    }
    databaseURL := url.URL{
        Scheme: "file",
        Path:   filepath.ToSlash(absolute),
    }
    query := databaseURL.Query()
    query.Set("_busy_timeout", "5000")
    query.Set("_journal_mode", "WAL")
    databaseURL.RawQuery = query.Encode()
    return databaseURL.String(), nil
}
```

Update `Open` to call `sqliteDSN`, use `sql.Open("sqlite", dsn)`, run
`db.Ping()`, then run `migrate()`. Close `db` before returning any ping or
migration error.

- [ ] **Step 3: Run focused Store tests and verify GREEN**

Run:

```bash
CGO_ENABLED=0 go test ./internal/store -count=1
```

Expected: PASS.

- [ ] **Step 4: Run the complete suite under the minimum Go version**

Run:

```bash
CGO_ENABLED=0 go test ./...
```

using Go 1.25.x. Expected: PASS.

### Task 3: Align Docker, CI, scripts, and documentation

**Files:**
- Modify: `Dockerfile`
- Modify: `.github/workflows/test.yml`
- Modify: `scripts/dev-docker.sh`
- Modify: `README.md`
- Modify: `docs/development.md`

**Interfaces:**
- Consumes: Go 1.25 module floor and CGO-free Store from Task 2.
- Produces: consistent supported toolchains for contributors and automation.

- [ ] **Step 1: Update the Docker build**

Use `golang:1.26.5` in `Dockerfile`, set `CGO_ENABLED=0`, and replace the
external static linker flags with:

```text
-ldflags='-s -w'
```

Keep `-tags="netgo osusergo"`, `-trimpath`, the scratch runtime image,
certificates, timezone data, writable `/tmp`, exposed port, and entrypoint.

- [ ] **Step 2: Update the CI matrix**

Change `.github/workflows/test.yml` to a matrix with:

```yaml
strategy:
  matrix:
    go-version: ['1.25.x', '1.26.x']
```

For each version run:

```bash
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o /tmp/vibe-proxy ./cmd/vibe-proxy
```

- [ ] **Step 3: Update local development references**

Change:

- `scripts/dev-docker.sh` builder image to `golang:1.26.5`.
- README local prerequisite from Go 1.22+ to Go 1.25+.
- `docs/development.md` Docker command to `golang:1.26.5`.

Document that release builds use the latest Go patch while the project minimum
tracks the older of the two officially supported major versions.

- [ ] **Step 4: Build the Docker image**

Run:

```bash
docker build -t vibe-proxy:sqlite-modernc .
```

Expected: PASS without installing a C compiler or enabling CGO.

### Task 4: Verify all platforms and record the size impact

**Files:**
- Modify only if verification exposes a defect in files already listed.

**Interfaces:**
- Consumes: completed migration and build configuration.
- Produces: evidence that the migration is releasable on the planned platform matrix.

- [ ] **Step 1: Run full tests on Go 1.25**

Run in `golang:1.25`:

```bash
CGO_ENABLED=0 go test ./...
go vet ./...
```

Expected: PASS.

- [ ] **Step 2: Run full tests on Go 1.26.5**

Run in `golang:1.26.5`:

```bash
CGO_ENABLED=0 go test ./...
go vet ./...
```

Expected: PASS.

- [ ] **Step 3: Cross-build every target**

For each `GOOS/GOARCH` pair below, run:

```bash
CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> \
  go build -trimpath -ldflags='-s -w' \
  -o /tmp/vibe-proxy-<os>-<arch> ./cmd/vibe-proxy
```

Targets:

```text
windows/amd64
windows/arm64
darwin/amd64
darwin/arm64
linux/amd64
linux/arm64
```

Expected: all six builds exit 0.

- [ ] **Step 4: Compare binary sizes**

Record the previous CGO release-mode binary size and the new
`linux/amd64` release-mode binary size. Report both exact byte counts and the
delta; do not reject the migration based on size alone unless it is
operationally unreasonable.

- [ ] **Step 5: Scan and review the final diff**

Run:

```bash
git diff --check
git diff --stat
git diff -- go.mod go.sum internal/store Dockerfile .github scripts README.md docs
git status --short
```

Confirm that no API keys, database files other than the intended fixture,
frontend build artifacts, or unrelated main-worktree files are included.

- [ ] **Step 6: Commit the implementation**

```bash
git add go.mod go.sum internal/store Dockerfile .github/workflows/test.yml \
  scripts/dev-docker.sh README.md docs/development.md
git commit -m "refactor: migrate sqlite storage to pure go"
```

The design and plan commit remains separate from this implementation commit.
