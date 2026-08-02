package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/types"
)

func TestSQLiteDSNFormatsWindowsDrivePath(t *testing.T) {
	dsn := sqliteDSNFromAbsolutePath(
		`C:\Users\Alice\App Data\events ? #.db`,
		"windows",
	)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "file" || parsed.Host != "" {
		t.Fatalf("expected a local file URI, got %q", dsn)
	}
	if parsed.Path != "/C:/Users/Alice/App Data/events ? #.db" {
		t.Fatalf("unexpected Windows drive path in %q: %q", dsn, parsed.Path)
	}
	if parsed.Query().Get("_busy_timeout") != "5000" ||
		parsed.Query().Get("_journal_mode") != "WAL" {
		t.Fatalf("missing SQLite connection options in %q", dsn)
	}
}

func TestSQLiteDSNFormatsWindowsUNCPath(t *testing.T) {
	dsn := sqliteDSNFromAbsolutePath(
		`\\server\share\vibe proxy\events.db`,
		"windows",
	)
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "file" || parsed.Host != "" {
		t.Fatalf("expected an empty-authority file URI, got %q", dsn)
	}
	if parsed.Path != "//server/share/vibe proxy/events.db" {
		t.Fatalf("unexpected Windows UNC path in %q: %q", dsn, parsed.Path)
	}
}

func TestSQLiteSupportsSpecialCharactersInPath(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "events # &.db")
	event := telemetry.Event{
		RequestID:    "special-path",
		StartedAt:    time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC),
		StatusCode:   200,
		ProtocolIn:   "openai_chat",
		ProtocolOut:  "openai_chat",
		VirtualModel: "vibe-chat",
	}

	sqlite, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	sqlite.RequestFinished(event)
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("database was not created at the requested path %q: %v", databasePath, err)
	}

	reopened, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recent, err := reopened.RecentFinished(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].RequestID != event.RequestID {
		t.Fatalf("unexpected persisted events: %+v", recent)
	}
}

func TestSQLiteConfiguresWALAndBusyTimeout(t *testing.T) {
	sqlite, err := Open(filepath.Join(t.TempDir(), "pragmas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()

	var journalMode string
	if err := sqlite.db.QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		t.Fatalf("expected WAL journal mode, got %q", journalMode)
	}

	var busyTimeout int
	if err := sqlite.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("expected a 5000ms busy timeout, got %d", busyTimeout)
	}
}

func TestSQLiteMigrationsCreateTraceSchema(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "migrations.db")
	sqlite, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	assertMigrationVersions(t, sqlite.db, []int{1, 2, 3, 4})
	assertTableColumns(t, sqlite.db, "request_logs", []string{
		"transformation_json",
		"trace_id",
		"span_id",
		"parent_span_id",
		"session_id",
		"session_name",
		"session_kind",
		"session_path",
		"parent_request_id",
		"principal_name",
		"agent_id",
		"agent_name",
		"agent_version",
		"agent_source",
		"agent_confidence",
		"project_id",
		"duration_ms",
		"request_shape_json",
		"capture_mode",
		"capture_status",
		"http_method",
		"http_path",
	})
	for _, table := range []string{"trace_observations", "payload_snapshots", "session_annotations"} {
		var name string
		if err := sqlite.db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("expected table %s: %v", table, err)
		}
	}
	assertTableColumns(t, sqlite.db, "payload_snapshots", []string{"capture_status", "headers_json", "capture_error"})
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(databasePath)
	if err != nil {
		t.Fatalf("reopening a migrated database must be idempotent: %v", err)
	}
	defer reopened.Close()
	assertMigrationVersions(t, reopened.db, []int{1, 2, 3, 4})
}

func assertMigrationVersions(t *testing.T, db *sql.DB, want []int) {
	t.Helper()
	rows, err := db.Query(`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := []int{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		got = append(got, version)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("unexpected migration versions: got %v want %v", got, want)
	}
}

func assertTableColumns(t *testing.T, db *sql.DB, table string, want []string) {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	for _, column := range want {
		if !columns[column] {
			t.Errorf("table %s is missing column %s", table, column)
		}
	}
}

func TestSQLiteConfiguresBusyTimeoutOnPooledConnections(t *testing.T) {
	sqlite, err := Open(filepath.Join(t.TempDir(), "pooled-pragmas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := sqlite.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := sqlite.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	for index, connection := range []*sql.Conn{first, second} {
		var busyTimeout int
		if err := connection.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
			t.Fatal(err)
		}
		if busyTimeout != 5000 {
			t.Fatalf("expected pooled connection %d to use a 5000ms busy timeout, got %d", index+1, busyTimeout)
		}
	}
}

func TestSQLitePersistsConcurrentWrites(t *testing.T) {
	sqlite, err := Open(filepath.Join(t.TempDir(), "concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const writers = 16
	start := make(chan struct{})
	results := make(chan error, writers)
	var ready sync.WaitGroup
	ready.Add(writers)
	for writer := range writers {
		go func(writer int) {
			connection, err := sqlite.db.Conn(ctx)
			ready.Done()
			if err != nil {
				results <- fmt.Errorf("writer %d connection: %w", writer, err)
				return
			}
			defer connection.Close()
			<-start
			_, err = connection.ExecContext(ctx,
				`INSERT INTO request_logs (request_id, started_at, status_code) VALUES (?, ?, ?)`,
				fmt.Sprintf("writer-%d", writer), time.Now().UTC(), 200,
			)
			if err != nil {
				err = fmt.Errorf("writer %d insert: %w", writer, err)
			}
			results <- err
		}(writer)
	}
	ready.Wait()
	close(start)

	for range writers {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var persisted int
	if err := sqlite.db.QueryRow(`SELECT COUNT(*) FROM request_logs`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != writers {
		t.Fatalf("expected %d concurrent writes, got %d", writers, persisted)
	}
}

func TestSQLiteOpensAndExtendsLegacyMattnDatabase(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy-mattn.db"))
	if err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(t.TempDir(), "legacy.db")
	if err := os.WriteFile(databasePath, fixture, 0o600); err != nil {
		t.Fatal(err)
	}

	sqlite, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	recent, err := sqlite.RecentFinished(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].RequestID != "legacy-request" {
		t.Fatalf("unexpected legacy events: %+v", recent)
	}
	if recent[0].Transformation == nil ||
		recent[0].Transformation.MultimodalRoute != "ocr_fallback" ||
		recent[0].Transformation.OCRProcessed != 1 {
		t.Fatalf("unexpected legacy transformation: %+v", recent[0].Transformation)
	}

	sqlite.RequestFinished(telemetry.Event{
		RequestID:     "modernc-request",
		VirtualModel:  "modern-model",
		UpstreamModel: "modern-upstream",
		StartedAt:     time.Date(2026, time.July, 2, 10, 0, 0, 0, time.UTC),
		StatusCode:    201,
	})
	if err := sqlite.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recent, err = reopened.RecentFinished(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected legacy and new events, got %+v", recent)
	}
	eventsByID := make(map[string]telemetry.Event, len(recent))
	for _, event := range recent {
		eventsByID[event.RequestID] = event
	}
	if eventsByID["legacy-request"].Transformation == nil ||
		eventsByID["legacy-request"].Transformation.MultimodalRoute != "ocr_fallback" {
		t.Fatalf("legacy event changed after writing new data: %+v", eventsByID["legacy-request"])
	}
	if eventsByID["modernc-request"].StatusCode != 201 {
		t.Fatalf("new event was not persisted: %+v", eventsByID["modernc-request"])
	}
}

func TestSQLiteRecoversLegacyMattnWAL(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "legacy-wal.db")
	copySQLiteFixture(t, "legacy-mattn-wal.db", databasePath)
	copySQLiteFixture(t, "legacy-mattn-wal.db-wal", databasePath+"-wal")

	sqlite, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqlite.Close()
	recent, err := sqlite.RecentFinished(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].RequestID != "legacy-wal-request" {
		t.Fatalf("expected the committed legacy WAL row, got %+v", recent)
	}
	if recent[0].Transformation == nil ||
		recent[0].Transformation.MultimodalRoute != "vision_fallback" {
		t.Fatalf("unexpected WAL transformation: %+v", recent[0].Transformation)
	}
}

func copySQLiteFixture(t *testing.T, fixtureName, destination string) {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("testdata", fixtureName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSQLitePersistsTransformationSummary(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.RequestFinished(telemetry.Event{RequestID: "ocr-1", Transformation: &telemetry.TransformationSummary{MultimodalRoute: "ocr_fallback", OCRProcessed: 2}})
	var raw string
	if err := store.db.QueryRow(`SELECT transformation_json FROM request_logs WHERE request_id = ?`, "ocr-1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"multimodal_route":"ocr_fallback"`) || !strings.Contains(raw, `"ocr_processed":2`) {
		t.Fatalf("unexpected transformation JSON: %s", raw)
	}
}

func TestSQLitePersistsIdentityAndRequestShape(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	completed := time.Now().UTC()
	store.RequestFinished(telemetry.Event{
		RequestID:         "identity-1",
		TraceID:           "4bf92f3577b34da6a3ce929d0e0e4736",
		SpanID:            "00f067aa0ba902b7",
		ParentSpanID:      "aabbccddeeff0011",
		SessionID:         "session-1",
		SessionName:       "Fix observability",
		SessionKind:       "coding",
		SessionPath:       "/implementation/store",
		ParentRequestID:   "identity-0",
		PrincipalName:     "local-user",
		ClientName:        "local-user",
		AgentID:           "codex",
		AgentName:         "Codex CLI",
		AgentVersion:      "1.2.3",
		AgentSource:       "header",
		AgentConfidence:   "explicit",
		ProjectID:         "vibe-proxy",
		HTTPMethod:        http.MethodPost,
		HTTPPath:          "/v1/chat/completions",
		StartedAt:         completed.Add(-50 * time.Millisecond),
		CompletedAt:       &completed,
		DurationMillis:    50,
		VirtualModel:      "vibe-fast",
		InitialProvider:   "mockai",
		InitialModel:      "raw-chat",
		ChannelID:         "mockai",
		UpstreamModel:     "raw-chat",
		FinishReason:      "stop",
		UpstreamRequestID: "chatcmpl-1",
		CaptureMode:       "structured",
		CaptureStatus:     telemetry.CaptureStatusTruncated,
		CaptureTruncated:  true,
		RedactionCount:    2,
		RequestShape: telemetry.RequestShapeSummary{
			InputMessageCount: 1, InputBlockCount: 2, InputToolCount: 1,
			InputImageCount: 1, InputTextChars: 12, OutputMessageCount: 1,
			OutputBlockCount: 2, OutputToolCallCount: 1, OutputReasoningChars: 7, OutputTextChars: 2,
		},
	})

	recent, err := store.RecentFinished(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 {
		t.Fatalf("unexpected events: %+v", recent)
	}
	got := recent[0]
	if got.TraceID == "" || got.SessionID != "session-1" || got.PrincipalName != "local-user" || got.AgentID != "codex" || got.ProjectID != "vibe-proxy" {
		t.Fatalf("identity was not persisted: %+v", got)
	}
	if got.HTTPMethod != http.MethodPost || got.HTTPPath != "/v1/chat/completions" || got.DurationMillis != 50 || got.InitialProvider != "mockai" || got.FinishReason != "stop" {
		t.Fatalf("request summary was not persisted: %+v", got)
	}
	if got.RequestShape.InputImageCount != 1 || got.RequestShape.OutputToolCallCount != 1 || got.RequestShape.OutputReasoningChars != 7 {
		t.Fatalf("request shape was not persisted: %+v", got.RequestShape)
	}
	if got.CaptureMode != "structured" || got.CaptureStatus != telemetry.CaptureStatusTruncated || !got.CaptureTruncated || got.RedactionCount != 2 {
		t.Fatalf("capture summary was not persisted: %+v", got)
	}
}

func TestSQLiteProvidesPersistedObservability(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	finishedOne := now.Add(-19 * time.Minute)
	finishedTwo := now.Add(-4 * time.Minute)
	store.RequestFinished(telemetry.Event{
		RequestID:     "request-1",
		VirtualModel:  "vibe-chat",
		UpstreamModel: "model-a",
		StartedAt:     now.Add(-20 * time.Minute),
		CompletedAt:   &finishedOne,
		TTFTMillis:    120,
		TPOTMillis:    10,
		StatusCode:    200,
		Usage:         types.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	})
	store.RequestFinished(telemetry.Event{
		RequestID:     "request-2",
		VirtualModel:  "vibe-chat",
		UpstreamModel: "model-b",
		StartedAt:     now.Add(-5 * time.Minute),
		CompletedAt:   &finishedTwo,
		TTFTMillis:    320,
		TPOTMillis:    30,
		StatusCode:    502,
		Usage:         types.Usage{PromptTokens: 20, CompletionTokens: 8, TotalTokens: 28},
		Transformation: &telemetry.TransformationSummary{
			MultimodalRoute: "vision_fallback",
		},
	})

	summary, err := store.MetricsSummary(now.Add(-24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if summary.TotalRequests != 2 ||
		summary.TodayTokens.Prompt != 30 ||
		summary.TodayTokens.Completion != 13 ||
		summary.TodayTokens.Total != 43 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	points, err := store.MetricsHistory(now.Add(-time.Hour), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("expected two metric buckets, got %+v", points)
	}
	if points[0].Requests != 1 || points[0].Errors != 0 || points[0].TTFTP50 != 120 {
		t.Fatalf("unexpected first point: %+v", points[0])
	}
	if points[1].Requests != 1 || points[1].Errors != 1 || points[1].TTFTP95 != 320 {
		t.Fatalf("unexpected second point: %+v", points[1])
	}

	recent, err := store.RecentFinished(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].RequestID != "request-2" || recent[1].RequestID != "request-1" {
		t.Fatalf("unexpected recent requests: %+v", recent)
	}
	if recent[0].Transformation == nil || recent[0].Transformation.MultimodalRoute != "vision_fallback" {
		t.Fatalf("missing persisted transformation: %+v", recent[0])
	}
}

func TestSQLiteRecordsPayloadSnapshotsAndObservations(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "payloads.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	snapshot := telemetry.PayloadSnapshot{
		RequestID:      "request-1",
		Stage:          telemetry.PayloadStageCanonicalRequest,
		SchemaVersion:  1,
		CaptureMode:    telemetry.CaptureModeStructured,
		CaptureStatus:  telemetry.CaptureStatusRedacted,
		MediaType:      "application/json",
		Body:           []byte(`{"api_key":"[REDACTED]"}`),
		OriginalBytes:  42,
		StoredBytes:    28,
		RedactionCount: 1,
		SHA256:         "digest",
		Headers:        http.Header{"User-Agent": []string{"codex/1"}},
		CreatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	}
	if err := database.RecordPayload(snapshot); err != nil {
		t.Fatal(err)
	}
	observation := telemetry.NewObservation("observation-1", "request-1", "trace-1", "span-1", "transform", "canonicalize", now)
	observation.Attributes = map[string]any{"provider": "mockai"}
	observation.Finish(now.Add(10*time.Millisecond), "ok", "")
	if err := database.RecordObservation(observation); err != nil {
		t.Fatal(err)
	}

	payloads, err := database.PayloadSnapshots("request-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || payloads[0].CaptureStatus != telemetry.CaptureStatusRedacted || payloads[0].Headers.Get("User-Agent") != "codex/1" || string(payloads[0].Body) != string(snapshot.Body) {
		t.Fatalf("payload snapshots = %+v", payloads)
	}
	observations, err := database.TraceObservations("request-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].DurationMillis != 10 || observations[0].Attributes["provider"] != "mockai" {
		t.Fatalf("observations = %+v", observations)
	}
}

func TestSQLiteExpiresAndEvictsPayloadsBeforeSummaries(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	now := time.Now().UTC().Truncate(time.Second)
	for index, requestID := range []string{"expired", "old", "new"} {
		started := now.Add(time.Duration(index) * time.Second)
		database.RequestFinished(telemetry.Event{RequestID: requestID, StartedAt: started, StatusCode: 200})
		expires := now.Add(time.Hour)
		if requestID == "expired" {
			expires = now.Add(-time.Second)
		}
		if err := database.RecordPayload(telemetry.PayloadSnapshot{
			RequestID: requestID, Stage: telemetry.PayloadStageClientRequest, SchemaVersion: 1,
			CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusCaptured,
			Body: []byte(strings.Repeat(requestID, 10)), StoredBytes: len(requestID) * 10,
			CreatedAt: started, ExpiresAt: expires,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.PrunePayloads(now, 40); err != nil {
		t.Fatal(err)
	}
	if payloads, err := database.PayloadSnapshots("expired"); err != nil || len(payloads) != 0 {
		t.Fatalf("expired payload remains: %+v err=%v", payloads, err)
	}
	if payloads, err := database.PayloadSnapshots("old"); err != nil || len(payloads) != 0 {
		t.Fatalf("oldest payload was not quota-evicted: %+v err=%v", payloads, err)
	}
	if payloads, err := database.PayloadSnapshots("new"); err != nil || len(payloads) != 1 {
		t.Fatalf("newest payload missing: %+v err=%v", payloads, err)
	}
	recent, err := database.RecentFinished(10)
	if err != nil || len(recent) != 3 {
		t.Fatalf("payload pruning removed summaries: count=%d err=%v", len(recent), err)
	}
}
