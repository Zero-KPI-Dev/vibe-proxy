package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
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
