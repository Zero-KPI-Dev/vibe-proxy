package store

import (
	"context"
	"database/sql"
	"fmt"
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
