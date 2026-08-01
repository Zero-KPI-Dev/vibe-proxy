package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateModelCatalogProxyPersistsAndClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := `version: vibeproxy.io/v1alpha1
server:
  listen: 127.0.0.1:8080
client_keys: []
providers: {}
models:
  allow_raw: true
  aliases: {}
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := UpdateModelCatalogProxy(path, "http://user:password@proxy.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ModelCatalog.ProxyURL; got != "http://user:password@proxy.example:8080" {
		t.Fatalf("proxy URL = %q", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "proxy_url: http://user:password@proxy.example:8080") {
		t.Fatalf("proxy not persisted:\n%s", raw)
	}

	cfg, err = UpdateModelCatalogProxy(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ModelCatalog.ProxyURL != "" {
		t.Fatalf("proxy was not cleared: %q", cfg.ModelCatalog.ProxyURL)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "proxy_url:") {
		t.Fatalf("cleared proxy remained in YAML:\n%s", raw)
	}
}

func TestUpdateModelCatalogProxyRejectsUnsupportedScheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`version: vibeproxy.io/v1alpha1
server:
  listen: 127.0.0.1:8080
providers: {}
models:
  allow_raw: true
  aliases: {}
`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateModelCatalogProxy(path, "socks5://proxy.example:1080"); err == nil {
		t.Fatal("expected unsupported proxy scheme to fail validation")
	}
}

func TestUpdateModelCatalogProxyRejectsLegacyConfigWithoutOverwriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := `server:
  listen: 127.0.0.1:8080
channels:
  - id: legacy
    protocol: openai_chat
    base_url: https://legacy.example/v1
    models:
      vibe-chat: legacy-chat
`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateModelCatalogProxy(path, "http://proxy.example:8080"); err == nil ||
		!strings.Contains(err.Error(), "legacy configuration is read-only") {
		t.Fatalf("expected legacy mutation rejection, got %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != original {
		t.Fatalf("legacy config was overwritten:\n%s", raw)
	}
}
