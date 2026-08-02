package telemetry

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCaptureRequestModeCanOnlyReduceConfiguredMode(t *testing.T) {
	policy := CapturePolicy{Mode: CaptureModeRaw, MaxSnapshotBytes: 4096}

	metadata := CapturePayload([]byte(`{"prompt":"hello"}`), http.Header{}, policy, "metadata")
	if metadata.Mode != CaptureModeMetadata || metadata.Status != CaptureStatusNotCaptured || len(metadata.Body) != 0 {
		t.Fatalf("metadata reduction = %+v", metadata)
	}

	off := CapturePayload([]byte(`{"prompt":"hello"}`), http.Header{}, policy, "off")
	if off.Mode != CaptureModeOff || off.Status != CaptureStatusNotCaptured || len(off.Body) != 0 {
		t.Fatalf("off reduction = %+v", off)
	}

	noElevation := CapturePayload([]byte(`{"prompt":"hello"}`), http.Header{}, CapturePolicy{Mode: CaptureModeMetadata, MaxSnapshotBytes: 4096}, "raw")
	if noElevation.Mode != CaptureModeMetadata || noElevation.Status != CaptureStatusNotCaptured {
		t.Fatalf("request elevated configured capture: %+v", noElevation)
	}
}

func TestCaptureStructuredRedactsSecretsHeadersAndImageData(t *testing.T) {
	body := []byte(`{
		"prompt":"keep me",
		"api_key":"sk-secret",
		"nested":{"password":"hunter2"},
		"image_url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB"
	}`)
	headers := http.Header{
		"User-Agent":    []string{"codex/1.0"},
		"Traceparent":   []string{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		"Authorization": []string{"Bearer top-secret"},
		"Cookie":        []string{"session=secret"},
	}
	result := CapturePayload(body, headers, CapturePolicy{
		Mode:             CaptureModeStructured,
		MaxSnapshotBytes: 4096,
		HeaderAllowlist:  []string{"user-agent", "traceparent", "authorization", "cookie"},
	}, "")

	if result.Status != CaptureStatusRedacted || result.RedactionCount < 3 {
		t.Fatalf("capture status = %+v", result)
	}
	if !json.Valid(result.Body) {
		t.Fatalf("sanitized capture is invalid JSON: %q", result.Body)
	}
	got := string(result.Body)
	for _, secret := range []string{"sk-secret", "hunter2", "iVBORw0KGgo", "top-secret", "session=secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("capture leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "keep me") || !strings.Contains(got, "REDACTED") || !strings.Contains(got, "IMAGE_DATA_OMITTED") || !strings.Contains(got, "sha256") {
		t.Fatalf("capture lost safe content or descriptors: %s", got)
	}
	if result.Headers.Get("User-Agent") != "codex/1.0" || result.Headers.Get("Traceparent") == "" {
		t.Fatalf("allowlisted headers missing: %#v", result.Headers)
	}
	if result.Headers.Get("Authorization") != "" || result.Headers.Get("Cookie") != "" {
		t.Fatalf("sensitive headers captured: %#v", result.Headers)
	}
}

func TestCaptureRawStillAppliesMandatoryRedaction(t *testing.T) {
	result := CapturePayload([]byte(`{"token":"secret","value":"visible"}`), nil, CapturePolicy{
		Mode:             CaptureModeRaw,
		MaxSnapshotBytes: 4096,
	}, "")
	if result.Status != CaptureStatusRedacted || strings.Contains(string(result.Body), "secret") || !strings.Contains(string(result.Body), "visible") {
		t.Fatalf("raw capture bypassed redaction: %+v", result)
	}
}

func TestCaptureTruncatesToConfiguredLimitWithExplicitStatus(t *testing.T) {
	result := CapturePayload([]byte(`{"prompt":"`+strings.Repeat("x", 4096)+`"}`), nil, CapturePolicy{
		Mode:             CaptureModeStructured,
		MaxSnapshotBytes: 256,
	}, "")
	if result.Status != CaptureStatusTruncated || !result.Truncated || result.TruncationReason != "max_snapshot_bytes" {
		t.Fatalf("truncation status = %+v", result)
	}
	if len(result.Body) > 256 || !json.Valid(result.Body) {
		t.Fatalf("truncated body bytes=%d valid=%v: %q", len(result.Body), json.Valid(result.Body), result.Body)
	}
}

func TestCaptureDropsMalformedJSON(t *testing.T) {
	result := CapturePayload([]byte(`{"prompt":`), nil, CapturePolicy{Mode: CaptureModeRaw, MaxSnapshotBytes: 4096}, "")
	if result.Status != CaptureStatusDropped || len(result.Body) != 0 || result.Error == "" {
		t.Fatalf("malformed JSON capture = %+v", result)
	}
}
