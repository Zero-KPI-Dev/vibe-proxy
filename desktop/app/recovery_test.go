package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/a448582655/vibe-proxy/internal/gatewayapp"
)

func TestRecoveryClassifiesStartupStages(t *testing.T) {
	ctx := context.Background()
	neverExisting := func(context.Context, string) bool { return false }

	tests := []struct {
		stage string
		kind  RecoveryKind
	}{
		{stage: "config", kind: RecoveryInvalidConfig},
		{stage: "validate", kind: RecoveryInvalidConfig},
		{stage: "database", kind: RecoveryDatabase},
		{stage: "listen", kind: RecoveryPortConflict},
	}
	for _, test := range tests {
		t.Run(test.stage, func(t *testing.T) {
			presentation := classifyStartupFailure(ctx, &gatewayapp.StartupError{
				Stage: test.stage, Address: "127.0.0.1:8080", Err: errors.New("boom"),
			}, neverExisting)
			if presentation.Kind != test.kind {
				t.Fatalf("kind = %q, want %q", presentation.Kind, test.kind)
			}
			if presentation.Address != "" {
				t.Fatalf("address = %q for unavailable service, want empty", presentation.Address)
			}
			if strings.Contains(presentation.Summary, "boom") {
				t.Fatal("native presentation leaked wrapped startup error")
			}
		})
	}
}

func TestRecoveryPortConflictRecognisesExistingVibeProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"started_at":"2026-07-30T00:00:00Z","loaded_at":"2026-07-30T00:00:00Z"}`))
	}))
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")

	presentation := classifyStartupFailure(context.Background(), &gatewayapp.StartupError{
		Stage: "listen", Address: address, Err: errors.New("address in use"),
	}, probeVibeProxy)
	if presentation.Kind != RecoveryPortConflict || presentation.Address != address {
		t.Fatalf("presentation = %+v, want existing port conflict at %q", presentation, address)
	}
}

func TestRecoveryProbeRejectsLookalikeHealthEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"loaded_at":"2026-07-30T00:00:00Z"}`))
	}))
	defer server.Close()
	if probeVibeProxy(context.Background(), strings.TrimPrefix(server.URL, "http://")) {
		t.Fatal("probe accepted health response without started_at")
	}
}

func TestHostRecoveryOpensExistingControlPlaneWithoutOwnership(t *testing.T) {
	fixture := newHostStartFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"started_at":"2026-07-30T00:00:00Z","loaded_at":"2026-07-30T00:00:00Z"}`))
	}))
	defer server.Close()
	address := strings.TrimPrefix(server.URL, "http://")
	fixture.dialogs.recoveryChoice = RecoveryOpenExisting

	err := fixture.host.RecoverStartup(context.Background(), &gatewayapp.StartupError{
		Stage: "listen", Address: address, Err: errors.New("address in use"),
	})
	if err != nil {
		t.Fatalf("RecoverStartup() error = %v", err)
	}
	fixture.system.mu.Lock()
	browsed := append([]string(nil), fixture.system.browsed...)
	fixture.system.mu.Unlock()
	if len(browsed) != 1 || browsed[0] != server.URL {
		t.Fatalf("browser targets = %v, want %q", browsed, server.URL)
	}
	if snapshot := fixture.host.controller.Snapshot(); snapshot.OwnsGateway {
		t.Fatalf("snapshot = %+v, external control plane must not be owned", snapshot)
	}
}

func TestStartupErrorLogIsStructuredOnOneLine(t *testing.T) {
	fixture := newHostStartFixture(t)
	startupErr := &gatewayapp.StartupError{
		Stage: "config", Address: "127.0.0.1:8080", Err: errors.New("bad config\nforged=true"),
	}
	fixture.host.writeStartupErrorLog(startupErr)

	data, err := os.ReadFile(fixture.paths.LogPath)
	if err != nil {
		t.Fatalf("ReadFile(log) error = %v", err)
	}
	text := string(data)
	if strings.Count(strings.TrimSpace(text), "\n") != 0 {
		t.Fatalf("log contains forged line break: %q", text)
	}
	if !strings.Contains(text, `stage="config"`) || !strings.Contains(text, `address="127.0.0.1:8080"`) {
		t.Fatalf("log missing structured fields: %q", text)
	}
}
