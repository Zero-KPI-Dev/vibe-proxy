package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
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

func TestBuildLocalProviderPreservesCapabilityOverrides(t *testing.T) {
	existing := ProviderConfig{
		Auth:                upstreamauth.Profile{Type: "custom_headers", Headers: map[string]upstreamauth.SecretRef{"X-Tenant": "literal:team-a"}},
		Priority:            7,
		Timeout:             Duration{Duration: 45 * time.Second},
		MaxConcurrency:      11,
		DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: modelcapability.SupportUnsupported},
		ModelCapabilities: map[string]modelcapability.ModelCapabilities{
			"vision": {ImageInput: modelcapability.SupportSupported},
		},
	}
	provider, err := BuildLocalProvider(LocalProviderInput{ID: "local", Type: "openai-compatible", BaseURL: "http://127.0.0.1:3000/v1", AuthType: "custom_headers"}, &existing)
	if err != nil {
		t.Fatal(err)
	}
	if provider.DefaultCapabilities.ImageInput != modelcapability.SupportUnsupported || provider.ModelCapabilities["vision"].ImageInput != modelcapability.SupportSupported {
		t.Fatalf("capability overrides were lost: %+v", provider)
	}
	if provider.Priority != 7 || provider.Timeout.Duration != 45*time.Second || provider.MaxConcurrency != 11 || provider.Auth.Headers["X-Tenant"] != "literal:team-a" {
		t.Fatalf("advanced provider settings were lost: %+v", provider)
	}
}

func TestBuildLocalProviderUpdatesCapabilityOverrides(t *testing.T) {
	defaultState := "unsupported"
	provider, err := BuildLocalProvider(LocalProviderInput{
		ID:                     "local",
		Type:                   "openai-compatible",
		BaseURL:                "http://127.0.0.1:3000/v1",
		AuthType:               "none",
		DefaultImageInput:      &defaultState,
		ModelImageCapabilities: map[string]string{"vision": "supported", "private": "unknown"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if provider.DefaultCapabilities.ImageInput != modelcapability.SupportUnsupported || provider.ModelCapabilities["vision"].ImageInput != modelcapability.SupportSupported || provider.ModelCapabilities["private"].ImageInput != modelcapability.SupportUnknown {
		t.Fatalf("capability overrides not updated: %+v", provider)
	}
}

func TestBuildLocalProviderRejectsInvalidCapabilityOverride(t *testing.T) {
	defaultState := "probably"
	if _, err := BuildLocalProvider(LocalProviderInput{Type: "openai-compatible", BaseURL: "http://127.0.0.1:3000/v1", AuthType: "none", DefaultImageInput: &defaultState}, nil); err == nil {
		t.Fatal("expected invalid default capability to be rejected")
	}
	if _, err := BuildLocalProvider(LocalProviderInput{Type: "openai-compatible", BaseURL: "http://127.0.0.1:3000/v1", AuthType: "none", ModelImageCapabilities: map[string]string{"model": "maybe"}}, nil); err == nil {
		t.Fatal("expected invalid model capability to be rejected")
	}
}

func TestUpdateProviderDoesNotBreakConfiguredVisionFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `version: vibeproxy.io/v1alpha1
providers:
  vision:
    type: openai-compatible
    base_url: http://127.0.0.1:3000/v1
    auth: {type: none}
    models: [vision-model]
    model_capabilities:
      vision-model: {image_input: supported}
models:
  allow_raw: true
  aliases: {}
multimodal:
  enabled: true
  vision_fallback_model: vision/vision-model
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	unsupported := "unsupported"
	_, err := UpdateProvider(path, LocalProviderInput{ID: "vision", Type: "openai-compatible", BaseURL: "http://127.0.0.1:3000/v1", AuthType: "none", Models: []string{"vision-model"}, DefaultImageInput: &unsupported, ModelImageCapabilities: map[string]string{}})
	if err == nil {
		t.Fatal("expected update that invalidates Vision fallback to be rejected")
	}
	written, readErr := os.ReadFile(path)
	if readErr != nil || !strings.Contains(string(written), "image_input: supported") {
		t.Fatalf("invalid update was written: err=%v config=%s", readErr, written)
	}
}
