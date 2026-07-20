package config

import (
	"testing"

	"github.com/a448582655/vibe-proxy/internal/modelcapability"
	"github.com/a448582655/vibe-proxy/internal/modelresolver"
	"github.com/a448582655/vibe-proxy/internal/upstreamauth"
)

func TestValidateRuntimeDetectsInvalidProviderAndAlias(t *testing.T) {
	cfg := &RuntimeConfig{Providers: map[string]ProviderConfig{"bad": {Type: "weird", BaseURL: "not url"}}, ModelResolver: modelresolver.Config{Aliases: map[string]modelresolver.Alias{"x": {Provider: "missing", Model: "model"}}}}
	issues := ValidateRuntime(cfg)
	if !HasErrors(issues) {
		t.Fatalf("expected errors: %+v", issues)
	}
	var sawType, sawAlias bool
	for _, i := range issues {
		if i.Code == "unsupported_provider_type" {
			sawType = true
		}
		if i.Code == "alias_provider_not_found" {
			sawAlias = true
		}
	}
	if !sawType || !sawAlias {
		t.Fatalf("missing expected issues: %+v", issues)
	}
}

func TestValidateRuntimeRejectsInvalidImageCapability(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Providers: map[string]ProviderConfig{
		"local": {
			Type:    "openai-compatible",
			BaseURL: "http://127.0.0.1:3000/v1",
			Auth:    upstreamauth.Profile{Type: "none"},
			DefaultCapabilities: modelcapability.ModelCapabilities{
				ImageInput: "maybe",
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if !HasErrors(issues) {
		t.Fatalf("expected invalid capability error: %+v", issues)
	}
	for _, issue := range issues {
		if issue.Code == "invalid_image_input_capability" {
			return
		}
	}
	t.Fatalf("missing capability validation issue: %+v", issues)
}

func TestValidateRuntimeAcceptsSimpleConfig(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Providers: map[string]ProviderConfig{"anthropic": {Type: "anthropic", APIKey: "env:ANTHROPIC_API_KEY", Auth: upstreamauth.Profile{Type: "api_key_header", Header: "x-api-key", Value: "env:ANTHROPIC_API_KEY"}, Models: []string{"claude"}}}, Models: ModelsConfig{AllowRaw: true, Aliases: map[string]string{"vibe": "anthropic/claude"}}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if HasErrors(issues) {
		t.Fatalf("unexpected validation errors: %+v", issues)
	}
}

func TestValidateRuntimeAllowsBootstrapWithoutProviders(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Providers: map[string]ProviderConfig{}, Models: ModelsConfig{AllowRaw: true}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if HasErrors(issues) {
		t.Fatalf("bootstrap should not have validation errors: %+v", issues)
	}
	if len(issues) == 0 || issues[0].Code != "missing_providers" {
		t.Fatalf("expected missing providers warning: %+v", issues)
	}
}

func TestValidateRuntimeAcceptsHTTPOCRFallback(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Multimodal: MultimodalConfig{
		Enabled: true,
		OCR: OCRConfig{
			Provider: "http",
			Endpoint: "http://127.0.0.1:32180/v1/ocr",
			Auth:     upstreamauth.Profile{Type: "none"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if issues := ValidateRuntime(cfg); HasErrors(issues) {
		t.Fatalf("unexpected OCR validation errors: %+v", issues)
	}
	if cfg.Multimodal.OCR.MaxImages != 4 || cfg.Multimodal.OCR.Cache.MaxEntries != 256 || !cfg.Multimodal.OCR.Cache.IsEnabled() {
		t.Fatalf("OCR defaults not applied: %+v", cfg.Multimodal.OCR)
	}
}

func TestValidateRuntimeRejectsUnsafeOCRConfig(t *testing.T) {
	cfg, err := CompileSimple(SimpleConfig{Multimodal: MultimodalConfig{
		Enabled: true,
		OCR:     OCRConfig{Provider: "http", Endpoint: "file:///tmp/ocr"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	issues := ValidateRuntime(cfg)
	if !HasErrors(issues) {
		t.Fatalf("expected invalid OCR endpoint: %+v", issues)
	}
	for _, issue := range issues {
		if issue.Code == "invalid_ocr_endpoint" {
			return
		}
	}
	t.Fatalf("missing endpoint issue: %+v", issues)
}
