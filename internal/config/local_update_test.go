package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertLocalProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: vibeproxy.io/v1alpha1\nproviders: {}\nmodels:\n  allow_raw: true\n  aliases: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := UpsertLocalProvider(path, LocalProviderInput{ID: "newapi", Type: "openai-compatible", BaseURL: "http://127.0.0.1:3000/v1", CatalogProvider: "deepseek", APIKeyEnv: "NEW_API_KEY", Models: []string{"deepseek-v4-flash"}, Alias: "vibe-chat", AliasModel: "deepseek-v4-flash", DefaultModel: "vibe-chat"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["newapi"].Auth.Type != "bearer" {
		t.Fatalf("unexpected auth: %+v", cfg.Providers["newapi"].Auth)
	}
	if cfg.Providers["newapi"].CatalogProvider != "deepseek" {
		t.Fatalf("catalog provider not written: %+v", cfg.Providers["newapi"])
	}
	if cfg.ModelResolver.Aliases["vibe-chat"].Provider != "newapi" {
		t.Fatalf("alias not written: %+v", cfg.ModelResolver.Aliases)
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), "NEW_API_KEY") || !strings.Contains(string(written), "vibe-chat") || !strings.Contains(string(written), "catalog_provider: deepseek") {
		t.Fatalf("config not written: %s", written)
	}
}
