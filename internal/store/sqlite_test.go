package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
	"github.com/a448582655/vibe-proxy/internal/types"
)

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
