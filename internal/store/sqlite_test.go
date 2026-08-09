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

	assertMigrationVersions(t, sqlite.db, []int{1, 2, 3, 4, 5})
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
		"principal_type",
		"client_key_prefix",
		"cache_metrics_reported",
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
	assertMigrationVersions(t, reopened.db, []int{1, 2, 3, 4, 5})
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
		PrincipalType:     "client_key",
		PrincipalName:     "local-user",
		ClientKeyPrefix:   "vibe_1234567",
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
	if got.TraceID == "" || got.SessionID != "session-1" || got.PrincipalType != "client_key" || got.PrincipalName != "local-user" || got.ClientKeyPrefix != "vibe_1234567" || got.AgentID != "codex" || got.ProjectID != "vibe-proxy" {
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
		Usage:         types.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, CacheReadTokens: 8, CacheWriteTokens: 1, CacheMetricsReported: true, CacheHitRatio: 0.8},
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
		summary.TodayRequests != 2 ||
		summary.TodayTokens.Prompt != 30 ||
		summary.TodayTokens.Completion != 13 ||
		summary.TodayTokens.Total != 43 ||
		summary.TodayTokens.CacheRead != 8 ||
		summary.TodayTokens.CacheWrite != 1 ||
		summary.PromptCache.ReportedRequests != 1 ||
		summary.PromptCache.EligiblePromptTokens != 10 ||
		summary.PromptCache.WeightedHitRatio != 0.8 ||
		summary.PromptCache.ReportingCoverage != 0.5 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	points, err := store.MetricsHistory(now.Add(-time.Hour), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("expected two metric buckets, got %+v", points)
	}
	if points[0].Requests != 1 || points[0].Errors != 0 || points[0].TTFTP50 != 120 || points[0].TokensCacheRead != 8 || points[0].TokensCacheWrite != 1 || points[0].CacheHitRatio != 0.8 || points[0].CacheCoverage != 1 {
		t.Fatalf("unexpected first point: %+v", points[0])
	}
	if points[1].Requests != 1 || points[1].Errors != 1 || points[1].TTFTP95 != 320 || points[1].CacheCoverage != 0 {
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
	if payloads, err := database.PayloadSnapshots("old"); err != nil || len(payloads) != 1 || payloads[0].CaptureStatus != telemetry.CaptureStatusDropped || len(payloads[0].Body) != 0 {
		t.Fatalf("oldest payload did not retain an explicit quota tombstone: %+v err=%v", payloads, err)
	}
	if payloads, err := database.PayloadSnapshots("new"); err != nil || len(payloads) != 1 {
		t.Fatalf("newest payload missing: %+v err=%v", payloads, err)
	}
	recent, err := database.RecentFinished(10)
	if err != nil || len(recent) != 3 {
		t.Fatalf("payload pruning removed summaries: count=%d err=%v", len(recent), err)
	}
}

func TestSQLitePayloadQuotaIncludesHeadersAndDiagnosticMetadata(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "header-quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	for index, requestID := range []string{"old", "new"} {
		created := now.Add(time.Duration(index) * time.Second)
		if err := database.RecordRequest(telemetry.Event{RequestID: requestID, StartedAt: created, StatusCode: 200}); err != nil {
			t.Fatal(err)
		}
		if err := database.RecordPayload(telemetry.PayloadSnapshot{
			RequestID: requestID, Stage: telemetry.PayloadStageClientRequest, SchemaVersion: 1,
			CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusCaptured,
			MediaType: "application/json", Body: []byte(`{}`), StoredBytes: 2,
			Headers: http.Header{"X-Debug": []string{strings.Repeat(requestID, 256)}},
			Error:   strings.Repeat("diagnostic-", 16), CreatedAt: created, ExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Migration 4 leaves capture_status NULL on legacy rows. They must still
	// participate in hard-quota eviction.
	if _, err := database.db.Exec(`UPDATE payload_snapshots SET capture_status = NULL WHERE request_id = 'old'`); err != nil {
		t.Fatal(err)
	}

	// Both bodies fit comfortably in this quota, but the captured headers and
	// diagnostic text do not. The oldest complete payload row must be evicted.
	if err := database.PrunePayloads(now, 1500); err != nil {
		t.Fatal(err)
	}
	if payloads, err := database.PayloadSnapshots("old"); err != nil || len(payloads) != 1 || payloads[0].CaptureStatus != telemetry.CaptureStatusDropped || len(payloads[0].Body) != 0 {
		t.Fatalf("oldest header-heavy payload did not retain a quota tombstone: %+v err=%v", payloads, err)
	}
	if payloads, err := database.PayloadSnapshots("new"); err != nil || len(payloads) != 1 {
		t.Fatalf("newest payload should remain after quota eviction: %+v err=%v", payloads, err)
	}
}

func TestSQLiteQuotaEvictionBeforeSummaryRemainsVisible(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "quota-ordering.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ConfigurePayloadStorage(24*time.Hour, 1); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := database.RecordPayload(telemetry.PayloadSnapshot{
		RequestID: "late-summary", Stage: telemetry.PayloadStageClientRequest,
		CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusCaptured,
		Body: []byte(`{"large":true}`), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordRequest(telemetry.Event{
		RequestID: "late-summary", StartedAt: now, StatusCode: 200,
		CaptureMode: string(telemetry.CaptureModeStructured), CaptureStatus: telemetry.CaptureStatusCaptured,
	}); err != nil {
		t.Fatal(err)
	}
	details, err := database.RequestDetails("late-summary")
	if err != nil {
		t.Fatal(err)
	}
	if details.Request.CaptureStatus != telemetry.CaptureStatusDropped || details.Payloads[0].State != telemetry.CaptureStatusDropped {
		t.Fatalf("quota eviction was hidden by a later summary: %+v", details)
	}
}

func TestSQLiteDroppedPayloadAfterSummaryUpdatesCaptureState(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "dropped-after-summary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UTC()
	if err := database.RecordRequest(telemetry.Event{
		RequestID: "early-summary", StartedAt: now, StatusCode: 200,
		CaptureMode: string(telemetry.CaptureModeStructured), CaptureStatus: telemetry.CaptureStatusCaptured,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.RecordPayload(telemetry.PayloadSnapshot{
		RequestID: "early-summary", Stage: telemetry.PayloadStageClientRequest,
		CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusDropped,
		Error: "recorder_queue_saturated", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	details, err := database.RequestDetails("early-summary")
	if err != nil {
		t.Fatal(err)
	}
	if details.Request.CaptureStatus != telemetry.CaptureStatusDropped || details.Payloads[0].State != telemetry.CaptureStatusDropped {
		t.Fatalf("late payload tombstone did not update summary: %+v", details)
	}
}

func TestSQLiteMetadataOnlyPayloadMarkerDoesNotConsumeContentQuota(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "metadata-marker-quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UTC()
	if err := database.RecordPayload(telemetry.PayloadSnapshot{
		RequestID: "marker", Stage: telemetry.PayloadStageCanonicalResponse,
		CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusNotCaptured,
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.PrunePayloads(now, 0); err != nil {
		t.Fatal(err)
	}
	payloads, err := database.PayloadSnapshots("marker")
	if err != nil || len(payloads) != 1 || payloads[0].CaptureStatus != telemetry.CaptureStatusNotCaptured {
		t.Fatalf("metadata-only marker was treated as quota content: %+v err=%v", payloads, err)
	}
}

func TestSQLiteRetentionRemovesOrphanedObservations(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "orphan-observations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	observation := telemetry.NewObservation("orphan", "missing-summary", "trace", "span", "upstream", "provider.http", time.Now().UTC().Add(-48*time.Hour))
	if err := database.RecordObservation(observation); err != nil {
		t.Fatal(err)
	}
	if err := database.Retain(1); err != nil {
		t.Fatal(err)
	}
	observations, err := database.TraceObservations("missing-summary")
	if err != nil || len(observations) != 0 {
		t.Fatalf("orphaned observations escaped retention: %+v err=%v", observations, err)
	}
}

func TestSQLiteQueryRequestsUsesStableCursorAndFilters(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "query.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	started := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	for _, event := range []telemetry.Event{
		{RequestID: "a", StartedAt: started, AgentID: "codex", PrincipalName: "alice", SessionID: "s1", ProjectID: "p1", VirtualModel: "vibe", ChannelID: "one", ProtocolIn: "openai_chat", StatusCode: 200, CaptureStatus: telemetry.CaptureStatusCaptured},
		{RequestID: "b", StartedAt: started, AgentID: "codex", PrincipalName: "alice", SessionID: "s1", ProjectID: "p1", VirtualModel: "vibe", ChannelID: "one", ProtocolIn: "openai_chat", StatusCode: 500, CaptureStatus: telemetry.CaptureStatusDropped},
		{RequestID: "c", StartedAt: started.Add(time.Second), AgentID: "other", PrincipalName: "bob", SessionID: "s2", ProjectID: "p2", VirtualModel: "other", ChannelID: "two", ProtocolIn: "anthropic_messages", StatusCode: 200, CaptureStatus: telemetry.CaptureStatusNotCaptured},
	} {
		database.RequestFinished(event)
	}

	first, err := database.QueryRequests(telemetry.RequestQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 2 || first.Items[0].RequestID != "c" || first.Items[1].RequestID != "b" || first.NextCursor == "" {
		t.Fatalf("first page = %+v", first)
	}
	second, err := database.QueryRequests(telemetry.RequestQuery{Limit: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].RequestID != "a" || second.NextCursor != "" {
		t.Fatalf("second page = %+v", second)
	}
	filtered, err := database.QueryRequests(telemetry.RequestQuery{
		AgentID: "codex", PrincipalName: "alice", SessionID: "s1", ProjectID: "p1",
		Model: "vibe", Provider: "one", Protocol: "openai_chat", StatusClass: 5,
		CaptureStatus: telemetry.CaptureStatusDropped, Query: "b", From: timePointer(started), To: timePointer(started),
	})
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].RequestID != "b" {
		t.Fatalf("filtered query = %+v err=%v", filtered, err)
	}
	details, err := database.RequestDetails("a")
	if err != nil || len(details.Payloads) != 9 {
		t.Fatalf("request details = %+v err=%v", details, err)
	}
	for _, payload := range details.Payloads {
		if payload.State != "missing" || len(payload.Body) != 0 {
			t.Fatalf("missing payload returned an ambiguous body: %+v", payload)
		}
	}
}

func TestSQLiteQuerySessionsAggregatesAndPages(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UTC().Truncate(time.Second)
	for _, event := range []telemetry.Event{
		{RequestID: "s1-a", SessionID: "s1", SessionName: "Fix bug", SessionKind: "coding", AgentID: "codex", PrincipalName: "alice", ProjectID: "p1", StartedAt: now.Add(-time.Minute), StatusCode: 200, Usage: types.Usage{TotalTokens: 10}},
		{RequestID: "s1-b", SessionID: "s1", SessionName: "Fix bug", SessionKind: "coding", AgentID: "codex", PrincipalName: "alice", ProjectID: "p1", StartedAt: now, StatusCode: 500, Usage: types.Usage{TotalTokens: 20}},
		{RequestID: "s2-a", SessionID: "s2", AgentID: "other", StartedAt: now.Add(time.Second), StatusCode: 200},
	} {
		database.RequestFinished(event)
	}
	page, err := database.QuerySessions(telemetry.SessionQuery{Limit: 1, AgentID: "codex", PrincipalName: "alice", ProjectID: "p1", Query: "Fix"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].SessionID != "s1" || page.Items[0].RequestCount != 2 || page.Items[0].ErrorCount != 1 || page.Items[0].TotalTokens != 30 {
		t.Fatalf("session aggregate = %+v", page)
	}
	timeline, err := database.SessionRequests("s1")
	if err != nil || len(timeline) != 2 || timeline[0].RequestID != "s1-a" || timeline[1].RequestID != "s1-b" {
		t.Fatalf("session timeline = %+v err=%v", timeline, err)
	}
	firstPage, err := database.QuerySessions(telemetry.SessionQuery{Limit: 1})
	if err != nil || len(firstPage.Items) != 1 || firstPage.Items[0].SessionID != "s2" || firstPage.NextCursor == "" {
		t.Fatalf("first session page = %+v err=%v", firstPage, err)
	}
	secondPage, err := database.QuerySessions(telemetry.SessionQuery{Limit: 1, Cursor: firstPage.NextCursor})
	if err != nil || len(secondPage.Items) != 1 || secondPage.Items[0].SessionID != "s1" || secondPage.NextCursor != "" {
		t.Fatalf("second session page = %+v err=%v", secondPage, err)
	}
}

func TestSQLiteQuerySessionsAcceptsRuntimeMonotonicTimestamps(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "sessions-monotonic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	// Runtime request envelopes originate from time.Now and carry a monotonic
	// clock reading. SQLite aggregate expressions return the stored value as a
	// string, so Session queries must remain readable for real traffic.
	started := time.Now()
	database.RequestFinished(telemetry.Event{
		RequestID: "runtime-request", SessionID: "runtime-session", StartedAt: started, StatusCode: 200,
	})

	page, err := database.QuerySessions(telemetry.SessionQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].SessionID != "runtime-session" {
		t.Fatalf("runtime session page = %+v", page)
	}
}

func TestSQLiteRequestDiffSelectsParentAndReportsExpiredContent(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "diff.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UTC().Truncate(time.Second)
	baseEvent := telemetry.Event{RequestID: "base", SessionID: "s1", StartedAt: now, ChannelID: "one", UpstreamModel: "m1", CaptureStatus: telemetry.CaptureStatusCaptured}
	currentEvent := telemetry.Event{RequestID: "current", SessionID: "s1", ParentRequestID: "base", StartedAt: now.Add(time.Second), ChannelID: "two", UpstreamModel: "m2", CaptureStatus: telemetry.CaptureStatusCaptured}
	database.RequestFinished(baseEvent)
	database.RequestFinished(currentEvent)
	for _, payload := range []telemetry.PayloadSnapshot{
		{RequestID: "base", Stage: telemetry.PayloadStageCanonicalRequest, CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusCaptured, Body: []byte(`{"requested_model":"vibe","messages":[]}`), CreatedAt: now},
		{RequestID: "current", Stage: telemetry.PayloadStageCanonicalRequest, CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusCaptured, Body: []byte(`{"requested_model":"vibe","messages":[{"role":"user","content":[]}]}`), CreatedAt: now},
	} {
		if err := database.RecordPayload(payload); err != nil {
			t.Fatal(err)
		}
	}
	diff, err := database.RequestDiff("current", "")
	if err != nil || diff.BaseRequestID != "base" || diff.BaseSelection != "parent" || !diff.AppendOnly {
		t.Fatalf("request diff = %+v err=%v", diff, err)
	}
	deleted, err := database.DeleteRequestContent("base")
	if err != nil || !deleted {
		t.Fatalf("delete content = %v err=%v", deleted, err)
	}
	diff, err = database.RequestDiff("current", "base")
	if err != nil || diff.State != telemetry.CaptureStatusExpired || diff.UnavailableSide != "base" {
		t.Fatalf("expired diff state = %+v err=%v", diff, err)
	}
}

func timePointer(value time.Time) *time.Time { return &value }
