package gatewayapp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	runtimepkg "github.com/a448582655/vibe-proxy/internal/runtime"
	"github.com/a448582655/vibe-proxy/internal/store"
	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

func TestStartServesHealthAndShutdownIsIdempotent(t *testing.T) {
	app, err := Start(context.Background(), Options{ConfigPath: writeTestConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	<-app.Ready()

	resp, err := http.Get("http://" + app.Address() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	adminResp, err := http.Get("http://" + app.AdminAddress() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer adminResp.Body.Close()
	if adminResp.StatusCode != http.StatusOK || app.AdminAddress() == app.Address() {
		t.Fatalf("admin health = %d, data=%q admin=%q", adminResp.StatusCode, app.Address(), app.AdminAddress())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", app.Address())
	if err != nil {
		t.Fatalf("address was not released: %v", err)
	}
	listener.Close()
	adminListener, err := net.Listen("tcp", app.AdminAddress())
	if err != nil {
		t.Fatalf("admin address was not released: %v", err)
	}
	adminListener.Close()
}

func TestStartIsolatesBoundDataAndControlListeners(t *testing.T) {
	app, err := Start(context.Background(), Options{ConfigPath: writeTestConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	}()

	assertStatus := func(base, target string, want int) {
		t.Helper()
		response, requestErr := http.Get("http://" + base + target)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("GET %s%s = %d, want %d", base, target, response.StatusCode, want)
		}
	}

	assertStatus(app.Address(), "/", http.StatusNotFound)
	assertStatus(app.Address(), "/admin/config/snapshot", http.StatusNotFound)
	assertStatus(app.AdminAddress(), "/", http.StatusOK)
	assertStatus(app.AdminAddress(), "/v1/models", http.StatusNotFound)
}

func TestShutdownWaitsForInflightHandler(t *testing.T) {
	app, err := Start(context.Background(), Options{ConfigPath: writeTestConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	<-app.Ready()

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseHandler()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	}()

	app.server.Handler = app.trackHandlers(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inflight" {
			http.NotFound(w, r)
			return
		}
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))

	response := make(chan error, 1)
	go func() {
		resp, err := http.Get("http://" + app.Address() + "/inflight")
		if err == nil {
			resp.Body.Close()
		}
		response <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight handler did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdown := make(chan error, 1)
	go func() { shutdown <- app.Shutdown(ctx) }()

	select {
	case <-app.Done():
		t.Fatal("Done closed before the in-flight handler was released")
	case err := <-shutdown:
		t.Fatalf("Shutdown returned before the in-flight handler was released: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if status := app.Status(); status.State != StateStopping {
		t.Fatalf("state = %q, want %q while handler is in flight", status.State, StateStopping)
	}

	releaseHandler()
	select {
	case err := <-response:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not finish")
	}
	select {
	case err := <-shutdown:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not finish after the in-flight handler was released")
	}
	select {
	case <-app.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not close after shutdown drained")
	}
}

func TestStartReportsListenerFailure(t *testing.T) {
	app, err := Start(context.Background(), Options{
		ConfigPath: writeTestConfig(t),
		Listen: func(network, address string) (net.Listener, error) {
			return nil, syscall.EADDRINUSE
		},
	})
	if app != nil {
		t.Fatalf("app = %#v, want nil", app)
	}
	var startupErr *StartupError
	if !errors.As(err, &startupErr) {
		t.Fatalf("error = %v, want StartupError", err)
	}
	if startupErr.Stage != "data_listen" {
		t.Fatalf("stage = %q, want data_listen", startupErr.Stage)
	}
}

func TestStartClosesDataListenerWhenControlBindFails(t *testing.T) {
	var dataAddress string
	calls := 0
	app, err := Start(context.Background(), Options{
		ConfigPath: writeTestConfig(t),
		Listen: func(network, address string) (net.Listener, error) {
			calls++
			if calls == 2 {
				return nil, syscall.EADDRINUSE
			}
			listener, listenErr := net.Listen(network, address)
			if listenErr == nil {
				dataAddress = listener.Addr().String()
			}
			return listener, listenErr
		},
	})
	if app != nil {
		t.Fatalf("app = %#v, want nil", app)
	}
	var startupErr *StartupError
	if !errors.As(err, &startupErr) || startupErr.Stage != "admin_listen" {
		t.Fatalf("error = %v, want admin_listen StartupError", err)
	}
	listener, listenErr := net.Listen("tcp", dataAddress)
	if listenErr != nil {
		t.Fatalf("data listener was not released: %v", listenErr)
	}
	listener.Close()
}

func TestUnexpectedControlServerStopTerminatesDataPlane(t *testing.T) {
	app, err := Start(context.Background(), Options{ConfigPath: writeTestConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	dataAddress := app.Address()
	if err := app.adminServer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-app.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("gateway remained half-running after the control server stopped")
	}
	if err := app.Wait(); err != nil {
		t.Fatalf("unexpected terminal error: %v", err)
	}
	listener, listenErr := net.Listen("tcp", dataAddress)
	if listenErr != nil {
		t.Fatalf("data listener was not released after control stop: %v", listenErr)
	}
	listener.Close()
}

func TestPayloadRecorderClosesGracefullyWithGateway(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "observability.db")
	configPath := filepath.Join(directory, "config.yaml")
	config := "version: vibeproxy.io/v1alpha1\n" +
		"server:\n  listen: 127.0.0.1:0\n  admin_listen: 127.0.0.1:0\n" +
		"storage:\n  sqlite_path: " + filepath.ToSlash(databasePath) + "\n" +
		"observability:\n  capture:\n    mode: structured\n  retention:\n    content_days: 1\n    max_content_storage_mb: 1\n" +
		"models:\n  allow_raw: true\nproviders: {}\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := Start(context.Background(), Options{ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if app.recorder == nil {
		t.Fatal("gateway did not attach the bounded observability recorder")
	}
	if err := app.recorder.RecordPayload(telemetry.PayloadSnapshot{
		RequestID: "request-1", Stage: telemetry.PayloadStageClientRequest, SchemaVersion: 1,
		CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusCaptured,
		Body: []byte(`{"prompt":"hello"}`), StoredBytes: 18, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	reopened, err := store.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	payloads, err := reopened.PayloadSnapshots("request-1")
	if err != nil || len(payloads) != 1 {
		t.Fatalf("payload was not flushed before close: %+v err=%v", payloads, err)
	}
}

func TestGatewayEnforcesConfiguredRetentionAtStartupAndPeriodically(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "observability.db")
	database, err := store.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.RecordRequest(telemetry.Event{
		RequestID: "expired-before-start", StartedAt: time.Now().UTC().Add(-48 * time.Hour), StatusCode: 200,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(directory, "config.yaml")
	config := "version: vibeproxy.io/v1alpha1\n" +
		"server:\n  listen: 127.0.0.1:0\n  admin_listen: 127.0.0.1:0\n" +
		"storage:\n  sqlite_path: " + filepath.ToSlash(databasePath) + "\n  retention_days: 14\n" +
		"observability:\n  retention:\n    summaries_days: 1\n    content_days: 1\n    max_content_storage_mb: 1\n" +
		"models:\n  allow_raw: true\nproviders: {}\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := Start(context.Background(), Options{
		ConfigPath: configPath, RetentionSweepInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	}()

	page, err := app.database.QueryRequests(telemetry.RequestQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("startup retained summaries older than observability policy: %+v", page.Items)
	}

	now := time.Now().UTC()
	if err := app.database.RecordRequest(telemetry.Event{
		RequestID: "expired-periodically", StartedAt: now.Add(-48 * time.Hour), StatusCode: 200,
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.database.RecordRequest(telemetry.Event{
		RequestID: "payload-periodically", StartedAt: now, StatusCode: 200,
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.database.RecordPayload(telemetry.PayloadSnapshot{
		RequestID: "payload-periodically", Stage: telemetry.PayloadStageClientRequest,
		CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusCaptured,
		Body: []byte(`{"prompt":"short lived"}`), CreatedAt: now, ExpiresAt: now.Add(30 * time.Millisecond),
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		page, queryErr := app.database.QueryRequests(telemetry.RequestQuery{Limit: 10, Query: "expired-periodically"})
		payloads, payloadErr := app.database.PayloadSnapshots("payload-periodically")
		if queryErr != nil || payloadErr != nil {
			t.Fatalf("query during retention sweep: requests=%v payloads=%v", queryErr, payloadErr)
		}
		if len(page.Items) == 0 && len(payloads) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("periodic retention did not remove summary/payload: summaries=%d payloads=%d", len(page.Items), len(payloads))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestGatewayReloadUpdatesRetentionAndPayloadStoragePolicy(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "reload-observability.db")
	database, err := store.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.RecordRequest(telemetry.Event{
		RequestID: "old-summary", StartedAt: time.Now().UTC().Add(-48 * time.Hour), StatusCode: 200,
	}); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(directory, "config.yaml")
	writeConfig := func(summariesDays, contentDays, quotaMB int) {
		t.Helper()
		content := "version: vibeproxy.io/v1alpha1\n" +
			"server:\n  listen: 127.0.0.1:0\n  admin_listen: 127.0.0.1:0\n" +
			"storage:\n  sqlite_path: " + filepath.ToSlash(databasePath) + "\n  retention_days: 14\n" +
			fmt.Sprintf("observability:\n  retention:\n    summaries_days: %d\n    content_days: %d\n    max_content_storage_mb: %d\n", summariesDays, contentDays, quotaMB) +
			"models:\n  allow_raw: true\nproviders: {}\n"
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig(14, 3, 2)
	app, err := Start(context.Background(), Options{
		ConfigPath:             configPath,
		Runtime:                runtimepkg.Options{AdminTokenOverride: "admin-token"},
		RetentionSweepInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	}()

	page, err := app.database.QueryRequests(telemetry.RequestQuery{Query: "old-summary", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("startup policy unexpectedly removed summary: items=%d err=%v", len(page.Items), err)
	}
	writeConfig(1, 1, 1)
	reloadRequest, err := http.NewRequest(http.MethodPost, "http://"+app.AdminAddress()+"/admin/config/reload", nil)
	if err != nil {
		t.Fatal(err)
	}
	reloadRequest.Header.Set("Authorization", "Bearer admin-token")
	reloadResponse, err := http.DefaultClient.Do(reloadRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer reloadResponse.Body.Close()
	if reloadResponse.StatusCode != http.StatusOK {
		t.Fatalf("reload status = %d", reloadResponse.StatusCode)
	}

	now := time.Now().UTC()
	if err := app.database.RecordPayload(telemetry.PayloadSnapshot{
		RequestID: "reload-payload", Stage: telemetry.PayloadStageClientRequest,
		CaptureMode: telemetry.CaptureModeStructured, CaptureStatus: telemetry.CaptureStatusCaptured,
		Body: []byte(`{"prompt":"reload"}`), CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	payloads, err := app.database.PayloadSnapshots("reload-payload")
	if err != nil || len(payloads) != 1 {
		t.Fatalf("reloaded payload policy was not applied: %+v err=%v", payloads, err)
	}
	if lifetime := payloads[0].ExpiresAt.Sub(payloads[0].CreatedAt); lifetime < 23*time.Hour || lifetime > 25*time.Hour {
		t.Fatalf("content retention remained stale after reload: %s", lifetime)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		page, queryErr := app.database.QueryRequests(telemetry.RequestQuery{Query: "old-summary", Limit: 10})
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		if len(page.Items) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("summary retention remained stale after reload")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	config := "version: vibeproxy.io/v1alpha1\n" +
		"server:\n" +
		"  listen: 127.0.0.1:0\n" +
		"  admin_listen: 127.0.0.1:0\n" +
		"storage:\n" +
		"  sqlite_path: " + filepath.ToSlash(filepath.Join(t.TempDir(), "vibe-proxy.db")) + "\n" +
		"models:\n" +
		"  allow_raw: true\n" +
		"providers: {}\n"
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
