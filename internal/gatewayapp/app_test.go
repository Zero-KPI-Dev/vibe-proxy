package gatewayapp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
