package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertClientKeyPersistsRecoverablePrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: vibeproxy.io/v1alpha1\nclient_keys: []\nproviders: {}\nmodels:\n  allow_raw: true\n  aliases: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, rawKey, err := UpsertClientKey(path, ClientKeyInput{Name: "agent", RPM: 60})
	if err != nil {
		t.Fatal(err)
	}
	if len(rawKey) < 12 || len(cfg.ClientKeys) != 1 || cfg.ClientKeys[0].KeyPrefix != rawKey[:12] {
		t.Fatalf("client key prefix was not preserved: raw=%q config=%+v", rawKey, cfg.ClientKeys)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "key_prefix: "+rawKey[:12]) || strings.Contains(string(written), rawKey+"\n") {
		t.Fatalf("unexpected persisted client key: %s", written)
	}
}

func TestSaveRawConfigRejectsInvalidRuntimeWithoutReplacingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "version: vibeproxy.io/v1alpha1\nproviders: {}\nmodels:\n  allow_raw: true\n  aliases: {}\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	invalid := "version: vibeproxy.io/v1alpha1\nproviders:\n  bad:\n    type: openai-compatible\n    base_url: /relative\n    auth:\n      type: none\nmodels:\n  allow_raw: true\n  aliases: {}\n"
	if _, err := SaveRawConfig(path, invalid); err == nil {
		t.Fatal("expected invalid raw config to be rejected")
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != original {
		t.Fatalf("invalid config replaced the active file:\n%s", written)
	}
}

func TestUpsertAliasRejectsUnknownProviderWithoutReplacingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "version: vibeproxy.io/v1alpha1\nproviders: {}\nmodels:\n  allow_raw: true\n  aliases: {}\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := UpsertAlias(path, "broken", "missing/model"); err == nil {
		t.Fatal("expected alias with unknown provider to be rejected")
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != original {
		t.Fatalf("invalid alias replaced the active file:\n%s", written)
	}
}

func TestDeleteProviderCleansVisionFallbackAndAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	input := `version: vibeproxy.io/v1alpha1
multimodal:
  enabled: true
  vision_fallback_model: vibe-vision
providers:
  vision:
    type: openai-compatible
    base_url: https://vision.example/v1
    auth:
      type: none
    models: [vision-model]
    default_capabilities:
      image_input: supported
models:
  allow_raw: true
  aliases:
    vibe-vision: vision/vision-model
`
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := DeleteProvider(path, "vision")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Multimodal.VisionFallbackModel != "" {
		t.Fatalf("vision fallback was not cleared: %+v", cfg.Multimodal)
	}
	if _, exists := cfg.ModelResolver.Aliases["vibe-vision"]; exists {
		t.Fatalf("provider alias was not removed: %+v", cfg.ModelResolver.Aliases)
	}
}
