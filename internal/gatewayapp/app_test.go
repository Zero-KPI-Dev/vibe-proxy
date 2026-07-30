package gatewayapp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
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
	if startupErr.Stage != "listen" {
		t.Fatalf("stage = %q, want listen", startupErr.Stage)
	}
}

func writeTestConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	config := "version: vibeproxy.io/v1alpha1\n" +
		"server:\n" +
		"  listen: 127.0.0.1:0\n" +
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
