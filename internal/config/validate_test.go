package config

import (
	"testing"

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
